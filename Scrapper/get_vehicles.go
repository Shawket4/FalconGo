package Scrapper

import (
	"Falcon/Models"
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

// GetVehicleData fetches vehicle data using authenticated client
func GetVehicleData() {
	// Get authenticated clients
	clients, err := GetClients(username, password)
	if err != nil {
		log.Println("Login failed:", err.Error())
		return
	}

	// Set the GlobalClient for use in this and other functions
	GlobalClient = clients.Collector

	// Initialize or reset the temporary vehicle status list
	VehicleStatusListTemp = []VehicleStatusStruct{}

	// Get current location data using the authenticated client
	err = GetCurrentLocationData(GlobalClient)
	if err != nil {
		log.Println("Failed to get current location data:", err.Error())
		return
	}

	// Set isLoaded flag if we have vehicle status data
	if VehicleStatusList != nil {
		isLoaded = true
	}
	for _, vehicle := range VehicleStatusList {
		address, err := getAddressFromCoords(vehicle.Latitude, vehicle.Longitude)
		if err != nil {
			log.Println(err)
		}
		if err := Models.DB.Model(&Models.Car{}).Where("etit_car_id = ?", vehicle.ID).Updates(&Models.Car{Latitude: vehicle.Latitude, Longitude: vehicle.Longitude, LocationTimeStamp: vehicle.Timestamp, EngineStatus: vehicle.EngineStatus, Location: address, Speed: vehicle.Speed}).Error; err != nil {
			log.Println(err)
		}
	}
	// Wait for data to be fully processed
	time.Sleep(time.Second * 20)
	RunSpeedCheckJob(80, true)
	// Print the vehicle status list
}

// GetAllVehicleData returns the current vehicle status list
func GetAllVehicleData() []VehicleStatusStruct {
	// If the data isn't loaded yet, get it
	if !isLoaded {
		GetVehicleData()
	}
	return VehicleStatusList
}
