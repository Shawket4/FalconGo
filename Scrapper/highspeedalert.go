package Scrapper

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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

// SpeedAlert represents a high-speed alert
type SpeedAlert struct {
	VehicleID  string
	PlateNo    string
	Speed      int
	Timestamp  string
	ParsedTime time.Time
	Latitude   string
	Longitude  string
	ExceedsBy  int // How much the speed exceeds the threshold
}

// Formats a date for the API URL with proper encoding
func formatDateForURL(date time.Time) string {
	// Format the date first
	formatted := date.Format("Mon, 02 Jan 2006 15:04:05 GMT")
	// URL encode the formatted date (ensure spaces are encoded as %20)
	return url.QueryEscape(formatted)
}

// GetSpeedData retrieves speed data for a specific vehicle on a specific date
// GetSpeedData retrieves speed data for a specific vehicle on a specific date
func GetSpeedData(vehicleID string, date time.Time) (*SpeedData, error) {
	// Create start and end dates (start of day to end of day)
	startDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endDate := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 0, 0, date.Location())

	// Format the dates for the URL
	startStr := url.QueryEscape(formatDateForURL(startDate))
	endStr := url.QueryEscape(formatDateForURL(endDate))

	// Create the URL
	url := fmt.Sprintf(
		"https://fms-gps.etit-eg.com/WebPages/GetSpeedData.aspx?id=%s&time=6&from=%s&to=%s",
		vehicleID,
		startStr,
		endStr,
	)

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
	fmt.Println(string(body))

	// Check if response is HTML instead of JSON (error case)
	if strings.Contains(string(body), "<!DOCTYPE HTML") {
		return nil, fmt.Errorf("received HTML instead of JSON: %s", string(body))
	}

	// Parse the JSON response
	var speedData SpeedData
	if err := json.Unmarshal(body, &speedData); err != nil {
		return nil, fmt.Errorf("error parsing speed data: %w", err)
	}
	return &speedData, nil
}

// CheckHighSpeedAlerts checks for high-speed alerts for a vehicle
func CheckHighSpeedAlerts(vehicleID, plateNo string, date time.Time, speedThreshold int) ([]SpeedAlert, error) {
	// Get the speed data
	speedData, err := GetSpeedData(vehicleID, date)
	if err != nil {
		return nil, err
	}

	// Find high-speed points
	var alerts []SpeedAlert
	for _, point := range speedData.Points {
		speed, err := strconv.Atoi(point.Speed)
		if err != nil {
			log.Printf("Error parsing speed value '%s': %v", point.Speed, err)
			continue
		}

		// Check if speed exceeds threshold
		if speed > speedThreshold {
			// Parse timestamp
			parsedTime, _ := time.Parse("2/1/2006 15:04:05", point.Timestamp)

			// Create alert
			alert := SpeedAlert{
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
func CheckAllVehiclesForSpeedAlerts(date time.Time, speedThreshold int) (map[string][]SpeedAlert, error) {
	// Get the list of vehicles
	vehicles := GetAllVehicleData()
	if len(vehicles) == 0 {
		return nil, fmt.Errorf("no vehicles found in the list")
	}

	// Check each vehicle for speed alerts
	allAlerts := make(map[string][]SpeedAlert)
	for _, vehicle := range vehicles {
		if vehicle.ID == "" {
			log.Printf("Skipping vehicle with empty ID: %s", vehicle.PlateNo)
			continue
		}

		log.Printf("Checking vehicle %s (ID: %s) for speed alerts", vehicle.PlateNo, vehicle.ID)

		alerts, err := CheckHighSpeedAlerts(vehicle.ID, vehicle.PlateNo, date, speedThreshold)
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
func CheckVehicleSpeedByDateRange(vehicleID, plateNo string, startDate, endDate time.Time, speedThreshold int) ([]SpeedAlert, error) {
	var allAlerts []SpeedAlert

	// Iterate through each day in the range
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		alerts, err := CheckHighSpeedAlerts(vehicleID, plateNo, d, speedThreshold)
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
func RunDailySpeedAlertCheck(speedThreshold int, saveToFile bool) (string, error) {
	// Get yesterday's date
	yesterday := time.Now().AddDate(0, 0, -1)

	// Step 1: Initialize authentication
	_, err := GetClients(username, password)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}
	fmt.Println("reached 2")
	// Step 3: Generate a report for all vehicles for yesterday
	report, err := GenerateSpeedAlertReport(yesterday, speedThreshold)
	if err != nil {
		return "", fmt.Errorf("error generating report: %w", err)
	}

	// Step 4: Save the report to a file if requested
	if saveToFile {
		reportFileName := fmt.Sprintf("speed_report_%s.txt", yesterday.Format("2006-01-02"))
		err = os.WriteFile(reportFileName, []byte(report), 0644)
		// Handle file writing errors...
	}

	return report, nil
}

// GenerateSpeedAlertReport generates a report of high-speed alerts for a specific date
func GenerateSpeedAlertReport(date time.Time, speedThreshold int) (string, error) {
	alerts, err := CheckAllVehiclesForSpeedAlerts(date, speedThreshold)
	if err != nil {
		return "", err
	}

	// Format the report
	report := fmt.Sprintf("Speed Alert Report for %s (Threshold: %d km/h)\n",
		date.Format("2006-01-02"), speedThreshold)
	report += "================================================\n\n"

	totalAlerts := 0
	for plateNo, vehicleAlerts := range alerts {
		totalAlerts += len(vehicleAlerts)
		report += fmt.Sprintf("Vehicle: %s\n", plateNo)
		report += fmt.Sprintf("Total Alerts: %d\n", len(vehicleAlerts))
		report += "-----------------------------\n"

		for i, alert := range vehicleAlerts {
			report += fmt.Sprintf("  %d. Time: %s, Speed: %d km/h (exceeds by %d km/h)\n",
				i+1, alert.Timestamp, alert.Speed, alert.ExceedsBy)
		}
		report += "\n"
	}

	report += fmt.Sprintf("\nTotal vehicles with alerts: %d\n", len(alerts))
	report += fmt.Sprintf("Total alerts: %d\n", totalAlerts)

	return report, nil
}
