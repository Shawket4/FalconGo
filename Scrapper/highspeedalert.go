package Scrapper

import (
	"Falcon/Models"
	"Falcon/Scrapper/Alerts"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yosuke-furukawa/json5/encoding/json5"
)

// SpeedPoint represents a point in the speed data
type SpeedPoint struct {
	Latitude  string `json:"a"` // Latitude
	Longitude string `json:"o"` // Longitude
	Speed     string `json:"s"` // Speed
	Timestamp string `json:"d"` // Timestamp
}

// SpeedData represents the GPS speed data response
type SpeedData struct {
	Points []SpeedPoint `json:"points"`
}

// Formats a date for the API URL with proper encoding
func formatDateForURL(date time.Time) string {
	// Format the date and URL encode it
	formatted := date.Format("Mon, 02 Jan 2006 15:04:05 GMT")
	return url.QueryEscape(formatted)
}

// GetSpeedData retrieves speed data for a specific vehicle on a specific date
// GetSpeedData retrieves speed data for a specific vehicle on a specific date
func GetSpeedData(vehicleID string) (*SpeedData, error) {
	// Use GMT (UTC) time zone
	gmtLoc, err := time.LoadLocation("GMT")
	if err != nil {
		return nil, fmt.Errorf("failed to load GMT timezone: %w", err)
	}

	// Get current time in Cairo
	nowCairo := time.Now().In(gmtLoc)
	// Start date is 15 minutes before now in Cairo
	startDate := nowCairo.Add(-15 * time.Minute)
	endDate := nowCairo

	// Format the dates for the URL
	startStr := formatDateForURL(startDate)
	endStr := formatDateForURL(endDate)

	// Create the URL
	url := fmt.Sprintf(
		"https://fms-gps.etit-eg.com/WebPages/GetSpeedData.aspx?_=1752264215278&id=%s&time=6&from=%s&to=%s",
		vehicleID,
		startStr,
		endStr,
	)
	fmt.Println(url)
	// Get authenticated clients
	clients, err := GetClients(username, password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	// Ensure we're using the authenticated client
	log.Printf("Fetching speed data from: %s", url)

	// Make sure we're authenticated with the GPS domain first
	// This is crucial - visit the domain first to ensure cookies are set properly
	preReq, err := clients.HttpClient.Get("https://fms-gps.etit-eg.com/WebPages/maps.aspx")
	if err != nil {
		return nil, fmt.Errorf("error establishing GPS session: %w", err)
	}
	preReq.Body.Close()

	// Now make the actual request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	// Add headers to match Postman
	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Add("Accept", "application/json, text/plain, */*")
	req.Header.Add("Accept-Language", "en-US,en;q=0.9")
	req.Header.Add("Referer", "https://fms-gps.etit-eg.com/WebPages/Maps.aspx")

	// Make the request with our authenticated client
	resp, err := clients.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching speed data: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}
	// Check if response is HTML instead of JSON (error case)
	if strings.Contains(string(body), "<!DOCTYPE HTML") {
		return nil, fmt.Errorf("received HTML instead of JSON: %s", string(body))
	}

	// Parse the JSON response
	var speedData SpeedData
	if err := json5.Unmarshal(body, &speedData); err != nil {
		return nil, fmt.Errorf("error parsing speed data: %w", err)
	}
	return &speedData, nil
}

func parseTimestampManual(timestamp string) (time.Time, error) {
	// Split by space
	parts := strings.Split(timestamp, " ")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid timestamp format: %s", timestamp)
	}

	// Parse date: "10/7/2025"
	dateParts := strings.Split(parts[0], "/")
	if len(dateParts) != 3 {
		return time.Time{}, fmt.Errorf("invalid date format: %s", parts[0])
	}

	month, err := strconv.Atoi(dateParts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month: %s", dateParts[0])
	}

	day, err := strconv.Atoi(dateParts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day: %s", dateParts[1])
	}

	year, err := strconv.Atoi(dateParts[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid year: %s", dateParts[2])
	}

	// Parse time: "10:49:7"
	timeParts := strings.Split(parts[1], ":")
	if len(timeParts) != 3 {
		return time.Time{}, fmt.Errorf("invalid time format: %s", parts[1])
	}

	hour, err := strconv.Atoi(timeParts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid hour: %s", timeParts[0])
	}

	minute, err := strconv.Atoi(timeParts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid minute: %s", timeParts[1])
	}

	second, err := strconv.Atoi(timeParts[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid second: %s", timeParts[2])
	}

	// Create time using time.Date
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC), nil
}

func padToTwoDigits(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func parseTimestampWithFullPadding(timestamp string) (time.Time, error) {
	// Split by space to separate date and time
	parts := strings.Split(timestamp, " ")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid timestamp format: %s", timestamp)
	}

	datePart := parts[0] // e.g., "10/7/2025" or "1/2/2025"
	timePart := parts[1] // e.g., "10:49:7" or "11:3:46"

	// Parse and pad date part
	dateParts := strings.Split(datePart, "/")
	if len(dateParts) != 3 {
		return time.Time{}, fmt.Errorf("invalid date format: %s", datePart)
	}

	month := padToTwoDigits(dateParts[0])
	day := padToTwoDigits(dateParts[1])
	year := dateParts[2] // Year should always be 4 digits

	// Parse and pad time part
	timeParts := strings.Split(timePart, ":")
	if len(timeParts) != 3 {
		return time.Time{}, fmt.Errorf("invalid time format: %s", timePart)
	}

	hour := padToTwoDigits(timeParts[0])
	minute := padToTwoDigits(timeParts[1])
	second := padToTwoDigits(timeParts[2])

	// Reconstruct with proper padding
	normalizedTimestamp := fmt.Sprintf("%s/%s/%s %s:%s:%s",
		month, day, year, hour, minute, second)

	// Parse with consistent format
	return time.Parse("01/02/2006 15:04:05", normalizedTimestamp)
}

func ParseGPSTimestamp(timestamp string) (time.Time, error) {
	// Try the padding solution first (fastest and most reliable)
	if parsed, err := parseTimestampWithFullPadding(timestamp); err == nil {
		return parsed, nil
	}

	// Fallback to manual parsing
	return parseTimestampManual(timestamp)
}

// CheckHighSpeedAlerts checks for high-speed alerts for a vehicle
func CheckHighSpeedAlerts(vehicleID, plateNo string, speedThreshold int) ([]Models.SpeedAlert, error) {
	// Get the speed data
	speedData, err := GetSpeedData(vehicleID)
	if err != nil {
		return nil, err
	}

	// Find high-speed points
	var alerts []Models.SpeedAlert
	for _, point := range speedData.Points {
		speed, err := strconv.Atoi(point.Speed)
		if err != nil {
			log.Printf("Error parsing speed value '%s': %v", point.Speed, err)
			continue
		}

		// Check if speed exceeds threshold
		if speed > speedThreshold {
			// Parse timestamp
			parsedTime, _ := ParseGPSTimestamp(point.Timestamp)

			// Create alert
			alert := Models.SpeedAlert{
				VehicleID:  vehicleID,
				PlateNo:    plateNo,
				Speed:      speed,
				Timestamp:  point.Timestamp,
				ParsedTime: parsedTime,
				Latitude:   point.Latitude,
				Longitude:  point.Longitude,
				ExceedsBy:  speed - speedThreshold,
			}

			alerts = append(alerts, alert)
		}
	}

	return alerts, nil
}

// CheckAllVehiclesForSpeedAlerts checks all vehicles for high-speed alerts
func CheckAllVehiclesForSpeedAlerts(speedThreshold int) (map[string][]Models.SpeedAlert, error) {
	// Get the list of vehicles
	vehicles := GetAllVehicleData()
	if len(vehicles) == 0 {
		return nil, fmt.Errorf("no vehicles found in the list")
	}

	// Check each vehicle for speed alerts
	allAlerts := make(map[string][]Models.SpeedAlert)
	for _, vehicle := range vehicles {
		if vehicle.ID == "" {
			log.Printf("Skipping vehicle with empty ID: %s", vehicle.PlateNo)
			continue
		}

		log.Printf("Checking vehicle %s (ID: %s) for speed alerts", vehicle.PlateNo, vehicle.ID)

		alerts, err := CheckHighSpeedAlerts(vehicle.ID, vehicle.PlateNo, speedThreshold)
		if err != nil {
			log.Printf("Error checking vehicle %s: %v", vehicle.PlateNo, err)
			continue
		}

		if len(alerts) > 0 {
			allAlerts[vehicle.PlateNo] = alerts
			log.Printf("Found %d speed alerts for vehicle %s", len(alerts), vehicle.PlateNo)
		}

		// Sleep a bit to avoid overwhelming the server
		time.Sleep(500 * time.Millisecond)
	}

	return allAlerts, nil
}

// CheckVehicleSpeedByDateRange checks a specific vehicle for speed alerts over a date range
func CheckVehicleSpeedByDateRange(vehicleID, plateNo string, startDate, endDate time.Time, speedThreshold int) ([]Models.SpeedAlert, error) {
	var allAlerts []Models.SpeedAlert

	// Iterate through each day in the range
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		alerts, err := CheckHighSpeedAlerts(vehicleID, plateNo, speedThreshold)
		if err != nil {
			log.Printf("Error checking date %s: %v", d.Format("2006-01-02"), err)
			continue
		}

		allAlerts = append(allAlerts, alerts...)

		// Sleep a bit to avoid overwhelming the server
		time.Sleep(1 * time.Second)
	}

	return allAlerts, nil
}

// RunDailySpeedAlertCheck performs a daily check for speed alerts and returns the report
// speedThreshold: The speed threshold in km/h (default 80)
// saveToFile: Whether to save the report to a file
// Returns the report as a string and any error that occurred
func RunSpeedCheckJob(speedThreshold int, saveToFile bool) error {
	// Get yesterday's date

	// Step 1: Initialize authentication
	_, err := GetClients(username, password)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	alerts, err := CheckAllVehiclesForSpeedAlerts(speedThreshold)
	var allAlerts []Models.SpeedAlert
	for _, vehicleAlerts := range alerts {
		allAlerts = append(allAlerts, vehicleAlerts...)
	}
	Alerts.StoreUniqueAlerts(allAlerts)
	if err != nil {
		return err
	}
	return nil
}
