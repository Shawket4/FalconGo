package Models

import (
	"Falcon/AbstractFunctions"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/360EntSecGroup-Skylar/excelize"

	// "github.com/joho/godotenv"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func Connect() {
	// if err := godotenv.Load(".env"); err != nil {
	// 	log.Fatalf("Error loading .env file")
	// }

	// DbHost := os.Getenv("DB_HOST")
	// DbUser := os.Getenv("DB_USER")
	// DbPassword := os.Getenv("DB_PASSWORD")
	// DbName := os.Getenv("DB_NAME")
	// DbPort := os.Getenv("DB_PORT")

	// dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable", DbHost, DbUser, DbPassword, DbName, DbPort)
	// connection, err := gorm.Open(postgres.Open("snap:Snapsnap@2@tcp(92.205.60.182:3306)/Falcon?parseTime=true"), &gorm.Config{})
	// connection, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	connection, err := gorm.Open(sqlite.Open("database.db"))
	DB = connection
	DB.AutoMigrate(&SpeedAlert{})
	DB.AutoMigrate(&PetroAppRecord{})
	DB.AutoMigrate(&PetroAppStation{})
	DB.AutoMigrate(
		&User{},     // Users typically have no dependencies
		&Location{}, // Base location data
		&Terminal{}, // Base terminal data
		&Driver{},   // Base driver information
		&Car{},      // Base car information
		&Tire{},     // Base tire data
		&SpeedAlert{},
		&FCMToken{},
		&LandMark{}, // No obvious dependencies shown
	)

	// 2. Then migrate models with simple foreign key relationships

	DB.AutoMigrate(
		&Truck{},        // Once tires are created
		&TirePosition{}, // Depends on Truck and Tire
		&Expense{},      // Depends on Driver
		&Loan{},         // Depends on Driver
		&Salary{},
	)

	// 3. Finally, migrate models with complex relationships or that depend on multiple other models
	DB.AutoMigrate(
		&FeeMapping{}, // Required for trips but has no dependencies itself
		&TripStruct{}, // Depends on Car, Driver, and relates to FeeMapping
		&FuelEvent{},  // Depends on Car info
		&Service{},    // Depends on Car info
		&OilChange{},  // Depends on Car info
	)

	DB.AutoMigrate(&Vendor{}, &VendorTransaction{})

	// 4. After migrations, set up any special indexes
	// var admin User
	// admin.Email = "Apex"
	// passwordByte, _ := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	// admin.Password = passwordByte
	// admin.Permission = 2
	// admin.Name = "Apex"
	// admin.IsApproved = 1
	if err != nil {
		log.Println(err)
	}
	batchSize := 50
	rateLimitDelay := 100 * time.Millisecond // Small delay between requests

	err = updateWatanyaOSRMDistancesBatch(connection, batchSize, rateLimitDelay)
	if err != nil {
		log.Printf("Error updating Watanya OSRM distances: %v", err)
	} else {
		log.Println("Successfully updated Watanya OSRM distances")
	}
	// connection.Save(&admin)
	// var location Location
	// location.Name = "جحدم"
	// connection.Save(&location)
	// var terminal Terminal
	// terminal.Name = "قنا"
	// connection.Save(&terminal)
	// DB = connection
	// if isAdmin {
	// 	connection.AutoMigrate(&AdminUser{})
	// } else {

	// }
	// SetupCars()
}

func getRouteFromOSRM(startLat, startLng, endLat, endLng float64) (map[string]interface{}, error) {
	// Replace with your OSRM server URL
	osrmURL := fmt.Sprintf("http://localhost:5000/route/v1/driving/%f,%f;%f,%f?overview=full&steps=false",
		startLng, startLat, endLng, endLat)
	fmt.Println(osrmURL)
	resp, err := http.Get(osrmURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// extractDistanceFromOSRMResponse extracts distance in kilometers from OSRM response
func extractDistanceFromOSRMResponse(response map[string]interface{}) (float64, error) {
	routes, ok := response["routes"].([]interface{})
	if !ok || len(routes) == 0 {
		return 0, fmt.Errorf("no routes found in OSRM response")
	}

	route, ok := routes[0].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid route format in OSRM response")
	}

	distance, ok := route["distance"].(float64)
	if !ok {
		return 0, fmt.Errorf("distance not found in OSRM response")
	}

	// Convert from meters to kilometers
	return distance / 1000.0, nil
}

// updateWatanyaOSRMDistancesBatch updates OSRM distances for Watanya fee mappings using batch processing
func updateWatanyaOSRMDistancesBatch(db *gorm.DB, batchSize int, rateLimitDelay time.Duration) error {
	// Count total Watanya records
	var totalCount int64
	if err := db.Model(&FeeMapping{}).Where("company = ?", "Watanya").Count(&totalCount).Error; err != nil {
		return fmt.Errorf("failed to count Watanya fee mappings: %w", err)
	}

	if totalCount == 0 {
		fmt.Println("No Watanya fee mappings found")
		return nil
	}

	fmt.Printf("Processing %d Watanya fee mappings in batches of %d\n", totalCount, batchSize)

	// Get all terminals for coordinate lookups
	var terminals []Terminal
	if err := db.Find(&terminals).Error; err != nil {
		return fmt.Errorf("failed to fetch terminals: %w", err)
	}

	// Create terminal lookup map
	terminalMap := make(map[string]Terminal)
	for _, terminal := range terminals {
		terminalMap[terminal.Name] = terminal
	}

	successCount := 0
	errorCount := 0
	offset := 0

	// Process in batches
	for offset < int(totalCount) {
		var feeMappings []FeeMapping
		if err := db.Where("company = ?", "Petrol Arrows").
			Offset(offset).
			Limit(batchSize).
			Find(&feeMappings).Error; err != nil {
			return fmt.Errorf("failed to fetch Watanya batch: %w", err)
		}

		fmt.Printf("\nProcessing Watanya batch: %d-%d of %d\n", offset+1, offset+len(feeMappings), int(totalCount))

		// Process each item in the batch
		for i, feeMapping := range feeMappings {
			fmt.Printf("  Processing %d/%d in batch: Terminal: %s -> DropOff: %s\n",
				i+1, len(feeMappings), feeMapping.Terminal, feeMapping.DropOffPoint)

			// Get terminal coordinates
			terminal, exists := terminalMap[feeMapping.Terminal]
			if !exists {
				fmt.Printf("  Warning: Terminal '%s' not found, skipping\n", feeMapping.Terminal)
				errorCount++
				continue
			}

			// Check if drop-off coordinates are available
			if feeMapping.Latitude == 0 && feeMapping.Longitude == 0 {
				fmt.Printf("  Warning: Drop-off coordinates not set for '%s', skipping\n", feeMapping.DropOffPoint)
				errorCount++
				continue
			}

			// Get route from OSRM
			response, err := getRouteFromOSRM(
				terminal.Latitude, terminal.Longitude,
				feeMapping.Latitude, feeMapping.Longitude,
			)
			if err != nil {
				fmt.Printf("  Error getting OSRM route: %v\n", err)
				errorCount++
				continue
			}

			// Extract distance from response
			distance, err := extractDistanceFromOSRMResponse(response)
			if err != nil {
				fmt.Printf("  Error extracting distance from OSRM response: %v\n", err)
				errorCount++
				continue
			}

			// Update the fee mapping with OSRM distance
			feeMapping.OSRMDistance = distance
			if err := db.Save(&feeMapping).Error; err != nil {
				fmt.Printf("  Error saving Watanya fee mapping: %v\n", err)
				errorCount++
				continue
			}

			fmt.Printf("  Updated OSRM distance: %.2f km\n", distance)
			successCount++

			// Rate limiting to avoid overwhelming OSRM server
			if rateLimitDelay > 0 {
				time.Sleep(rateLimitDelay)
			}
		}

		offset += len(feeMappings)
	}

	fmt.Printf("\nWatanya OSRM distance update completed. Success: %d, Errors: %d\n", successCount, errorCount)
	return nil
}

func CreateDefaultPositions(db *gorm.DB, truckID uint) error {
	positions := []TirePosition{
		// Steering positions (2)
		{TruckID: truckID, PositionType: "steering", PositionIndex: 1, Side: "left"},
		{TruckID: truckID, PositionType: "steering", PositionIndex: 2, Side: "right"},

		// Head axle 1 positions (4) - Properly ordered
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 4, Side: "right"},       // Outer Right

		// Head axle 2 positions (4) - Properly ordered
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 1 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 2 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 3 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 4 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 4, Side: "right"},       // Outer Right

		// Spare positions (2)
		{TruckID: truckID, PositionType: "spare", PositionIndex: 1, Side: "none"},
		{TruckID: truckID, PositionType: "spare", PositionIndex: 2, Side: "none"},
	}

	result := db.Create(&positions)
	return result.Error
}

func SetupCars() {
	var OldCars []Car
	if err := DB.Model(&Car{}).Find(&OldCars).Error; err != nil {
		panic(err)
	}
	DB.Delete(&OldCars)
	f, err := excelize.OpenFile("Book2.xlsx")
	if err != nil {
		fmt.Println(err)
		return
	}
	var Cars []Car
	_ = Cars
	rows := f.GetRows("Sheet1")
	for _, row := range rows {
		var car Car
		for columnIndex, data := range row {
			if columnIndex == 0 {
				car.CarNoPlate = data
			}
			if columnIndex == 1 {
				compartment1, err := strconv.Atoi(data)
				if err != nil {
					panic(err)
				}
				car.TankCapacity = compartment1
				car.Compartments = append(car.Compartments, compartment1)
			}
			if columnIndex == 2 {
				car.LicenseExpirationDate, _ = AbstractFunctions.GetFormattedDateExcel(data)
			}
			if columnIndex == 3 {
				car.CalibrationExpirationDate, _ = AbstractFunctions.GetFormattedDateExcel(data)
			}
			if columnIndex == 4 {
				car.TankLicenseExpirationDate, _ = AbstractFunctions.GetFormattedDateExcel(data)
			}
			if columnIndex == 5 {
				fmt.Println(data)
				if data == "1" {
					car.CarType = "Trailer"
				} else if data == "0" {
					car.CarType = "No Trailer"
				}
			}
		}
		car.Transporter = "Apex"

		jsonCompartments, err := json.Marshal(car.Compartments)
		car.JSONCompartments = jsonCompartments
		if err != nil {
			panic(err)
		}
		Cars = append(Cars, car)
	}
	if err := DB.Create(&Cars).Error; err != nil {
		panic(err)
	}
}
