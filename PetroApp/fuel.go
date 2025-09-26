package PetroApp

import (
	"Falcon/Constants"
	"Falcon/Models"
	"Falcon/Whatsapp"
	"bytes"
	"context"
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
type PlateNumberObj struct {
	PlateNumberDB     string `json:"plate_number_db"`
	PetroAppVehicleID uint   `json:"petro_app_vehicle_id"`
}

var PlateNumberMap = map[string]PlateNumberObj{
	"C E F-4 3 8 1": PlateNumberObj{
		PlateNumberDB:     "ف ع ص 4381",
		PetroAppVehicleID: 53008,
	},
	"C Q F-4 2 5 3": PlateNumberObj{
		PlateNumberDB:     "ف ق ص 4253",
		PetroAppVehicleID: 53005,
	},
	"R Y F-9 1 5 6": PlateNumberObj{
		PlateNumberDB:     "ف ى ر 9156",
		PetroAppVehicleID: 53004,
	},
	"N A F-5 1 3 9": PlateNumberObj{
		PlateNumberDB:     "ف أ ن 5139",
		PetroAppVehicleID: 53007,
	},
	"S M F-9 2 4 7": PlateNumberObj{
		PlateNumberDB:     "ف م س 9247",
		PetroAppVehicleID: 53010,
	},
	"S R F-4 5 9 3": PlateNumberObj{
		PlateNumberDB:     "ف ر س 4593",
		PetroAppVehicleID: 53012,
	},
	"Y D F-6 5 8 4": PlateNumberObj{
		PlateNumberDB:     "ف د ى 6584",
		PetroAppVehicleID: 53006,
	},
	"Y D F-6 8 3 4": PlateNumberObj{
		PlateNumberDB:     "ف د ى 6834",
		PetroAppVehicleID: 53011,
	},
	"N A F-7 4 2 1": PlateNumberObj{
		PlateNumberDB:     "ف ا ن 7421",
		PetroAppVehicleID: 53009,
	},
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

// FetchPetroAppRecords fetches records from PetroApp API for the last day
func FetchPetroAppRecords() ([]Models.PetroAppRecord, error) {
	log.Println("Starting PetroApp records fetch operation")

	// Get date range: last day to current day
	now := time.Now()
	startDate := now.AddDate(0, 0, -1).Format("2006/01/02")
	endDate := now.Format("2006/01/02")

	params := fmt.Sprintf("?dates=%s-%s&limit=1000&page=1", startDate, endDate)
	url := baseUrl + "/bills" + params

	log.Printf("Fetching records from %s to %s", startDate, endDate)
	log.Printf("Request URL: %s", url)

	// Create HTTP request with shorter timeout to prevent hangs
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

	// Reduced timeout to prevent hanging - fail fast
	client := &http.Client{
		Timeout: 30 * time.Second,
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

	// Decode response
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

	// Use shorter transaction timeout to prevent hangs
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use context-aware transaction
	tx := Models.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("Transaction rolled back due to panic: %v", r)
		}
	}()

	// Batch check for existing records to reduce DB queries
	var existingIDs []uint
	recordIDs := make([]uint, len(records))
	for i, record := range records {
		recordIDs[i] = record.ID
	}

	if err := tx.Model(&Models.PetroAppRecord{}).
		Where("id IN ?", recordIDs).
		Pluck("id", &existingIDs).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to check existing records: %w", err)
	}

	// Create map for faster lookup
	existingMap := make(map[uint]bool)
	for _, id := range existingIDs {
		existingMap[id] = true
	}

	// Only insert non-existing records
	var newRecords []Models.PetroAppRecord
	for _, record := range records {
		if !existingMap[record.ID] {
			newRecords = append(newRecords, record)
		} else {
			skipped++
			log.Printf("PetroApp record ID %d already exists, skipping", record.ID)
		}
	}

	// Batch insert new records
	if len(newRecords) > 0 {
		if err := tx.CreateInBatches(newRecords, 100).Error; err != nil {
			tx.Rollback()
			log.Printf("Error batch creating PetroApp records: %v", err)
			return fmt.Errorf("failed to batch create records: %w", err)
		}
		stored = len(newRecords)
		log.Printf("Batch created %d new PetroApp records", stored)
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

	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var unsyncedRecords []Models.PetroAppRecord

	// Get all unsynced records with timeout and reasonable limit
	if err := Models.DB.WithContext(ctx).Where("is_synced = ?", false).
		Order("date ASC").
		Limit(100).
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
	errors := 0

	// Process each record individually to prevent long-running transactions
	for _, record := range unsyncedRecords {
		if err := syncSingleRecord(record); err != nil {
			log.Printf("Error syncing record ID %d: %v", record.ID, err)
			errors++
			continue
		}
		synced++
	}

	log.Printf("Synchronization complete: %d synced, %d errors", synced, errors)

	if errors > 0 {
		log.Printf("Warning: synchronization completed with %d errors", errors)
	}

	return nil
}

// syncSingleRecord syncs a single PetroApp record to avoid transaction hangs
func syncSingleRecord(record Models.PetroAppRecord) error {
	// Use short timeout for each individual record
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx := Models.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("Single record sync rolled back due to panic: %v", r)
		}
	}()

	// Convert PetroApp record to FuelEvent
	fuelEvent, err := convertPetroAppToFuelEvent(record)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error converting record: %w", err)
	}

	// Check if this FuelEvent already exists
	if fuelEventExistsInTx(tx, *fuelEvent) {
		log.Printf("FuelEvent already exists for PetroApp record ID %d, marking as synced", record.ID)

		// Mark as synced but DON'T create the FuelEvent
		if err := tx.Model(&record).Update("is_synced", true).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("error updating sync status for duplicate: %w", err)
		}

		if err := tx.Commit().Error; err != nil {
			return fmt.Errorf("error committing duplicate sync: %w", err)
		}
		return nil
	}

	// Create the FuelEvent in database FIRST
	if err := tx.Create(fuelEvent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("error creating FuelEvent: %w", err)
	}

	// Update the car's last_fuel_odometer
	if err := tx.Model(&Models.Car{}).
		Where("car_no_plate = ?", fuelEvent.CarNoPlate).
		Update("last_fuel_odometer", fuelEvent.OdometerAfter).Error; err != nil {
		log.Printf("Warning: Error updating car's last_fuel_odometer for vehicle %s: %v", fuelEvent.CarNoPlate, err)
	}

	// Mark PetroApp record as synced
	if err := tx.Model(&record).Update("is_synced", true).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("error updating sync status: %w", err)
	}

	// Commit transaction BEFORE sending messages
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}

	// Send notifications AFTER database commit to ensure consistency
	sendNotificationsAsync(*fuelEvent, record.ID)

	log.Printf("Successfully synced PetroApp record ID %d to FuelEvent ID %d", record.ID, fuelEvent.ID)
	return nil
}

// sendNotificationsAsync sends WhatsApp notifications asynchronously
func sendNotificationsAsync(fuelEvent Models.FuelEvent, recordID uint) {
	go func() {
		// Send WhatsApp message
		whatsappMessage := createWhatsAppMessage(fuelEvent)
		if err := Whatsapp.SendMessage(Constants.WhatsAppGPIDFuel, whatsappMessage); err != nil {
			log.Printf("Error sending WhatsApp message for record ID %d: %v", recordID, err)
		} else {
			log.Printf("WhatsApp notification sent for vehicle %s (record ID %d)", fuelEvent.CarNoPlate, recordID)
		}

		// Log anomalies
		if fuelEvent.FuelRate > 2.8 || fuelEvent.FuelRate < 1.9 {
			log.Printf("Fuel rate anomaly detected for vehicle %s: %.3f km/L (record ID %d)",
				fuelEvent.CarNoPlate, fuelEvent.FuelRate, recordID)
		}
	}()
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

	// Calculate price per liter
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
	if parsedTimeObj, _ := time.Parse("2006-01-02", parsedDate); parsedTimeObj.After(time.Now().AddDate(0, 0, 1)) {
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

	// Get driver name with timeout to prevent hanging
	var driverID uint
	var driverName string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Models.DB.WithContext(ctx).Model(&Models.Car{}).Where("car_no_plate = ?", convertedPlate).Select("driver_id").Scan(&driverID).Error; err != nil {
		log.Printf("Warning: Error getting driver ID for vehicle %s: %v", convertedPlate, err)
	}

	if driverID != 0 {
		if err := Models.DB.WithContext(ctx).Model(&Models.Driver{}).Where("id = ?", driverID).Select("name").Scan(&driverName).Error; err != nil {
			log.Printf("Warning: Error getting driver name for ID %d: %v", driverID, err)
		}
	}

	if driverName == "" {
		driverName = strings.TrimSpace(record.DelegateName)
		if driverName == "" {
			driverName = "PetroApp Unknown Driver"
		}
	}

	transporter := strings.TrimSpace(record.Station)
	if transporter == "" {
		transporter = "Unknown PetroApp Station"
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
		Method:         "PetroApp",
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
		return converted.PlateNumberDB
	}

	log.Printf("Warning: No plate mapping found for '%s', using original format", trimmedPlate)
	return trimmedPlate
}

// UpdatePetroAppOdometerFromManualFuelEvent updates odometer in PetroApp system
func UpdatePetroAppOdometerFromManualFuelEvent(plateNumber string, odometer int) error {
	var PlateObj PlateNumberObj
	found := false
	for _, v := range PlateNumberMap {
		if v.PlateNumberDB == plateNumber {
			PlateObj = v
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no PetroApp vehicle mapping found for plate %s", plateNumber)
	}

	log.Printf("Updating PetroApp odometer for vehicle ID %d to %d", PlateObj.PetroAppVehicleID, odometer)

	url := baseUrl + "/edit_odometer"
	log.Printf("Request URL: %s", url)

	// Create request body
	reqBodyStr := fmt.Sprintf(`{"vehicle_id": %d, "odometer": %d}`, PlateObj.PetroAppVehicleID, odometer)
	reqBody := bytes.NewBuffer([]byte(reqBodyStr))

	// Create HTTP request with timeout
	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		log.Printf("Error creating HTTP request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Validate required headers
	if token == "" {
		return fmt.Errorf("authorization token is not set")
	}
	if cookie == "" {
		log.Println("Warning: cookie is empty, this might cause authentication issues")
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Falcon-PetroApp-Sync/1.0")

	// Create HTTP client with timeout to prevent hangs
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error executing HTTP request: %v", err)
		return fmt.Errorf("failed to execute request: %w", err)
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
		log.Printf("Successfully updated odometer for vehicle %s to %d", plateNumber, odometer)
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("authentication failed - check token and cookie")
	case http.StatusForbidden:
		return fmt.Errorf("access forbidden - insufficient permissions")
	case http.StatusTooManyRequests:
		return fmt.Errorf("rate limit exceeded - try again later")
	default:
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}
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

	// Validate date is reasonable
	now := time.Now()
	if t.Before(now.AddDate(-10, 0, 0)) {
		return "", "", fmt.Errorf("date is too far in the past: %s", trimmedDate)
	}
	if t.After(now.AddDate(0, 0, 1)) {
		return "", "", fmt.Errorf("date is in the future: %s", trimmedDate)
	}

	// Return date in FuelEvent format
	return t.Format("2006-01-02"), t.Format("3:04 PM"), nil
}

// getPreviousOdometerReading gets the previous odometer reading for accurate fuel rate calculation
func getPreviousOdometerReading(carNoPlate string, currentOdo int, currentDate string) int {
	if carNoPlate == "" {
		log.Printf("Warning: Empty car plate provided")
		return max(0, currentOdo-500)
	}

	// Use timeout to prevent database hangs
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var lastFuelEvent Models.FuelEvent

	// Get the most recent FuelEvent for this car BEFORE the current date
	err := Models.DB.WithContext(ctx).Where("car_no_plate = ? AND date < ?", carNoPlate, currentDate).
		Order("date DESC, odometer_after DESC").
		First(&lastFuelEvent).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// No previous records found
			log.Printf("No previous fuel events found for vehicle %s before %s", carNoPlate, currentDate)

			// Check if there are any records for this car at all
			var anyFuelEvent Models.FuelEvent
			err2 := Models.DB.WithContext(ctx).Where("car_no_plate = ?", carNoPlate).
				Order("date ASC, odometer_after ASC").
				First(&anyFuelEvent).Error

			if err2 == gorm.ErrRecordNotFound {
				// This is the first record for this vehicle
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
