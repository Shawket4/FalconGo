package Scrapper

import (
	"Falcon/Models"
	"Falcon/Structs"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	// "time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
	"github.com/gofiber/fiber/v2"
)

type App struct {
	Client *http.Client
}

type Project struct {
	Name string
}

func (app *App) getToken() AuthenticityToken {
	loginURL := baseURL
	// client := app.Client
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := &http.Client{Transport: customTransport}
	response, err := client.Get(loginURL)

	if err != nil {
		log.Fatalln("Error fetching response. ", err)
	}

	defer response.Body.Close()

	document, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		log.Fatal("Error loading HTTP response body. ", err)
	}

	token, _ := document.Find("input[name='__VIEWSTATE']").Attr("value")

	authenticityToken := AuthenticityToken{
		Token: token,
	}

	return authenticityToken
}

type RouteData struct {
	Coordinates []Structs.Coordinate `json:"coordinates"`
	Stops       []Structs.Stop       `json:"stops"`
}

func (app *App) GetVehicleHistoryData(VehicleID string, from, to string) (*RouteData, error) {
	// Create the URL
	url := fmt.Sprintf(
		"https://fms-gps.etit-eg.com/WebPages/GetAllHistoryData.aspx?id=%s&time=6&from=%s&to=%s",
		VehicleID,
		from,
		to,
	)
	fmt.Println(url)

	// Get authenticated clients
	clients, err := GetClients(username, password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	log.Printf("Fetching history data from: %s", url)

	// Visit the domain first to ensure cookies are set properly
	preReq, err := clients.HttpClient.Get("https://fms-gps.etit-eg.com/WebPages/maps.aspx")
	if err != nil {
		return nil, fmt.Errorf("error establishing GPS session: %w", err)
	}
	preReq.Body.Close()

	// Make the actual request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	// Add headers to match browser behavior
	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Add("Accept", "application/json, text/plain, */*")
	req.Header.Add("Accept-Language", "en-US,en;q=0.9")
	req.Header.Add("Referer", "https://fms-gps.etit-eg.com/WebPages/Maps.aspx")
	req.Header.Add("Content-Type", "text/html; charset=utf-8")

	// Make the request with authenticated client
	resp, err := clients.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching history data: %w", err)
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

	jsonString := string(body)
	fmt.Println("Raw response:", jsonString)

	// Check if response is HTML instead of JSON (error case)
	if strings.Contains(jsonString, "<!DOCTYPE HTML") || strings.Contains(jsonString, "<html") {
		return nil, fmt.Errorf("received HTML instead of JSON, authentication may have failed")
	}

	// Check for empty or invalid responses
	if len(strings.TrimSpace(jsonString)) == 0 {
		return nil, fmt.Errorf("received empty response")
	}

	// Fix the malformed JSON by adding quotes to property names
	fixedJSON := fixMalformedJSON(jsonString)
	fmt.Println("Fixed JSON:", fixedJSON)

	// Parse the fixed JSON
	var historyData Structs.TimeLineStruct
	err = json.Unmarshal([]byte(fixedJSON), &historyData)
	if err != nil {
		return nil, fmt.Errorf("error parsing history data: %w, fixed JSON: %s", err, fixedJSON[:min(500, len(fixedJSON))])
	}

	// Convert to coordinate slice with validation
	var coordinates []Structs.Coordinate
	for i, historyPoint := range historyData.History {
		// Validate that we have point data
		if len(historyPoint.P) == 0 {
			log.Printf("Warning: history point %d has no coordinate data", i)
			continue
		}

		// Validate coordinate data
		latStr := historyPoint.P[0].A
		lonStr := historyPoint.P[0].O

		// Validate that coordinates are not empty
		if latStr == "" || lonStr == "" {
			log.Printf("Warning: history point %d has empty coordinates", i)
			continue
		}

		// Validate that coordinates are valid numbers
		if lat, err := strconv.ParseFloat(latStr, 64); err != nil {
			log.Printf("Warning: invalid latitude '%s' at point %d: %v", latStr, i, err)
			continue
		} else if lat < -90 || lat > 90 {
			log.Printf("Warning: latitude '%s' out of range at point %d", latStr, i)
			continue
		}

		if lon, err := strconv.ParseFloat(lonStr, 64); err != nil {
			log.Printf("Warning: invalid longitude '%s' at point %d: %v", lonStr, i, err)
			continue
		} else if lon < -180 || lon > 180 {
			log.Printf("Warning: longitude '%s' out of range at point %d", lonStr, i)
			continue
		}

		// Validate datetime
		if historyPoint.D == "" {
			log.Printf("Warning: history point %d has empty datetime", i)
			continue
		}

		// Create coordinate
		coordinate := Structs.Coordinate{
			Latitude:  latStr,
			Longitude: lonStr,
			DateTime:  historyPoint.D,
		}
		coordinates = append(coordinates, coordinate)
	}

	// Parse stops with validation
	var stops []Structs.Stop
	for i, stopPoint := range historyData.Stops {
		// Validate stop data
		if stopPoint.Lon == "" || stopPoint.Lat == "" {
			log.Printf("Warning: stop %d has empty coordinates", i)
			continue
		}

		// Validate that coordinates are valid numbers
		if lat, err := strconv.ParseFloat(stopPoint.Lat, 64); err != nil {
			log.Printf("Warning: invalid stop latitude '%s' at stop %d: %v", stopPoint.Lat, i, err)
			continue
		} else if lat < -90 || lat > 90 {
			log.Printf("Warning: stop latitude '%s' out of range at stop %d", stopPoint.Lat, i)
			continue
		}

		if lon, err := strconv.ParseFloat(stopPoint.Lon, 64); err != nil {
			log.Printf("Warning: invalid stop longitude '%s' at stop %d: %v", stopPoint.Lon, i, err)
			continue
		} else if lon < -180 || lon > 180 {
			log.Printf("Warning: stop longitude '%s' out of range at stop %d", stopPoint.Lon, i)
			continue
		}

		// Validate datetime fields
		if stopPoint.From == "" || stopPoint.To == "" {
			log.Printf("Warning: stop %d has empty from/to datetime", i)
			continue
		}

		// Create stop
		stop := Structs.Stop{
			Longitude: stopPoint.Lon,
			Latitude:  stopPoint.Lat,
			ID:        stopPoint.ID,
			From:      stopPoint.From,
			To:        stopPoint.To,
			Duration:  stopPoint.Duration,
			Address:   stopPoint.Address,
		}
		stops = append(stops, stop)
	}

	if len(coordinates) == 0 {
		return nil, fmt.Errorf("no valid coordinates found in response")
	}

	log.Printf("Successfully parsed %d coordinates and %d stops", len(coordinates), len(stops))

	return &RouteData{
		Coordinates: coordinates,
		Stops:       stops,
	}, nil
}

// fixMalformedJSON fixes the malformed JSON from the ETIT API
func fixMalformedJSON(jsonStr string) string {
	// Remove any BOM or invisible characters
	jsonStr = strings.TrimPrefix(jsonStr, "\ufeff")
	jsonStr = strings.TrimSpace(jsonStr)

	// Define the property names that need quotes
	propertyNames := []string{
		"history",
		"DisconnectedPoints",
		"stops",
		"Fuel",
		"Sensors",
		"HistoryWO",
		// Coordinate properties
		"p", "d", "s", "l", "f", "rpm", "a", "o",
		// Stop properties
		"lon", "lat", "id", "from", "to", "duration", "address",
		// Sensor properties
		"strtDate", "endDate", "SensorID", "typeName",
	}

	// Fix unquoted property names by adding quotes
	for _, prop := range propertyNames {
		// Pattern: property_name: (with possible whitespace)
		pattern := fmt.Sprintf(`\b%s\s*:`, prop)
		replacement := fmt.Sprintf(`"%s":`, prop)
		re := regexp.MustCompile(pattern)
		jsonStr = re.ReplaceAllString(jsonStr, replacement)
	}

	// Additional fixes for common malformed JSON patterns

	// Fix trailing commas before closing brackets/braces
	re := regexp.MustCompile(`,\s*([}\]])`)
	jsonStr = re.ReplaceAllString(jsonStr, "$1")

	// Fix missing commas between array elements (if any)
	re = regexp.MustCompile(`}\s*{`)
	jsonStr = re.ReplaceAllString(jsonStr, "},{")

	// Fix missing commas between object properties (more specific)
	re = regexp.MustCompile(`"\s*\n\s*"`)
	jsonStr = re.ReplaceAllString(jsonStr, "\",\n\"")

	return jsonStr
}

func GetVehicleRouteByDate(c *fiber.Ctx) error {
	// Get query parameters
	carID := c.Query("car_id")
	from := c.Query("from")
	to := c.Query("to")

	// Validate required parameters
	if carID == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "car_id is required",
		})
	}

	if from == "" || to == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "from and to date parameters are required",
		})
	}

	// Find the car by ID and get etit_car_id
	var car Models.Car
	if err := Models.DB.First(&car, carID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": "Car not found",
		})
	}

	// Check if etit_car_id exists
	if car.EtitCarID == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "Car does not have etit_car_id",
		})
	}

	// Get vehicle history data using the updated function
	routeData, err := app.GetVehicleHistoryData(car.EtitCarID, from, to)

	if err != nil {
		log.Println(err)
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Return the route data with both coordinates and stops
	return c.JSON(fiber.Map{
		"success":      true,
		"car_id":       carID,
		"etit_car_id":  car.EtitCarID,
		"from":         from,
		"to":           to,
		"coordinates":  routeData.Coordinates,
		"stops":        routeData.Stops,
		"total_points": len(routeData.Coordinates),
		"total_stops":  len(routeData.Stops),
	})
}

func (app *App) Login() (*colly.Collector, error) {
	authenticityToken := app.getToken()
	client := colly.NewCollector()
	client.WithTransport(&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}})
	// http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	loginURL := baseURL + "/"
	data := map[string]string{
		"ScriptManager1":          "UpdatePanel1|lg_AltairLogin$LoginButton",
		"__EVENTTARGET":           "lg_AltairLogin$LoginButton",
		"__VIEWSTATE":             authenticityToken.Token,
		"__VIEWSTATEGENERATOR":    "0C2F32F0",
		"lg_AltairLogin$UserName": username,
		"lg_AltairLogin$Password": password,
	}

	if err := client.Post(loginURL, data); err != nil {
		return nil, err
	}
	fmt.Println("Logged In.")
	return client, nil
}

func (app *App) GetCurrentLocationData(client *colly.Collector) error {
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
		// fmt.Println(Data.Data.Rows[0])
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

var jar, _ = cookiejar.New(nil)

var app = App{
	Client: &http.Client{Jar: jar},
}

// func GetVehicleHistoryData() {
// 	if isLoaded {
// 		for _, s := range VehicleStatusList {
// 			client := app.Login()
// 			fmt.Println(s.ID)
// 			app.GetVehicleHistoryData(s.ID, client)
// 			time.Sleep(time.Second * 20)
// 			//fmt.Printf("%s Cooridinates %v", s.ID, AllCoordinates[s.ID][0:5])
// 		}
// 	}
// }

type MileageStruct struct {
	VehiclePlateNo string `json:"VehiclePlateNo"`
	StartTime      string `json:"StartTime"`
	EndTime        string `json:"EndTime"`
	VehicleID      string
}

// func CalculateDistanceWorker() {
// 	var Trips []Models.TripStruct
// 	if err := Models.DB.Model(&Models.TripStruct{}).Where("is_closed = ?", true).Where("mileage = 0").Find(&Trips).Error; err != nil {
// 		log.Println(err.Error())
// 	}
// 	for _, trip := range Trips {
// 		var truckID string
// 		for _, vehicle := range VehicleStatusList {
// 			if vehicle.PlateNo == trip.CarNoPlate {
// 				truckID = vehicle.ID
// 			}
// 		}
// 		feeRate, mileage, err := GetFeeRate(MileageStruct{VehiclePlateNo: trip.CarNoPlate, StartTime: trip.StartTime, EndTime: trip.EndTime, VehicleID: truckID})
// 		if err != nil {
// 			log.Println(err.Error())
// 		}
// 		trip.FeeRate = feeRate
// 		trip.Route.Mileage = mileage
// 		if err := Models.DB.Save(&trip).Error; err != nil {
// 			log.Println(err.Error())
// 		}
// 		time.Sleep(time.Second * 10)
// 	}
// }

func GetVehicleMileageHistory(c *fiber.Ctx) error {
	if err := app.GetCurrentLocationData(GlobalClient); err != nil {
		var loginErr error
		GlobalClient, loginErr = app.Login()
		if loginErr != nil {
			log.Println(err.Error())
			return err
		}
		app.GetCurrentLocationData(GlobalClient)
	}
	var data MileageStruct
	err := c.BodyParser(&data)
	if err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	fmt.Println(len(VehicleStatusList))
	fmt.Println(data.VehiclePlateNo)
	for _, s := range VehicleStatusList {
		if s.PlateNo == data.VehiclePlateNo {
			data.VehicleID = s.ID
		}
	}
	feeRate, mileage, err := GetFeeRate(data)
	if err != nil {
		log.Println(err.Error())
		return err
	}
	return c.JSON(fiber.Map{
		"Fee":     feeRate,
		"mileage": mileage,
	})
}

type Jar struct {
	lk      sync.Mutex
	cookies map[string][]*http.Cookie
}

func NewJar() *Jar {
	jar := new(Jar)
	jar.cookies = make(map[string][]*http.Cookie)
	return jar
}

func (jar *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	jar.lk.Lock()
	jar.cookies[u.Host] = cookies
	jar.lk.Unlock()
}

func (jar *Jar) Cookies(u *url.URL) []*http.Cookie {
	return jar.cookies[u.Host]
}

func trimLeftChars(s string, n int) string {
	m := 0
	for i := range s {
		if m >= n {
			return s[i:]
		}
		m++
	}
	return s[:0]
}

func GetFeeRate(data MileageStruct) (float64, float64, error) {
	GlobalClient, _ = app.Login()
	app.GetCurrentLocationData(GlobalClient)
	// reqString := fmt.Sprintf("https://fms-gps.etit-eg.com/WebPages/GetHistoryTripSummary.ashx?id=%s&time=6&from=%s&to=%s", data.VehicleID, "11/1/2022%2000:00:00", "11/1/2022%2023:59:59")
	reqString := fmt.Sprintf("https://fms-gps.etit-eg.com/WebPages/GetHistoryTripSummary.ashx?id=%s&time=6&from=%s&to=%s", data.VehicleID, data.StartTime, data.EndTime)
	GlobalClient.Request("GET", "https://fms-gps.etit-eg.com", nil, nil, http.Header{})
	cookies := GlobalClient.Cookies("https://fms-gps.etit-eg.com")
	req, _ := http.NewRequest("GET", reqString, nil)
	req.Header.Set("Cookie", fmt.Sprintf("%s;", cookies[4]))
	res, err := app.Client.Do(req)
	if err != nil {
		log.Println(err.Error())
		return 0, 0, err
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		log.Println(err.Error())
		return 0, 0, err
	}
	jsonData, err := json.Marshal(fmt.Sprintf("%s", buf))
	if err != nil {
		log.Println(err.Error())
		return 0, 0, err
	}
	var jsonString string
	err = json.Unmarshal(jsonData, &jsonString)
	if err != nil {
		log.Println(err.Error())
		return 0, 0, err
	}
	jsonString = trimLeftChars(jsonString, 13)
	stringLen := len(jsonString)
	fmt.Println(stringLen)
	if len(jsonString) > 0 {
		jsonString = jsonString[:len(jsonString)-5]
	} else {
		return 0, 0, err
	}

	// jsonString = strings.Trim(jsonString, ", ")
	jsonString = jsonString + "\n}"
	fmt.Println(jsonString)
	// fmt.Println(jsonString)
	var unMarshalledData struct {
		TotalMilage string `json:"TotalMileage"`
	}
	err = json.Unmarshal([]byte(jsonString), &unMarshalledData)
	if err != nil {
		log.Println(err.Error())
		return 0, 0, err
	}

	fmt.Println(unMarshalledData.TotalMilage)
	mileage, err := strconv.ParseFloat(unMarshalledData.TotalMilage, 64)
	if err != nil {
		log.Println(err.Error())
	}
	if mileage == 0 {
		GlobalClient, err = app.Login()
		if err != nil {
			return 0, 0, err
		}
		return GetFeeRate(data)
	}
	feeRate := GetFeeFromMilage(mileage)

	return feeRate, mileage, nil
}

func GetFeeFromMilage(mileage float64) float64 {
	if mileage > 0 {
		if mileage <= 100 {
			return 76
		} else if mileage <= 150 {
			return 91
		} else if mileage <= 200 {
			return 107
		} else if mileage <= 250 {
			return 122
		} else if mileage <= 300 {
			return 138
		} else if mileage <= 350 {
			return 154
		} else if mileage <= 400 {
			return 169
		} else if mileage <= 450 {
			return 185
		} else if mileage <= 500 {
			return 200
		} else if mileage <= 550 {
			return 216
		} else if mileage <= 600 {
			return 268
		} else if mileage <= 650 {
			return 283
		} else if mileage <= 700 {
			return 299
		} else if mileage <= 750 {
			return 350
		} else if mileage <= 800 {
			return 366
		} else if mileage <= 850 {
			return 418
		} else if mileage <= 900 {
			return 433
		} else {
			return 485
		}
	}
	return 0
}
