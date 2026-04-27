package Controllers

import (
	"Falcon/Models"
	"fmt"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// GetViolations fetches violations with search, filter, and sort
func GetViolations(c *fiber.Ctx) error {
	// Query parameters
	search := c.Query("search", "")        // Search in location, car plate, driver name
	sortBy := c.Query("sort_by", "date")   // date, car, driver, amount, confidence
	startDate := c.Query("start_date", "") // YYYY-MM-DD
	endDate := c.Query("end_date", "")     // YYYY-MM-DD
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 50)

	// Build query
	query := Models.DB.Model(&Models.SpeedViolation{}).
		Preload("Car").
		Preload("Driver")

	// Date range filter
	if startDate != "" {
		if parsedDate, err := time.Parse("2006-01-02", startDate); err == nil {
			query = query.Where("violation_time >= ?", parsedDate)
		}
	}
	if endDate != "" {
		if parsedDate, err := time.Parse("2006-01-02", endDate); err == nil {
			// Add 1 day to include the end date
			query = query.Where("violation_time < ?", parsedDate.AddDate(0, 0, 1))
		}
	}

	// Search filter
	if search != "" {
		query = query.Where(
			Models.DB.Where("violation_location ILIKE ?", "%"+search+"%").
				Or("raw_message ILIKE ?", "%"+search+"%").
				Or("EXISTS (SELECT 1 FROM cars WHERE cars.id = speed_violations.car_id AND cars.car_no_plate ILIKE ?)", "%"+search+"%").
				Or("EXISTS (SELECT 1 FROM drivers WHERE drivers.id = speed_violations.driver_id AND drivers.name ILIKE ?)", "%"+search+"%"),
		)
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Fetch violations based on sort type
	var violations []Models.SpeedViolation

	switch sortBy {
	case "date":
		query.Order("violation_time DESC").
			Offset((page - 1) * limit).
			Limit(limit).
			Find(&violations)
		return buildDateGroupedResponse(c, violations, total, page, limit)

	case "car":
		query.Order("car_id ASC, violation_time DESC").
			Offset((page - 1) * limit).
			Limit(limit).
			Find(&violations)
		return buildCarGroupedResponse(c, violations, total, page, limit)

	case "driver":
		query.Order("driver_id ASC, violation_time DESC").
			Offset((page - 1) * limit).
			Limit(limit).
			Find(&violations)
		return buildDriverGroupedResponse(c, violations, total, page, limit)

	case "amount":
		query.Order("fine_amount DESC, violation_time DESC").
			Offset((page - 1) * limit).
			Limit(limit).
			Find(&violations)
		return buildAmountGroupedResponse(c, violations, total, page, limit)

	case "confidence":
		query.Order("driver_confidence DESC, violation_time DESC").
			Offset((page - 1) * limit).
			Limit(limit).
			Find(&violations)
		return buildConfidenceGroupedResponse(c, violations, total, page, limit)

	default:
		// Default to simple list
		query.Order("violation_time DESC").
			Offset((page - 1) * limit).
			Limit(limit).
			Find(&violations)

		return c.JSON(fiber.Map{
			"total":      total,
			"page":       page,
			"limit":      limit,
			"pages":      (total + int64(limit) - 1) / int64(limit),
			"violations": violations,
		})
	}
}

// buildDateGroupedResponse groups violations by date
// buildDateGroupedResponse groups violations by date
func buildDateGroupedResponse(c *fiber.Ctx, violations []Models.SpeedViolation, total int64, page, limit int) error {
	grouped := make(map[string][]Models.SpeedViolation)
	var dates []string

	for _, v := range violations {
		dateKey := v.ViolationTime.Format("2006-01-02")
		if _, exists := grouped[dateKey]; !exists {
			dates = append(dates, dateKey)
		}
		grouped[dateKey] = append(grouped[dateKey], v)
	}

	// Sort dates in descending order (newest first)
	sort.Slice(dates, func(i, j int) bool {
		return dates[i] > dates[j]
	})

	// Convert to array format in sorted order
	var result []fiber.Map
	for _, date := range dates {
		vios := grouped[date]
		sortViolationsByDate(vios) // ADD THIS LINE
		result = append(result, fiber.Map{
			"date":       date,
			"count":      len(vios),
			"violations": vios,
		})
	}

	return c.JSON(fiber.Map{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"pages":   (total + int64(limit) - 1) / int64(limit),
		"sort_by": "date",
		"data":    result,
	})
}

// buildCarGroupedResponse groups violations by car
func buildCarGroupedResponse(c *fiber.Ctx, violations []Models.SpeedViolation, total int64, page, limit int) error {
	grouped := make(map[string][]Models.SpeedViolation)
	carInfo := make(map[string]fiber.Map)
	var carKeys []string

	for _, v := range violations {
		carKey := fmt.Sprintf("%d", v.CarID)
		if _, exists := grouped[carKey]; !exists {
			carKeys = append(carKeys, carKey)
		}
		grouped[carKey] = append(grouped[carKey], v)

		if v.Car != nil {
			carInfo[carKey] = fiber.Map{
				"id":    v.Car.ID,
				"plate": v.Car.CarNoPlate,
			}
		}
	}

	// Sort by car plate
	sort.Slice(carKeys, func(i, j int) bool {
		plateI := ""
		plateJ := ""
		if info, ok := carInfo[carKeys[i]]; ok {
			plateI = info["plate"].(string)
		}
		if info, ok := carInfo[carKeys[j]]; ok {
			plateJ = info["plate"].(string)
		}
		return plateI < plateJ
	})

	var result []fiber.Map
	for _, carKey := range carKeys {
		vios := grouped[carKey]
		sortViolationsByDate(vios) // ADD THIS LINE
		result = append(result, fiber.Map{
			"car":         carInfo[carKey],
			"count":       len(vios),
			"total_fines": calculateTotalFines(vios),
			"violations":  vios,
		})
	}

	return c.JSON(fiber.Map{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"pages":   (total + int64(limit) - 1) / int64(limit),
		"sort_by": "car",
		"data":    result,
	})
}

// buildDriverGroupedResponse groups violations by driver
func buildDriverGroupedResponse(c *fiber.Ctx, violations []Models.SpeedViolation, total int64, page, limit int) error {
	grouped := make(map[string][]Models.SpeedViolation)
	driverInfo := make(map[string]fiber.Map)
	var driverKeys []string

	for _, v := range violations {
		var driverKey string
		if v.DriverID != nil {
			driverKey = fmt.Sprintf("%d", *v.DriverID)
		} else {
			driverKey = "unassigned"
		}

		if _, exists := grouped[driverKey]; !exists {
			driverKeys = append(driverKeys, driverKey)
		}
		grouped[driverKey] = append(grouped[driverKey], v)

		if v.Driver != nil {
			driverInfo[driverKey] = fiber.Map{
				"id":    v.Driver.ID,
				"name":  v.Driver.Name,
				"phone": v.Driver.MobileNumber,
			}
		} else {
			driverInfo[driverKey] = fiber.Map{
				"id":   nil,
				"name": "Unassigned",
			}
		}
	}

	// Sort by driver name (unassigned last)
	sort.Slice(driverKeys, func(i, j int) bool {
		if driverKeys[i] == "unassigned" {
			return false
		}
		if driverKeys[j] == "unassigned" {
			return true
		}
		nameI := ""
		nameJ := ""
		if info, ok := driverInfo[driverKeys[i]]; ok {
			nameI = info["name"].(string)
		}
		if info, ok := driverInfo[driverKeys[j]]; ok {
			nameJ = info["name"].(string)
		}
		return nameI < nameJ
	})

	var result []fiber.Map
	for _, driverKey := range driverKeys {
		vios := grouped[driverKey]
		sortViolationsByDate(vios) // ADD THIS LINE
		result = append(result, fiber.Map{
			"driver":         driverInfo[driverKey],
			"count":          len(vios),
			"total_fines":    calculateTotalFines(vios),
			"avg_confidence": calculateAvgConfidence(vios),
			"violations":     vios,
		})
	}

	return c.JSON(fiber.Map{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"pages":   (total + int64(limit) - 1) / int64(limit),
		"sort_by": "driver",
		"data":    result,
	})
}

func sortViolationsByDate(violations []Models.SpeedViolation) {
	sort.Slice(violations, func(i, j int) bool {
		return violations[i].ViolationTime.After(violations[j].ViolationTime)
	})
}

// buildAmountGroupedResponse groups violations by fine amount ranges
func buildAmountGroupedResponse(c *fiber.Ctx, violations []Models.SpeedViolation, total int64, page, limit int) error {
	ranges := []struct {
		Label string
		Min   float64
		Max   float64
	}{
		{"Under 100 EGP", 0, 100},
		{"100-300 EGP", 100, 300},
		{"300-500 EGP", 300, 500},
		{"500-1000 EGP", 500, 1000},
		{"Over 1000 EGP", 1000, 999999},
	}

	grouped := make(map[string][]Models.SpeedViolation)

	for _, v := range violations {
		for _, r := range ranges {
			if v.FineAmount >= r.Min && v.FineAmount < r.Max {
				grouped[r.Label] = append(grouped[r.Label], v)
				break
			}
		}
	}

	var result []fiber.Map
	for _, r := range ranges {
		if vios, exists := grouped[r.Label]; exists {
			sortViolationsByDate(vios) // ADD THIS LINE
			result = append(result, fiber.Map{
				"range":       r.Label,
				"min":         r.Min,
				"max":         r.Max,
				"count":       len(vios),
				"total_fines": calculateTotalFines(vios),
				"violations":  vios,
			})
		}
	}

	return c.JSON(fiber.Map{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"pages":   (total + int64(limit) - 1) / int64(limit),
		"sort_by": "amount",
		"data":    result,
	})
}

// buildConfidenceGroupedResponse groups violations by confidence ranges
func buildConfidenceGroupedResponse(c *fiber.Ctx, violations []Models.SpeedViolation, total int64, page, limit int) error {
	ranges := []struct {
		Label string
		Min   float64
		Max   float64
	}{
		{"Very High (90-100%)", 90, 100},
		{"High (75-90%)", 75, 90},
		{"Medium (50-75%)", 50, 75},
		{"Low (25-50%)", 25, 50},
		{"Very Low (0-25%)", 0, 25},
	}

	grouped := make(map[string][]Models.SpeedViolation)

	for _, v := range violations {
		for _, r := range ranges {
			if v.DriverConfidence >= r.Min && v.DriverConfidence < r.Max {
				grouped[r.Label] = append(grouped[r.Label], v)
				break
			}
		}
	}

	var result []fiber.Map
	for _, r := range ranges {
		if vios, exists := grouped[r.Label]; exists {
			sortViolationsByDate(vios) // ADD THIS LINE
			result = append(result, fiber.Map{
				"confidence_range": r.Label,
				"min":              r.Min,
				"max":              r.Max,
				"count":            len(vios),
				"violations":       vios,
			})
		}
	}

	return c.JSON(fiber.Map{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"pages":   (total + int64(limit) - 1) / int64(limit),
		"sort_by": "confidence",
		"data":    result,
	})
}

// GetViolation fetches a single violation by ID
func GetViolation(c *fiber.Ctx) error {
	id := c.Params("id")

	var violation Models.SpeedViolation

	// Preload Car and Driver relationships
	if err := Models.DB.
		Preload("Car").
		Preload("Driver").
		First(&violation, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Violation not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch violation",
			"details": err.Error(),
		})
	}

	return c.JSON(violation)
}

// UpdateViolation updates a violation
func UpdateViolation(c *fiber.Ctx) error {
	id := c.Params("id")

	var violation Models.SpeedViolation
	if err := Models.DB.First(&violation, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Violation not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch violation",
			"details": err.Error(),
		})
	}

	// Parse update data
	updates := new(Models.SpeedViolation)
	if err := c.BodyParser(updates); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
	}

	// Update allowed fields
	if updates.ViolationLocation != "" {
		violation.ViolationLocation = updates.ViolationLocation
	}
	if !updates.ViolationTime.IsZero() {
		violation.ViolationTime = updates.ViolationTime
	}
	if updates.FineAmount > 0 {
		violation.FineAmount = updates.FineAmount
	}
	if updates.DriverID != nil {
		violation.DriverID = updates.DriverID
	}
	if updates.DriverConfidence > 0 {
		violation.DriverConfidence = updates.DriverConfidence
	}

	if err := Models.DB.Save(&violation).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to update violation",
			"details": err.Error(),
		})
	}

	// Preload relationships
	Models.DB.Preload("Car").Preload("Driver").First(&violation, violation.ID)

	return c.JSON(fiber.Map{
		"message":   "Violation updated successfully",
		"violation": violation,
	})
}

// DeleteViolation deletes a violation
func DeleteViolation(c *fiber.Ctx) error {
	id := c.Params("id")

	var violation Models.SpeedViolation
	if err := Models.DB.First(&violation, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Violation not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch violation",
			"details": err.Error(),
		})
	}

	if err := Models.DB.Delete(&violation).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to delete violation",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Violation deleted successfully",
	})
}

// GetViolationStats returns statistics about violations
func GetViolationStats(c *fiber.Ctx) error {
	startDate := c.Query("start_date", "")
	endDate := c.Query("end_date", "")

	query := Models.DB.Model(&Models.SpeedViolation{})

	// Date range filter
	if startDate != "" {
		if parsedDate, err := time.Parse("2006-01-02", startDate); err == nil {
			query = query.Where("violation_time >= ?", parsedDate)
		}
	}
	if endDate != "" {
		if parsedDate, err := time.Parse("2006-01-02", endDate); err == nil {
			query = query.Where("violation_time < ?", parsedDate.AddDate(0, 0, 1))
		}
	}

	var stats struct {
		TotalViolations int64
		TotalFines      float64
		AvgFine         float64
		AvgConfidence   float64
	}

	query.Count(&stats.TotalViolations)
	query.Select("COALESCE(SUM(fine_amount), 0)").Scan(&stats.TotalFines)
	query.Select("COALESCE(AVG(fine_amount), 0)").Scan(&stats.AvgFine)
	query.Select("COALESCE(AVG(driver_confidence), 0)").Scan(&stats.AvgConfidence)

	// Top violating cars
	var topCars []struct {
		CarID      uint
		CarPlate   string
		Count      int64
		TotalFines float64
	}
	Models.DB.Model(&Models.SpeedViolation{}).
		Select("speed_violations.car_id, cars.car_no_plate as car_plate, COUNT(*) as count, SUM(fine_amount) as total_fines").
		Joins("JOIN cars ON cars.id = speed_violations.car_id").
		Group("speed_violations.car_id, cars.car_no_plate").
		Order("count DESC").
		Limit(10).
		Scan(&topCars)

	// Top violating drivers
	var topDrivers []struct {
		DriverID   *uint
		DriverName string
		Count      int64
		TotalFines float64
	}
	Models.DB.Model(&Models.SpeedViolation{}).
		Select("speed_violations.driver_id, drivers.name as driver_name, COUNT(*) as count, SUM(fine_amount) as total_fines").
		Joins("LEFT JOIN drivers ON drivers.id = speed_violations.driver_id").
		Where("speed_violations.driver_id IS NOT NULL").
		Group("speed_violations.driver_id, drivers.name").
		Order("count DESC").
		Limit(10).
		Scan(&topDrivers)

	return c.JSON(fiber.Map{
		"summary":     stats,
		"top_cars":    topCars,
		"top_drivers": topDrivers,
	})
}

// Helper functions
func calculateTotalFines(violations []Models.SpeedViolation) float64 {
	total := 0.0
	for _, v := range violations {
		total += v.FineAmount
	}
	return total
}

func calculateAvgConfidence(violations []Models.SpeedViolation) float64 {
	if len(violations) == 0 {
		return 0
	}
	total := 0.0
	for _, v := range violations {
		total += v.DriverConfidence
	}
	return total / float64(len(violations))
}
