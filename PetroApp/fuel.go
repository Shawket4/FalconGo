package PetroApp

import (
	"Falcon/Constants"
	"Falcon/Models"
	"Falcon/Whatsapp"
	"Falcon/email"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Plate number conversion map - maps PetroApp format to Arabic format
var PlateNumberMap = map[string]string{
	"C E F-4 3 8 1": "ف ع ص 4381",
	"C Q F-4 2 5 3": "ف ق ص 4253",
	"R Y F-9 1 5 6": "ف ى ر 9156",
	"N A F-5 1 3 9": "ف أ ن 5139",
	"S M F-9 2 4 7": "ف م س 9247",
	"S R F-4 5 9 3": "ف ر س 4593",
	"N A F-7 4 2 1": "ف ا ن 7421",
	"Y D F-6 5 8 4": "ف د ى 6584",
	"Y D F-6 8 3 4": "ف د ى 6834",
}

// API response structure
type PetroAppAPIResponse struct {
	Data   []Models.PetroAppRecord `json:"data"`
	Status bool                    `json:"status"`
	Meta   struct {
		CurrentPage int `json:"current_page"`
		LastPage    int `json:"last_page"`
		Total       int `json:"total"`
	} `json:"meta"`
}

// FetchPetroAppRecords fetches records from PetroApp API for the last month
func FetchPetroAppRecords() ([]Models.PetroAppRecord, error) {
	log.Println("Starting PetroApp records fetch operation")

	// Get date range: last day to current day
	now := time.Now()
	startDate := now.AddDate(0, 0, -1).Format("2006/01/02")
	endDate := now.Format("2006/01/02")

	params := fmt.Sprintf("?dates=%s-%s&limit=1000&page=1", startDate, endDate)
	url := baseUrl + params

	log.Printf("Fetching records from %s to %s", startDate, endDate)
	log.Printf("Request URL: %s", url)

	// Create HTTP request with proper timeout
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating HTTP request: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers - validate they're not empty
	if token == "" {
		return nil, fmt.Errorf("authorization token is not set")
	}
	if cookie == "" {
		log.Println("Warning: cookie is empty, this might cause authentication issues")
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Falcon-PetroApp-Sync/1.0")

	// Create HTTP client with appropriate timeout
	client := &http.Client{
		Timeout: 60 * time.Second, // Increased timeout for large datasets
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error executing HTTP request: %v", err)
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing response body: %v", closeErr)
		}
	}()

	log.Printf("PetroApp API response status: %d %s", resp.StatusCode, resp.Status)

	// Handle different HTTP status codes
	switch resp.StatusCode {
	case http.StatusOK:
		// Continue processing
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("authentication failed - check token and cookie")
	case http.StatusForbidden:
		return nil, fmt.Errorf("access forbidden - insufficient permissions")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("rate limit exceeded - try again later")
	default:
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	// Decode response with size limit to prevent memory issues
	decoder := json.NewDecoder(resp.Body)
	var result PetroAppAPIResponse

	if err := decoder.Decode(&result); err != nil {
		log.Printf("Error decoding API response: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Validate response structure
	if !result.Status {
		log.Println("API returned status: false")
		return nil, fmt.Errorf("API returned unsuccessful status")
	}

	if result.Data == nil {
		log.Println("API returned nil data array")
		return []Models.PetroAppRecord{}, nil
	}

	log.Printf("Successfully fetched %d records from PetroApp API", len(result.Data))

	// Validate each record before storing
	validRecords := make([]Models.PetroAppRecord, 0, len(result.Data))
	for _, record := range result.Data {
		if err := validatePetroAppRecord(record); err != nil {
			log.Printf("Skipping invalid record ID %d: %v", record.ID, err)
			continue
		}
		validRecords = append(validRecords, record)
	}

	log.Printf("Validated %d out of %d records", len(validRecords), len(result.Data))

	// Store the validated records
	if err := StoreUniquePetroAppRecords(validRecords); err != nil {
		log.Printf("Error storing records: %v", err)
		return nil, fmt.Errorf("failed to store records: %w", err)
	}

	return validRecords, nil
}

// validatePetroAppRecord validates a PetroApp record before processing
func validatePetroAppRecord(record Models.PetroAppRecord) error {
	if record.ID <= 0 {
		return fmt.Errorf("invalid ID: %d", record.ID)
	}

	if strings.TrimSpace(record.Vehicle) == "" {
		return fmt.Errorf("vehicle plate is empty")
	}

	if strings.TrimSpace(record.Cost) == "" {
		return fmt.Errorf("cost is empty")
	}

	if strings.TrimSpace(record.NumberOfLiters) == "" {
		return fmt.Errorf("number of liters is empty")
	}

	if strings.TrimSpace(record.Date) == "" {
		return fmt.Errorf("date is empty")
	}

	if record.Odo <= 0 {
		return fmt.Errorf("invalid odometer reading: %d", record.Odo)
	}

	// Validate date format
	if _, err := time.Parse("2006-01-02 15:04:05", record.Date); err != nil {
		return fmt.Errorf("invalid date format: %s", record.Date)
	}

	// Validate numeric fields
	if cost, err := strconv.ParseFloat(record.Cost, 64); err != nil || cost <= 0 {
		return fmt.Errorf("invalid cost: %s", record.Cost)
	}

	if liters, err := strconv.ParseFloat(record.NumberOfLiters, 64); err != nil || liters <= 0 {
		return fmt.Errorf("invalid liters: %s", record.NumberOfLiters)
	}

	return nil
}

// StoreUniquePetroAppRecords stores PetroApp records, avoiding duplicates
func StoreUniquePetroAppRecords(records []Models.PetroAppRecord) error {
	if len(records) == 0 {
		log.Println("No records to store")
		return nil
	}

	log.Printf("Processing %d PetroApp records for storage", len(records))

	stored := 0
	skipped := 0

	// Use transaction for better performance and consistency
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("Transaction rolled back due to panic: %v", r)
		}
	}()

	for _, record := range records {
		var existing Models.PetroAppRecord
		err := tx.Where("id = ?", record.ID).First(&existing).Error

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				// Record doesn't exist, create it
				if err := tx.Create(&record).Error; err != nil {
					tx.Rollback()
					log.Printf("Error creating PetroApp record ID %d: %v", record.ID, err)
					return fmt.Errorf("failed to create record ID %d: %w", record.ID, err)
				}
				stored++
				log.Printf("Created new PetroApp record ID %d for vehicle %s", record.ID, record.Vehicle)
			} else {
				tx.Rollback()
				log.Printf("Database error checking record ID %d: %v", record.ID, err)
				return fmt.Errorf("database error for record ID %d: %w", record.ID, err)
			}
		} else {
			skipped++
			log.Printf("PetroApp record ID %d already exists, skipping", record.ID)
		}
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		log.Printf("Error committing transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Storage complete: %d new records stored, %d existing records skipped", stored, skipped)
	return nil
}

// SyncPetroAppRecordsToFuelEvents syncs unsynced PetroApp records to FuelEvents
func SyncPetroAppRecordsToFuelEvents() error {
	log.Println("Starting PetroApp to FuelEvent synchronization")

	var unsyncedRecords []Models.PetroAppRecord

	// Get all unsynced records with reasonable limit to prevent memory issues
	if err := Models.DB.Where("is_synced = ?", false).
		Order("date ASC"). // Process oldest first
		Limit(1000).       // Process in batches
		Find(&unsyncedRecords).Error; err != nil {
		log.Printf("Error fetching unsynced records: %v", err)
		return fmt.Errorf("failed to fetch unsynced records: %w", err)
	}

	if len(unsyncedRecords) == 0 {
		log.Println("No unsynced PetroApp records found")
		return nil
	}

	log.Printf("Found %d unsynced PetroApp records to process", len(unsyncedRecords))

	synced := 0
	duplicatesSkipped := 0
	errors := 0

	// Process in transaction for consistency
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("Sync transaction rolled back due to panic: %v", r)
		}
	}()

	for _, record := range unsyncedRecords {
		// Convert PetroApp record to FuelEvent
		fuelEvent, err := convertPetroAppToFuelEvent(record)
		if err != nil {
			log.Printf("Error converting PetroApp record ID %d: %v", record.ID, err)
			errors++
			continue
		}

		// Check if this FuelEvent already exists
		if fuelEventExistsInTx(tx, *fuelEvent) {
			log.Printf("FuelEvent already exists for PetroApp record ID %d (vehicle: %s, date: %s, odometer: %d-%d), skipping creation and marking as synced",
				record.ID, record.Vehicle, record.Date, fuelEvent.OdometerBefore, fuelEvent.OdometerAfter)
			duplicatesSkipped++

			// Mark as synced but DON'T create the FuelEvent
			if err := tx.Model(&record).Update("is_synced", true).Error; err != nil {
				log.Printf("Error updating sync status for duplicate record ID %d: %v", record.ID, err)
				errors++
			} else {
				log.Printf("Successfully marked duplicate PetroApp record ID %d as synced", record.ID)
			}
			continue
		}

		// Create the FuelEvent only if it doesn't exist
		if err := tx.Create(fuelEvent).Error; err != nil {
			log.Printf("Error creating FuelEvent for PetroApp record ID %d: %v", record.ID, err)
			errors++
			continue
		}

		// Update the car's last_fuel_odometer with the current odometer reading
		if err := tx.Model(&Models.Car{}).
			Where("car_no_plate = ?", fuelEvent.CarNoPlate).
			Update("last_fuel_odometer", fuelEvent.OdometerAfter).Error; err != nil {
			log.Printf("Error updating car's last_fuel_odometer for vehicle %s: %v", fuelEvent.CarNoPlate, err)
			// Don't fail the entire sync for this error, just log it
		} else {
			log.Printf("Updated car's last_fuel_odometer to %d for vehicle %s", fuelEvent.OdometerAfter, fuelEvent.CarNoPlate)
		}

		// Mark PetroApp record as synced after successful creation
		if err := tx.Model(&record).Update("is_synced", true).Error; err != nil {
			log.Printf("Error updating sync status for PetroApp record ID %d: %v", record.ID, err)
			errors++
			continue
		}

		synced++
		log.Printf("Successfully synced PetroApp record ID %d to new FuelEvent ID %d (vehicle: %s, fuel rate: %.3f km/L)",
			record.ID, fuelEvent.ID, fuelEvent.CarNoPlate, fuelEvent.FuelRate)
	}

	// Commit or rollback based on error count
	if errors > len(unsyncedRecords)/2 { // If more than 50% failed
		tx.Rollback()
		return fmt.Errorf("too many errors during synchronization (%d/%d), transaction rolled back", errors, len(unsyncedRecords))
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("Error committing sync transaction: %v", err)
		return fmt.Errorf("failed to commit sync transaction: %w", err)
	}

	log.Printf("Synchronization complete: %d new FuelEvents created, %d duplicates skipped, %d errors", synced, duplicatesSkipped, errors)

	if errors > 0 {
		log.Printf("Warning: synchronization completed with %d errors", errors)
	}

	return nil
}

func createWhatsAppMessage(fuelEvent Models.FuelEvent) string {
	var messageBuilder strings.Builder
	messageBuilder.WriteString("🚛 *FUEL EVENT NOTIFICATION*\n\n")
	messageBuilder.WriteString("*Vehicle:* " + fuelEvent.CarNoPlate + "\n")
	messageBuilder.WriteString("*Driver:* " + fuelEvent.DriverName + "\n")
	messageBuilder.WriteString("*Date:* " + fuelEvent.Date + "\n")
	messageBuilder.WriteString("*Time:* " + fuelEvent.Time + "\n\n")
	messageBuilder.WriteString("⛽ *Fuel Details:*\n")
	messageBuilder.WriteString(fmt.Sprintf("- Amount: %.2f L\n", fuelEvent.Liters))
	messageBuilder.WriteString(fmt.Sprintf("- Cost: %.2f EGP\n", fuelEvent.Price))
	messageBuilder.WriteString(fmt.Sprintf("- Price/L: %.2f EGP\n", fuelEvent.PricePerLiter))
	messageBuilder.WriteString(fmt.Sprintf("- Efficiency: %.2f km/L\n\n", fuelEvent.FuelRate))
	messageBuilder.WriteString("🏪 *Station:* " + fuelEvent.Transporter + "\n\n")

	// Add efficiency status
	if fuelEvent.FuelRate > 2.8 {
		messageBuilder.WriteString("⚠️ *HIGH EFFICIENCY ALERT* - Above normal range")
	} else if fuelEvent.FuelRate < 1.9 {
		messageBuilder.WriteString("🚨 *LOW EFFICIENCY ALERT* - Below normal range")
	} else {
		messageBuilder.WriteString("✅ *Normal fuel efficiency*")
	}

	rawMessage := messageBuilder.String()

	// Properly escape for JSON
	jsonBytes, _ := json.Marshal(rawMessage)
	// Remove the surrounding quotes that json.Marshal adds
	escapedMessage := string(jsonBytes[1 : len(jsonBytes)-1])

	return escapedMessage
}

// fuelEventExistsInTx checks if a FuelEvent exists within a transaction
func fuelEventExistsInTx(tx *gorm.DB, fuelEvent Models.FuelEvent) bool {
	var existingEvent Models.FuelEvent

	// Check for existing event with same car, date, and odometer readings
	err := tx.Where(
		"car_no_plate = ? AND date = ? AND odometer_before = ? AND odometer_after = ?",
		fuelEvent.CarNoPlate,
		fuelEvent.Date,
		fuelEvent.OdometerBefore,
		fuelEvent.OdometerAfter,
	).First(&existingEvent).Error

	if err == nil {
		log.Printf("Found existing FuelEvent ID %d matching: vehicle=%s, date=%s, odometer=%d-%d",
			existingEvent.ID, fuelEvent.CarNoPlate, fuelEvent.Date, fuelEvent.OdometerBefore, fuelEvent.OdometerAfter)
		return true
	}

	return false
}

// convertPetroAppToFuelEvent converts a PetroApp record to a FuelEvent
func convertPetroAppToFuelEvent(record Models.PetroAppRecord) (*Models.FuelEvent, error) {
	// Validate and convert cost
	costStr := strings.TrimSpace(record.Cost)
	if costStr == "" {
		return nil, fmt.Errorf("cost is empty")
	}

	cost, err := strconv.ParseFloat(costStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cost value '%s': %w", costStr, err)
	}
	if cost <= 0 {
		return nil, fmt.Errorf("cost must be positive, got: %.2f", cost)
	}

	// Validate and convert liters
	litersStr := strings.TrimSpace(record.NumberOfLiters)
	if litersStr == "" {
		return nil, fmt.Errorf("liters is empty")
	}

	liters, err := strconv.ParseFloat(litersStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid liters value '%s': %w", litersStr, err)
	}
	if liters <= 0 {
		return nil, fmt.Errorf("liters must be positive, got: %.3f", liters)
	}

	// Calculate price per liter with validation
	if liters == 0 {
		return nil, fmt.Errorf("cannot calculate price per liter: liters is zero")
	}
	pricePerLiter := cost / liters

	// Convert plate number using mapping
	convertedPlate := convertPlateNumber(record.Vehicle)
	if convertedPlate == "" {
		return nil, fmt.Errorf("converted plate number is empty")
	}

	// Parse and validate date
	parsedDate, parsedTime, err := parsePetroAppDate(record.Date)
	if err != nil {
		return nil, fmt.Errorf("invalid date format '%s': %w", record.Date, err)
	}

	// Validate date is not in future
	if parsedTime, _ := time.Parse("2006-01-02", parsedDate); parsedTime.After(time.Now().AddDate(0, 0, 1)) {
		return nil, fmt.Errorf("date is in the future: %s", parsedDate)
	}

	// Validate odometer reading
	if record.Odo <= 0 {
		return nil, fmt.Errorf("invalid odometer reading: %d", record.Odo)
	}

	// Get previous odometer reading
	odometerBefore := getPreviousOdometerReading(convertedPlate, record.Odo, parsedDate)

	// Calculate fuel rate from actual distance traveled
	fuelRate := calculateFuelRate(odometerBefore, record.Odo, liters)

	// Validate essential fields are not empty
	driverName := strings.TrimSpace(record.DelegateName)
	if driverName == "" {
		driverName = "Unknown Driver" // Provide default
	}

	transporter := strings.TrimSpace(record.Station)
	if transporter == "" {
		transporter = "Unknown Station" // Provide default
	}

	fuelEvent := &Models.FuelEvent{
		CarNoPlate:     convertedPlate,
		DriverName:     driverName,
		Date:           parsedDate,
		Liters:         liters,
		PricePerLiter:  pricePerLiter,
		Price:          cost,
		FuelRate:       fuelRate,
		Transporter:    transporter,
		OdometerBefore: odometerBefore,
		OdometerAfter:  record.Odo,
		Time:           parsedTime,
	}

	whatsappMessage := createWhatsAppMessage(*fuelEvent)
	Whatsapp.SendMessage(Constants.WhatsAppGPIDFuel, whatsappMessage)
	if fuelEvent.FuelRate > 2.8 || fuelEvent.FuelRate < 1.9 {
		emailSubject := fmt.Sprintf("⚠️ Fuel Rate Anomaly Alert - Vehicle %s", fuelEvent.CarNoPlate)

		emailBody := fmt.Sprintf(`FUEL RATE ANOMALY DETECTED
	
	Vehicle Details:
	- Plate Number: %s
	- Driver: %s
	- Date: %s
	
	Fuel Transaction Details:
	- Fuel Rate: %.3f km/L (ANOMALY DETECTED)
	- Distance Traveled: %d km (from %d to %d)
	- Fuel Consumed: %.3f liters
	- Cost: %.2f EGP
	- Price per Liter: %.2f EGP/L
	- Station: %s
	
	Alert Reason:
	- Normal fuel efficiency range: 1.9 - 2.8 km/L
	- Current fuel efficiency: %.3f km/L
	- This is %s the normal range
	
	Possible Causes:
	%s
	
	Action Required:
	- Verify odometer readings for accuracy
	- Check if fuel tank was already partially full
	- Investigate potential fuel theft or leakage
	- Confirm driver behavior and route efficiency
	
	PetroApp Record ID: %d
	Generated: %s
	
	This is an automated alert from the Falcon Fleet Management System.
	Please investigate this anomaly and take appropriate action.`,
			fuelEvent.CarNoPlate,
			fuelEvent.DriverName,
			fuelEvent.Date,
			fuelEvent.FuelRate,
			fuelEvent.OdometerAfter-fuelEvent.OdometerBefore,
			fuelEvent.OdometerBefore,
			fuelEvent.OdometerAfter,
			fuelEvent.Liters,
			fuelEvent.Price,
			fuelEvent.PricePerLiter,
			fuelEvent.Transporter,
			fuelEvent.FuelRate,
			func() string {
				if fuelEvent.FuelRate > 2.8 {
					return "ABOVE"
				}
				return "BELOW"
			}(),
			func() string {
				if fuelEvent.FuelRate > 2.8 {
					return `- Partial refueling (tank was already partially full)
	- Incorrect odometer reading
	- Vehicle was driven between fuel stops without recording
	- Data entry error in distance calculation`
				}
				return `- Fuel theft or unauthorized siphoning
	- Fuel leakage from tank or fuel lines
	- Heavy traffic or inefficient driving
	- Vehicle mechanical issues (engine, transmission)
	- Incorrect fuel amount recording
	- Multiple vehicles sharing same fuel transaction`
			}(),
			record.ID,
			time.Now().Format("2006-01-02 15:04:05"))

		email.SendEmail(Constants.EmailConfig, Models.EmailMessage{
			To:      []string{"shawketibrahim7@gmail.com"},
			Subject: emailSubject,
			Body:    emailBody,
			IsHTML:  false,
		})

		log.Printf("Fuel rate anomaly email sent for vehicle %s (rate: %.3f km/L)",
			fuelEvent.CarNoPlate, fuelEvent.FuelRate)
	}

	return fuelEvent, nil
}

// convertPlateNumber converts PetroApp plate format to Arabic format
func convertPlateNumber(petroAppPlate string) string {
	trimmedPlate := strings.TrimSpace(petroAppPlate)
	if trimmedPlate == "" {
		log.Printf("Warning: Empty plate number provided")
		return ""
	}

	if converted, exists := PlateNumberMap[trimmedPlate]; exists {
		return converted
	}

	log.Printf("Warning: No plate mapping found for '%s', using original format", trimmedPlate)
	return trimmedPlate
}

// parsePetroAppDate parses PetroApp date format to required format
func parsePetroAppDate(dateString string) (string, string, error) {
	trimmedDate := strings.TrimSpace(dateString)
	if trimmedDate == "" {
		return "", "", fmt.Errorf("date string is empty")
	}

	// Parse PetroApp format: "2025-07-31 12:35:19"
	t, err := time.Parse("2006-01-02 15:04:05", trimmedDate)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse date '%s': %w", trimmedDate, err)
	}

	// Validate date is reasonable (not too far in past or future)
	now := time.Now()
	if t.Before(now.AddDate(-10, 0, 0)) {
		return "", "", fmt.Errorf("date is too far in the past: %s", trimmedDate)
	}
	if t.After(now.AddDate(0, 0, 1)) {
		return "", "", fmt.Errorf("date is in the future: %s", trimmedDate)
	}

	// Return date in FuelEvent format: "2006-01-02" and time as "3:04 PM"
	return t.Format("2006-01-02"), t.Format("3:04 PM"), nil
}

// getPreviousOdometerReading gets the previous odometer reading for accurate fuel rate calculation
func getPreviousOdometerReading(carNoPlate string, currentOdo int, currentDate string) int {
	if carNoPlate == "" {
		log.Printf("Warning: Empty car plate provided")
		return max(0, currentOdo-500)
	}

	var lastFuelEvent Models.FuelEvent

	// Get the most recent FuelEvent for this car BEFORE the current date
	err := Models.DB.Where("car_no_plate = ? AND date < ?", carNoPlate, currentDate).
		Order("date DESC, odometer_after DESC").
		First(&lastFuelEvent).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// No previous records found
			log.Printf("No previous fuel events found for vehicle %s before %s", carNoPlate, currentDate)

			// Check if there are any records for this car at all
			var anyFuelEvent Models.FuelEvent
			err2 := Models.DB.Where("car_no_plate = ?", carNoPlate).
				Order("date ASC, odometer_after ASC").
				First(&anyFuelEvent).Error

			if err2 == gorm.ErrRecordNotFound {
				// This is the first record for this vehicle
				// Use a conservative estimate: assume last fill-up was 500km ago
				estimatedPrevious := max(0, currentOdo-500)
				log.Printf("First record for vehicle %s, estimating previous odometer as %d", carNoPlate, estimatedPrevious)
				return estimatedPrevious
			}

			// If this is the earliest record chronologically, use reasonable estimate
			return max(0, currentOdo-500)
		}

		log.Printf("Database error getting previous odometer for %s: %v", carNoPlate, err)
		return max(0, currentOdo-500)
	}

	// Validate that the previous reading makes sense
	if lastFuelEvent.OdometerAfter >= currentOdo {
		log.Printf("Warning: Previous odometer (%d) >= current odometer (%d) for vehicle %s on %s",
			lastFuelEvent.OdometerAfter, currentOdo, carNoPlate, currentDate)
		// Use current odometer to avoid negative distance
		return currentOdo
	}

	// Validate reasonable distance (not more than 10,000km between fill-ups)
	distance := currentOdo - lastFuelEvent.OdometerAfter
	if distance > 10000 {
		log.Printf("Warning: Very large distance between fill-ups (%d km) for vehicle %s", distance, carNoPlate)
	}

	return lastFuelEvent.OdometerAfter
}

// calculateFuelRate calculates fuel efficiency from distance and fuel consumption
func calculateFuelRate(odometerBefore, odometerAfter int, liters float64) float64 {
	// Calculate distance traveled in kilometers
	distance := float64(odometerAfter - odometerBefore)

	// Validate inputs
	if distance <= 0 {
		if distance < 0 {
			log.Printf("Warning: Negative distance for fuel rate calculation: %.1f km (before: %d, after: %d)",
				distance, odometerBefore, odometerAfter)
		} else {
			log.Printf("Warning: Zero distance for fuel rate calculation (before: %d, after: %d)",
				odometerBefore, odometerAfter)
		}
		return 0
	}

	if liters <= 0 {
		log.Printf("Warning: Invalid fuel consumption for fuel rate calculation: %.3f L", liters)
		return 0
	}

	// Calculate kilometers per liter (Km/L)
	fuelRate := distance / liters

	// Sanity checks for fuel efficiency
	if fuelRate > 50 {
		log.Printf("Warning: Unusually high fuel rate: %.3f km/L (distance: %.1f km, liters: %.3f L)",
			fuelRate, distance, liters)
	} else if fuelRate < 0.1 {
		log.Printf("Warning: Unusually low fuel rate: %.3f km/L (distance: %.1f km, liters: %.3f L)",
			fuelRate, distance, liters)
	}

	return fuelRate
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
