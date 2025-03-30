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

type TripStatisticsDetails struct {
	GroupName     string  `json:"group_name"` // Terminal, DropOffPoint, or Fee level
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue"`
	CarRental     float64 `json:"car_rental,omitempty"` // Only for TAQA
	VAT           float64 `json:"vat,omitempty"`        // For Watanya and TAQA
	TotalWithVAT  float64 `json:"total_with_vat,omitempty"`
	Fee           float64 `json:"fee,omitempty"`
	DistinctCars  int64   `json:"distinct_cars,omitempty"` // Only for TAQA
	DistinctDays  int64   `json:"distinct_days,omitempty"` // Only for TAQA
	CarDays       int64   `json:"car_days,omitempty"`      // Number of car-days for this terminal
}

// Update the TripStatistics struct to include new fields:

type TripStatistics struct {
	Company       string                  `json:"company"`
	TotalTrips    int64                   `json:"total_trips"`
	TotalVolume   float64                 `json:"total_volume"`
	TotalDistance float64                 `json:"total_distance"`
	TotalRevenue  float64                 `json:"total_revenue"`
	TotalCarRent  float64                 `json:"total_car_rent,omitempty"` // Only for TAQA
	TotalVAT      float64                 `json:"total_vat,omitempty"`      // For Watanya and TAQA
	TotalAmount   float64                 `json:"total_amount,omitempty"`   // Total including VAT and rentals
	Details       []TripStatisticsDetails `json:"details,omitempty"`
}

// GetTripStatistics returns aggregated trip statistics grouped by company

func (h *TripHandler) GetTripStatistics(c *fiber.Ctx) error {
	// Get filters from query parameters
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	companyFilter := c.Query("company")

	// Get user from context (set by Verify middleware)
	user, ok := c.Locals("user").(Models.User)

	// Check if user has permission to view financial data
	hasFinancialAccess := ok && user.Permission >= 3

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
		// Create a new query specific to this company
		companyQuery := h.DB.Model(&Models.TripStruct{}).Where("company = ?", company)

		// Apply the same date filters
		if startDate != "" && endDate != "" {
			companyQuery = companyQuery.Where("date >= ? AND date <= ?", startDate, endDate)
		}

		// Initialize the company statistics
		companyStats := TripStatistics{
			Company: company,
		}

		// Get total trips count correctly
		var totalTrips int64
		companyQuery.Count(&totalTrips)
		companyStats.TotalTrips = totalTrips

		// Get total volume
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
			AND (t.date >= ? OR ? = '')
			AND (t.date <= ? OR ? = '')
		`, company, startDate, startDate, endDate, endDate).Row().Scan(&totalDistance)

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
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY t.drop_off_point, fm.fee
			`, company, startDate, startDate, endDate, endDate).Scan(&dropOffStats)

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

				// Clear financial data if user doesn't have access
				if !hasFinancialAccess {
					detail.TotalRevenue = 0
					detail.Fee = 0
				}

				companyStats.Details = append(companyStats.Details, detail)
				if hasFinancialAccess {
					companyStats.TotalRevenue += revenue
				}
			}

		case "TAQA":
			// First, we need to calculate the actual car rental days - each car's working days
			var carWorkingDays []struct {
				CarID string
				Date  string
			}

			// This query gets distinct car_id and date combinations
			h.DB.Raw(`
				SELECT DISTINCT car_id, date
				FROM trips
				WHERE company = ? AND deleted_at IS NULL
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
				ORDER BY car_id, date
			`, company, startDate, startDate, endDate, endDate).Scan(&carWorkingDays)

			// Calculate total rental fee: 1433 per car per working day
			var totalCarRentalFee float64 = float64(len(carWorkingDays)) * 1433.0 * 1.14

			// Group by terminal
			var terminalStats []struct {
				Terminal      string
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
				DistinctCars  int64
			}

			// For each terminal, join with fee_mappings to get the distance
			h.DB.Raw(`
				SELECT t.terminal, 
					COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT t.car_id) as distinct_cars
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY t.terminal
			`, company, startDate, startDate, endDate, endDate).Scan(&terminalStats)

			// Now we need to get car working days per terminal
			terminalCarDays := make(map[string]int)

			for _, terminal := range terminalStats {
				var terminalWorkingDays []struct {
					CarID string
					Date  string
				}

				h.DB.Raw(`
					SELECT DISTINCT car_id, date
					FROM trips
					WHERE company = ? AND deleted_at IS NULL
					AND terminal = ?
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					ORDER BY car_id, date
				`, company, terminal.Terminal, startDate, startDate, endDate, endDate).Scan(&terminalWorkingDays)

				terminalCarDays[terminal.Terminal] = len(terminalWorkingDays)
			}

			// Calculate details and total revenue
			companyStats.Details = make([]TripStatisticsDetails, 0, len(terminalStats))
			companyStats.TotalRevenue = 0
			companyStats.TotalVAT = 0
			companyStats.TotalCarRent = totalCarRentalFee
			companyStats.TotalAmount = 0

			for _, stat := range terminalStats {
				var ratePerKm float64
				if stat.Terminal == "Alex" {
					ratePerKm = 33.9
				} else if stat.Terminal == "Suez" {
					ratePerKm = 30.9
				} else {
					ratePerKm = 0 // Default if unknown terminal
				}

				// Base revenue from distance
				baseRevenue := stat.TotalDistance * ratePerKm

				// Get the terminal's portion of car rental fee
				// Proportionally based on the number of car-days
				carRentalFee := 0.0
				if terminalCarDays[stat.Terminal] > 0 {
					carRentalFee = float64(terminalCarDays[stat.Terminal]) * 1433.0 * 1.14
				}

				// Calculate 14% VAT on the base revenue and car rental
				vat := (baseRevenue + carRentalFee) * 0.14

				// Total revenue including VAT
				totalRevenue := baseRevenue + carRentalFee + vat

				// Get distinct days for this terminal
				var distinctDays int64
				h.DB.Raw(`
					SELECT COUNT(DISTINCT date)
					FROM trips
					WHERE company = ? AND deleted_at IS NULL
					AND terminal = ?
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
				`, company, stat.Terminal, startDate, startDate, endDate, endDate).Row().Scan(&distinctDays)

				detail := TripStatisticsDetails{
					GroupName:     stat.Terminal,
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.TotalDistance,
					TotalRevenue:  baseRevenue,
					CarRental:     carRentalFee,
					VAT:           vat,
					TotalWithVAT:  totalRevenue,
					Fee:           ratePerKm,
					DistinctCars:  stat.DistinctCars,
					DistinctDays:  distinctDays,
					CarDays:       int64(terminalCarDays[stat.Terminal]),
				}

				// Clear financial data if user doesn't have access
				if !hasFinancialAccess {
					detail.TotalRevenue = 0
					detail.CarRental = 0
					detail.VAT = 0
					detail.TotalWithVAT = 0
					detail.Fee = 0
				}

				companyStats.Details = append(companyStats.Details, detail)
				if hasFinancialAccess {
					companyStats.TotalRevenue += baseRevenue
					// NOTE: We're not adding carRentalFee to TotalCarRent here because
					// we've already set the total car rental fee for the company above
					companyStats.TotalVAT += vat
					companyStats.TotalAmount += totalRevenue
				}
			}

			// Update TripStatisticsDetails struct to include CarDays field
			// type TripStatisticsDetails struct {
			//     ...existing fields...
			//     CarDays        int64   `json:"car_days,omitempty"`  // Number of car-days for this terminal
			// }

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
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY f.fee
			`, company, startDate, startDate, endDate, endDate).Scan(&feeStats)

			// Calculate details and total revenue
			companyStats.Details = make([]TripStatisticsDetails, 0, len(feeStats))
			companyStats.TotalRevenue = 0
			companyStats.TotalVAT = 0
			companyStats.TotalAmount = 0

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

				// Base revenue from volume
				baseRevenue := stat.TotalVolume * ratePerVolume / 1000

				// Calculate 14% VAT
				vat := baseRevenue * 0.14

				// Total revenue including VAT
				totalRevenue := baseRevenue + vat

				detail := TripStatisticsDetails{
					GroupName:     "Fee " + fmt.Sprintf("%.0f", stat.Fee),
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.TotalDistance,
					TotalRevenue:  baseRevenue,
					VAT:           vat,
					TotalWithVAT:  totalRevenue,
					Fee:           stat.Fee,
				}

				// Clear financial data if user doesn't have access
				if !hasFinancialAccess {
					detail.TotalRevenue = 0
					detail.VAT = 0
					detail.TotalWithVAT = 0
					detail.Fee = 0
				}

				companyStats.Details = append(companyStats.Details, detail)
				if hasFinancialAccess {
					companyStats.TotalRevenue += baseRevenue
					companyStats.TotalVAT += vat
					companyStats.TotalAmount += totalRevenue
				}
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

			// Only set revenue if user has financial access
			if hasFinancialAccess {
				companyStats.TotalRevenue = revenue
			} else {
				companyStats.TotalRevenue = 0
			}
		}

		// Verify the sum of detail trips matches total trips
		var detailTripSum int64 = 0
		for _, detail := range companyStats.Details {
			detailTripSum += detail.TotalTrips
		}

		// If we have details but the sum doesn't match the total, reconcile
		if len(companyStats.Details) > 0 && detailTripSum != companyStats.TotalTrips {
			// Log the discrepancy for debugging
			fmt.Printf("Trip count discrepancy for %s: Total=%d, Sum of details=%d\n",
				company, companyStats.TotalTrips, detailTripSum)
		}

		// Add to the response array
		statistics = append(statistics, companyStats)
	}

	// Add a flag to inform frontend whether financial data is visible
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":            "Trip statistics retrieved successfully",
		"data":               statistics,
		"hasFinancialAccess": hasFinancialAccess,
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
