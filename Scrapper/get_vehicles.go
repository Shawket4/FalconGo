package Scrapper

import (
	"Falcon/Models"
	"Falcon/Slack"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
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

// checkDropOffPoints checks if vehicle is at any drop-off point

// sendFleetUpdatesToSlack sends updates to appropriate Slack channels

// GetVehicleData - enhanced version with geofence-only updates and individual vehicle change detection
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

	// Track which vehicles had status changes
	var changedVehicles []string
	var statusChanges bool = false

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

		// Always update car with new location data for database
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

		// Update geofence ONLY if timestamp is newer AND vehicle has a geofence
		// This returns true only if status actually changed
		if Slack.UpdateCarGeofence(car, lat, lng, vehicleStatus.Timestamp) {
			changedVehicles = append(changedVehicles, car.CarNoPlate)
			statusChanges = true
		}

		// Save updated car to database (always save location data)
		if err := Models.DB.Save(car).Error; err != nil {
			log.Printf("Error updating car %s: %v", car.CarNoPlate, err)
		}
	}

	// Step 3: Only send Slack update if there were actual status changes
	if statusChanges {
		log.Printf("Status changes detected for vehicles: %v", changedVehicles)

		// Group cars by operating company
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

		// Send fleet status to Slack
		if err := Slack.SendFleetUpdatesToSlack(carsByCompany); err != nil {
			log.Printf("Error sending fleet updates to Slack: %v", err)
		}
	} else {
		log.Printf("No status changes detected, skipping Slack update")
	}

	// Your existing speed check
	time.Sleep(time.Second * 20)
	RunSpeedCheckJob(80, true)

	log.Printf("Fleet status updated - %d companies processed, status changes: %t", len(getAllCompanies(allCars)), statusChanges)
}

// Helper function to get unique companies
func getAllCompanies(cars []Models.Car) []string {
	companySet := make(map[string]bool)
	for _, car := range cars {
		if car.OperatingCompany != "" {
			company := strings.ToLower(car.OperatingCompany)
			if company == "petrol_arrows" || company == "taqa" || company == "watanya" {
				companySet[company] = true
			}
		}
	}

	companies := make([]string, 0, len(companySet))
	for company := range companySet {
		companies = append(companies, company)
	}
	return companies
}
