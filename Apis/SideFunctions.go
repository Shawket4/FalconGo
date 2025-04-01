package Apis

import (
	"Falcon/Models"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func sortFuelByDate(events []Models.FuelEvent) []Models.FuelEvent {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	// Parse date using multiple layouts
	parseDate := func(dateStr string) (time.Time, error) {
		var t time.Time
		var err error
		for _, layout := range layouts {
			t, err = time.Parse(layout, dateStr)
			if err == nil {
				return t, nil
			}
		}
		return t, err
	}

	sort.Slice(events, func(i, j int) bool {
		dateI, errI := parseDate(events[i].Date)
		dateJ, errJ := parseDate(events[j].Date)
		if errI != nil || errJ != nil {
			// Handle error (for simplicity, we can consider them equal if parsing fails)
			return false
		}
		return dateI.Before(dateJ)
	})
	return events
}

func GetFuelEventById(c *fiber.Ctx) error {
	id := c.Params("id") // Get the ID from the URL parameter

	var fuelEvent Models.FuelEvent

	// Find the fuel event by ID
	if err := Models.DB.First(&fuelEvent, id).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Fuel event not found",
		})
	}

	// If you need permission checks like in your commented code:
	/*
		if Controllers.CurrentUser.Permission != 4 && fuelEvent.Transporter != Controllers.CurrentUser.Name {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "You don't have permission to view this fuel event",
			})
		}
	*/

	return c.JSON(fuelEvent)
}

func GetFuelEvents(c *fiber.Ctx) error {
	var FuelEvents []Models.FuelEvent

	// Get query parameters for date filtering
	startDateStr := c.Query("startDate")
	endDateStr := c.Query("endDate")

	// Build the query
	query := Models.DB

	// If no dates provided, default to current month
	if startDateStr == "" && endDateStr == "" {
		// Get current time
		now := time.Now()

		// Format first day and last day of current month for SQL (YYYY-MM-DD format)
		firstDayStr := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
		lastDayStr := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Format("2006-01-02")

		// Use string comparison for dates - this often works better with SQL databases
		// DATE(date) extracts just the date part, ignoring the time component
		log.Printf("Default date range: %s to %s\n", firstDayStr, lastDayStr)
		query = query.Where("DATE(date) >= ? AND DATE(date) <= ?", firstDayStr, lastDayStr)
	} else {
		// For manually provided dates - use string comparison approach
		if startDateStr != "" {
			log.Printf("Manual start date: %s\n", startDateStr)
			query = query.Where("DATE(date) >= ?", startDateStr)
		}

		if endDateStr != "" {
			log.Printf("Manual end date: %s\n", endDateStr)
			query = query.Where("DATE(date) <= ?", endDateStr)
		}
	}

	// Execute the query
	if err := query.Find(&FuelEvents).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Sort the results
	FuelEvents = sortFuelByDate(FuelEvents)

	return c.JSON(FuelEvents)
}

// FuelStatistics represents fuel consumption statistics grouped by car
type FuelStatistics struct {
	CarNoPlate       string             `json:"car_no_plate"`
	TotalLiters      float64            `json:"total_liters"`
	TotalDistance    float64            `json:"total_distance"`
	TotalCost        float64            `json:"total_cost"`
	AvgConsumption   float64            `json:"avg_consumption"` // Liters per 100km
	MinConsumption   float64            `json:"min_consumption"` // Min liters per 100km for any refill
	MaxConsumption   float64            `json:"max_consumption"` // Max liters per 100km for any refill
	AvgPricePerLiter float64            `json:"avg_price_per_liter"`
	EventCount       int64              `json:"event_count"`
	DistinctDrivers  int64              `json:"distinct_drivers"`
	Events           []Models.FuelEvent `json:"events,omitempty"`
}

// FuelSummary represents overall fuel statistics
type FuelSummary struct {
	TotalLiters      float64 `json:"total_liters"`
	TotalDistance    float64 `json:"total_distance"`
	TotalCost        float64 `json:"total_cost"`
	AvgConsumption   float64 `json:"avg_consumption"`     // Overall average liters per 100km
	AvgPricePerLiter float64 `json:"avg_price_per_liter"` // Overall average price per liter
	TotalEvents      int64   `json:"total_events"`        // Total number of fuel events
	DistinctCars     int64   `json:"distinct_cars"`       // Number of distinct cars
	DistinctDrivers  int64   `json:"distinct_drivers"`    // Number of distinct drivers
	DistinctDates    int64   `json:"distinct_dates"`      // Number of distinct dates
}

// FuelHandler handles fuel-related requests
type FuelHandler struct {
	DB *gorm.DB
}

// GetFuelStatistics returns detailed fuel consumption statistics
func (h *FuelHandler) GetFuelStatistics(c *fiber.Ctx) error {
	// Get filters from query parameters
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	carFilter := c.Query("car_no_plate")
	driverFilter := c.Query("driver_name")
	transporterFilter := c.Query("transporter")

	// Base query
	query := h.DB.Model(&Models.FuelEvent{})

	// Apply date filters if provided
	if startDate != "" && endDate != "" {
		// Parse dates to ensure proper format
		startTime, errStart := time.Parse("2006-01-02", startDate)
		endTime, errEnd := time.Parse("2006-01-02", endDate)

		if errStart == nil && errEnd == nil {
			// Format back to string to ensure consistent format
			formattedStart := startTime.Format("2006-01-02")
			formattedEnd := endTime.Format("2006-01-02")

			query = query.Where("date >= ? AND date <= ?", formattedStart, formattedEnd)
		}
	}

	// Apply other filters if provided
	if carFilter != "" {
		query = query.Where("car_no_plate = ?", carFilter)
	}
	if driverFilter != "" {
		query = query.Where("driver_name LIKE ?", "%"+driverFilter+"%")
	}
	if transporterFilter != "" {
		query = query.Where("transporter LIKE ?", "%"+transporterFilter+"%")
	}

	// First, get all distinct cars
	var cars []string
	if err := query.Distinct("car_no_plate").Pluck("car_no_plate", &cars).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch fuel statistics",
			"error":   err.Error(),
		})
	}

	// Create response array for car statistics
	statistics := make([]FuelStatistics, 0, len(cars))

	// Initialize summary statistics
	summary := FuelSummary{}

	// For each car, calculate statistics
	for _, car := range cars {
		// Create a new query specific to this car
		carQuery := query.Where("car_no_plate = ?", car)

		// Initialize the car statistics
		carStats := FuelStatistics{
			CarNoPlate:     car,
			MinConsumption: -1, // Initialize to impossible value to detect if it's been set
		}

		// Count events
		var eventCount int64
		carQuery.Count(&eventCount)
		carStats.EventCount = eventCount

		// Get total liters
		carQuery.Select("COALESCE(SUM(liters), 0)").Row().Scan(&carStats.TotalLiters)

		// Get total cost
		carQuery.Select("COALESCE(SUM(price), 0)").Row().Scan(&carStats.TotalCost)

		// Get average price per liter
		if carStats.TotalLiters > 0 {
			carStats.AvgPricePerLiter = carStats.TotalCost / carStats.TotalLiters
		}

		// Count distinct drivers
		carQuery.Distinct("driver_name").Count(&carStats.DistinctDrivers)

		// Get all events for this car to calculate distances and consumption
		var events []Models.FuelEvent
		if err := carQuery.Order("date, id").Find(&events).Error; err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to fetch fuel events",
				"error":   err.Error(),
			})
		}

		// Calculate total distance and consumption statistics
		carStats.TotalDistance = 0
		var validConsumptionEvents int = 0

		for i, event := range events {
			// Calculate distance for this event
			distance := 0.0
			if event.OdometerAfter > 0 && event.OdometerBefore > 0 {
				// Normal case: use odometer readings from this event
				distance = float64(event.OdometerAfter - event.OdometerBefore)
			} else if i > 0 && events[i-1].OdometerAfter > 0 && event.OdometerBefore > 0 {
				// Alternative: use previous event's odometer_after as the before value
				distance = float64(event.OdometerBefore - events[i-1].OdometerAfter)
			}

			// Only add positive distances
			if distance > 0 {
				carStats.TotalDistance += distance
			}

			// Calculate consumption for this event (liters per 100km)
			if distance > 0 && event.Liters > 0 {
				consumption := (event.Liters / distance) * 100

				// Update min/max consumption
				if carStats.MinConsumption < 0 || consumption < carStats.MinConsumption {
					carStats.MinConsumption = consumption
				}
				if consumption > carStats.MaxConsumption {
					carStats.MaxConsumption = consumption
				}

				validConsumptionEvents++
			}
		}

		// If no valid consumption events were found, set min to 0
		if carStats.MinConsumption < 0 {
			carStats.MinConsumption = 0
		}

		// Calculate average consumption for car
		if carStats.TotalDistance > 0 {
			carStats.AvgConsumption = (carStats.TotalLiters / carStats.TotalDistance) * 100
		}

		// Add events to car statistics
		carStats.Events = events

		// Add to response array
		statistics = append(statistics, carStats)

		// Update summary statistics
		summary.TotalLiters += carStats.TotalLiters
		summary.TotalDistance += carStats.TotalDistance
		summary.TotalCost += carStats.TotalCost
		summary.TotalEvents += carStats.EventCount
		summary.DistinctDrivers += carStats.DistinctDrivers // Note: This might count some drivers multiple times if they drive different cars
	}

	// Calculate overall summary statistics
	if summary.TotalDistance > 0 {
		summary.AvgConsumption = (summary.TotalLiters / summary.TotalDistance) * 100
	}
	if summary.TotalLiters > 0 {
		summary.AvgPricePerLiter = summary.TotalCost / summary.TotalLiters
	}

	// Count distinct cars, drivers, and dates
	h.DB.Model(&Models.FuelEvent{}).Where(query.Statement.Clauses).Distinct("car_no_plate").Count(&summary.DistinctCars)
	h.DB.Model(&Models.FuelEvent{}).Where(query.Statement.Clauses).Distinct("driver_name").Count(&summary.DistinctDrivers)
	h.DB.Model(&Models.FuelEvent{}).Where(query.Statement.Clauses).Distinct("date").Count(&summary.DistinctDates)

	// Return response
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Fuel statistics retrieved successfully",
		"data":    statistics,
		"summary": summary,
	})
}

func AddFuelEvent(c *fiber.Ctx) error {
	var inputJson Models.FuelEvent
	if err := c.BodyParser(&inputJson); err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", inputJson.CarID).Find(&car).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	fmt.Println(car)
	inputJson.CarNoPlate = car.CarNoPlate
	inputJson.Transporter = "Apex"
	inputJson.Price = inputJson.PricePerLiter * inputJson.Liters
	fmt.Println(inputJson)
	inputJson.FuelRate = float64(inputJson.OdometerAfter-inputJson.OdometerBefore) / inputJson.Liters
	if err := Models.DB.Create(&inputJson).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", car.ID).UpdateColumn("last_fuel_odometer", inputJson.OdometerAfter).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.JSON(inputJson)
}

func EditFuelEvent(c *fiber.Ctx) error {
	var inputJson Models.FuelEvent
	if err := c.BodyParser(&inputJson); err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", inputJson.CarID).Find(&car).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	inputJson.CarNoPlate = car.CarNoPlate

	inputJson.Price = inputJson.PricePerLiter * inputJson.Liters
	inputJson.FuelRate = float64(inputJson.OdometerAfter-inputJson.OdometerBefore) / inputJson.Liters
	var fuelEvent Models.FuelEvent
	if err := Models.DB.Model(&Models.FuelEvent{}).Where("id = ?", inputJson.ID).Find(&fuelEvent).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	inputJson.CreatedAt = fuelEvent.CreatedAt

	if err := Models.DB.Save(&inputJson).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(inputJson)
}

func DeleteFuelEvent(c *fiber.Ctx) error {
	var inputJson struct {
		ID uint `json:"ID"`
	}
	if err := c.BodyParser(&inputJson); err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	var fuelEvent Models.FuelEvent
	if err := Models.DB.Model(&Models.FuelEvent{}).Where("id = ?", inputJson.ID).Find(&fuelEvent).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	// _, err := fuelEvent.Delete()
	if err := Models.DB.Delete(&Models.FuelEvent{}, fuelEvent).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	return c.JSON(fiber.Map{
		"message": "Fuel Event Deleted Successfully",
	})
}
