package Scrapper

import (
	"Falcon/Models"
	"Falcon/Slack"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
	"github.com/joho/godotenv"
)

const (
	username = "magda.adly"
	password = "etit135"
)

type VehicleStatusStruct struct {
	PlateNo      string
	Speed        int
	Longitude    string
	Latitude     string
	EngineStatus string
	ID           string
	Timestamp    string
}

var VehicleStatusList []VehicleStatusStruct
var VehicleStatusListTemp []VehicleStatusStruct
var isLoaded bool = false
var GlobalClient *colly.Collector

var Data struct {
	Data struct {
		Rows []struct {
			PlateNo string `json:"plateNo"`
			CodeNo  string `json:"CodePlateNumber"`
			ID      string `json:"ID"`
		} `json:"rows"`
	} `json:"d"`
}

// GetCurrentLocationData fetches the current location data for vehicles
func GetCurrentLocationData(client *colly.Collector) error {
	client.OnHTML("#ctl00_ContentPlaceHolder1_grd_TransportersData_ctl00", func(h *colly.HTMLElement) {
		h.ForEach("tr.rgRow", func(_ int, tr *colly.HTMLElement) {
			var CurrentVehicleStatus VehicleStatusStruct
			tr.ForEach("td", func(i int, td *colly.HTMLElement) {
				if i == 2 {
					CurrentVehicleStatus.PlateNo = td.Text
				} else if i == 7 {
					CurrentVehicleStatus.Latitude = td.Text
				} else if i == 8 {
					CurrentVehicleStatus.Longitude = td.Text
				} else if i == 11 {
					// Convert to 2006-01-02 15:04:05
					// Example input: "\n                                        10-07-2025 06:59:16 PM\n"
					raw := strings.TrimSpace(td.Text)
					parsedTime, err := time.Parse("02-01-2006 03:04:05 PM", raw)
					if err == nil {
						CurrentVehicleStatus.Timestamp = parsedTime.Format("2006-01-02 15:04:05")
					} else {
						CurrentVehicleStatus.Timestamp = raw // fallback to raw if parsing fails
					}
				} else if i == 12 {
					CurrentVehicleStatus.EngineStatus = td.Text
				} else if i == 13 {
					id, _ := strconv.Atoi(td.Text)
					CurrentVehicleStatus.Speed = id
					VehicleStatusListTemp = append(VehicleStatusListTemp, CurrentVehicleStatus)
				}
			})
		})
		h.ForEach("tr.rgAltRow", func(_ int, tr *colly.HTMLElement) {
			var CurrentVehicleStatus VehicleStatusStruct
			tr.ForEach("td", func(i int, td *colly.HTMLElement) {
				if i == 2 {
					CurrentVehicleStatus.PlateNo = td.Text
				} else if i == 7 {
					CurrentVehicleStatus.Latitude = td.Text
				} else if i == 8 {
					CurrentVehicleStatus.Longitude = td.Text
				} else if i == 11 {
					// Convert to 2006-01-02 15:04:05
					// Example input: "\n                                        10-07-2025 06:59:16 PM\n"
					raw := strings.TrimSpace(td.Text)
					parsedTime, err := time.Parse("02-01-2006 03:04:05 PM", raw)
					if err == nil {
						CurrentVehicleStatus.Timestamp = parsedTime.Format("2006-01-02 15:04:05")
					} else {
						CurrentVehicleStatus.Timestamp = raw // fallback to raw if parsing fails
					}
				} else if i == 12 {
					CurrentVehicleStatus.EngineStatus = td.Text
				} else if i == 13 {
					id, _ := strconv.Atoi(td.Text)
					CurrentVehicleStatus.Speed = id
					VehicleStatusListTemp = append(VehicleStatusListTemp, CurrentVehicleStatus)
				}
			})
		})
	})
	err := client.Request("GET", "https://fms-gps.etit-eg.com/WebPages/UpdateTransportersData.aspx", nil, nil, http.Header{"Content-Type": []string{"text/html; charset=utf-8"}})
	if err != nil {
		log.Println(err)
		return err
	}
	client.OnResponse(func(r *colly.Response) {
		jsonString := string(r.Body)
		jsonString = strings.Replace(jsonString, "\\", "", -1)
		jsonString = strings.Replace(jsonString, "\"{", "{", -1)
		jsonString = strings.Replace(jsonString, "\"}", "}", -1)

		err := json.Unmarshal([]byte(jsonString), &Data)
		if err != nil {
			log.Println(err.Error())
		}
		for i := 0; i < len(Data.Data.Rows); i++ {
			for i2 := 0; i2 < len(VehicleStatusListTemp); i2++ {
				if Data.Data.Rows[i].PlateNo == VehicleStatusListTemp[i2].PlateNo {
					VehicleStatusListTemp[i2].ID = Data.Data.Rows[i].ID
				}
			}
		}
	})
	jsonString := `{"transpoterCriteria":{"SubId":"","StuffId":"","TransporterCodeName":"","TransporterId":"","TransporterTypeId":"","TransporterGroupID":"","LandmarkId":"","ManufacturerId":"","ProductionYearID":"","BranchID":"","SubBranchID":"","NextExaminationDate":"","LicenseExpiryDate":"","InsuranceEndDate":"","EntranceDate":"","TransporterBrand":"","DasboardGPsStatus":"","IsdasboardGPsStatus":0,"TransporterStatus":"","Category":"","EntranceDateBeforeAfter":"","InsuranceEndDateBeforeAfter":"","LicenseExpiryDateBeforeAfter":"","NextExaminationDateBeforAfter":"","PageCount":0,"PageIndex":1,"PageSize":20,"TotalTransCount":0,"TransporterIdList":[]}}`

	err = client.Request("POST", baseURL+"/WebPages/Transporters/List.aspx/GetAllTransporterBySearchCriteria", strings.NewReader(jsonString), nil, http.Header{"Content-Type": []string{"application/json; charset=utf-8"}})
	if err != nil {
		log.Println(err)
		return err
	}

	if len(VehicleStatusListTemp) == 0 {
		return errors.New("Empty")
	}
	VehicleStatusList = VehicleStatusListTemp
	return nil
}

type NominatimResponse struct {
	DisplayName string `json:"display_name"`
}

func getAddressFromCoords(lat, lng string) (string, error) {
	url := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?format=json&lat=%s&lon=%s&zoom=18&addressdetails=1", lat, lng)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	// Add User-Agent header (required by Nominatim)
	req.Header.Set("User-Agent", "Apex/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result NominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.DisplayName, nil
}

// GetAllVehicleData returns the current vehicle status list
func GetAllVehicleData() []VehicleStatusStruct {
	// If the data isn't loaded yet, get it
	if !isLoaded {
		GetVehicleData()
	}
	return VehicleStatusList
}

// GeoFence represents a geographical boundary
type GeoFence struct {
	Name      string
	Latitude  float64
	Longitude float64
	Radius    float64 // in kilometers
	Type      string  // "garage", "terminal", or "dropoff"
}

// Define all geofences
var AllGeoFences = []GeoFence{
	// Garage
	{
		Name:      "garage",
		Latitude:  30.128955,
		Longitude: 31.298539,
		Radius:    0.4, // 400 meters radius
		Type:      "garage",
	},
	// Terminals
	{
		Name:      "Badr Terminal",
		Latitude:  30.1020583,
		Longitude: 31.81396,
		Radius:    0.5, // 500 meters radius
		Type:      "terminal",
	},
	{
		Name:      "CPC Mostorod Terminal",
		Latitude:  30.144197,
		Longitude: 31.296322,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Fayoum Terminal",
		Latitude:  29.3391616,
		Longitude: 30.9257033,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Misr Petroleum Bor Saed Terminal",
		Latitude:  31.235575,
		Longitude: 32.301198,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Mobil Bor Saed Terminal",
		Latitude:  31.23365,
		Longitude: 32.298082,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Haykstep Terminal",
		Latitude:  30.12486,
		Longitude: 31.3580633,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Somed Terminal",
		Latitude:  29.594416,
		Longitude: 32.329073,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Agroud Terminal",
		Latitude:  30.071958,
		Longitude: 32.381296,
		Radius:    0.5,
		Type:      "terminal",
	},
}

// calculateDistance calculates distance between two coordinates using Haversine formula
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth's radius in kilometers

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	distance := R * c

	return distance
}

// checkGeofences checks which geofence the vehicle is in (if any)
func checkGeofences(lat, lng float64, car *Models.Car) (string, string, bool) {
	// First check static geofences (garage and terminals)
	for _, geofence := range AllGeoFences {
		distance := calculateDistance(lat, lng, geofence.Latitude, geofence.Longitude)
		if distance <= geofence.Radius {
			return geofence.Name, geofence.Type, true
		}
	}

	// Then check drop-off point geofences from database
	// Only check if vehicle is stopped (speed <= 5)
	if car.Speed <= 5 {
		dropoffName, found := checkDropOffPoints(lat, lng, car.OperatingCompany)
		if found {
			return dropoffName, "dropoff", true
		}
	}

	return "", "", false
}

// checkDropOffPoints checks if vehicle is at any drop-off point
func checkDropOffPoints(lat, lng float64, company string) (string, bool) {
	var feeMappings []Models.FeeMapping

	// Get all fee mappings for this company
	if err := Models.DB.Where("company = ?", company).Find(&feeMappings).Error; err != nil {
		log.Printf("Error fetching fee mappings: %v", err)
		return "", false
	}

	// Check each drop-off point (500m radius)
	for _, mapping := range feeMappings {
		distance := calculateDistance(lat, lng, mapping.Latitude, mapping.Longitude)
		if distance <= 0.5 { // 500 meters radius
			return mapping.DropOffPoint, true
		}
	}

	return "", false
}

// updateCarGeofence updates car's geofence based on current location
func updateCarGeofence(car *Models.Car, lat, lng float64, timestamp string) bool {
	// Parse the timestamp from VehicleStatusStruct
	newTimestamp, err := time.Parse("2006-01-02 15:04:05", timestamp)
	if err != nil {
		log.Printf("Error parsing timestamp %s for car %s: %v", timestamp, car.CarNoPlate, err)
		return false
	}

	// Check if new timestamp is after last updated slack status
	if !car.LastUpdatedSlackStatus.IsZero() && !newTimestamp.After(car.LastUpdatedSlackStatus) {
		log.Printf("Skipping update for car %s - timestamp %s is not newer than last update %s",
			car.CarNoPlate, timestamp, car.LastUpdatedSlackStatus.Format("2006-01-02 15:04:05"))
		return false
	}

	// Check all geofences (including drop-off points)
	geofenceName, geofenceType, inGeofence := checkGeofences(lat, lng, car)

	if inGeofence {
		car.GeoFence = geofenceName
		switch geofenceType {
		case "garage":
			car.SlackStatus = "Parked"
		case "terminal":
			car.SlackStatus = "At Terminal"
		case "dropoff":
			car.SlackStatus = "At Delivery"
		}
	} else {
		car.GeoFence = ""
		// Determine slack status from engine status and speed
		if car.EngineStatus == "Ignition On" {
			car.SlackStatus = "Available & On"
		} else {
			car.SlackStatus = "Available & Off"
		}
	}

	// Update the last updated timestamp
	car.LastUpdatedSlackStatus = newTimestamp

	return true // Indicates update was performed
}

// generateSlackMessage generates formatted slack message for a company
func generateSlackMessage(cars []Models.Car, company string) string {
	var message strings.Builder

	// Header
	message.WriteString(fmt.Sprintf("# %s FLEET STATUS\n", strings.ToUpper(company)))
	message.WriteString(fmt.Sprintf("*Last Updated: %s*\n\n", time.Now().Format("January 2, 2006 - 15:04:05 MST")))
	message.WriteString("---\n\n")

	// Vehicle details
	for _, car := range cars {
		// Get driver name
		driverName := "Unknown Driver"
		if car.Driver.Name != "" {
			driverName = car.Driver.Name
		}

		// Determine location to display
		displayLocation := car.Location
		if car.GeoFence != "" {
			// Check if it's a terminal, garage, or drop-off point
			for _, geofence := range AllGeoFences {
				if geofence.Name == car.GeoFence {
					if geofence.Type == "garage" {
						displayLocation = "Garage - Parked"
					} else if geofence.Type == "terminal" {
						displayLocation = fmt.Sprintf("%s", geofence.Name)
					}
					break
				}
			}
			// If not found in static geofences, it might be a drop-off point
			if displayLocation == car.Location {
				displayLocation = fmt.Sprintf("%s - Drop Off Point", car.GeoFence)
			}
		}

		// Get status emoji
		statusEmoji := getStatusEmoji(car.SlackStatus)

		// Generate Google Maps link
		mapsLink := ""
		if car.Latitude != "" && car.Longitude != "" {
			mapsLink = fmt.Sprintf("https://maps.google.com/?q=%s,%s", car.Latitude, car.Longitude)
		}

		message.WriteString(fmt.Sprintf("## **%s**\n", car.CarNoPlate))
		message.WriteString(fmt.Sprintf("**Driver:** %s  \n", driverName))
		message.WriteString(fmt.Sprintf("**Area:** %s  \n", car.OperatingArea))
		message.WriteString(fmt.Sprintf("**Status:** %s %s  \n", car.SlackStatus, statusEmoji))
		message.WriteString(fmt.Sprintf("**Location:** %s  \n", displayLocation))

		// Add Google Maps link if coordinates are available
		if mapsLink != "" {
			message.WriteString(fmt.Sprintf("**Maps:** [View Location](%s)  \n", mapsLink))
		}

		// Parse and format timestamp
		if car.LocationTimeStamp != "" {
			if parsedTime, err := time.Parse("2006-01-02 15:04:05", car.LocationTimeStamp); err == nil {
				message.WriteString(fmt.Sprintf("**Last Update:** %s\n\n", parsedTime.Format("15:04")))
			} else {
				message.WriteString(fmt.Sprintf("**Last Update:** %s\n\n", car.LocationTimeStamp))
			}
		} else {
			message.WriteString("**Last Update:** Unknown\n\n")
		}

		message.WriteString("---\n\n")
	}

	// Status legend
	message.WriteString("### **Status Legend:**\n")
	message.WriteString("🟢 **Available & On** - Ready for dispatch (engine on)  \n")
	message.WriteString("🟢 **Available & Off** - Ready for dispatch (engine off)  \n")
	message.WriteString("🟡 **At Terminal** - At fuel terminal  \n")
	message.WriteString("🔵 **Loading** - Loading fuel at terminal  \n")
	message.WriteString("🔴 **En Route** - Traveling to destination  \n")
	message.WriteString("🟠 **At Delivery** - Unloading at drop-off point  \n")
	message.WriteString("🅿️ **Parked** - At garage/depot  \n")
	message.WriteString("⚫ **Off Duty** - Driver break/end of shift\n\n")

	// Footer
	message.WriteString("---\n")
	message.WriteString("*Auto-updated by Apex Transport System*")

	return message.String()
}

// getStatusEmoji returns appropriate emoji for status
func getStatusEmoji(status string) string {
	switch strings.ToLower(status) {
	case "available & on":
		return "🟢"
	case "available & off":
		return "🟢"
	case "at terminal":
		return "🟡"
	case "loading":
		return "🔵"
	case "en route":
		return "🔴"
	case "at delivery":
		return "🟠"
	case "parked":
		return "🅿️"
	case "off duty":
		return "⚫"
	default:
		return "❓"
	}
}

// Company to Slack channel mapping
var CompanyChannelMap = map[string]string{
	"petrol_arrows": "C09GSBV2TSR",
	"taqa":          "C09H1DFP21J",
	"watanya":       "C09GW6QNT46",
}

// sendFleetUpdatesToSlack sends updates to appropriate Slack channels
func sendFleetUpdatesToSlack(carsByCompany map[string][]Models.Car) error {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("Error loading .env file")
	}
	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not set")
	}

	client := Slack.NewSlackClient(slackToken)

	for company, cars := range carsByCompany {
		if len(cars) == 0 {
			continue
		}

		// Get channel ID from mapping
		channelID, exists := CompanyChannelMap[company]
		if !exists {
			log.Printf("No channel mapping found for company: %s", company)
			continue
		}

		// Generate message
		message := generateSlackMessage(cars, company)

		log.Printf("Sending %s fleet status to channel %s (%d vehicles)", company, channelID, len(cars))

		if err := client.SendAndPinWithCleanup(channelID, message); err != nil {
			log.Printf("Error sending to channel %s: %v", channelID, err)
		} else {
			log.Printf("Successfully sent %s fleet status", company)
		}
	}

	return nil
}

// GetVehicleData - enhanced version with multiple geofences
func GetVehicleData() {
	// Your existing vehicle data fetching logic
	clients, err := GetClients(username, password)
	if err != nil {
		log.Println("Login failed:", err.Error())
		return
	}

	GlobalClient = clients.Collector
	VehicleStatusListTemp = []VehicleStatusStruct{}

	err = GetCurrentLocationData(GlobalClient)
	if err != nil {
		log.Println("Failed to get current location data:", err.Error())
		return
	}

	if VehicleStatusList != nil {
		isLoaded = true
	}

	// Step 1: Get all cars and update their locations/geofences
	var allCars []Models.Car
	if err := Models.DB.Preload("Driver").Find(&allCars).Error; err != nil {
		log.Printf("Error fetching cars: %v", err)
		return
	}

	// Create map of cars by ETIT ID for faster lookup
	carsByEtitID := make(map[string]*Models.Car)
	for i := range allCars {
		carsByEtitID[allCars[i].EtitCarID] = &allCars[i]
	}

	// Step 2: Process each vehicle status and update corresponding car
	for _, vehicleStatus := range VehicleStatusList {
		car, exists := carsByEtitID[vehicleStatus.ID]
		if !exists {
			log.Printf("Car not found for ETIT ID: %s", vehicleStatus.ID)
			continue
		}

		// Parse coordinates
		lat, err := strconv.ParseFloat(vehicleStatus.Latitude, 64)
		if err != nil {
			log.Printf("Invalid latitude for car %s: %s", car.CarNoPlate, vehicleStatus.Latitude)
			continue
		}
		lng, err := strconv.ParseFloat(vehicleStatus.Longitude, 64)
		if err != nil {
			log.Printf("Invalid longitude for car %s: %s", car.CarNoPlate, vehicleStatus.Longitude)
			continue
		}

		// Update car with new location data
		car.Latitude = vehicleStatus.Latitude
		car.Longitude = vehicleStatus.Longitude
		car.LocationTimeStamp = vehicleStatus.Timestamp
		car.EngineStatus = vehicleStatus.EngineStatus
		car.Speed = vehicleStatus.Speed

		// Get address from coordinates
		address, err := getAddressFromCoords(vehicleStatus.Latitude, vehicleStatus.Longitude)
		if err != nil {
			log.Printf("Error getting address for car %s: %v", car.CarNoPlate, err)
			address = "Unknown Location"
		}
		car.Location = address

		// Update geofence based on coordinates (only if timestamp is newer)
		updated := updateCarGeofence(car, lat, lng, vehicleStatus.Timestamp)
		if !updated {
			continue // Skip saving if no update was made
		}

		// Save updated car to database
		if err := Models.DB.Save(car).Error; err != nil {
			log.Printf("Error updating car %s: %v", car.CarNoPlate, err)
		}
	}

	// Step 3: Group cars by operating company
	carsByCompany := make(map[string][]Models.Car)
	for _, car := range allCars {
		// Skip cars without operating company or recent location data
		if car.OperatingCompany == "" || car.LocationTimeStamp == "" {
			continue
		}

		// Check if location data is recent (within 24 hours)
		if car.LocationTimeStamp != "" {
			if parsedTime, err := time.Parse("2006-01-02 15:04:05", car.LocationTimeStamp); err == nil {
				if time.Since(parsedTime) > 24*time.Hour {
					continue // Skip cars with old location data
				}
			}
		}

		company := strings.ToLower(car.OperatingCompany)
		if company == "petrol_arrows" || company == "taqa" || company == "watanya" {
			carsByCompany[company] = append(carsByCompany[company], car)
		}
	}

	// Step 4: Send fleet status to Slack
	if err := sendFleetUpdatesToSlack(carsByCompany); err != nil {
		log.Printf("Error sending fleet updates to Slack: %v", err)
	}

	// Your existing speed check
	time.Sleep(time.Second * 20)
	RunSpeedCheckJob(80, true)

	log.Printf("Fleet status updated for %d companies", len(carsByCompany))
}
