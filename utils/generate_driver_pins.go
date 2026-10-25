package utils

import (
	"Falcon/Models"
	"crypto/rand"
	"encoding/csv"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	MaxPINRetries = 10 // Maximum attempts to generate a unique PIN
	PINLength     = 4  // 4-digit PIN
)

// PINAssignment represents a driver's PIN assignment
type PINAssignment struct {
	DriverID   uint
	DriverName string
	PlainPIN   string
}

// GenerateUniquePIN generates a unique PIN that doesn't exist in the database
func GenerateUniquePIN(db *gorm.DB) (string, string, error) {
	for attempt := 0; attempt < MaxPINRetries; attempt++ {
		// Generate random 4-digit PIN
		plainPIN, err := generateRandomPIN(PINLength)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate random PIN: %v", err)
		}

		// Hash the PIN
		hashedPIN, err := bcrypt.GenerateFromPassword([]byte(plainPIN), bcrypt.DefaultCost)
		if err != nil {
			return "", "", fmt.Errorf("failed to hash PIN: %v", err)
		}

		// Check if this hashed PIN already exists
		var count int64
		err = db.Model(&Models.Driver{}).Where("pin = ?", string(hashedPIN)).Count(&count).Error
		if err != nil {
			return "", "", fmt.Errorf("failed to check PIN uniqueness: %v", err)
		}

		// If PIN doesn't exist, we found a unique one
		if count == 0 {
			return plainPIN, string(hashedPIN), nil
		}

		// Log collision (rare but possible)
		log.Printf("⚠️  PIN collision detected (attempt %d/%d), generating new PIN...", attempt+1, MaxPINRetries)
	}

	return "", "", fmt.Errorf("failed to generate unique PIN after %d attempts", MaxPINRetries)
}

// generateRandomPIN generates a random numeric PIN of specified length
func generateRandomPIN(length int) (string, error) {
	// Calculate the range: 10^(length-1) to 10^length - 1
	// For 6 digits: 100000 to 999999
	min := int64(1)
	for i := 1; i < length; i++ {
		min *= 10
	}
	max := min*10 - 1

	// Generate random number in range
	rangeSize := max - min + 1
	n, err := rand.Int(rand.Reader, big.NewInt(rangeSize))
	if err != nil {
		return "", err
	}

	pin := min + n.Int64()
	return fmt.Sprintf("%0*d", length, pin), nil
}

// RegeneratePIN is now an alias for GenerateUniquePIN for backward compatibility
func RegeneratePIN(db *gorm.DB) (string, string, error) {
	return GenerateUniquePIN(db)
}

// GeneratePINsForExistingDrivers generates PINs for all drivers that don't have one
func GeneratePINsForExistingDrivers(db *gorm.DB) error {
	log.Println("========================================")
	log.Println("Generating PINs for Existing Drivers")
	log.Println("========================================")

	// Find all drivers without a PIN
	var drivers []Models.Driver
	err := db.Where("pin IS NULL OR pin = ''").Find(&drivers).Error
	if err != nil {
		return fmt.Errorf("failed to fetch drivers: %v", err)
	}

	if len(drivers) == 0 {
		log.Println("✅ No drivers found without PINs")
		return nil
	}

	log.Printf("Found %d drivers without PINs\n", len(drivers))
	log.Println("Generating unique PINs with collision detection...")

	var assignments []PINAssignment
	successCount := 0
	errorCount := 0

	// Generate unique PIN for each driver
	for _, driver := range drivers {
		plainPIN, hashedPIN, err := GenerateUniquePIN(db)
		if err != nil {
			log.Printf("❌ Error generating unique PIN for driver %d (%s): %v\n",
				driver.ID, driver.Name, err)
			errorCount++
			continue
		}

		// Update driver with hashed PIN
		result := db.Model(&driver).Update("pin", hashedPIN)
		if result.Error != nil {
			log.Printf("❌ Error saving PIN for driver %d (%s): %v\n",
				driver.ID, driver.Name, result.Error)
			errorCount++
			continue
		}

		assignments = append(assignments, PINAssignment{
			DriverID:   driver.ID,
			DriverName: driver.Name,
			PlainPIN:   plainPIN,
		})

		log.Printf("✅ Generated unique PIN for driver %d (%s)", driver.ID, driver.Name)
		successCount++
	}

	// Print results
	log.Println("\n========================================")
	log.Println("PIN Generation Complete!")
	log.Println("========================================")
	log.Printf("✅ Success: %d drivers\n", successCount)
	log.Printf("❌ Errors: %d drivers\n", errorCount)
	log.Println("\n========================================")
	log.Println("⚠️  IMPORTANT: SAVE THESE PINS!")
	log.Println("========================================")
	log.Println("These PINs are shown ONLY ONCE and cannot be recovered.")
	log.Println("Share these PINs with the respective drivers securely.")

	// Print PIN assignments in a table format
	if len(assignments) > 0 {
		fmt.Println("┌──────────┬────────────────────────────────────────┬────────┐")
		fmt.Println("│ ID       │ Driver Name                            │ PIN    │")
		fmt.Println("├──────────┼────────────────────────────────────────┼────────┤")

		for _, assignment := range assignments {
			fmt.Printf("│ %-8d │ %-38s │ %s │\n",
				assignment.DriverID,
				truncateString(assignment.DriverName, 38),
				assignment.PlainPIN)
		}

		fmt.Println("└──────────┴────────────────────────────────────────┴────────┘")

		// Export to CSV
		csvFilename := generateCSVFilename()
		if err := ExportPINsToCSV(assignments, csvFilename); err != nil {
			log.Printf("⚠️  Warning: Failed to export PINs to CSV: %v\n", err)
			log.Println("⚠️  Please copy the PINs from console output above!")
		} else {
			log.Printf("\n✅ PINs exported to CSV file: %s\n", csvFilename)
			log.Println("📁 You can find this file in the 'pin_exports' directory")
		}
	}

	log.Println("\n✅ PINs have been saved to the database (hashed)")
	log.Println("⚠️  Copy this output now - PINs cannot be retrieved later!")

	return nil
}

// ExportPINsToCSV exports driver PINs to a CSV file
func ExportPINsToCSV(assignments []PINAssignment, filename string) error {
	// Create exports directory if it doesn't exist
	exportDir := "pin_exports"
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return fmt.Errorf("failed to create export directory: %v", err)
	}

	// Create full file path
	fullPath := filepath.Join(exportDir, filename)

	// Create CSV file
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %v", err)
	}
	defer file.Close()

	// Create CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"Driver ID", "Driver Name", "PIN", "Generated At"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %v", err)
	}

	// Write data rows
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	for _, assignment := range assignments {
		row := []string{
			fmt.Sprintf("%d", assignment.DriverID),
			assignment.DriverName,
			assignment.PlainPIN,
			timestamp,
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %v", err)
		}
	}

	log.Printf("✅ Successfully exported %d PINs to %s", len(assignments), fullPath)
	return nil
}

// RegeneratePINForDriver regenerates a PIN for a specific driver and exports to CSV
func RegeneratePINForDriver(db *gorm.DB, driverID uint) (string, error) {
	var driver Models.Driver
	if err := db.First(&driver, driverID).Error; err != nil {
		return "", fmt.Errorf("driver not found: %v", err)
	}

	plainPIN, hashedPIN, err := GenerateUniquePIN(db)
	if err != nil {
		return "", fmt.Errorf("failed to generate unique PIN: %v", err)
	}

	// Update driver with new PIN
	if err := db.Model(&driver).Update("pin", hashedPIN).Error; err != nil {
		return "", fmt.Errorf("failed to save PIN: %v", err)
	}

	log.Printf("✅ New unique PIN generated for driver %d (%s): %s",
		driver.ID, driver.Name, plainPIN)

	// Export single PIN to CSV
	assignments := []PINAssignment{
		{
			DriverID:   driver.ID,
			DriverName: driver.Name,
			PlainPIN:   plainPIN,
		},
	}

	csvFilename := fmt.Sprintf("driver_%d_pin_%s.csv",
		driverID,
		time.Now().Format("20060102_150405"))

	if err := ExportPINsToCSV(assignments, csvFilename); err != nil {
		log.Printf("⚠️  Warning: Failed to export PIN to CSV: %v", err)
	} else {
		log.Printf("✅ PIN exported to: pin_exports/%s", csvFilename)
	}

	return plainPIN, nil
}

// ValidateUniquePINs checks if all PINs in the database are unique
// This is a diagnostic function to ensure data integrity
func ValidateUniquePINs(db *gorm.DB) error {
	log.Println("Validating PIN uniqueness...")

	var drivers []Models.Driver
	if err := db.Where("pin IS NOT NULL AND pin != ''").Find(&drivers).Error; err != nil {
		return fmt.Errorf("failed to fetch drivers: %v", err)
	}

	pinMap := make(map[string][]uint)
	for _, driver := range drivers {
		// Extra safety check - skip if PIN is somehow nil despite the WHERE clause
		if driver.PIN == nil || *driver.PIN == "" {
			log.Printf("⚠️  Warning: Driver %d has null/empty PIN despite filter", driver.ID)
			continue
		}
		pinMap[*driver.PIN] = append(pinMap[*driver.PIN], driver.ID)
	}

	duplicates := false
	for pin, driverIDs := range pinMap {
		if len(driverIDs) > 1 {
			duplicates = true
			// Safe substring - check length first
			pinPreview := pin
			if len(pin) > 20 {
				pinPreview = pin[:20]
			}
			log.Printf("⚠️  DUPLICATE PIN found! Hash: %s... used by drivers: %v",
				pinPreview, driverIDs)
		}
	}

	if duplicates {
		return fmt.Errorf("duplicate PINs detected in database")
	}

	log.Printf("✅ All %d driver PINs are unique", len(drivers))
	return nil
}

// generateCSVFilename generates a timestamped filename for CSV exports
func generateCSVFilename() string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("driver_pins_%s.csv", timestamp)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ListPINExports lists all PIN export CSV files
func ListPINExports() ([]string, error) {
	exportDir := "pin_exports"

	// Check if directory exists
	if _, err := os.Stat(exportDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	// Read directory
	files, err := os.ReadDir(exportDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read export directory: %v", err)
	}

	var csvFiles []string
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".csv" {
			csvFiles = append(csvFiles, file.Name())
		}
	}

	return csvFiles, nil
}

// GetPINExportPath returns the full path to a PIN export file
func GetPINExportPath(filename string) string {
	return filepath.Join("pin_exports", filename)
}
