package Apis

import (
	"Falcon/Models"
	"Falcon/PetroApp"
	"fmt"
	"log"
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

	// Get method filter parameter (new)
	methodFilter := c.Query("method")

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

	// Apply method filter (new)
	if methodFilter == "PetroApp" {
		log.Printf("Filtering by payment method: %s\n", methodFilter)
		// Assuming your FuelEvent model has a payment_method or method field
		// Adjust the field name based on your actual database schema
		query = query.Where("method = ?", "PetroApp")

		// Alternative if the field is named differently:
		// query = query.Where("method = ?", "PetroApp")
		// or
		// query = query.Where("fuel_method = ?", "PetroApp")
	}
	// If methodFilter is "all" or empty, no additional filter is applied

	// Execute the query
	if err := query.Find(&FuelEvents).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Log the count for debugging
	log.Printf("Found %d fuel events with filters - method: %s\n", len(FuelEvents), methodFilter)

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
	// Parse query parameters for date range
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	now := time.Now()
	// Parse dates or default to last 30 days
	var startDate, endDate time.Time
	var err error
	if startDateStr != "" {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid start date format. Use YYYY-MM-DD",
			})
		}
	} else {
		// First day of current month at start of day
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}

	if endDateStr != "" {
		endDate, err = time.Parse("2006-01-02", endDateStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid end date format. Use YYYY-MM-DD",
			})
		}
	} else {
		// Last day of current month at end of day
		endDate = time.Date(now.Year(), now.Month()+1, 0, 23, 59, 59, 999999999, now.Location())
	}

	// Prepare query to fetch fuel events within date range
	var fuelEvents []Models.FuelEvent
	query := h.DB.Where("date BETWEEN ? AND ?", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	// Optional filtering by car or other parameters
	carID := c.Query("car_id")
	if carID != "" {
		query = query.Where("car_id = ?", carID)
	}

	err = query.Find(&fuelEvents).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve fuel events",
		})
	}

	// Group events by date and calculate daily statistics
	dailyStats := make(map[string]struct {
		Date             string  `json:"date"`
		TotalLiters      float64 `json:"total_liters"`
		TotalPrice       float64 `json:"total_price"`
		LoanCount        int     `json:"loan_count"`
		AvgPricePerLiter float64 `json:"avg_price_per_liter"`
		AvgLiters        float64 `json:"avg_liters"`
	})

	// Compute summary statistics
	var totalLiters, totalPrice float64
	var minLiters, maxLiters float64
	var minPrice, maxPrice float64
	var averageLiters, averagePrice float64

	for _, event := range fuelEvents {
		// Parse date
		eventDate, err := time.Parse("2006-01-02", event.Date)
		if err != nil {
			continue // Skip invalid dates
		}
		dateKey := eventDate.Format("2006-01-02")

		// Initialize or update daily stats
		daily, exists := dailyStats[dateKey]
		if !exists {
			daily = struct {
				Date             string  `json:"date"`
				TotalLiters      float64 `json:"total_liters"`
				TotalPrice       float64 `json:"total_price"`
				LoanCount        int     `json:"loan_count"`
				AvgPricePerLiter float64 `json:"avg_price_per_liter"`
				AvgLiters        float64 `json:"avg_liters"`
			}{
				Date: dateKey,
			}
		}

		// Update daily statistics
		daily.TotalLiters += event.Liters
		daily.TotalPrice += event.Price
		daily.LoanCount++
		daily.AvgPricePerLiter = daily.TotalPrice / daily.TotalLiters
		daily.AvgLiters = daily.TotalLiters / float64(daily.LoanCount)

		dailyStats[dateKey] = daily

		// Compute overall statistics
		totalLiters += event.Liters
		totalPrice += event.Price

		// Track min/max
		if minLiters == 0 || event.Liters < minLiters {
			minLiters = event.Liters
		}
		if event.Liters > maxLiters {
			maxLiters = event.Liters
		}

		if minPrice == 0 || event.Price < minPrice {
			minPrice = event.Price
		}
		if event.Price > maxPrice {
			maxPrice = event.Price
		}
	}

	// Compute averages
	if len(fuelEvents) > 0 {
		averageLiters = totalLiters / float64(len(fuelEvents))
		averagePrice = totalPrice / float64(len(fuelEvents))
	}

	// Convert daily stats to slice for easier frontend consumption
	dailyStatsList := make([]struct {
		Date             string  `json:"date"`
		TotalLiters      float64 `json:"total_liters"`
		TotalPrice       float64 `json:"total_price"`
		LoanCount        int     `json:"loan_count"`
		AvgPricePerLiter float64 `json:"avg_price_per_liter"`
		AvgLiters        float64 `json:"avg_liters"`
	}, 0, len(dailyStats))
	for _, stat := range dailyStats {
		dailyStatsList = append(dailyStatsList, stat)
	}

	// Sort daily stats by date
	sort.Slice(dailyStatsList, func(i, j int) bool {
		return dailyStatsList[i].Date < dailyStatsList[j].Date
	})

	// Prepare response
	response := fiber.Map{
		"daily_stats":    dailyStatsList,
		"total_liters":   totalLiters,
		"total_price":    totalPrice,
		"average_liters": averageLiters,
		"average_price":  averagePrice,
		"min_liters":     minLiters,
		"max_liters":     maxLiters,
		"min_price":      minPrice,
		"max_price":      maxPrice,
		"period_days":    len(dailyStatsList),
		"period_weeks":   float64(len(dailyStatsList)) / 7,
	}

	return c.JSON(response)
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
	inputJson.Method = "Manual"
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
	PetroApp.UpdatePetroAppOdometerFromManualFuelEvent(inputJson.CarNoPlate, inputJson.OdometerAfter)
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
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", car.ID).UpdateColumn("last_fuel_odometer", inputJson.OdometerAfter).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	PetroApp.UpdatePetroAppOdometerFromManualFuelEvent(inputJson.CarNoPlate, inputJson.OdometerAfter)

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
