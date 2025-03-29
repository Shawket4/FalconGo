package Apis

import (
	"Falcon/Models"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
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
		// Define Cairo timezone (UTC+2)
		cairoLocation, _ := time.LoadLocation("Africa/Cairo")
		if cairoLocation == nil {
			// Fallback if timezone data is not available
			cairoLocation = time.FixedZone("EET", 2*60*60) // UTC+2
		}

		// Get current time in Cairo timezone
		nowCairo := time.Now().In(cairoLocation)

		// First day of month in Cairo timezone at 00:00:00
		firstDay := time.Date(nowCairo.Year(), nowCairo.Month(), 1, 0, 0, 0, 0, cairoLocation)

		// Last day of month in Cairo timezone at 23:59:59
		lastDay := time.Date(nowCairo.Year(), nowCairo.Month()+1, 0, 23, 59, 59, 999, cairoLocation)

		// Convert to server's timezone for SQL query if needed
		// No conversion needed here since we're using the date objects directly

		// Log the date range for debugging
		log.Printf("Default date range: %s to %s\n", firstDay.Format(time.RFC3339), lastDay.Format(time.RFC3339))

		// Apply the date filter directly with the timezone-aware date objects
		query = query.Where("date BETWEEN ? AND ?", firstDay, lastDay)
	} else {
		// For manually provided dates
		if startDateStr != "" {
			// Parse the date in Cairo timezone
			cairoLocation, _ := time.LoadLocation("Africa/Cairo")
			if cairoLocation == nil {
				cairoLocation = time.FixedZone("EET", 2*60*60) // UTC+2
			}

			// Parse the date (assuming format YYYY-MM-DD)
			t, err := time.Parse("2006-01-02", startDateStr)
			if err == nil {
				// Create a new date in Cairo timezone at 00:00:00
				startDate := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, cairoLocation)
				log.Printf("Start date: %s\n", startDate.Format(time.RFC3339))
				query = query.Where("date >= ?", startDate)
			} else {
				log.Printf("Error parsing start date: %v\n", err)
			}
		}

		if endDateStr != "" {
			// Parse the date in Cairo timezone
			cairoLocation, _ := time.LoadLocation("Africa/Cairo")
			if cairoLocation == nil {
				cairoLocation = time.FixedZone("EET", 2*60*60) // UTC+2
			}

			// Parse the date (assuming format YYYY-MM-DD)
			t, err := time.Parse("2006-01-02", endDateStr)
			if err == nil {
				// Create a new date in Cairo timezone at 23:59:59
				endDate := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999, cairoLocation)
				log.Printf("End date: %s\n", endDate.Format(time.RFC3339))
				query = query.Where("date <= ?", endDate)
			} else {
				log.Printf("Error parsing end date: %v\n", err)
			}
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
