package Controllers

import (
	"Falcon/Models"
	"encoding/json"
	"fmt"
	"log"
	"math"
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

type TripRevenueDateResponse struct {
	Date           string                  `json:"date"`
	TotalTrips     int64                   `json:"total_trips"`
	TotalVolume    float64                 `json:"total_volume"`
	TotalDistance  float64                 `json:"total_distance"`
	TotalRevenue   float64                 `json:"total_revenue,omitempty"`
	CompanyDetails []CompanyRevenueDetails `json:"company_details"`
}

// GetTripStatistics returns aggregated trip statistics grouped by company

// CompanyRevenueDetails represents revenue statistics for a specific company on a specific date
type CompanyRevenueDetails struct {
	Company       string  `json:"company"`
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue,omitempty"`
	// Optional fields that may be used for specific companies
	VAT          float64 `json:"vat,omitempty"`
	CarRental    float64 `json:"car_rental,omitempty"`
	TotalWithVAT float64 `json:"total_with_vat,omitempty"`
	Fee          float64 `json:"fee,omitempty"`
	DistinctCars int64   `json:"distinct_cars,omitempty"`
	DistinctDays int64   `json:"distinct_days,omitempty"`
	CarDays      int64   `json:"car_days,omitempty"`
}

// Add this new struct to represent route-based statistics
type RouteRevenueStats struct {
	RouteName     string     `json:"route_name"` // Terminal-DropOffPoint pair or Fee category or Terminal name
	TotalTrips    int64      `json:"total_trips"`
	TotalVolume   float64    `json:"total_volume"`
	TotalDistance float64    `json:"total_distance"`
	TotalRevenue  float64    `json:"total_revenue,omitempty"`
	VAT           float64    `json:"vat,omitempty"`
	CarRental     float64    `json:"car_rental,omitempty"`
	TotalWithVAT  float64    `json:"total_with_vat,omitempty"`
	Fee           float64    `json:"fee,omitempty"`
	RouteType     string     `json:"route_type"` // "terminal-dropoff", "fee", or "terminal"
	Terminal      string     `json:"terminal,omitempty"`
	DropOffPoint  string     `json:"drop_off_point,omitempty"`
	FeeCategory   int        `json:"fee_category,omitempty"`
	Cars          []CarStats `json:"cars,omitempty"`
}

// Add this struct to represent car-level statistics
type CarStats struct {
	CarNoPlate    string  `json:"car_no_plate"` // Using car_no_plate instead of car_id
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue,omitempty"`
	WorkingDays   int64   `json:"working_days"`
	CarRental     float64 `json:"car_rental,omitempty"`
	VAT           float64 `json:"vat,omitempty"`
	TotalWithVAT  float64 `json:"total_with_vat,omitempty"`
}

// Update TripStatistics struct to include the route-based data
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
	RouteDetails  []RouteRevenueStats     `json:"route_details,omitempty"` // New field for route-based stats
}

type CarTotal struct {
	CarNoPlate  string  `json:"car_no_plate"`
	Liters      float64 `json:"liters"`
	Distance    float64 `json:"distance"`
	BaseRevenue float64 `json:"base_revenue"`
	VAT         float64 `json:"vat"`
	Rent        float64 `json:"rent"`
}

// Add a new function to calculate revenue statistics by route
func (h *TripHandler) GetTripStatsByRoute(company, startDate, endDate string, hasFinancialAccess bool) []RouteRevenueStats {
	var routeStats []RouteRevenueStats

	// Base query for company's trips
	query := h.DB.Model(&Models.TripStruct{}).Where("company = ?", company)

	// Apply date filters if provided
	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	}

	// Handle different route grouping based on company
	switch company {
	case "Petrol Arrows":
		// For Petrol Arrows, we use terminal + drop-off point pairs
		var routeData []struct {
			Terminal      string
			DropOffPoint  string
			TotalTrips    int64
			TotalVolume   float64
			TotalDistance float64
			Fee           float64
		}

		h.DB.Raw(`
			SELECT 
				t.terminal, 
				t.drop_off_point, 
				COUNT(*) as total_trips, 
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
			GROUP BY t.terminal, t.drop_off_point, fm.fee
		`, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		// Process each route
		for _, route := range routeData {
			// Calculate revenue: fee * Total Volume / 1000
			revenue := route.Fee * route.TotalVolume / 1000

			routeStat := RouteRevenueStats{
				RouteName:     fmt.Sprintf("%s to %s", route.Terminal, route.DropOffPoint),
				Terminal:      route.Terminal,
				DropOffPoint:  route.DropOffPoint,
				RouteType:     "terminal-dropoff",
				TotalTrips:    route.TotalTrips,
				TotalVolume:   route.TotalVolume,
				TotalDistance: route.TotalDistance,
				Fee:           route.Fee,
			}

			if hasFinancialAccess {
				routeStat.TotalRevenue = revenue
				routeStat.TotalWithVAT = revenue // No VAT for Petrol Arrows
			}

			// Get car-specific stats for this route
			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT t.date) as working_days
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND t.terminal = ? AND t.drop_off_point = ?
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY t.car_no_plate
			`, company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate).Scan(&carStats)

			// Calculate revenue for each car
			for i := range carStats {
				if hasFinancialAccess {
					carStats[i].TotalRevenue = route.Fee * carStats[i].TotalVolume / 1000
					carStats[i].TotalWithVAT = carStats[i].TotalRevenue // No VAT
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}

	case "TAQA":
		// For TAQA, we group by terminal only
		var routeData []struct {
			Terminal      string
			TotalTrips    int64
			TotalVolume   float64
			TotalDistance float64
			DistinctCars  int64
		}

		h.DB.Raw(`
			SELECT 
				t.terminal, 
				COUNT(*) as total_trips, 
				COALESCE(SUM(t.tank_capacity), 0) as total_volume,
				COALESCE(SUM(fm.distance), 0) as total_distance,
				COUNT(DISTINCT t.car_no_plate) as distinct_cars
			FROM trips t
			LEFT JOIN fee_mappings fm 
				ON t.company = fm.company 
				AND t.terminal = fm.terminal 
				AND t.drop_off_point = fm.drop_off_point
			WHERE t.company = ? AND t.deleted_at IS NULL
			AND (t.date >= ? OR ? = '')
			AND (t.date <= ? OR ? = '')
			GROUP BY t.terminal
		`, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		// Process each terminal route
		for _, route := range routeData {
			// Calculate rate per km based on terminal
			var ratePerKm float64
			if route.Terminal == "Alex" {
				ratePerKm = 40.7
			} else if route.Terminal == "Suez" {
				ratePerKm = 38.2
			} else {
				ratePerKm = 0 // Default if unknown terminal
			}

			// Calculate working days for this terminal
			var terminalWorkingDays []struct {
				CarNoPlate string
				Date       string
			}

			h.DB.Raw(`
				SELECT DISTINCT car_no_plate, date
				FROM trips
				WHERE company = ? AND deleted_at IS NULL
				AND terminal = ?
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
				ORDER BY car_no_plate, date
			`, company, route.Terminal, startDate, startDate, endDate, endDate).Scan(&terminalWorkingDays)

			// Calculate base revenue and car rental
			baseRevenue := route.TotalDistance * ratePerKm
			carRentalFee := float64(len(terminalWorkingDays)) * 1433.0
			vat := (baseRevenue + carRentalFee) * 0.14
			totalRevenue := baseRevenue + carRentalFee + vat

			routeStat := RouteRevenueStats{
				RouteName:     route.Terminal,
				Terminal:      route.Terminal,
				RouteType:     "terminal",
				TotalTrips:    route.TotalTrips,
				TotalVolume:   route.TotalVolume,
				TotalDistance: route.TotalDistance,
				Fee:           ratePerKm,
			}

			if hasFinancialAccess {
				routeStat.TotalRevenue = baseRevenue
				routeStat.CarRental = carRentalFee
				routeStat.VAT = vat
				routeStat.TotalWithVAT = totalRevenue
			}

			// Get car-specific stats for this terminal
			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT t.date) as working_days
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND t.terminal = ?
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY t.car_no_plate
			`, company, route.Terminal, startDate, startDate, endDate, endDate).Scan(&carStats)

			// Calculate revenue for each car
			for i := range carStats {
				if hasFinancialAccess {
					// Base revenue from distance
					carBaseRevenue := carStats[i].TotalDistance * ratePerKm

					// Car rental fee for this specific car based on working days
					carRental := float64(carStats[i].WorkingDays) * 1433.0

					// VAT calculation
					carVAT := (carBaseRevenue + carRental) * 0.14

					// Total revenue
					carTotal := carBaseRevenue + carRental + carVAT

					carStats[i].TotalRevenue = carBaseRevenue
					carStats[i].CarRental = carRental
					carStats[i].VAT = carVAT
					carStats[i].TotalWithVAT = carTotal
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}
	case "Petromin":
		// Get grouped trips based on car capacity logic
		groupedTrips := h.groupPetrominTripsByCapacity(company, startDate, endDate)

		// Group the trip groups by terminal
		terminalGroups := make(map[string][][]Models.TripStruct)
		for _, tripGroup := range groupedTrips {
			if len(tripGroup) > 0 {
				terminal := tripGroup[0].Terminal
				terminalGroups[terminal] = append(terminalGroups[terminal], tripGroup)
			}
		}

		// Process each terminal
		for terminal, groups := range terminalGroups {
			var totalTrips int64 = 0
			var totalVolume float64 = 0
			var revenueTotalDistance float64 = 0 // Used for both display AND billing
			var distinctCars = make(map[string]bool)
			var workingDaysSet = make(map[string]bool)

			// Calculate stats from all groups in this terminal
			for _, tripGroup := range groups {
				groupTrips, groupVolume, maxDistance, _ := h.calculateGroupStats(tripGroup)

				totalTrips += groupTrips
				totalVolume += groupVolume
				revenueTotalDistance += maxDistance // Sum of maximum distances (used for both display and revenue)

				// Track distinct cars and working days
				for _, trip := range tripGroup {
					distinctCars[trip.CarNoPlate] = true
					workingDaysSet[trip.Date] = true
				}
			}

			// Calculate working days for this terminal
			var terminalWorkingDays []struct {
				CarNoPlate string
				Date       string
			}

			h.DB.Raw(`
			SELECT DISTINCT car_no_plate, date
			FROM trips
			WHERE company = ? AND deleted_at IS NULL
			AND terminal = ?
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
			ORDER BY car_no_plate, date
		`, company, terminal, startDate, startDate, endDate, endDate).Scan(&terminalWorkingDays)

			// Fixed rate per km for all terminals (42.5)
			ratePerKm := 42.5

			// Calculate base revenue using revenue distance
			baseRevenue := revenueTotalDistance * ratePerKm
			carRentalFee := float64(len(terminalWorkingDays)) * 2000.0
			vat := (baseRevenue + carRentalFee) * 0.14
			totalRevenue := baseRevenue + carRentalFee + vat

			routeStat := RouteRevenueStats{
				RouteName:     terminal,
				Terminal:      terminal,
				RouteType:     "terminal",
				TotalTrips:    totalTrips,
				TotalVolume:   totalVolume,
				TotalDistance: revenueTotalDistance, // Show revenue distance (same as used for calculation)
				Fee:           ratePerKm,
			}

			if hasFinancialAccess {
				routeStat.TotalRevenue = baseRevenue
				routeStat.CarRental = carRentalFee
				routeStat.VAT = vat
				routeStat.TotalWithVAT = totalRevenue
			}

			// Get car-specific stats for this terminal using grouped approach
			var carStats []CarStats
			carStatsMap := make(map[string]*CarStats)

			for _, tripGroup := range groups {
				if len(tripGroup) == 0 {
					continue
				}

				carPlate := tripGroup[0].CarNoPlate
				groupTrips, groupVolume, maxDistance, groupWorkingDays := h.calculateGroupStats(tripGroup)

				// Initialize car stats if not exists
				if carStatsMap[carPlate] == nil {
					carStatsMap[carPlate] = &CarStats{
						CarNoPlate: carPlate,
					}
				}

				// Accumulate stats for this car
				carStatsMap[carPlate].TotalTrips += groupTrips
				carStatsMap[carPlate].TotalVolume += groupVolume
				carStatsMap[carPlate].TotalDistance += maxDistance // Use max distance (same as revenue calculation)
				carStatsMap[carPlate].WorkingDays += groupWorkingDays
			}

			// Calculate revenue for each car
			for _, carStat := range carStatsMap {
				if hasFinancialAccess {
					// Base revenue from distance (using the same distance shown)
					carBaseRevenue := carStat.TotalDistance * ratePerKm

					// Car rental fee for this specific car based on working days
					carRental := float64(carStat.WorkingDays) * 2000.0

					// VAT calculation
					carVAT := (carBaseRevenue + carRental) * 0.14

					// Total revenue
					carTotal := carBaseRevenue + carRental + carVAT

					carStat.TotalRevenue = carBaseRevenue
					carStat.CarRental = carRental
					carStat.VAT = carVAT
					carStat.TotalWithVAT = carTotal
				}

				carStats = append(carStats, *carStat)
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}
	case "Watanya":
		// For Watanya, we group by fee category
		var routeData []struct {
			Fee           float64
			TotalTrips    int64
			TotalVolume   float64
			TotalDistance float64
		}

		h.DB.Raw(`
			SELECT 
				f.fee, 
				COUNT(*) as total_trips, 
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
		`, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		// Process each fee category route
		for _, route := range routeData {
			var ratePerVolume float64

			switch int(route.Fee) {
			case 1:
				ratePerVolume = 82.5
			case 2:
				ratePerVolume = 104.5
			case 3:
				ratePerVolume = 126.5
			case 4:
				ratePerVolume = 148.5
			case 5:
				ratePerVolume = 170.5
			default:
				ratePerVolume = 0 // Default if unknown fee
			}

			// Base revenue from volume
			baseRevenue := route.TotalVolume * ratePerVolume / 1000

			// Calculate 14% VAT
			vat := baseRevenue * 0.14

			// Total revenue including VAT
			totalRevenue := baseRevenue + vat

			routeStat := RouteRevenueStats{
				RouteName:     fmt.Sprintf("Fee Category %d", int(route.Fee)),
				RouteType:     "fee",
				FeeCategory:   int(route.Fee),
				TotalTrips:    route.TotalTrips,
				TotalVolume:   route.TotalVolume,
				TotalDistance: route.TotalDistance,
				Fee:           route.Fee,
			}

			if hasFinancialAccess {
				routeStat.TotalRevenue = baseRevenue
				routeStat.VAT = vat
				routeStat.TotalWithVAT = totalRevenue
			}

			// Get car-specific stats for this fee category
			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(f.distance), 0) as total_distance,
					COUNT(DISTINCT t.date) as working_days
				FROM trips t
				LEFT JOIN fee_mappings f 
					ON t.company = f.company 
					AND t.terminal = f.terminal 
					AND t.drop_off_point = f.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND f.fee = ?
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY t.car_no_plate
			`, company, route.Fee, startDate, startDate, endDate, endDate).Scan(&carStats)

			// Calculate revenue for each car
			for i := range carStats {
				if hasFinancialAccess {
					// Base revenue from volume
					carBaseRevenue := carStats[i].TotalVolume * ratePerVolume / 1000

					// VAT calculation
					carVAT := carBaseRevenue * 0.14

					// Total revenue
					carTotal := carBaseRevenue + carVAT

					carStats[i].TotalRevenue = carBaseRevenue
					carStats[i].VAT = carVAT
					carStats[i].TotalWithVAT = carTotal
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}

	default:
		// For other companies, use terminal + drop-off point pairs
		var routeData []struct {
			Terminal      string
			DropOffPoint  string
			TotalTrips    int64
			TotalVolume   float64
			TotalDistance float64
			AvgFee        float64
		}

		h.DB.Raw(`
			SELECT 
				t.terminal, 
				t.drop_off_point, 
				COUNT(*) as total_trips, 
				COALESCE(SUM(t.tank_capacity), 0) as total_volume,
				COALESCE(SUM(fm.distance), 0) as total_distance,
				COALESCE(AVG(fm.fee), 50) as avg_fee
			FROM trips t
			LEFT JOIN fee_mappings fm 
				ON t.company = fm.company 
				AND t.terminal = fm.terminal 
				AND t.drop_off_point = fm.drop_off_point
			WHERE t.company = ? AND t.deleted_at IS NULL
			AND (t.date >= ? OR ? = '')
			AND (t.date <= ? OR ? = '')
			GROUP BY t.terminal, t.drop_off_point
		`, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		// Process each route
		for _, route := range routeData {
			// Use simple revenue calculation with average fee
			revenue := route.TotalVolume * route.AvgFee

			routeStat := RouteRevenueStats{
				RouteName:     fmt.Sprintf("%s to %s", route.Terminal, route.DropOffPoint),
				Terminal:      route.Terminal,
				DropOffPoint:  route.DropOffPoint,
				RouteType:     "terminal-dropoff",
				TotalTrips:    route.TotalTrips,
				TotalVolume:   route.TotalVolume,
				TotalDistance: route.TotalDistance,
				Fee:           route.AvgFee,
			}

			if hasFinancialAccess {
				routeStat.TotalRevenue = revenue
				routeStat.TotalWithVAT = revenue // No VAT
			}

			// Get car-specific stats for this route
			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					COUNT(*) as total_trips, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT t.date) as working_days
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND t.terminal = ? AND t.drop_off_point = ?
				AND (t.date >= ? OR ? = '')
				AND (t.date <= ? OR ? = '')
				GROUP BY t.car_no_plate
			`, company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate).Scan(&carStats)

			// Calculate revenue for each car
			for i := range carStats {
				if hasFinancialAccess {
					carStats[i].TotalRevenue = route.AvgFee * carStats[i].TotalVolume
					carStats[i].TotalWithVAT = carStats[i].TotalRevenue // No VAT
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}
	}

	return routeStats
}

func (h *TripHandler) GetTripStatsByTime(StartDate, EndDate, CompanyFilter string, hasFinancialAccess bool) []TripRevenueDateResponse {
	var output []TripRevenueDateResponse

	// Base query
	query := h.DB.Model(&Models.TripStruct{})

	// Apply date filters if provided
	if StartDate != "" && EndDate != "" {
		query = query.Where("date >= ? AND date <= ?", StartDate, EndDate)
	}

	// Apply company filter if provided
	if CompanyFilter != "" {
		query = query.Where("company = ?", CompanyFilter)
	}

	// Get all distinct dates in the range
	var dates []string
	if err := query.Distinct("date").Order("date ASC").Pluck("date", &dates).Error; err != nil {
		// Return empty result if there's an error
		return output
	}

	// For each date, calculate revenue statistics
	for _, date := range dates {
		// Create a response struct for this date
		dateStats := TripRevenueDateResponse{
			Date: date,
		}

		// Get all companies for this date
		var companies []string
		dateQuery := h.DB.Model(&Models.TripStruct{}).Where("date = ?", date)

		// Apply company filter if provided
		if CompanyFilter != "" {
			dateQuery = dateQuery.Where("company = ?", CompanyFilter)
		}

		if err := dateQuery.Distinct("company").Pluck("company", &companies).Error; err != nil {
			continue // Skip this date if there's an error
		}

		// If we're filtering for a specific company and it doesn't have trips on this date, skip
		if CompanyFilter != "" && len(companies) == 0 {
			continue
		}

		// Initialize company details slice
		dateStats.CompanyDetails = make([]CompanyRevenueDetails, 0, len(companies))
		dateStats.TotalTrips = 0
		dateStats.TotalVolume = 0
		dateStats.TotalDistance = 0
		dateStats.TotalRevenue = 0

		// For each company on this date, calculate statistics
		for _, company := range companies {
			// Query for this company on this date
			companyDateQuery := h.DB.Model(&Models.TripStruct{}).
				Where("date = ?", date).
				Where("company = ?", company)

			// Initialize company details
			companyDetail := CompanyRevenueDetails{
				Company: company,
			}

			// Get trips count
			var tripCount int64
			companyDateQuery.Count(&tripCount)
			companyDetail.TotalTrips = tripCount
			dateStats.TotalTrips += tripCount

			// Get total volume
			var volume float64
			companyDateQuery.Select("COALESCE(SUM(tank_capacity), 0)").Row().Scan(&volume)
			companyDetail.TotalVolume = volume
			dateStats.TotalVolume += volume

			// Calculate distance by joining with fee_mappings
			var distance float64
			h.DB.Raw(`
				SELECT COALESCE(SUM(fm.distance), 0) as total_distance
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.date = ? AND t.deleted_at IS NULL
			`, company, date).Row().Scan(&distance)
			companyDetail.TotalDistance = distance
			dateStats.TotalDistance += distance

			// Calculate revenue based on company-specific logic (similar to GetTripStatistics)
			if hasFinancialAccess {

				var revenue float64 = 0

				switch company {
				case "Petrol Arrows":
					// For Petrol Arrows, revenue is based on fee * volume / 1000
					h.DB.Raw(`
					SELECT COALESCE(SUM(fm.fee * t.tank_capacity / 1000), 0) as total_revenue
					FROM trips t
					LEFT JOIN fee_mappings fm 
						ON t.company = fm.company 
						AND t.terminal = fm.terminal 
						AND t.drop_off_point = fm.drop_off_point
					WHERE t.company = ? AND t.date = ? AND t.deleted_at IS NULL
				`, company, date).Row().Scan(&revenue)

				case "TAQA":
					// For TAQA, revenue is based on distance * rate per km + car rental
					// Calculate distance revenue
					var baseRevenue float64 = 0
					h.DB.Raw(`
					SELECT 
						COALESCE(SUM(
							CASE 
								WHEN t.terminal = 'Alex' THEN fm.distance * 40.7
								WHEN t.terminal = 'Suez' THEN fm.distance * 38.2
								ELSE 0
							END
						), 0) as base_revenue
					FROM trips t
					LEFT JOIN fee_mappings fm 
						ON t.company = fm.company 
						AND t.terminal = fm.terminal 
						AND t.drop_off_point = fm.drop_off_point
					WHERE t.company = ? AND t.date = ? AND t.deleted_at IS NULL
				`, company, date).Row().Scan(&baseRevenue)

					// Calculate car rental fee
					var carCount int64
					h.DB.Model(&Models.TripStruct{}).
						Where("company = ? AND date = ?", company, date).
						Distinct("car_id").
						Count(&carCount)

					carRentalFee := float64(carCount) * 1433.0

					// Calculate VAT
					vat := (baseRevenue + carRentalFee) * 0.14

					// Total revenue includes base revenue, car rental, and VAT
					revenue = baseRevenue + carRentalFee + vat
				case "Petromin":
					// Get all trips for this company and date
					var dayTrips []Models.TripStruct
					h.DB.Where("company = ? AND date = ? AND deleted_at IS NULL", company, date).
						Order("car_no_plate ASC, receipt_no ASC").Find(&dayTrips)

					// Group trips for this specific date
					dayGroupedTrips := h.groupTripsForSingleDate(dayTrips)

					// Calculate revenue using grouped trips approach
					var baseRevenue float64 = 0
					for _, tripGroup := range dayGroupedTrips {
						maxDistance := h.getMaxDistanceFromTripGroup(tripGroup)
						baseRevenue += maxDistance * 42.5 // Only count the furthest distance per group
					}

					// Calculate car rental fee - count distinct cars for this date
					var carCount int64
					h.DB.Model(&Models.TripStruct{}).
						Where("company = ? AND date = ?", company, date).
						Distinct("car_no_plate").
						Count(&carCount)

					carRentalFee := float64(carCount) * 2000.0

					// Calculate VAT
					vat := (baseRevenue + carRentalFee) * 0.14

					// Total revenue includes base revenue, car rental, and VAT
					revenue = baseRevenue + carRentalFee + vat
				case "Watanya":
					// For Watanya, revenue is based on volume and fee category
					h.DB.Raw(`
					SELECT COALESCE(SUM(
						CASE 
							WHEN fm.fee = 1 THEN t.tank_capacity * 82.5 / 1000
							WHEN fm.fee = 2 THEN t.tank_capacity * 104.5 / 1000
							WHEN fm.fee = 3 THEN t.tank_capacity * 126.5 / 1000
							WHEN fm.fee = 4 THEN t.tank_capacity * 148.5 / 1000
							WHEN fm.fee = 5 THEN t.tank_capacity * 170.5 / 1000
							ELSE 0
						END
					), 0) as total_revenue
					FROM trips t
					LEFT JOIN fee_mappings fm 
						ON t.company = fm.company 
						AND t.terminal = fm.terminal 
						AND t.drop_off_point = fm.drop_off_point
					WHERE t.company = ? AND t.date = ? AND t.deleted_at IS NULL
				`, company, date).Row().Scan(&revenue)
					revenue *= 1.14
				}

				companyDetail.TotalRevenue = revenue
				dateStats.TotalRevenue += revenue

				// Add company detail to date stats
				dateStats.CompanyDetails = append(dateStats.CompanyDetails, companyDetail)
			}
		}

		// Add date stats to output
		output = append(output, dateStats)
	}

	return output
}

// Helper function to group Petromin trips by car when tank_capacity < car.TankCapacity
func (h *TripHandler) groupPetrominTripsByCapacity(company, startDate, endDate string) [][]Models.TripStruct {
	// Get all Petromin trips ordered by car_no_plate and receipt_no
	var trips []Models.TripStruct
	query := h.DB.Where("company = ? AND deleted_at IS NULL", company).
		Order("car_no_plate ASC, receipt_no ASC")

	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	}

	query.Find(&trips)

	if len(trips) == 0 {
		return nil
	}

	// Get car tank capacities from Car model
	var carPlates []string
	for _, trip := range trips {
		carPlates = append(carPlates, trip.CarNoPlate)
	}

	var cars []Models.Car
	h.DB.Where("car_no_plate IN ?", carPlates).Find(&cars)

	carCapacities := make(map[string]int)
	for _, car := range cars {
		carCapacities[car.CarNoPlate] = car.TankCapacity
	}

	// Group trips by car first
	tripsByCar := make(map[string][]Models.TripStruct)
	for _, trip := range trips {
		tripsByCar[trip.CarNoPlate] = append(tripsByCar[trip.CarNoPlate], trip)
	}

	var allGroups [][]Models.TripStruct

	// Process each car's trips
	for carPlate, carTrips := range tripsByCar {
		carTankCapacity, carExists := carCapacities[carPlate]

		// If we don't have the car info, treat each trip individually
		if !carExists || carTankCapacity == 0 {
			for _, trip := range carTrips {
				allGroups = append(allGroups, []Models.TripStruct{trip})
			}
			continue
		}

		// Group consecutive trips for this car
		i := 0
		for i < len(carTrips) {
			currentGroup := []Models.TripStruct{}
			totalTankCapacity := 0

			// If the first trip already equals or exceeds car capacity, it's a standalone trip
			if carTrips[i].TankCapacity >= carTankCapacity {
				allGroups = append(allGroups, []Models.TripStruct{carTrips[i]})
				i++
				continue
			}

			// Collect consecutive trips until we reach car tank capacity
			for i < len(carTrips) {
				trip := carTrips[i]

				// If adding this trip would exceed car capacity and we already have trips, stop
				if totalTankCapacity+trip.TankCapacity > carTankCapacity && len(currentGroup) > 0 {
					break
				}

				currentGroup = append(currentGroup, trip)
				totalTankCapacity += trip.TankCapacity
				i++

				// If we've reached exactly the car capacity, this group is complete
				if totalTankCapacity == carTankCapacity {
					break
				}
			}

			// Add the group if it has trips
			if len(currentGroup) > 0 {
				allGroups = append(allGroups, currentGroup)
			}
		}
	}

	return allGroups
}

func (h *TripHandler) groupTripsForSingleDate(trips []Models.TripStruct) [][]Models.TripStruct {
	if len(trips) == 0 {
		return nil
	}

	// Get car tank capacities from Car model
	var carPlates []string
	for _, trip := range trips {
		carPlates = append(carPlates, trip.CarNoPlate)
	}

	var cars []Models.Car
	h.DB.Where("car_no_plate IN ?", carPlates).Find(&cars)

	carCapacities := make(map[string]int)
	for _, car := range cars {
		carCapacities[car.CarNoPlate] = car.TankCapacity
	}

	// Group trips by car
	tripsByCar := make(map[string][]Models.TripStruct)
	for _, trip := range trips {
		tripsByCar[trip.CarNoPlate] = append(tripsByCar[trip.CarNoPlate], trip)
	}

	var allGroups [][]Models.TripStruct

	// Process each car's trips
	for carPlate, carTrips := range tripsByCar {
		carTankCapacity, carExists := carCapacities[carPlate]

		// If we don't have the car info, treat each trip individually
		if !carExists || carTankCapacity == 0 {
			for _, trip := range carTrips {
				allGroups = append(allGroups, []Models.TripStruct{trip})
			}
			continue
		}

		// Group consecutive trips for this car
		i := 0
		for i < len(carTrips) {
			currentGroup := []Models.TripStruct{}
			totalTankCapacity := 0

			// If the first trip already equals or exceeds car capacity, it's a standalone trip
			if carTrips[i].TankCapacity >= carTankCapacity {
				allGroups = append(allGroups, []Models.TripStruct{carTrips[i]})
				i++
				continue
			}

			// Collect consecutive trips until we reach car tank capacity
			for i < len(carTrips) {
				trip := carTrips[i]

				// If adding this trip would exceed car capacity and we already have trips, stop
				if totalTankCapacity+trip.TankCapacity > carTankCapacity && len(currentGroup) > 0 {
					break
				}

				currentGroup = append(currentGroup, trip)
				totalTankCapacity += trip.TankCapacity
				i++

				// If we've reached exactly the car capacity, this group is complete
				if totalTankCapacity == carTankCapacity {
					break
				}
			}

			// Add the group if it has trips
			if len(currentGroup) > 0 {
				allGroups = append(allGroups, currentGroup)
			}
		}
	}

	return allGroups
}

// Helper function to get the maximum distance from a group of trips
func (h *TripHandler) getMaxDistanceFromTripGroup(tripGroup []Models.TripStruct) float64 {
	if len(tripGroup) == 0 {
		return 0
	}

	var maxDistance float64 = 0

	for _, trip := range tripGroup {
		// Get distance from fee mapping
		var mapping Models.FeeMapping
		err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trip.Company, trip.Terminal, trip.DropOffPoint).First(&mapping).Error

		if err == nil && mapping.Distance > maxDistance {
			maxDistance = mapping.Distance
		}
	}

	return maxDistance
}

// Helper function to calculate group statistics
func (h *TripHandler) calculateGroupStats(tripGroup []Models.TripStruct) (int64, float64, float64, int64) {
	if len(tripGroup) == 0 {
		return 0, 0, 0, 0
	}

	var totalTrips int64 = int64(len(tripGroup))
	var totalVolume float64 = 0
	var maxDistance float64 = h.getMaxDistanceFromTripGroup(tripGroup)
	var uniqueDates = make(map[string]bool)

	for _, trip := range tripGroup {
		totalVolume += float64(trip.TankCapacity)
		uniqueDates[trip.Date] = true
	}

	workingDays := int64(len(uniqueDates))

	return totalTrips, totalVolume, maxDistance, workingDays
}

// Updated Petromin case for GetTripStatistics function

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
			var totalCarRentalFee float64 = float64(len(carWorkingDays)) * 1433.0

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
					ratePerKm = 40.7
				} else if stat.Terminal == "Suez" {
					ratePerKm = 38.2
				} else {
					ratePerKm = 0 // Default if unknown terminal
				}

				// Base revenue from distance
				baseRevenue := stat.TotalDistance * ratePerKm

				// Get the terminal's portion of car rental fee
				// Proportionally based on the number of car-days
				carRentalFee := 0.0
				if terminalCarDays[stat.Terminal] > 0 {
					carRentalFee = float64(terminalCarDays[stat.Terminal]) * 1433.0
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
		case "Petromin":
			// Get grouped trips based on car capacity logic
			groupedTrips := h.groupPetrominTripsByCapacity(company, startDate, endDate)

			// Calculate REVENUE DISTANCE (max distance per group for billing AND display)
			var revenueDistance float64 = 0
			for _, tripGroup := range groupedTrips {
				maxDistance := h.getMaxDistanceFromTripGroup(tripGroup)
				revenueDistance += maxDistance
			}

			// Calculate total car rental fee based on actual working days
			var allWorkingDays []struct {
				CarNoPlate string
				Date       string
			}

			h.DB.Raw(`
		SELECT DISTINCT car_no_plate, date
		FROM trips
		WHERE company = ? AND deleted_at IS NULL
		AND (date >= ? OR ? = '')
		AND (date <= ? OR ? = '')
		ORDER BY car_no_plate, date
	`, company, startDate, startDate, endDate, endDate).Scan(&allWorkingDays)

			var totalCarRentalFee float64 = float64(len(allWorkingDays)) * 2000.0

			// Group stats by terminal
			terminalStats := make(map[string]*struct {
				Terminal        string
				TotalTrips      int64
				TotalVolume     float64
				RevenueDistance float64 // Used for both display AND billing
				DistinctCars    map[string]bool
				WorkingDays     map[string]bool
			})

			// Process each grouped trip
			for _, tripGroup := range groupedTrips {
				if len(tripGroup) == 0 {
					continue
				}

				// Use the first trip's terminal as the group's terminal
				terminal := tripGroup[0].Terminal
				carPlate := tripGroup[0].CarNoPlate

				// Initialize terminal stats if needed
				if terminalStats[terminal] == nil {
					terminalStats[terminal] = &struct {
						Terminal        string
						TotalTrips      int64
						TotalVolume     float64
						RevenueDistance float64
						DistinctCars    map[string]bool
						WorkingDays     map[string]bool
					}{
						Terminal:     terminal,
						DistinctCars: make(map[string]bool),
						WorkingDays:  make(map[string]bool),
					}
				}

				// Calculate group metrics
				totalTrips, totalVolume, maxDistance, _ := h.calculateGroupStats(tripGroup)

				// Update terminal stats
				terminalStats[terminal].TotalTrips += totalTrips
				terminalStats[terminal].TotalVolume += totalVolume
				terminalStats[terminal].RevenueDistance += maxDistance // Only max distance (used for both display and revenue)

				// Track distinct cars and working days
				terminalStats[terminal].DistinctCars[carPlate] = true
				for _, trip := range tripGroup {
					terminalStats[terminal].WorkingDays[trip.Date] = true
				}
			}

			// Calculate car working days per terminal for rental fee distribution
			terminalCarDays := make(map[string]int)
			for terminal := range terminalStats {
				var terminalWorkingDays []struct {
					CarNoPlate string
					Date       string
				}

				h.DB.Raw(`
			SELECT DISTINCT car_no_plate, date
			FROM trips
			WHERE company = ? AND deleted_at IS NULL
			AND terminal = ?
			AND (date >= ? OR ? = '')
			AND (date <= ? OR ? = '')
			ORDER BY car_no_plate, date
		`, company, terminal, startDate, startDate, endDate, endDate).Scan(&terminalWorkingDays)

				terminalCarDays[terminal] = len(terminalWorkingDays)
			}

			// Calculate details and total revenue
			companyStats.Details = make([]TripStatisticsDetails, 0, len(terminalStats))
			companyStats.TotalRevenue = 0
			companyStats.TotalVAT = 0
			companyStats.TotalCarRent = totalCarRentalFee
			companyStats.TotalAmount = 0

			// Set the total distance to match revenue calculation (sum of max distances)
			companyStats.TotalDistance = revenueDistance

			for _, stat := range terminalStats {
				// Fixed rate per km for all terminals (42.5)
				ratePerKm := 42.5

				// Base revenue from revenue distance
				baseRevenue := stat.RevenueDistance * ratePerKm

				// Car rental fee for this terminal
				carRentalFee := 0.0
				if terminalCarDays[stat.Terminal] > 0 {
					carRentalFee = float64(terminalCarDays[stat.Terminal]) * 2000.0
				}

				// Calculate 14% VAT on the base revenue and car rental
				vat := (baseRevenue + carRentalFee) * 0.14

				// Total revenue including VAT
				totalRevenue := baseRevenue + carRentalFee + vat

				detail := TripStatisticsDetails{
					GroupName:     stat.Terminal,
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.RevenueDistance, // Show revenue distance (same as used for calculation)
					TotalRevenue:  baseRevenue,
					CarRental:     carRentalFee,
					VAT:           vat,
					TotalWithVAT:  totalRevenue,
					Fee:           ratePerKm,
					DistinctCars:  int64(len(stat.DistinctCars)),
					DistinctDays:  int64(len(stat.WorkingDays)),
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
					companyStats.TotalVAT += vat
					companyStats.TotalAmount += totalRevenue
				}
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
					ratePerVolume = 82.5
				case 2:
					ratePerVolume = 104.5
				case 3:
					ratePerVolume = 126.5
				case 4:
					ratePerVolume = 148.5
				case 5:
					ratePerVolume = 170.5
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
		routeStats := h.GetTripStatsByRoute(company, startDate, endDate, hasFinancialAccess)
		companyStats.RouteDetails = routeStats
		// Add to the response array
		statistics = append(statistics, companyStats)
	}
	statsByDate := h.GetTripStatsByTime(startDate, endDate, companyFilter, hasFinancialAccess)
	carTotals := GetCarTotals(statistics)
	// Add a flag to inform frontend whether financial data is visible
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":            "Trip statistics retrieved successfully",
		"data":               statistics,
		"statsByDate":        statsByDate,
		"hasFinancialAccess": hasFinancialAccess,
		"carTotals":          carTotals,
	})
}

func GetCarTotals(statistics []TripStatistics) []CarTotal {
	// Create a map to aggregate data by car number plate
	carTotalsMap := make(map[string]*CarTotal)

	// Iterate through all statistics
	for _, statistic := range statistics {
		// Access the route details which contain car information
		for _, routeDetail := range statistic.RouteDetails {
			// Process each car in the route
			for _, car := range routeDetail.Cars {
				// Check if we already have an entry for this car
				carTotal, exists := carTotalsMap[car.CarNoPlate]

				if !exists {
					// If not, create a new CarTotal
					carTotal = &CarTotal{
						CarNoPlate: car.CarNoPlate,
					}
					carTotalsMap[car.CarNoPlate] = carTotal
				}

				// Aggregate the data
				carTotal.Liters += car.TotalVolume // Assuming Liters corresponds to TotalVolume
				carTotal.Distance += car.TotalDistance
				carTotal.BaseRevenue += car.TotalRevenue

				// Add VAT if available
				if car.VAT > 0 {
					carTotal.VAT += car.VAT
				}

				// Add Rent if available (assuming car rental corresponds to rent)
				if car.CarRental > 0 {
					carTotal.Rent += car.CarRental
				}
			}
		}
	}

	// Convert the map to a slice
	result := make([]CarTotal, 0, len(carTotalsMap))
	for _, carTotal := range carTotalsMap {
		result = append(result, *carTotal)
	}

	return result
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
			Date    string  `json:"date"`
			Count   int64   `json:"count"`
			Revenue float64 `json:"revenue"`
		} `json:"activity_heatmap"`
		RouteDistribution []struct {
			Terminal     string
			DropOffPoint string
			Count        int64   `json:"count"`
			Distance     float64 `json:"distance"`
			Percent      float64 `json:"percent"` // Percentage of driver's total trips
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
		Revenue float64
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

		// Fetch route distribution for the driver
		var routeDistribution []struct {
			Terminal     string
			DropOffPoint string
			Count        int64   `json:"count"`
			Distance     float64 `json:"distance"`
			Percent      float64 `json:"percent"` // Percentage of driver's total trips
		}

		err := h.DB.Raw(`
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
		}
		driverAnalytics.RouteDistribution = routeDistribution

		// Prepare activity and revenue tracking
		var activityHeatmap []struct {
			Date    string  `json:"date"`
			Count   int64   `json:"count"`
			Revenue float64 `json:"revenue"`
		}

		var totalTrips int64 = 0
		var totalRevenue float64 = 0
		var totalDistance float64 = 0
		var totalVolume float64 = 0
		var totalFees float64 = 0
		var workingDays int64 = 0

		// Track daily route revenues
		dailyRouteRevenuesMap := make(map[string]float64)
		dailyTripsMap := make(map[string]int64)

		// Process each route for the driver
		for _, route := range routeDistribution {
			// Get fee mapping for this route
			var feeMapping Models.FeeMapping
			err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
				"Watanya", route.Terminal, route.DropOffPoint).
				First(&feeMapping).Error

			if err != nil {
				log.Printf("No fee mapping found for route %s → %s", route.Terminal, route.DropOffPoint)
				continue
			}

			// Determine fee amount based on fee index
			var feeAmount float64
			switch int64(feeMapping.Fee) {
			case 1:
				feeAmount = 82.5
			case 2:
				feeAmount = 104.5
			case 3:
				feeAmount = 126.5
			case 4:
				feeAmount = 148.5
			case 5:
				feeAmount = 170.5
			default:
				feeAmount = 0.0
			}
			feeAmount *= 1.14
			// Get daily route details
			var dailyRouteDetails []struct {
				Date     string
				Count    int64
				Volume   float64
				Distance float64
			}

			err = h.DB.Raw(`
				SELECT 
					date, 
					COUNT(*) as count, 
					COALESCE(SUM(tank_capacity), 0) as volume,
					? * COUNT(*) as distance
				FROM trips
				WHERE company = 'Watanya' 
				AND driver_name = ? 
				AND terminal = ? 
				AND drop_off_point = ? 
				AND deleted_at IS NULL
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
				GROUP BY date
			`, feeMapping.Distance, driverName, route.Terminal, route.DropOffPoint,
				startDate, startDate, endDate, endDate).
				Find(&dailyRouteDetails).Error

			if err != nil {
				log.Printf("Error fetching daily route details: %v", err)
				continue
			}

			// Process daily route details
			for _, daily := range dailyRouteDetails {
				// Calculate route revenue
				routeRevenue := (feeAmount * daily.Volume) / 1000.0

				// Aggregate daily data
				dailyRouteRevenuesMap[daily.Date] += routeRevenue
				dailyTripsMap[daily.Date] += daily.Count
				totalTrips += daily.Count
				totalVolume += daily.Volume
				totalDistance += daily.Distance
				totalFees += routeRevenue
				totalRevenue += routeRevenue
			}
		}

		// Convert daily revenues to activity heatmap
		for date, revenue := range dailyRouteRevenuesMap {
			activityHeatmap = append(activityHeatmap, struct {
				Date    string  `json:"date"`
				Count   int64   `json:"count"`
				Revenue float64 `json:"revenue"`
			}{
				Date:    date,
				Count:   dailyTripsMap[date],
				Revenue: revenue,
			})

			// Count working days
			if dailyTripsMap[date] > 0 {
				workingDays++
			}
		}

		// Sort activity heatmap by date
		sort.Slice(activityHeatmap, func(i, j int) bool {
			return activityHeatmap[i].Date < activityHeatmap[j].Date
		})

		// Populate driver analytics
		driverAnalytics.ActivityHeatmap = activityHeatmap
		driverAnalytics.TotalTrips = totalTrips
		driverAnalytics.TotalDistance = totalDistance
		driverAnalytics.TotalVolume = totalVolume
		driverAnalytics.TotalFees = totalFees
		driverAnalytics.TotalRevenue = totalRevenue
		driverAnalytics.WorkingDays = workingDays

		// Calculate averages
		if workingDays > 0 {
			driverAnalytics.AvgTripsPerDay = float64(totalTrips) / float64(workingDays)
			driverAnalytics.AvgKmPerDay = totalDistance / float64(workingDays)
			driverAnalytics.AvgFeesPerDay = totalFees / float64(workingDays)
		}

		if totalDistance > 0 {
			driverAnalytics.AvgTripsPerKm = float64(totalTrips) / totalDistance
			driverAnalytics.AvgVolumePerKm = totalVolume / totalDistance
		}

		// Update global totals
		globalTotalTrips += totalTrips
		globalTotalDistance += totalDistance
		globalTotalVolume += totalVolume
		globalTotalFees += totalFees
		globalTotalRevenue += totalRevenue
		globalTotalWorkingDays += workingDays

		// Add to driver performance tracking
		driverPerformance = append(driverPerformance, DriverPerformance{
			Name:    driverName,
			Revenue: totalFees,
		})

		// Clear financial data if no access
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
		response.GlobalStats.AvgRevenuePerDay = globalTotalFees / float64(globalTotalWorkingDays)
	} else {
		response.GlobalStats.AvgTripsPerDay = 0
		response.GlobalStats.AvgRevenuePerDay = 0
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
		response.GlobalStats.TotalRevenue = globalTotalFees
	}

	// Calculate efficiency score for each driver (compared to average)
	globalAvgRevenuePerDay := response.GlobalStats.AvgRevenuePerDay
	for i := range response.Drivers {
		// Only calculate efficiency for drivers with revenue
		if response.Drivers[i].TotalFees > 0 && globalAvgRevenuePerDay > 0 {
			// Calculate daily revenue and compare to global average
			driverDailyRevenue := response.Drivers[i].TotalFees / float64(response.Drivers[i].WorkingDays)
			response.Drivers[i].Efficiency = driverDailyRevenue / globalAvgRevenuePerDay
		} else {
			// For drivers with no revenue, set efficiency to 0
			response.Drivers[i].Efficiency = 0
		}
	}

	// Sort drivers by performance
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

type GlobalStats struct {
	// Fuel statistics
	TotalFuelEvents      int64   `json:"total_fuel_events"`
	TotalLiters          float64 `json:"total_liters"`
	AveragePricePerLiter float64 `json:"average_price_per_liter"`
	TotalFuelCost        float64 `json:"total_fuel_cost"`

	// Trip statistics
	TotalTrips    int64   `json:"total_trips"`
	TotalRevenue  float64 `json:"total_revenue"`
	TotalMileage  float64 `json:"total_mileage"`
	TotalDistance float64 `json:"total_distance"`

	// Fleet statistics
	UniqueCars     int64  `json:"unique_cars"`
	UniqueDrivers  int64  `json:"unique_drivers"`
	TopTransporter string `json:"top_transporter"`

	// Time statistics
	LastUpdate string `json:"last_update"`

	// Combined efficiency metrics
	RevenuePerLiter float64 `json:"revenue_per_liter"`
	RevenuePerKm    float64 `json:"revenue_per_km"`
	LitersPerKm     float64 `json:"liters_per_km"`
}

func (h *TripHandler) GetGlobalStats(c *fiber.Ctx) error {

	var stats GlobalStats
	stats.LastUpdate = time.Now().Format(time.RFC3339)

	// Get fuel statistics
	h.DB.Model(&Models.FuelEvent{}).Count(&stats.TotalFuelEvents)

	if stats.TotalFuelEvents > 0 {
		h.DB.Model(&Models.FuelEvent{}).Select("SUM(liters) as total_liters").Scan(&stats.TotalLiters)
		h.DB.Model(&Models.FuelEvent{}).Select("SUM(price) as total_fuel_cost").Scan(&stats.TotalFuelCost)

		// Calculate average price per liter
		if stats.TotalLiters > 0 {
			h.DB.Model(&Models.FuelEvent{}).Select("SUM(price) / SUM(liters) as average_price_per_liter").Scan(&stats.AveragePricePerLiter)
		}
	}

	// Get trip statistics
	h.DB.Model(&Models.TripStruct{}).Count(&stats.TotalTrips)

	if stats.TotalTrips > 0 {
		h.DB.Model(&Models.TripStruct{}).Select("SUM(revenue) as total_revenue").Scan(&stats.TotalRevenue)
		h.DB.Model(&Models.TripStruct{}).Select("SUM(mileage) as total_mileage").Scan(&stats.TotalMileage)

		// Since distance is a calculated field (not stored), we need to calculate it
		// This assumes the distance field is populated before saving to DB or there's a way to calculate it
		var trips []Models.TripStruct
		h.DB.Find(&trips)

		for _, trip := range trips {
			stats.TotalDistance += trip.Distance
		}
	}

	// Get fleet statistics
	h.DB.Model(&Models.FuelEvent{}).Distinct("car_id").Count(&stats.UniqueCars)
	h.DB.Model(&Models.TripStruct{}).Distinct("driver_id").Count(&stats.UniqueDrivers)

	// Get top transporter
	type TransporterCount struct {
		Transporter string
		Count       int
	}
	var topTransporter TransporterCount
	h.DB.Model(&Models.FuelEvent{}).
		Select("transporter, COUNT(*) as count").
		Group("transporter").
		Order("count DESC").
		Limit(1).
		Scan(&topTransporter)

	stats.TopTransporter = topTransporter.Transporter

	// Calculate efficiency metrics
	if stats.TotalLiters > 0 {
		stats.RevenuePerLiter = stats.TotalRevenue / stats.TotalLiters
	}

	if stats.TotalDistance > 0 {
		stats.RevenuePerKm = stats.TotalRevenue / stats.TotalDistance
		stats.LitersPerKm = stats.TotalLiters / stats.TotalDistance
	}

	return c.JSON(fiber.Map{
		"success": true,
		"stats":   stats,
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
		1: 82.5,
		2: 104.5,
		3: 126.5,
		4: 148.5,
		5: 170.5,
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

	// Map to store fee mapping info for each route
	routeFeeInfo := make(map[string]struct {
		Distance  float64
		FeeIndex  int
		ActualFee float64
	})

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

		// Determine the fee amount based on the fee index
		actualFee := feeIndexToAmount[int(feeMapping.Fee)]

		// Store fee information for this route (for the detailed sheet)
		routeFeeInfo[key] = struct {
			Distance  float64
			FeeIndex  int
			ActualFee float64
		}{
			Distance:  feeMapping.Distance,
			FeeIndex:  int(feeMapping.Fee),
			ActualFee: actualFee,
		}

		if _, exists := groupedTrips[key]; !exists {
			// Initialize a new summary record if this combination doesn't exist
			groupedTrips[key] = &WatanyaReportSummary{
				DropOffPoint:  trip.DropOffPoint,
				Terminal:      trip.Terminal,
				TripCount:     0,
				TotalCapacity: 0,
				Distance:      feeMapping.Distance,
				Fee:           int(feeMapping.Fee),
				ActualFee:     actualFee,
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

	// Define table headers for detailed trips (removed Transporter, Gas Type, Revenue, Distance)
	tripHeaders := []string{"No", "Date", "Driver Name", "Car No Plate", "Tank Capacity", "Receipt No", "Mileage", "Fee Index", "Fee Amount", "Trip Cost"}

	// Set column widths for detailed sheet to fit all columns in 1 page width
	f.SetColWidth(detailedSheetName, "A", "A", 7)  // No
	f.SetColWidth(detailedSheetName, "B", "B", 13) // Date
	f.SetColWidth(detailedSheetName, "C", "C", 18) // Driver Name
	f.SetColWidth(detailedSheetName, "D", "D", 13) // Car No Plate
	f.SetColWidth(detailedSheetName, "E", "E", 12) // Tank Capacity
	f.SetColWidth(detailedSheetName, "F", "F", 13) // Receipt No
	f.SetColWidth(detailedSheetName, "G", "G", 12) // Mileage
	f.SetColWidth(detailedSheetName, "H", "H", 10) // Fee Index
	f.SetColWidth(detailedSheetName, "I", "I", 12) // Fee Amount
	f.SetColWidth(detailedSheetName, "J", "J", 13) // Trip Cost

	// Add a note for users about print settings
	note := "Note: To print, use 'Fit All Columns on One Page' in Excel's print settings. Each table is about 100 rows long."
	f.SetCellValue(detailedSheetName, "A1", note)
	f.MergeCell(detailedSheetName, "A1", "J1")
	currentRow := 2 // Start tables after the note

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
	currentRow = 2
	pageLimit := 100 // Target rows per page
	rowsOnPage := 0

	for _, summary := range summaries {
		key := fmt.Sprintf("%s|%s", summary.Terminal, summary.DropOffPoint)
		routeTrips, exists := tripsByRoute[key]
		if !exists {
			continue
		}

		feeInfo := routeFeeInfo[key]

		// Calculate how many rows this group will take:
		// 1 (title) + 1 (header) + len(routeTrips) (data) + 1 (total) + 3 (empty rows)
		groupRows := 1 + 1 + len(routeTrips) + 1 + 3

		// If adding this group would exceed the page limit, insert a page break before the group
		if rowsOnPage > 0 && (rowsOnPage+groupRows > pageLimit) {
			f.InsertPageBreak(detailedSheetName, fmt.Sprintf("A%d", currentRow))
			rowsOnPage = 0
		}

		// Set table title - Terminal & Drop-off Point
		tableTitle := fmt.Sprintf("Terminal: %s | Drop-off Point: %s", summary.Terminal, summary.DropOffPoint)
		titleCell := fmt.Sprintf("A%d", currentRow)
		f.SetCellValue(detailedSheetName, titleCell, tableTitle)
		f.MergeCell(detailedSheetName, titleCell, fmt.Sprintf("J%d", currentRow))
		f.SetCellStyle(detailedSheetName, titleCell, titleCell, routeTitleStyle)

		currentRow++
		rowsOnPage++

		// Set table headers
		for i, header := range tripHeaders {
			cell := fmt.Sprintf("%c%d", 'A'+i, currentRow)
			f.SetCellValue(detailedSheetName, cell, header)
		}
		f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
			fmt.Sprintf("%c%d", 'A'+len(tripHeaders)-1, currentRow), tableHeaderStyle)

		currentRow++
		rowsOnPage++

		// Add table data
		tableStartRow := currentRow
		for i, trip := range routeTrips {
			tripCost := float64(trip.TankCapacity) * feeInfo.ActualFee / 1000.0
			f.SetCellValue(detailedSheetName, fmt.Sprintf("A%d", currentRow), i+1)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("B%d", currentRow), trip.Date)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("C%d", currentRow), trip.DriverName)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("D%d", currentRow), trip.CarNoPlate)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("E%d", currentRow), trip.TankCapacity)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("F%d", currentRow), trip.ReceiptNo)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("G%d", currentRow), trip.Mileage)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("H%d", currentRow), feeInfo.FeeIndex)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("I%d", currentRow), feeInfo.ActualFee)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("J%d", currentRow), tripCost)
			f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
				fmt.Sprintf("J%d", currentRow), dataStyle)
			currentRow++
			rowsOnPage++
		}

		// Add table totals
		f.SetCellValue(detailedSheetName, fmt.Sprintf("A%d", currentRow), "TOTAL")
		f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
			fmt.Sprintf("A%d", currentRow), totalStyle)
		f.SetCellFormula(detailedSheetName, fmt.Sprintf("E%d", currentRow),
			fmt.Sprintf("SUM(E%d:E%d)", tableStartRow, currentRow-1))
		f.SetCellFormula(detailedSheetName, fmt.Sprintf("G%d", currentRow),
			fmt.Sprintf("SUM(G%d:G%d)", tableStartRow, currentRow-1))
		f.SetCellFormula(detailedSheetName, fmt.Sprintf("J%d", currentRow),
			fmt.Sprintf("SUM(J%d:J%d)", tableStartRow, currentRow-1))
		f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", currentRow),
			fmt.Sprintf("J%d", currentRow), totalStyle)
		currentRow++
		rowsOnPage++

		// Add 3 empty rows before the next table
		currentRow += 3
		rowsOnPage += 3
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

type RouteResult struct {
	OSRMData      map[string]interface{} `json:"osrm_data"`
	GoogleMapsURL string                 `json:"googe_maps_url"`
}

func getRouteFromOSRM(startLat, startLng, endLat, endLng float64) (*RouteResult, error) {
	// Get route from OSRM
	osrmURL := fmt.Sprintf("http://localhost:5001/route/v1/driving/%f,%f;%f,%f?overview=full&steps=false",
		startLng, startLat, endLng, endLat)

	fmt.Println("OSRM URL:", osrmURL)

	resp, err := http.Get(osrmURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Generate Waze URL from OSRM data
	googleMapsURL := generateGoogleMapsURLFromOSRM(result)

	return &RouteResult{
		OSRMData:      result,
		GoogleMapsURL: googleMapsURL,
	}, nil
}

func decodePolyline(encoded string) [][]float64 {
	var coordinates [][]float64
	index := 0
	length := len(encoded)
	lat := 0
	lng := 0
	for index < length {
		// Decode latitude
		b := 0
		shift := 0
		result := 0
		for {
			if index >= length {
				break
			}
			b = int(encoded[index]) - 63
			index++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}

		var dlat int
		if (result & 1) != 0 {
			dlat = ^(result >> 1)
		} else {
			dlat = result >> 1
		}
		lat += dlat
		// Decode longitude
		shift = 0
		result = 0
		for {
			if index >= length {
				break
			}
			b = int(encoded[index]) - 63
			index++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}

		var dlng int
		if (result & 1) != 0 {
			dlng = ^(result >> 1)
		} else {
			dlng = result >> 1
		}
		lng += dlng
		coordinates = append(coordinates, []float64{
			float64(lat) / 1e5,
			float64(lng) / 1e5,
		})
	}
	return coordinates
}

func generateGoogleMapsURLFromOSRM(osrmData map[string]interface{}) string {
	routes, ok := osrmData["routes"].([]interface{})
	if !ok || len(routes) == 0 {
		return ""
	}

	route := routes[0].(map[string]interface{})
	geometry, ok := route["geometry"].(string)
	if !ok || geometry == "" {
		return ""
	}

	coordinates := decodePolyline(geometry)
	if len(coordinates) < 2 {
		return ""
	}

	start := coordinates[0]
	destination := coordinates[len(coordinates)-1]

	// Use strategic waypoints (fewer, better spaced)
	waypoints := sampleStrategicWaypoints(coordinates, 15) // Use 15 instead of 23

	// Build Google Maps URL
	gmapsURL := fmt.Sprintf("https://www.google.com/maps/dir/%f,%f", start[0], start[1])

	for _, waypoint := range waypoints {
		gmapsURL += fmt.Sprintf("/%f,%f", waypoint[0], waypoint[1])
	}

	gmapsURL += fmt.Sprintf("/%f,%f", destination[0], destination[1])

	return gmapsURL
}

func sampleStrategicWaypoints(coordinates [][]float64, maxWaypoints int) [][]float64 {
	if len(coordinates) <= 2 {
		return [][]float64{}
	}

	intermediateCoords := coordinates[1 : len(coordinates)-1]

	// Use fewer waypoints (10-15) for cleaner routing
	targetWaypoints := maxWaypoints
	if maxWaypoints > 15 {
		targetWaypoints = 15 // Reduce to 15 for cleaner routing
	}

	if len(intermediateCoords) <= targetWaypoints {
		return filterWaypoints(intermediateCoords)
	}

	// Sample waypoints with larger intervals
	var waypoints [][]float64
	step := len(intermediateCoords) / targetWaypoints

	for i := 0; i < targetWaypoints; i++ {
		index := i * step
		if index < len(intermediateCoords) {
			waypoints = append(waypoints, intermediateCoords[index])
		}
	}

	return filterWaypoints(waypoints)
}

func filterWaypoints(waypoints [][]float64) [][]float64 {
	if len(waypoints) == 0 {
		return waypoints
	}

	filtered := [][]float64{waypoints[0]}
	minDistance := 0.002 // ~200 meters minimum distance between waypoints

	for i := 1; i < len(waypoints); i++ {
		lastWaypoint := filtered[len(filtered)-1]
		currentWaypoint := waypoints[i]

		// Calculate distance between waypoints
		latDiff := currentWaypoint[0] - lastWaypoint[0]
		lngDiff := currentWaypoint[1] - lastWaypoint[1]
		distance := latDiff*latDiff + lngDiff*lngDiff // Simple distance squared

		if distance > minDistance*minDistance {
			filtered = append(filtered, currentWaypoint)
		}
	}

	return filtered
}

func (h *TripHandler) GetTripDetails(c *fiber.Ctx) error {
	tripID := c.Params("id")

	// Convert tripID to uint
	id, err := strconv.ParseUint(tripID, 10, 32)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid trip ID",
			"error":   err.Error(),
		})
	}

	// Get the trip
	var trip Models.TripStruct
	result := h.DB.First(&trip, uint(id))
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

	// Verify that the company, terminal, and drop-off point exist in mappings
	var mapping Models.FeeMapping
	result = h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
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

	// Get terminal location
	var Terminal Models.Terminal
	if err := h.DB.Model(&Models.Terminal{}).Where("name = ?", trip.Terminal).First(&Terminal).Error; err != nil {
		log.Printf("Terminal lookup error: %v", err)
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to find terminal location",
			"error":   err.Error(),
		})
	}

	// Add fee mapping data to trip (same as CreateTrip)
	trip.Distance = mapping.Distance
	trip.Fee = mapping.Fee

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip details retrieved successfully",
		"data":    trip,
		"terminal_location": map[string]interface{}{
			"lat": Terminal.Latitude,
			"lng": Terminal.Longitude,
		},
		"drop_off_point_location": map[string]interface{}{
			"lat": mapping.Latitude,
			"lng": mapping.Longitude,
		},
		"route_data": func() map[string]interface{} {
			osrmData, err := getRouteFromOSRM(Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
			if err != nil {
				log.Printf("OSRM error: %v", err)
				// Fallback to simple calculation
				distance := calculateDistance(Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
				return map[string]interface{}{
					"distance": distance,
					"duration": distance * 2 * 60, // rough estimate in seconds
					"geometry": nil,
				}
			}

			if routes, ok := osrmData.OSRMData["routes"].([]interface{}); ok && len(routes) > 0 {
				route := routes[0].(map[string]interface{})
				return map[string]interface{}{
					"distance":        route["distance"].(float64) / 1000, // Convert to km
					"duration":        route["duration"].(float64),        // Already in seconds
					"geometry":        route["geometry"],
					"google_maps_url": osrmData.GoogleMapsURL,
				}
			}

			return nil
		}(),
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

	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", trip.CarID).Update("driver_id", trip.DriverID).Error; err != nil {
		log.Println(err)
	}

	var Terminal Models.Terminal
	if err := Models.DB.Model(&Models.Terminal{}).Where("name = ?", trip.Terminal).First(&Terminal).Error; err != nil {
		log.Println(err)
	}

	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"message": "Trip created successfully",
		"data":    trip,
		"terminal_location": map[string]interface{}{
			"lat": Terminal.Latitude,
			"lng": Terminal.Longitude,
		},
		"drop_off_point_location": map[string]interface{}{
			"lat": mapping.Latitude,
			"lng": mapping.Longitude,
		},
		"route_data": func() map[string]interface{} {
			osrmData, err := getRouteFromOSRM(Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
			if err != nil {
				log.Printf("OSRM error: %v", err)
				// Fallback to simple calculation
				distance := calculateDistance(Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
				return map[string]interface{}{
					"distance": distance,
					"duration": distance * 2 * 60, // rough estimate in seconds
					"geometry": nil,
				}
			}

			if routes, ok := osrmData.OSRMData["routes"].([]interface{}); ok && len(routes) > 0 {
				route := routes[0].(map[string]interface{})
				return map[string]interface{}{
					"distance": route["distance"].(float64) / 1000, // Convert to km
					"duration": route["duration"].(float64),        // Already in seconds
					"geometry": route["geometry"],
				}
			}

			return nil
		}(),
	})
}

func calculateDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371 // Earth's radius in kilometers

	// Convert latitude and longitude from degrees to radians
	lat1Rad := lat1 * math.Pi / 180
	lng1Rad := lng1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lng2Rad := lng2 * math.Pi / 180

	// Haversine formula
	dLat := lat2Rad - lat1Rad
	dLng := lng2Rad - lng1Rad

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLng/2)*math.Sin(dLng/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Distance in kilometers
	distance := R * c

	return distance
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
	var Terminal Models.Terminal
	if err := Models.DB.Model(&Models.Terminal{}).Where("name = ?", existingTrip.Terminal).First(&Terminal).Error; err != nil {
		log.Println(err)
	}

	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"message": "Trip updated successfully",
		"data":    existingTrip,
		"terminal_location": map[string]interface{}{
			"lat": Terminal.Latitude,
			"lng": Terminal.Longitude,
		},
		"drop_off_point_location": map[string]interface{}{
			"lat": mapping.Latitude,
			"lng": mapping.Longitude,
		},
		"route_data": func() map[string]interface{} {
			osrmData, err := getRouteFromOSRM(Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
			if err != nil {
				log.Printf("OSRM error: %v", err)
				// Fallback to simple calculation
				distance := calculateDistance(Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
				return map[string]interface{}{
					"distance": distance,
					"duration": distance * 2 * 60, // rough estimate in seconds
					"geometry": nil,
				}
			}

			if routes, ok := osrmData.OSRMData["routes"].([]interface{}); ok && len(routes) > 0 {
				route := routes[0].(map[string]interface{})
				return map[string]interface{}{
					"distance": route["distance"].(float64) / 1000, // Convert to km
					"duration": route["duration"].(float64),        // Already in seconds
					"geometry": route["geometry"],
				}
			}

			return nil
		}(),
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

	// defer func() {
	// 	go func() {
	// 		emailBody := fmt.Sprintf(`
	// 		Trip details:

	// 		Receipt No: %s
	// 		Date: %s
	// 		Company: %s
	// 		Terminal: %s
	// 		Drop-off: %s
	// 		Tank: %d
	// 		Driver: %s
	// 		Car: %s
	// 		Distance: %.2f km
	// 		Fee: $%.2f

	// 		This is an automated message.
	// 		`,
	// 			trip.ReceiptNo,
	// 			trip.Date,
	// 			trip.Company,
	// 			trip.Terminal,
	// 			trip.DropOffPoint,
	// 			trip.TankCapacity,
	// 			trip.DriverName,
	// 			trip.CarNoPlate,
	// 			trip.Distance,
	// 			trip.Fee,
	// 		)
	// 		email.SendEmail(Constants.EmailConfig, Models.EmailMessage{
	// 			To:      []string{"shawket.4@icloud.com", "mohamedeltaef44@gmail.com"},
	// 			Subject: fmt.Sprintf("%s: Receipt No: %s Has Been Deleted", trip.Company, trip.ReceiptNo),
	// 			Body:    emailBody,
	// 			IsHTML:  false,
	// 		})
	// 	}()

	// }()

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
