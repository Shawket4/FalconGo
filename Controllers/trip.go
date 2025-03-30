package Controllers

import (
	"Falcon/Models"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// TripHandler contains handler methods for trip routes
type TripHandler struct {
	DB *gorm.DB
}

// NewTripHandler creates a new trip handler
func NewTripHandler(db *gorm.DB) *TripHandler {
	return &TripHandler{
		DB: db,
	}
}

type TripStatistics struct {
	Company       string                  `json:"company"`
	TotalTrips    int64                   `json:"total_trips"`
	TotalVolume   float64                 `json:"total_volume"`
	TotalDistance float64                 `json:"total_distance"`
	TotalRevenue  float64                 `json:"total_revenue"`
	Details       []TripStatisticsDetails `json:"details,omitempty"`
}

// TripStatisticsDetails holds the detailed statistics for each group
type TripStatisticsDetails struct {
	GroupName     string  `json:"group_name"` // Terminal, DropOffPoint, or Fee level
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue"`
	Fee           float64 `json:"fee,omitempty"`
}

// GetTripStatistics returns aggregated trip statistics grouped by company
func (h *TripHandler) GetTripStatistics(c *fiber.Ctx) error {
	// Get filters from query parameters
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	companyFilter := c.Query("company")

	// Base query
	query := h.DB.Model(&Models.TripStruct{})

	// Apply date filters if provided
	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	}

	// Apply company filter if provided
	if companyFilter != "" {
		query = query.Where("company = ?", companyFilter)
	}

	// First, get all distinct companies
	var companies []string
	if err := query.Distinct("company").Pluck("company", &companies).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch company statistics",
			"error":   err.Error(),
		})
	}

	// Create the response array
	statistics := make([]TripStatistics, 0, len(companies))

	// For each company, calculate statistics
	for _, company := range companies {
		companyQuery := query.Where("company = ?", company)

		// Initialize the company statistics
		companyStats := TripStatistics{
			Company: company,
		}

		// Calculate totals for the company
		companyQuery.Count(&companyStats.TotalTrips)
		companyQuery.Select("COALESCE(SUM(tank_capacity), 0)").Row().Scan(&companyStats.TotalVolume)

		// Distance is a virtual field (gorm:"-"), so we need to calculate it by joining with fee_mappings
		var totalDistance float64
		h.DB.Raw(`
			SELECT COALESCE(SUM(fm.distance), 0) as total_distance
			FROM trips t
			LEFT JOIN fee_mappings fm 
				ON t.company = fm.company 
				AND t.terminal = fm.terminal 
				AND t.drop_off_point = fm.drop_off_point
			WHERE t.company = ? AND t.deleted_at IS NULL
		`, company).Row().Scan(&totalDistance)

		companyStats.TotalDistance = totalDistance

		// Handle company-specific grouping and revenue calculation
		switch company {
		case "Petrol Arrows":
			// Group by drop off location
			var dropOffStats []struct {
				DropOffPoint  string
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
				Fee           float64
			}

			// For each drop-off point, we need to join with fee_mappings to get the distance
			h.DB.Raw(`
				SELECT t.drop_off_point, COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					fm.fee
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				GROUP BY t.drop_off_point, fm.fee
			`, company).Scan(&dropOffStats)

			// Calculate details and total revenue
			companyStats.Details = make([]TripStatisticsDetails, 0, len(dropOffStats))
			companyStats.TotalRevenue = 0

			for _, stat := range dropOffStats {
				// Calculate revenue: fee * Total Volume / 1000
				revenue := stat.Fee * stat.TotalVolume / 1000

				detail := TripStatisticsDetails{
					GroupName:     stat.DropOffPoint,
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.TotalDistance,
					TotalRevenue:  revenue,
					Fee:           stat.Fee,
				}

				companyStats.Details = append(companyStats.Details, detail)
				companyStats.TotalRevenue += revenue
			}

		case "TAQA":
			// Group by terminal
			var terminalStats []struct {
				Terminal      string
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
			}

			// For each terminal, join with fee_mappings to get the distance
			h.DB.Raw(`
				SELECT t.terminal, COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				GROUP BY t.terminal
			`, company).Scan(&terminalStats)

			// Calculate details and total revenue
			companyStats.Details = make([]TripStatisticsDetails, 0, len(terminalStats))
			companyStats.TotalRevenue = 0

			for _, stat := range terminalStats {
				var ratePerKm float64
				if stat.Terminal == "Alex" {
					ratePerKm = 33.9
				} else if stat.Terminal == "Suez" {
					ratePerKm = 30.9
				} else {
					ratePerKm = 0 // Default if unknown terminal
				}

				revenue := stat.TotalDistance * ratePerKm

				detail := TripStatisticsDetails{
					GroupName:     stat.Terminal,
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.TotalDistance,
					TotalRevenue:  revenue,
					Fee:           ratePerKm,
				}

				companyStats.Details = append(companyStats.Details, detail)
				companyStats.TotalRevenue += revenue
			}

		case "Watanya":
			// Need to determine which fee category from fee mappings
			var feeStats []struct {
				Fee           float64
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
			}

			// Join with fee mappings to get fee categories
			h.DB.Raw(`
				SELECT f.fee, COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume, 
					COALESCE(SUM(f.distance), 0) as total_distance
				FROM trips t
				LEFT JOIN fee_mappings f 
					ON t.company = f.company 
					AND t.terminal = f.terminal 
					AND t.drop_off_point = f.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				GROUP BY f.fee
			`, company).Scan(&feeStats)

			// Calculate details and total revenue
			companyStats.Details = make([]TripStatisticsDetails, 0, len(feeStats))
			companyStats.TotalRevenue = 0

			for _, stat := range feeStats {
				var ratePerVolume float64

				switch int(stat.Fee) {
				case 1:
					ratePerVolume = 75
				case 2:
					ratePerVolume = 95
				case 3:
					ratePerVolume = 115
				case 4:
					ratePerVolume = 135
				case 5:
					ratePerVolume = 155
				default:
					ratePerVolume = 0 // Default if unknown fee
				}

				revenue := stat.TotalVolume * ratePerVolume

				detail := TripStatisticsDetails{
					GroupName:     "Fee " + fmt.Sprintf("%.0f", stat.Fee),
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.TotalDistance,
					TotalRevenue:  revenue,
					Fee:           stat.Fee,
				}

				companyStats.Details = append(companyStats.Details, detail)
				companyStats.TotalRevenue += revenue
			}

		default:
			// For other companies, just calculate basic totals
			// Fetch average fee from mappings
			var avgFee float64
			h.DB.Raw(`
				SELECT COALESCE(AVG(fee), 0)
				FROM fee_mappings
				WHERE company = ?
			`, company).Row().Scan(&avgFee)

			if avgFee == 0 {
				avgFee = 50 // Default if no mappings found
			}

			revenue := companyStats.TotalVolume * avgFee
			companyStats.TotalRevenue = revenue
		}

		// Add to the response array
		statistics = append(statistics, companyStats)
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip statistics retrieved successfully",
		"data":    statistics,
	})
}

// GetAllTrips returns all trips
func (h *TripHandler) GetAllTrips(c *fiber.Ctx) error {
	var trips []Models.TripStruct

	// Support pagination
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	// Get search term from query parameter
	searchTerm := c.Query("search", "")

	// Create a base query with proper sorting
	query := h.DB.Model(&Models.TripStruct{}).Order("date DESC, receipt_no DESC")

	// Add search condition if search term is provided
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%" // For LIKE query
		query = query.Where("car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	// Count total records
	var total int64
	query.Count(&total)

	// Get trips with pagination (using the same sorting)
	result := query.Limit(limit).Offset(offset).Find(&trips)
	if result.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   result.Error.Error(),
		})
	}

	// Enrich trip data with fee mapping details
	for i := range trips {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint).First(&mapping)

		// Add fee mapping data if found
		if mapping.ID > 0 {
			trips[i].Distance = mapping.Distance
			trips[i].Fee = mapping.Fee
		}
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trips retrieved successfully",
		"data":    trips,
		"meta": fiber.Map{
			"total": total,
			"page":  page,
			"limit": limit,
			"pages": (total + int64(limit) - 1) / int64(limit),
		},
	})
}

// GetTripsByCompany handles retrieving trips by company with search functionality
func (h *TripHandler) GetTripsByCompany(c *fiber.Ctx) error {
	company := c.Params("company")
	if company == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Company parameter is required",
		})
	}

	company, err := url.QueryUnescape(company)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid company name format",
			"error":   err.Error(),
		})
	}

	// Get search term from query parameter
	searchTerm := c.Query("search", "")

	var trips []Models.TripStruct

	// Support pagination
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	// Create a base query with company filter and proper sorting
	query := h.DB.Model(&Models.TripStruct{}).
		Where("company = ?", company).
		Order("date DESC, receipt_no DESC")

	// Add search condition if search term is provided
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%" // For LIKE query
		query = query.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	// Count total records for this company
	var total int64
	query.Count(&total)

	// Get trips for this company with pagination (using the same sorting)
	result := query.Limit(limit).Offset(offset).Find(&trips)

	if result.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   result.Error.Error(),
		})
	}

	// Enrich trip data with fee mapping details
	for i := range trips {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint).First(&mapping)

		// Add fee mapping data if found
		if mapping.ID > 0 {
			trips[i].Distance = mapping.Distance
			trips[i].Fee = mapping.Fee
		}
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trips retrieved successfully",
		"data":    trips,
		"meta": fiber.Map{
			"total":   total,
			"page":    page,
			"limit":   limit,
			"pages":   (total + int64(limit) - 1) / int64(limit),
			"company": company,
			"search":  searchTerm,
		},
	})
}

// GetTrip returns a specific trip by ID
func (h *TripHandler) GetTrip(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid ID",
			"error":   err.Error(),
		})
	}

	var trip Models.TripStruct
	result := h.DB.First(&trip, id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"message": "Trip not found",
			})
		}

		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trip",
			"error":   result.Error.Error(),
		})
	}

	// Enrich trip data with fee mapping details
	var mapping Models.FeeMapping
	h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
		trip.Company, trip.Terminal, trip.DropOffPoint).First(&mapping)

	if mapping.ID > 0 {
		trip.Distance = mapping.Distance
		trip.Fee = mapping.Fee
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip retrieved successfully",
		"data":    trip,
	})
}

// CreateTrip creates a new trip
func (h *TripHandler) CreateTrip(c *fiber.Ctx) error {
	trip := new(Models.TripStruct)

	if err := c.BodyParser(trip); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	// Validate required fields
	if trip.Company == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Company is required",
		})
	}

	if trip.Terminal == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Terminal is required",
		})
	}

	if trip.DropOffPoint == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Drop-off point is required",
		})
	}

	if trip.Date == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Date is required",
		})
	}

	// Verify that the company, terminal, and drop-off point exist in mappings
	var mapping Models.FeeMapping
	result := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
		trip.Company, trip.Terminal, trip.DropOffPoint).First(&mapping)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"message": "Invalid mapping: the specified company, terminal, and drop-off point combination doesn't exist",
			})
		}

		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to validate mapping",
			"error":   result.Error.Error(),
		})
	}

	// Create the trip
	result = h.DB.Create(trip)
	if result.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to create trip",
			"error":   result.Error.Error(),
		})
	}

	// Add fee mapping data
	trip.Distance = mapping.Distance
	trip.Fee = mapping.Fee

	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"message": "Trip created successfully",
		"data":    trip,
	})
}

// UpdateTrip updates an existing trip
func (h *TripHandler) UpdateTrip(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid ID",
			"error":   err.Error(),
		})
	}

	// Check if trip exists
	var existingTrip Models.TripStruct
	result := h.DB.First(&existingTrip, id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"message": "Trip not found",
			})
		}

		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch existing trip",
			"error":   result.Error.Error(),
		})
	}

	// Parse the update data
	updatedTrip := new(Models.TripStruct)
	if err := c.BodyParser(updatedTrip); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	// Check if company, terminal, or drop-off point changed
	companyChanged := updatedTrip.Company != "" && updatedTrip.Company != existingTrip.Company
	terminalChanged := updatedTrip.Terminal != "" && updatedTrip.Terminal != existingTrip.Terminal
	dropOffPointChanged := updatedTrip.DropOffPoint != "" && updatedTrip.DropOffPoint != existingTrip.DropOffPoint

	// If any mapping-related field changed, verify that the new mapping exists
	if companyChanged || terminalChanged || dropOffPointChanged {
		company := existingTrip.Company
		if companyChanged {
			company = updatedTrip.Company
		}

		terminal := existingTrip.Terminal
		if terminalChanged {
			terminal = updatedTrip.Terminal
		}

		dropOffPoint := existingTrip.DropOffPoint
		if dropOffPointChanged {
			dropOffPoint = updatedTrip.DropOffPoint
		}

		var mapping Models.FeeMapping
		result = h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			company, terminal, dropOffPoint).First(&mapping)

		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				return c.Status(http.StatusBadRequest).JSON(fiber.Map{
					"message": "Invalid mapping: the specified company, terminal, and drop-off point combination doesn't exist",
				})
			}

			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to validate mapping",
				"error":   result.Error.Error(),
			})
		}
	}

	// Update all provided fields
	// Only update non-zero/non-empty values
	if updatedTrip.CarID != 0 {
		existingTrip.CarID = updatedTrip.CarID
	}
	if updatedTrip.DriverID != 0 {
		existingTrip.DriverID = updatedTrip.DriverID
	}
	if updatedTrip.CarNoPlate != "" {
		existingTrip.CarNoPlate = updatedTrip.CarNoPlate
	}
	if updatedTrip.DriverName != "" {
		existingTrip.DriverName = updatedTrip.DriverName
	}
	if updatedTrip.Transporter != "" {
		existingTrip.Transporter = updatedTrip.Transporter
	}
	if updatedTrip.TankCapacity != 0 {
		existingTrip.TankCapacity = updatedTrip.TankCapacity
	}
	if updatedTrip.Company != "" {
		existingTrip.Company = updatedTrip.Company
	}
	if updatedTrip.Terminal != "" {
		existingTrip.Terminal = updatedTrip.Terminal
	}
	if updatedTrip.DropOffPoint != "" {
		existingTrip.DropOffPoint = updatedTrip.DropOffPoint
	}
	if updatedTrip.LocationName != "" {
		existingTrip.LocationName = updatedTrip.LocationName
	}
	if updatedTrip.Capacity != 0 {
		existingTrip.Capacity = updatedTrip.Capacity
	}
	if updatedTrip.GasType != "" {
		existingTrip.GasType = updatedTrip.GasType
	}
	if updatedTrip.Date != "" {
		existingTrip.Date = updatedTrip.Date
	}

	// For numerical fields, check explicitly to allow setting to zero
	if c.Body() != nil {
		if c.Get("X-Update-Revenue") != "" {
			existingTrip.Revenue = updatedTrip.Revenue
		} else if updatedTrip.Revenue != 0 {
			existingTrip.Revenue = updatedTrip.Revenue
		}

		if c.Get("X-Update-Mileage") != "" {
			existingTrip.Mileage = updatedTrip.Mileage
		} else if updatedTrip.Mileage != 0 {
			existingTrip.Mileage = updatedTrip.Mileage
		}
	}

	if updatedTrip.ReceiptNo != "" {
		existingTrip.ReceiptNo = updatedTrip.ReceiptNo
	}

	// Save the updated trip
	result = h.DB.Save(&existingTrip)
	if result.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update trip",
			"error":   result.Error.Error(),
		})
	}

	// Refresh fee mapping data
	var mapping Models.FeeMapping
	h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
		existingTrip.Company, existingTrip.Terminal, existingTrip.DropOffPoint).First(&mapping)

	if mapping.ID > 0 {
		existingTrip.Distance = mapping.Distance
		existingTrip.Fee = mapping.Fee
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip updated successfully",
		"data":    existingTrip,
	})
}

// DeleteTrip deletes a trip
func (h *TripHandler) DeleteTrip(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid ID",
			"error":   err.Error(),
		})
	}

	var trip Models.TripStruct
	result := h.DB.First(&trip, id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"message": "Trip not found",
			})
		}

		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trip",
			"error":   result.Error.Error(),
		})
	}

	// Perform soft delete (GORM default with DeletedAt field)
	result = h.DB.Delete(&trip)
	if result.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to delete trip",
			"error":   result.Error.Error(),
		})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip deleted successfully",
	})
}

// GetTripStats returns statistics about trips
func (h *TripHandler) GetTripStats(c *fiber.Ctx) error {
	// Optional company filter
	company := c.Query("company")

	type StatsResult struct {
		TotalTrips     int64   `json:"total_trips"`
		TotalRevenue   float64 `json:"total_revenue"`
		TotalMileage   float64 `json:"total_mileage"`
		AverageRevenue float64 `json:"average_revenue"`
		AverageMileage float64 `json:"average_mileage"`
	}

	var stats StatsResult

	// Base query
	query := h.DB.Model(&Models.TripStruct{})

	// Apply company filter if provided
	if company != "" {
		query = query.Where("company = ?", company)
	}

	// Get total trips
	query.Count(&stats.TotalTrips)

	// Get sum and average of revenue
	query.Select("COALESCE(SUM(revenue), 0) as total_revenue, COALESCE(AVG(revenue), 0) as average_revenue").
		Row().Scan(&stats.TotalRevenue, &stats.AverageRevenue)

	// Get sum and average of mileage
	query.Select("COALESCE(SUM(mileage), 0) as total_mileage, COALESCE(AVG(mileage), 0) as average_mileage").
		Row().Scan(&stats.TotalMileage, &stats.AverageMileage)

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip statistics retrieved successfully",
		"data":    stats,
		"filter": fiber.Map{
			"company": company,
		},
	})
}

// GetTripsByDate handles retrieving trips by date range with search functionality
func (h *TripHandler) GetTripsByDate(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	if startDate == "" || endDate == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Start date and end date are required",
		})
	}

	// Get search term from query parameter
	searchTerm := c.Query("search", "")

	var trips []Models.TripStruct

	// Support pagination
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	// Optional company filter
	company := c.Query("company")

	company, err := url.QueryUnescape(company)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid company name format",
			"error":   err.Error(),
		})
	}

	// Base query with model and proper sorting
	query := h.DB.Model(&Models.TripStruct{}).
		Where("date >= ? AND date <= ?", startDate, endDate).
		Order("date DESC, receipt_no DESC")

	// Apply company filter if provided
	if company != "" {
		query = query.Where("company = ?", company)
	}

	// Add search condition if search term is provided
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%" // For LIKE query
		query = query.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	// Count total records for this date range
	var total int64
	query.Count(&total)

	// Get trips for this date range with pagination (using the same query)
	result := query.Limit(limit).Offset(offset).Find(&trips)

	if result.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   result.Error.Error(),
		})
	}

	// Enrich trip data with fee mapping details
	for i := range trips {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint).First(&mapping)

		// Add fee mapping data if found
		if mapping.ID > 0 {
			trips[i].Distance = mapping.Distance
			trips[i].Fee = mapping.Fee
		}
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trips retrieved successfully",
		"data":    trips,
		"meta": fiber.Map{
			"total":      total,
			"page":       page,
			"limit":      limit,
			"pages":      (total + int64(limit) - 1) / int64(limit),
			"start_date": startDate,
			"end_date":   endDate,
			"company":    company,
			"search":     searchTerm,
		},
	})
}
