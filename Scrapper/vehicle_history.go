package Scrapper

import (
	"Falcon/Structs"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gocolly/colly"
)

// Map to store coordinates for each vehicle
var AllCoordinates = make(map[string][]Structs.Coordinate)
var coordinatesMutex sync.Mutex

// GetVehicleHistoryData fetches historical location data for a specific vehicle
func GetVehicleHistoryData(vehicleID string, fromDate, toDate string) ([]Structs.Coordinate, error) {
	// Get authenticated clients
	clients, err := GetClients(username, password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	// Use the collector from authenticated clients
	client := clients.Collector

	// Initialize the coordinates slice for this vehicle ID if it doesn't exist
	coordinatesMutex.Lock()
	if _, exists := AllCoordinates[vehicleID]; !exists {
		AllCoordinates[vehicleID] = []Structs.Coordinate{}
	}
	coordinatesMutex.Unlock()

	// Create variable to store history data
	var historyData Structs.TimeLineStruct

	// Set up response handler
	client.OnResponse(func(r *colly.Response) {
		jsonString := string(r.Body)

		// Use a secondary collector to clean/format the JSON
		client2 := colly.NewCollector()
		client2.OnResponse(func(jsonR *colly.Response) {
			formattedJson := string(jsonR.Body)
			var data struct {
				Result struct {
					Data string `json:"data"`
				} `json:"result"`
			}

			err := json.Unmarshal([]byte(formattedJson), &data)
			if err != nil {
				log.Println("Error parsing formatted JSON:", err.Error())
				return
			}

			finalJson := data.Result.Data

			err = json.Unmarshal([]byte(finalJson), &historyData)
			if err != nil {
				log.Println("Error parsing history data:", err.Error())
				return
			}
		})

		// Send JSON to formatting service
		// Note: In production, consider parsing JSON locally rather than using an external service
		err := client2.Post("https://jsonformatter.curiousconcept.com/process", map[string]string{
			"data":         jsonString,
			"jsontemplate": "1",
			"jsonfix":      "yes",
		})

		if err != nil {
			log.Println("Error formatting JSON:", err.Error())
			return
		}

		if len(historyData.History) > 0 {
			log.Println("Received history data for vehicle:", vehicleID)
			log.Println("First history point:", historyData.History[0])
		}
	})

	// If dates not provided, use default values
	if fromDate == "" {
		fromDate = "12/24/2022%2000:00:00"
	}
	if toDate == "" {
		toDate = "12/24/2022%2023:59:59"
	}

	// Create query URL
	queryString := fmt.Sprintf(
		"https://fms-gps.etit-eg.com/WebPages/GetAllHistoryData.aspx?id=%s&time=6&from=%s&to=%s",
		vehicleID,
		fromDate,
		toDate,
	)

	log.Println("Fetching vehicle history data from URL:", queryString)

	// Make the request
	err = client.Request("GET", queryString, nil, nil, http.Header{
		"Content-Type": []string{"text/html; charset=utf-8"},
	})

	if err != nil {
		return nil, fmt.Errorf("error requesting vehicle history: %w", err)
	}

	// Process history data
	coordinatesMutex.Lock()
	defer coordinatesMutex.Unlock()

	// Clear previous coordinates for this vehicle
	AllCoordinates[vehicleID] = []Structs.Coordinate{}

	// Process history points into coordinates
	for _, historyPoint := range historyData.History {
		// Skip if there are no points in this history entry
		if len(historyPoint.P) == 0 {
			continue
		}

		var coordinate Structs.Coordinate
		coordinate.Longitude = historyPoint.P[0].A
		coordinate.Latitude = historyPoint.P[0].O
		coordinate.DateTime = historyPoint.D

		AllCoordinates[vehicleID] = append(AllCoordinates[vehicleID], coordinate)
	}

	return AllCoordinates[vehicleID], nil
}

// GetHistoryForAllVehicles fetches history data for all vehicles in the provided list
func GetHistoryForAllVehicles(vehicles []VehicleStatusStruct, fromDate, toDate string) map[string][]Structs.Coordinate {
	results := make(map[string][]Structs.Coordinate)
	var wg sync.WaitGroup
	var resultsMutex sync.Mutex

	// Process up to 3 vehicles concurrently
	semaphore := make(chan struct{}, 3)

	for _, vehicle := range vehicles {
		if vehicle.ID == "" {
			log.Println("Skipping vehicle with empty ID:", vehicle.PlateNo)
			continue
		}

		wg.Add(1)
		go func(v VehicleStatusStruct) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("Fetching history for vehicle %s (ID: %s)\n", v.PlateNo, v.ID)

			coords, err := GetVehicleHistoryData(v.ID, fromDate, toDate)
			if err != nil {
				log.Printf("Error fetching history for vehicle %s: %v\n", v.PlateNo, err)
				return
			}

			resultsMutex.Lock()
			results[v.ID] = coords
			resultsMutex.Unlock()

			// Avoid overwhelming the server
			time.Sleep(500 * time.Millisecond)
		}(vehicle)
	}

	wg.Wait()
	return results
}

// GetVehicleHistoryByPlate fetches historical data for a vehicle identified by plate number
func GetVehicleHistoryByPlate(plateNo, fromDate, toDate string) ([]Structs.Coordinate, error) {
	// Ensure we have vehicle data
	vehicleList := GetAllVehicleData()
	if len(vehicleList) == 0 {
		return nil, fmt.Errorf("no vehicle data available")
	}

	// Find the vehicle by plate number
	var vehicleID string
	for _, v := range vehicleList {
		if v.PlateNo == plateNo {
			vehicleID = v.ID
			break
		}
	}

	if vehicleID == "" {
		return nil, fmt.Errorf("vehicle with plate number %s not found", plateNo)
	}

	// Get the history data
	return GetVehicleHistoryData(vehicleID, fromDate, toDate)
}

// FormatDateForQuery formats a date string for use in API queries
func FormatDateForQuery(date string) string {
	// Example input: "2022-12-24 00:00:00"
	// Expected output: "12/24/2022%2000:00:00"

	// This is a simple implementation - you may need to adjust based on your input format
	t, err := time.Parse("2006-01-02 15:04:05", date)
	if err != nil {
		log.Println("Error parsing date:", err)
		return date // Return original if parsing fails
	}

	return fmt.Sprintf("%02d/%02d/%04d%%20%02d:%02d:%02d",
		t.Month(), t.Day(), t.Year(), t.Hour(), t.Minute(), t.Second())
}
