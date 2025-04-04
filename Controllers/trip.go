package Controllers

import (
	"Falcon/Models"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
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

func (h *TripHandler) GetWatanyaDriverAnalytics(c *fiber.Ctx) error {
	// Get filters from query parameters
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	// Get user from context (set by Verify middleware)
	user, ok := c.Locals("user").(Models.User)

	// Check if user has permission to view financial data
	hasFinancialAccess := ok && user.Permission >= 3

	// Define the response structure for driver analytics
	type DriverAnalytics struct {
		DriverName      string  `json:"driver_name"`
		TotalTrips      int64   `json:"total_trips"`
		TotalDistance   float64 `json:"total_distance"`
		TotalVolume     float64 `json:"total_volume"`
		TotalFees       float64 `json:"total_fees,omitempty"`    // Omit if no financial access
		TotalRevenue    float64 `json:"total_revenue,omitempty"` // Omit if no financial access
		TotalVAT        float64 `json:"total_vat,omitempty"`     // Omit if no financial access
		TotalAmount     float64 `json:"total_amount,omitempty"`  // Omit if no financial access
		WorkingDays     int64   `json:"working_days"`
		AvgTripsPerDay  float64 `json:"avg_trips_per_day"`
		AvgKmPerDay     float64 `json:"avg_km_per_day"`
		AvgFeesPerDay   float64 `json:"avg_fees_per_day,omitempty"` // Omit if no financial access
		AvgTripsPerKm   float64 `json:"avg_trips_per_km"`
		AvgVolumePerKm  float64 `json:"avg_volume_per_km"`
		Efficiency      float64 `json:"efficiency"` // Ratio of their performance to average
		ActivityHeatmap []struct {
			Date  string `json:"date"`
			Count int64  `json:"count"`
		} `json:"activity_heatmap"`
		RouteDistribution []struct {
			Route    string  `json:"route"` // Terminal to Drop-off point
			Count    int64   `json:"count"`
			Distance float64 `json:"distance"`
			Percent  float64 `json:"percent"` // Percentage of driver's total trips
		} `json:"route_distribution"`
	}

	// Define response structure with global stats
	type DriverAnalyticsResponse struct {
		Drivers     []DriverAnalytics `json:"drivers"`
		GlobalStats struct {
			AvgTripsPerDriver    float64  `json:"avg_trips_per_driver"`
			AvgRevenuePerDay     float64  `json:"avg_revenue_per_day"`
			AvgDistancePerDriver float64  `json:"avg_distance_per_driver"`
			AvgTripsPerDay       float64  `json:"avg_trips_per_day"`
			AvgKmPerDay          float64  `json:"avg_km_per_day"`
			AvgVolumePerKm       float64  `json:"avg_volume_per_km"`
			TotalTrips           int64    `json:"total_trips"`
			TotalDistance        float64  `json:"total_distance"`
			TotalVolume          float64  `json:"total_volume"`
			TotalFees            float64  `json:"total_fees,omitempty"`    // Omit if no financial access
			TotalRevenue         float64  `json:"total_revenue,omitempty"` // Omit if no financial access
			TopDrivers           []string `json:"top_drivers"`             // Top 5 drivers by trip count
		} `json:"global_stats"`
	}

	// Check if fee mappings exist for Watanya
	var feeMappingCount int64
	h.DB.Model(&Models.FeeMapping{}).
		Where("company = ?", "Watanya").
		Count(&feeMappingCount)

	if feeMappingCount == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "No fee mappings found for Watanya company",
			"error":   "Fee mappings are required for proper analytics",
		})
	}

	// Base query for Watanya trips
	baseQuery := h.DB.Model(&Models.TripStruct{}).
		Where("company = ? AND deleted_at IS NULL", "Watanya")

	// Apply date filters if provided
	if startDate != "" && endDate != "" {
		baseQuery = baseQuery.Where("date BETWEEN ? AND ?", startDate, endDate)
	}

	// Get all distinct drivers
	var driverNames []string
	if err := baseQuery.Distinct("driver_name").
		Where("driver_name IS NOT NULL AND driver_name != ''").
		Pluck("driver_name", &driverNames).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch driver names",
			"error":   err.Error(),
		})
	}

	// Check if we found any drivers
	if len(driverNames) == 0 {
		// Return empty response with proper structure
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"message": "No drivers found for the given filters",
			"data": DriverAnalyticsResponse{
				Drivers: []DriverAnalytics{},
				GlobalStats: struct {
					AvgTripsPerDriver    float64  `json:"avg_trips_per_driver"`
					AvgRevenuePerDay     float64  `json:"avg_revenue_per_day"`
					AvgDistancePerDriver float64  `json:"avg_distance_per_driver"`
					AvgTripsPerDay       float64  `json:"avg_trips_per_day"`
					AvgKmPerDay          float64  `json:"avg_km_per_day"`
					AvgVolumePerKm       float64  `json:"avg_volume_per_km"`
					TotalTrips           int64    `json:"total_trips"`
					TotalDistance        float64  `json:"total_distance"`
					TotalVolume          float64  `json:"total_volume"`
					TotalFees            float64  `json:"total_fees,omitempty"`
					TotalRevenue         float64  `json:"total_revenue,omitempty"`
					TopDrivers           []string `json:"top_drivers"`
				}{
					AvgTripsPerDriver:    0,
					AvgDistancePerDriver: 0,
					AvgTripsPerDay:       0,
					AvgKmPerDay:          0,
					AvgVolumePerKm:       0,
					TotalTrips:           0,
					TotalDistance:        0,
					TotalVolume:          0,
					TopDrivers:           []string{},
				},
			},
			"hasFinancialAccess": hasFinancialAccess,
		})
	}

	// Prepare response
	response := DriverAnalyticsResponse{
		Drivers: make([]DriverAnalytics, 0, len(driverNames)),
	}

	// Global counters
	var globalTotalTrips int64 = 0
	var globalTotalDistance float64 = 0
	var globalTotalVolume float64 = 0
	var globalTotalFees float64 = 0
	var globalTotalRevenue float64 = 0
	var globalTotalWorkingDays int64 = 0
	var globalDistinctDays int64 = 0

	// First count global distinct days for global averages
	h.DB.Raw(`
		SELECT COUNT(DISTINCT date)
		FROM trips
		WHERE company = 'Watanya' AND deleted_at IS NULL
		AND (date >= ? OR ? = '')
		AND (date <= ? OR ? = '')
	`, startDate, startDate, endDate, endDate).Row().Scan(&globalDistinctDays)

	// Prevent division by zero later
	if globalDistinctDays == 0 {
		globalDistinctDays = 1
	}

	// Track driver performance for ranking
	type DriverPerformance struct {
		Name    string
		Revenue float64 // Change from TripCount to Revenue
	}
	driverPerformance := make([]DriverPerformance, 0, len(driverNames))
	// Process each driver's data
	for _, driverName := range driverNames {
		// Skip empty or invalid driver names (should be caught by the query, but just in case)
		if driverName == "" {
			continue
		}

		driverAnalytics := DriverAnalytics{
			DriverName: driverName,
		}

		// Get activity heatmap data first, as we'll use it to verify trip counts
		var activityHeatmap []struct {
			Date  string `json:"date"`
			Count int64  `json:"count"`
		}

		err := h.DB.Raw(`
			SELECT date, COUNT(*) as count
			FROM trips
			WHERE company = 'Watanya' AND driver_name = ? AND deleted_at IS NULL
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
			GROUP BY date
			ORDER BY date
		`, driverName, startDate, startDate, endDate, endDate).Scan(&activityHeatmap).Error

		if err != nil {
			// Log error but continue
			log.Printf("Error fetching activity heatmap for driver %s: %v", driverName, err)
			activityHeatmap = []struct {
				Date  string `json:"date"`
				Count int64  `json:"count"`
			}{}
		}

		driverAnalytics.ActivityHeatmap = activityHeatmap

		// Count total trips directly from the database
		var totalTrips int64
		err = h.DB.Raw(`
			SELECT COUNT(*) 
			FROM trips
			WHERE company = 'Watanya' AND driver_name = ? AND deleted_at IS NULL
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
		`, driverName, startDate, startDate, endDate, endDate).Row().Scan(&totalTrips)

		if err != nil {
			log.Printf("Error counting trips for driver %s: %v", driverName, err)
			totalTrips = 0
		}

		// Double-check with activity heatmap
		var heatmapSum int64 = 0
		for _, activity := range activityHeatmap {
			heatmapSum += activity.Count
		}

		// Use the higher value between DB count and heatmap sum
		if heatmapSum > totalTrips {
			log.Printf("Heatmap sum (%d) > DB trip count (%d) for driver %s", heatmapSum, totalTrips, driverName)
			totalTrips = heatmapSum
		}

		driverAnalytics.TotalTrips = totalTrips
		globalTotalTrips += totalTrips

		// Get total volume from the driver's trips
		var totalVolume float64
		err = h.DB.Raw(`
			SELECT COALESCE(SUM(tank_capacity), 0)
			FROM trips
			WHERE company = 'Watanya' AND driver_name = ? AND deleted_at IS NULL
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
		`, driverName, startDate, startDate, endDate, endDate).Row().Scan(&totalVolume)

		if err != nil {
			log.Printf("Error calculating total volume for driver %s: %v", driverName, err)
			totalVolume = 0
		}

		driverAnalytics.TotalVolume = totalVolume
		globalTotalVolume += totalVolume

		// Get route distribution
		var routeDistribution []struct {
			Terminal     string
			DropOffPoint string
			Count        int64
		}

		// Get the terminal and drop-off points combinations used by this driver
		err = h.DB.Raw(`
			SELECT terminal, drop_off_point, COUNT(*) as count
			FROM trips
			WHERE company = 'Watanya' AND driver_name = ? AND deleted_at IS NULL
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
			GROUP BY terminal, drop_off_point
			ORDER BY count DESC
		`, driverName, startDate, startDate, endDate, endDate).
			Find(&routeDistribution).Error

		if err != nil {
			// Log error but continue with other calculations
			log.Printf("Error fetching route distribution for driver %s: %v", driverName, err)
			routeDistribution = []struct {
				Terminal     string
				DropOffPoint string
				Count        int64
			}{}
		}

		// Now get distance and fee for each route from fee_mappings
		var driverRoutes []struct {
			Route    string  `json:"route"`
			Count    int64   `json:"count"`
			Distance float64 `json:"distance"`
			Fee      float64 `json:"fee"`
			Percent  float64 `json:"percent"`
		}

		var totalDistance float64 = 0
		var totalFees float64 = 0

		// Process each route used by the driver
		for _, route := range routeDistribution {
			// Get the fee mapping for this route
			var feeMapping Models.FeeMapping
			err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
				"Watanya", route.Terminal, route.DropOffPoint).
				First(&feeMapping).Error

			if err != nil {
				// Fee mapping not found, use a default or skip
				log.Printf("No fee mapping found for route %s → %s", route.Terminal, route.DropOffPoint)
				continue
			}

			// Look up the actual fee amount based on the fee index
			var feeAmount float64
			switch int64(feeMapping.Fee) {
			case 1:
				feeAmount = 75.0
			case 2:
				feeAmount = 95.0
			case 3:
				feeAmount = 115.0
			case 4:
				feeAmount = 135.0
			case 5:
				feeAmount = 155.0
			default:
				feeAmount = 0.0
			}

			// Calculate fees for this route: fee * volume / 1000
			var routeVolume float64
			h.DB.Raw(`
				SELECT COALESCE(SUM(tank_capacity), 0)
				FROM trips
				WHERE company = 'Watanya' AND driver_name = ? AND terminal = ? AND drop_off_point = ? AND deleted_at IS NULL
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
			`, driverName, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate).
				Row().Scan(&routeVolume)

			routeFee := (feeAmount * routeVolume) / 1000.0
			routeDistance := feeMapping.Distance * float64(route.Count)

			// Add to driver totals
			totalDistance += routeDistance
			totalFees += routeFee

			// Add route to driver's route distribution
			driverRoutes = append(driverRoutes, struct {
				Route    string  `json:"route"`
				Count    int64   `json:"count"`
				Distance float64 `json:"distance"`
				Fee      float64 `json:"fee"`
				Percent  float64 `json:"percent"`
			}{
				Route:    fmt.Sprintf("%s → %s", route.Terminal, route.DropOffPoint),
				Count:    route.Count,
				Distance: routeDistance,
				Fee:      routeFee,
				Percent:  0, // Will calculate percentages after all routes are processed
			})
		}

		// Calculate proper percentages for route distribution
		if len(driverRoutes) > 0 && totalTrips > 0 {
			for i := range driverRoutes {
				driverRoutes[i].Percent = (float64(driverRoutes[i].Count) / float64(totalTrips)) * 100
			}
		}

		// Update driver and global totals
		driverAnalytics.TotalDistance = totalDistance
		driverAnalytics.TotalFees = totalFees
		globalTotalDistance += totalDistance
		globalTotalFees += totalFees

		// Calculate revenue (fees) and VAT
		totalRevenue := totalFees
		totalVAT := totalRevenue * 0.14
		driverAnalytics.TotalRevenue = totalRevenue
		driverAnalytics.TotalVAT = totalVAT
		driverAnalytics.TotalAmount = totalRevenue + totalVAT
		globalTotalRevenue += totalRevenue

		// Get working days - days when the driver had at least one trip
		var workingDays int64
		h.DB.Raw(`
			SELECT COUNT(DISTINCT date)
			FROM trips
			WHERE company = 'Watanya' AND driver_name = ? AND deleted_at IS NULL
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
		`, driverName, startDate, startDate, endDate, endDate).Row().Scan(&workingDays)

		// Ensure working days is at least 1 to avoid division by zero
		if workingDays == 0 && totalTrips > 0 {
			workingDays = 1
		}

		driverAnalytics.WorkingDays = workingDays
		globalTotalWorkingDays += workingDays

		// Calculate averages - with safety checks to prevent division by zero
		if workingDays > 0 {
			driverAnalytics.AvgTripsPerDay = float64(totalTrips) / float64(workingDays)
			driverAnalytics.AvgKmPerDay = totalDistance / float64(workingDays)
			driverAnalytics.AvgFeesPerDay = totalFees / float64(workingDays)
		}

		if totalDistance > 0 {
			driverAnalytics.AvgTripsPerKm = float64(totalTrips) / totalDistance
			driverAnalytics.AvgVolumePerKm = totalVolume / totalDistance
		}

		// Update route distribution in the driver analytics
		// Initialize as empty array if no routes found, not null
		if len(driverRoutes) > 0 {
			// Convert to the expected struct type
			routeDistributionData := make([]struct {
				Route    string  `json:"route"`
				Count    int64   `json:"count"`
				Distance float64 `json:"distance"`
				Percent  float64 `json:"percent"`
			}, len(driverRoutes))

			for i, route := range driverRoutes {
				routeDistributionData[i] = struct {
					Route    string  `json:"route"`
					Count    int64   `json:"count"`
					Distance float64 `json:"distance"`
					Percent  float64 `json:"percent"`
				}{
					Route:    route.Route,
					Count:    route.Count,
					Distance: route.Distance,
					Percent:  route.Percent,
				}
			}

			driverAnalytics.RouteDistribution = routeDistributionData
		} else {
			// Initialize as empty array, not null
			driverAnalytics.RouteDistribution = []struct {
				Route    string  `json:"route"`
				Count    int64   `json:"count"`
				Distance float64 `json:"distance"`
				Percent  float64 `json:"percent"`
			}{}
		}

		// Add to performance tracking for ranking
		driverPerformance = append(driverPerformance, DriverPerformance{
			Name:    driverName,
			Revenue: totalRevenue, // Use totalRevenue instead of TripCount
		})

		// Clear financial data if user doesn't have access
		if !hasFinancialAccess {
			driverAnalytics.TotalFees = 0
			driverAnalytics.TotalRevenue = 0
			driverAnalytics.TotalVAT = 0
			driverAnalytics.TotalAmount = 0
			driverAnalytics.AvgFeesPerDay = 0
		}

		// Add to response
		response.Drivers = append(response.Drivers, driverAnalytics)
	}

	// Calculate global stats
	driverCount := len(response.Drivers)
	if driverCount > 0 {
		response.GlobalStats.AvgTripsPerDriver = float64(globalTotalTrips) / float64(driverCount)
		response.GlobalStats.AvgDistancePerDriver = globalTotalDistance / float64(driverCount)
	}

	// Calculate average trips per day based on total working days across all drivers
	// This gives us the average trips per day per working driver
	if globalTotalWorkingDays > 0 {
		response.GlobalStats.AvgTripsPerDay = float64(globalTotalTrips) / float64(globalTotalWorkingDays)
		response.GlobalStats.AvgRevenuePerDay = globalTotalRevenue / float64(globalTotalWorkingDays)
	} else {
		response.GlobalStats.AvgTripsPerDay = 0
	}

	// Calculate average km per day (still based on total days in the period for global coverage)
	if globalDistinctDays > 0 {
		response.GlobalStats.AvgKmPerDay = globalTotalDistance / float64(globalDistinctDays)
	} else {
		response.GlobalStats.AvgKmPerDay = 0
	}

	if globalTotalDistance > 0 {
		response.GlobalStats.AvgVolumePerKm = globalTotalVolume / globalTotalDistance
	}

	response.GlobalStats.TotalTrips = globalTotalTrips
	response.GlobalStats.TotalDistance = globalTotalDistance
	response.GlobalStats.TotalVolume = globalTotalVolume

	if hasFinancialAccess {
		response.GlobalStats.TotalFees = globalTotalFees
		response.GlobalStats.TotalRevenue = globalTotalRevenue
	}

	// Calculate efficiency score for each driver (compared to average)
	globalAvgRevenuePerDay := response.GlobalStats.TotalRevenue / float64(globalTotalWorkingDays)
	if globalAvgRevenuePerDay > 0 {
		for i := range response.Drivers {
			// Only calculate efficiency for drivers with revenue
			if response.Drivers[i].TotalRevenue > 0 {
				// Calculate daily revenue and compare to global average
				driverDailyRevenue := response.Drivers[i].TotalRevenue / float64(response.Drivers[i].WorkingDays)
				response.Drivers[i].Efficiency = driverDailyRevenue / globalAvgRevenuePerDay
			} else {
				// For drivers with no revenue, set efficiency to 0
				response.Drivers[i].Efficiency = 0
			}
		}
	}

	// Sort drivers by trip count for top performers
	sort.Slice(driverPerformance, func(i, j int) bool {
		return driverPerformance[i].Revenue > driverPerformance[j].Revenue
	})

	// Get top 5 drivers or all drivers if less than 5
	topDriversCount := 5
	if len(driverPerformance) < topDriversCount {
		topDriversCount = len(driverPerformance)
	}

	response.GlobalStats.TopDrivers = make([]string, topDriversCount)
	for i := 0; i < topDriversCount; i++ {
		response.GlobalStats.TopDrivers[i] = driverPerformance[i].Name
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":            "Watanya driver analytics retrieved successfully",
		"data":               response,
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

// DateRangeRequest represents the request body for date range filtering
type DateRangeRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// WatanyaReportSummary represents the summarized data for reporting
type WatanyaReportSummary struct {
	ID            int     // Sequential ID starting from 1
	DropOffPoint  string  // Drop-off point
	Terminal      string  // Terminal
	TripCount     int     // Number of trips
	TotalCapacity int     // Sum of tank capacities
	Distance      float64 // Distance from fee mapping
	Fee           int     // Fee index (1-5)
	ActualFee     float64 // Actual fee amount based on index
	Cost          float64 // Cost calculated as TotalCapacity * ActualFee / 1000
}

// GetWatanyaTripsReport generates an Excel report for Watanya trips within a date range
func (h *TripHandler) GetWatanyaTripsReport(c *fiber.Ctx) error {
	// Parse date range from request
	var req DateRangeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse request body",
		})
	}

	// Validate date range
	if req.StartDate == "" || req.EndDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Start date and end date are required",
		})
	}

	// Query trips for Watanya company within the date range
	var trips []Models.TripStruct
	result := h.DB.Where("company = ? AND date BETWEEN ? AND ?",
		"Watanya", req.StartDate, req.EndDate).Find(&trips)

	if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error: " + result.Error.Error(),
		})
	}

	// Define fee mapping (fee index to actual fee amount)
	feeIndexToAmount := map[int]float64{
		1: 75.0,
		2: 95.0,
		3: 115.0,
		4: 135.0,
		5: 155.0,
	}

	// Get the fee mappings from the database
	var feeMappings []Models.FeeMapping
	if err := h.DB.Where("company = ?", "Watanya").Find(&feeMappings).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve fee mappings: " + err.Error(),
		})
	}

	// Create a map for quick lookup of fee mapping data
	feeMappingMap := make(map[string]Models.FeeMapping)
	for _, mapping := range feeMappings {
		key := mapping.Terminal + "|" + mapping.DropOffPoint
		feeMappingMap[key] = mapping
	}

	// Map to store trips by terminal and drop-off point combination
	tripsByRoute := make(map[string][]Models.TripStruct)

	// Group trips by drop-off point and terminal
	groupedTrips := make(map[string]*WatanyaReportSummary)

	for _, trip := range trips {
		// Create a unique key for each terminal + drop-off point combination
		key := fmt.Sprintf("%s|%s", trip.Terminal, trip.DropOffPoint)

		// Look up the fee mapping for this route
		feeMapping, exists := feeMappingMap[key]
		if !exists {
			// If no mapping exists, log an error and skip this trip
			continue
		}

		// Determine the fee index based on the fee amount
		feeMapping.Fee = feeIndexToAmount[int(feeMapping.Fee)]

		if _, exists := groupedTrips[key]; !exists {
			// Initialize a new summary record if this combination doesn't exist
			groupedTrips[key] = &WatanyaReportSummary{
				DropOffPoint:  trip.DropOffPoint,
				Terminal:      trip.Terminal,
				TripCount:     0,
				TotalCapacity: 0,
				Distance:      feeMapping.Distance,
				Fee:           int(feeMapping.Fee),
				ActualFee:     feeMapping.Fee,
			}
		}

		// Store trips for the detailed sheet
		tripsByRoute[key] = append(tripsByRoute[key], trip)

		// Update the summary data
		summary := groupedTrips[key]
		summary.TripCount++
		summary.TotalCapacity += trip.TankCapacity
	}

	// Convert grouped data to a slice and calculate costs
	summaries := make([]WatanyaReportSummary, 0, len(groupedTrips))

	for _, summary := range groupedTrips {
		// Calculate cost - use the actual fee from the mapping
		cost := float64(summary.TotalCapacity) * summary.ActualFee / 1000.0

		// Add cost to the summary
		summary.Cost = cost

		// Create final summary object (ID will be assigned after sorting)
		summaries = append(summaries, *summary)
	}

	// Sort summaries by drop-off point first, then by terminal
	sort.Slice(summaries, func(i, j int) bool {
		// Primary sort by drop-off point
		if summaries[i].DropOffPoint != summaries[j].DropOffPoint {
			return summaries[i].DropOffPoint < summaries[j].DropOffPoint
		}
		// Secondary sort by terminal
		return summaries[i].Terminal < summaries[j].Terminal
	})

	// Assign sequential IDs after sorting
	for i := range summaries {
		summaries[i].ID = i + 1
	}

	// Create Excel file
	f := excelize.NewFile()
	summarySheetName := "Watanya Summary"

	// Rename the default sheet
	f.SetSheetName("Sheet1", summarySheetName)

	// ----------- SUMMARY SHEET ------------
	// Set headers
	headers := []string{"ID", "Drop-off Point", "Terminal", "Trip Count",
		"Total Capacity", "Distance (km)", "Fee Index", "Fee Amount", "Cost"}

	for i, header := range headers {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(summarySheetName, cell, header)
	}

	// Set column width
	f.SetColWidth(summarySheetName, "A", "A", 10)
	f.SetColWidth(summarySheetName, "B", "C", 25)
	f.SetColWidth(summarySheetName, "D", "I", 15)

	// Add style to header row
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
			Size: 12,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#DCE6F1"},
			Pattern: 1,
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	f.SetCellStyle(summarySheetName, "A1", string(rune('A'+len(headers)-1))+"1", headerStyle)

	// Add data rows
	for i, summary := range summaries {
		row := i + 2 // Row 1 is header

		f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", row), summary.ID)
		f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", row), summary.DropOffPoint)
		f.SetCellValue(summarySheetName, fmt.Sprintf("C%d", row), summary.Terminal)
		f.SetCellValue(summarySheetName, fmt.Sprintf("D%d", row), summary.TripCount)
		f.SetCellValue(summarySheetName, fmt.Sprintf("E%d", row), summary.TotalCapacity)
		f.SetCellValue(summarySheetName, fmt.Sprintf("F%d", row), summary.Distance)
		f.SetCellValue(summarySheetName, fmt.Sprintf("G%d", row), summary.Fee)
		f.SetCellValue(summarySheetName, fmt.Sprintf("H%d", row), summary.ActualFee)
		f.SetCellValue(summarySheetName, fmt.Sprintf("I%d", row), summary.Cost)
	}

	// Add totals row
	totalRow := len(summaries) + 2
	f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", totalRow), "TOTAL")

	// Set formulas for totals
	f.SetCellFormula(summarySheetName, fmt.Sprintf("D%d", totalRow), fmt.Sprintf("SUM(D2:D%d)", totalRow-1))
	f.SetCellFormula(summarySheetName, fmt.Sprintf("E%d", totalRow), fmt.Sprintf("SUM(E2:E%d)", totalRow-1))
	f.SetCellFormula(summarySheetName, fmt.Sprintf("I%d", totalRow), fmt.Sprintf("SUM(I2:I%d)", totalRow-1))

	// Style for totals row
	totalStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
			Size: 12,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#F2F2F2"},
			Pattern: 1,
		},
		Border: []excelize.Border{
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
		},
	})
	f.SetCellStyle(summarySheetName, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("I%d", totalRow), totalStyle)

	// ----------- DETAILED SHEET ------------
	// Create a second sheet for detailed trip data
	detailedSheetName := "Detailed Trips"
	f.NewSheet(detailedSheetName)

	// Define table headers for detailed trips
	tripHeaders := []string{"No", "Date", "Driver Name", "Car No Plate", "Transporter", "Tank Capacity",
		"Gas Type", "Receipt No", "Revenue", "Mileage"}

	// Set column widths for detailed sheet
	f.SetColWidth(detailedSheetName, "A", "A", 10) // No
	f.SetColWidth(detailedSheetName, "B", "B", 15) // Date
	f.SetColWidth(detailedSheetName, "C", "C", 20) // Driver Name
	f.SetColWidth(detailedSheetName, "D", "D", 15) // Car No Plate
	f.SetColWidth(detailedSheetName, "E", "E", 20) // Transporter
	f.SetColWidth(detailedSheetName, "F", "F", 15) // Tank Capacity
	f.SetColWidth(detailedSheetName, "G", "G", 15) // Gas Type
	f.SetColWidth(detailedSheetName, "H", "H", 15) // Receipt No
	f.SetColWidth(detailedSheetName, "I", "I", 15) // Revenue
	f.SetColWidth(detailedSheetName, "J", "J", 15) // Mileage

	// Styles for detailed sheet
	routeTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:  true,
			Size:  14,
			Color: "#000000",
		},
	})

	tableHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
			Size: 11,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#E0EBF5"},
			Pattern: 1,
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: "#AAAAAA", Style: 1},
			{Type: "top", Color: "#AAAAAA", Style: 1},
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})

	dataStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "bottom", Color: "#EEEEEE", Style: 1},
		},
	})

	// Track current row for positioning tables
	currentRow := 1

	// Create a table for each unique drop-off point and terminal combination
	// Use the same sort order as the summary sheet
	for _, summary := range summaries {
		key := fmt.Sprintf("%s|%s", summary.Terminal, summary.DropOffPoint)
		routeTrips, exists := tripsByRoute[key]
		if !exists {
			continue
		}

		// Set table title - Terminal & Drop-off Point
		tableTitle := fmt.Sprintf("Terminal: %s | Drop-off Point: %s", summary.Terminal, summary.DropOffPoint)
		titleCell := fmt.Sprintf("A%d", currentRow)
		f.SetCellValue(detailedSheetName, titleCell, tableTitle)
		f.MergeCell(detailedSheetName, titleCell, fmt.Sprintf("J%d", currentRow))
		f.SetCellStyle(detailedSheetName, titleCell, titleCell, routeTitleStyle)

		currentRow++

		// Set table headers
		for i, header := range tripHeaders {
			cell := fmt.Sprintf("%c%d", 'A'+i, currentRow)
			f.SetCellValue(detailedSheetName, cell, header)
		}

		// Apply header style
		f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
			fmt.Sprintf("%c%d", 'A'+len(tripHeaders)-1, currentRow), tableHeaderStyle)

		currentRow++

		// Add table data
		tableStartRow := currentRow
		for i, trip := range routeTrips {
			// Set data rows
			f.SetCellValue(detailedSheetName, fmt.Sprintf("A%d", currentRow), i+1)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("B%d", currentRow), trip.Date)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("C%d", currentRow), trip.DriverName)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("D%d", currentRow), trip.CarNoPlate)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("E%d", currentRow), trip.Transporter)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("F%d", currentRow), trip.TankCapacity)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("G%d", currentRow), trip.GasType)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("H%d", currentRow), trip.ReceiptNo)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("I%d", currentRow), trip.Revenue)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("J%d", currentRow), trip.Mileage)

			// Apply data style
			f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
				fmt.Sprintf("J%d", currentRow), dataStyle)

			currentRow++
		}

		// Add table totals
		f.SetCellValue(detailedSheetName, fmt.Sprintf("A%d", currentRow), "TOTAL")
		f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
			fmt.Sprintf("A%d", currentRow), totalStyle)

		// Set formulas for totals
		f.SetCellFormula(detailedSheetName, fmt.Sprintf("F%d", currentRow),
			fmt.Sprintf("SUM(F%d:F%d)", tableStartRow, currentRow-1))
		f.SetCellFormula(detailedSheetName, fmt.Sprintf("I%d", currentRow),
			fmt.Sprintf("SUM(I%d:I%d)", tableStartRow, currentRow-1))
		f.SetCellFormula(detailedSheetName, fmt.Sprintf("J%d", currentRow),
			fmt.Sprintf("SUM(J%d:J%d)", tableStartRow, currentRow-1))

		// Apply total row style
		f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
			fmt.Sprintf("J%d", currentRow), totalStyle)

		// Add 3 empty rows before the next table
		currentRow += 4
	}

	// Set the active sheet to the summary sheet
	summarySheetIndex, _ := f.GetSheetIndex(summarySheetName)
	f.SetActiveSheet(summarySheetIndex)

	// Create a unique filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("watanya_report_%s.xlsx", timestamp)
	filepath := filepath.Join(os.TempDir(), filename)

	// Save the Excel file
	if err := f.SaveAs(filepath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate Excel report: " + err.Error(),
		})
	}

	// Return the file for download
	return c.SendFile(filepath, true)
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
