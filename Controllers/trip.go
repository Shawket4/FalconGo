package Controllers

import (
	"Falcon/Models"
	"Falcon/services"
	"Falcon/utils"
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

// =============================================================================
// GetTripStatsByTime - Daily statistics breakdown
// =============================================================================
func (h *TripHandler) GetTripStatsByTime(StartDate, EndDate, CompanyFilter string, hasFinancialAccess bool) []TripRevenueDateResponse {
	var output []TripRevenueDateResponse

	query := h.DB.Model(&Models.TripStruct{})

	if StartDate != "" && EndDate != "" {
		query = query.Where("date >= ? AND date <= ?", StartDate, EndDate)
	}

	if CompanyFilter != "" {
		query = query.Where("company = ?", CompanyFilter)
	}

	var dates []string
	if err := query.Distinct("date").Order("date ASC").Pluck("date", &dates).Error; err != nil {
		return output
	}
	log.Println("sssssss")
	log.Println(dates)

	for _, date := range dates {
		dateStats := TripRevenueDateResponse{
			Date: date,
		}

		var companies []string
		dateQuery := h.DB.Model(&Models.TripStruct{}).Where("date = ?", date)

		if CompanyFilter != "" {
			dateQuery = dateQuery.Where("company = ?", CompanyFilter)
		}

		if err := dateQuery.Distinct("company").Pluck("company", &companies).Error; err != nil {
			continue
		}

		if CompanyFilter != "" && len(companies) == 0 {
			continue
		}

		dateStats.CompanyDetails = make([]CompanyRevenueDetails, 0, len(companies))
		dateStats.TotalTrips = 0
		dateStats.TotalVolume = 0
		dateStats.TotalDistance = 0
		dateStats.TotalRevenue = 0

		for _, company := range companies {
			companyDetail := CompanyRevenueDetails{
				Company: company,
			}

			// Count trips properly - standalone trips + count of distinct parent_trip_ids
			// Count trips properly using two separate queries
			var containerParentCount int64
			h.DB.Raw(`
    SELECT COUNT(DISTINCT parent_trip_id) 
    FROM trips 
    WHERE company = ? AND date = ? AND deleted_at IS NULL
    AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
`, company, date).Row().Scan(&containerParentCount)

			var standaloneCount int64
			h.DB.Raw(`
    SELECT COUNT(ID)
    FROM trips
    WHERE company = ? AND date = ? AND deleted_at IS NULL
    AND (parent_trip_id IS NULL OR parent_trip_id = 0)
`, company, date).Row().Scan(&standaloneCount)

			tripCount := containerParentCount + standaloneCount
			fmt.Printf("DEBUG: Company=%s, Date=%s, Containers=%d, Standalone=%d, Total=%d\n",
				company, date, containerParentCount, standaloneCount, tripCount)
			companyDetail.TotalTrips = tripCount
			dateStats.TotalTrips += tripCount
			// Sum ALL volumes
			var volume float64
			h.DB.Model(&Models.TripStruct{}).
				Where("date = ?", date).
				Where("company = ?", company).
				Select("COALESCE(SUM(tank_capacity), 0)").
				Row().Scan(&volume)
			companyDetail.TotalVolume = volume
			dateStats.TotalVolume += volume

			// Sum ALL distances
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

			if hasFinancialAccess {
				var revenue float64 = 0

				switch company {
				case "Petrol Arrows":
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
					var baseRevenue float64 = 0
					h.DB.Raw(`
						SELECT 
							COALESCE(SUM(
								CASE 
									WHEN t.terminal = 'Alex' THEN fm.distance * 44.5
									WHEN t.terminal = 'Suez' THEN fm.distance * 44.5
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

					// CORRECTED: Calculate total rental for entire period, then prorate per day
					var totalPeriodRental float64 = 0

					// Get working days per car for the ENTIRE period
					var carWorkingDays []struct {
						CarNoPlate  string
						WorkingDays int64
					}

					h.DB.Raw(`
						SELECT 
							car_no_plate,
							COUNT(DISTINCT date) as working_days
						FROM trips
						WHERE company = ? 
						AND deleted_at IS NULL
						AND (parent_trip_id IS NULL OR parent_trip_id = 0)
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						GROUP BY car_no_plate
					`, company, StartDate, StartDate, EndDate, EndDate).Scan(&carWorkingDays)

					// Calculate total rental for the period
					for _, car := range carWorkingDays {
						var carRental float64
						if car.WorkingDays >= 28 {
							carRental = 43000.0
						} else {
							carRental = 43000.0 - (float64(28-car.WorkingDays) * 1433.0)
							if carRental < 0 {
								carRental = 0
							}
						}
						totalPeriodRental += carRental
					}

					// Count distinct days in the period
					var distinctDaysInPeriod int64
					h.DB.Raw(`
						SELECT COUNT(DISTINCT date)
						FROM trips
						WHERE company = ? 
						AND deleted_at IS NULL
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
					`, company, StartDate, StartDate, EndDate, EndDate).Row().Scan(&distinctDaysInPeriod)

					// Prorate rental per day
					var dailyRental float64 = 0
					if distinctDaysInPeriod > 0 {
						dailyRental = totalPeriodRental / float64(distinctDaysInPeriod)
					}

					// VAT applied AFTER adding base revenue and daily rental
					vat := (baseRevenue + dailyRental) * 0.14
					revenue = baseRevenue + dailyRental + vat

				case "Petromin":
					var dayTrips []Models.TripStruct
					h.DB.Where("company = ? AND date = ? AND deleted_at IS NULL", company, date).
						Order("car_no_plate ASC, receipt_no ASC").Find(&dayTrips)

					dayGroupedTrips := h.groupTripsForSingleDate(dayTrips)

					var baseRevenue float64 = 0
					for _, tripGroup := range dayGroupedTrips {
						maxDistance := h.getMaxDistanceFromTripGroup(tripGroup)
						baseRevenue += maxDistance * 42.5
					}

					var carCount int64
					h.DB.Raw(`
						SELECT COUNT(DISTINCT car_no_plate)
						FROM trips
						WHERE company = ? AND date = ? AND deleted_at IS NULL
						AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					`, company, date).Row().Scan(&carCount)

					carRentalFee := float64(carCount) * 2000.0
					vat := (baseRevenue + carRentalFee) * 0.14
					revenue = baseRevenue + carRentalFee + vat

				case "Watanya":
					h.DB.Raw(`
						SELECT COALESCE(SUM(
							CASE 
								WHEN fm.fee = 1 THEN t.tank_capacity * 95.0 / 1000
                WHEN fm.fee = 2 THEN t.tank_capacity * 111.0 / 1000
                WHEN fm.fee = 3 THEN t.tank_capacity * 118.0 / 1000
                WHEN fm.fee = 4 THEN t.tank_capacity * 142.0 / 1000
                WHEN fm.fee = 5 THEN t.tank_capacity * 167.0 / 1000
                WHEN fm.fee = 6 THEN t.tank_capacity * 179.0 / 1000
                WHEN fm.fee = 7 THEN t.tank_capacity * 191.0 / 1000
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

				default:
					// Calculate revenue for default companies using average fee
					var avgFee float64
					h.DB.Raw(`
						SELECT COALESCE(AVG(fee), 0)
						FROM fee_mappings
						WHERE company = ?
					`, company).Row().Scan(&avgFee)

					if avgFee == 0 {
						avgFee = 50
					}

					revenue = volume * avgFee
				}

				companyDetail.TotalRevenue = revenue
				dateStats.TotalRevenue += revenue
			}

			dateStats.CompanyDetails = append(dateStats.CompanyDetails, companyDetail)
		}

		output = append(output, dateStats)
	}

	return output
}

func GetCarTotals(statistics []TripStatistics) []CarTotal {
	carTotalsMap := make(map[string]*CarTotal)

	for _, statistic := range statistics {
		for _, routeDetail := range statistic.RouteDetails {
			for _, car := range routeDetail.Cars {
				carTotal, exists := carTotalsMap[car.CarNoPlate]

				if !exists {
					carTotal = &CarTotal{
						CarNoPlate: car.CarNoPlate,
					}
					carTotalsMap[car.CarNoPlate] = carTotal
				}

				carTotal.Liters += car.TotalVolume
				carTotal.Distance += car.TotalDistance
				carTotal.BaseRevenue += car.TotalRevenue
				carTotal.VAT += car.VAT
				carTotal.Rent += car.CarRental
			}
		}
	}

	result := make([]CarTotal, 0, len(carTotalsMap))
	for _, carTotal := range carTotalsMap {
		result = append(result, *carTotal)
	}

	return result
}

// Helper function to calculate group statistics - CORRECTED TRIP COUNTING
func (h *TripHandler) calculateGroupStats(tripGroup []Models.TripStruct) (int64, float64, float64, int64) {
	if len(tripGroup) == 0 {
		return 0, 0, 0, 0
	}

	// CORRECTED: Count trips properly - unique parent_trip_ids + standalone trips
	var totalTrips int64 = 0
	parentTripIDs := make(map[uint]bool)

	for _, trip := range tripGroup {
		if trip.ParentTripID != nil && *trip.ParentTripID != 0 {
			// Has a parent - only count unique parent IDs
			parentTripIDs[*trip.ParentTripID] = true
		} else {
			// Standalone trip - count each one
			totalTrips++
		}
	}

	// Add the count of unique parent trip IDs
	totalTrips += int64(len(parentTripIDs))

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

// groupPetrominTripsByCapacity - NO CHANGES NEEDED (grouping logic is fine)
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

// groupTripsForSingleDate - NO CHANGES NEEDED (grouping logic is fine)
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

func (h *TripHandler) GetTripStatistics(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	companyFilter := c.Query("company")

	user, ok := c.Locals("user").(*Models.User)
	hasFinancialAccess := ok && user.Permission >= 3

	query := h.DB.Model(&Models.TripStruct{})

	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	}

	if companyFilter != "" {
		query = query.Where("company = ?", companyFilter)
	}

	var companies []string
	if err := query.Distinct("company").Pluck("company", &companies).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch company statistics",
			"error":   err.Error(),
		})
	}

	statistics := make([]TripStatistics, 0, len(companies))

	for _, company := range companies {
		companyStats := TripStatistics{
			Company: company,
		}

		// Sum ALL volumes
		var totalVolume float64
		h.DB.Model(&Models.TripStruct{}).
			Where("company = ?", company).
			Where("date >= ? AND date <= ?", startDate, endDate).
			Select("COALESCE(SUM(tank_capacity), 0)").
			Row().Scan(&totalVolume)
		companyStats.TotalVolume = totalVolume

		// Sum ALL distances
		var totalDistance float64
		h.DB.Raw(`
			SELECT COALESCE(SUM(fm.distance), 0) as total_distance
			FROM trips t
			LEFT JOIN fee_mappings fm 
				ON t.company = fm.company 
				AND t.terminal = fm.terminal 
				AND t.drop_off_point = fm.drop_off_point
			WHERE t.company = ? AND t.deleted_at IS NULL
			AND t.date >= ? AND t.date <= ?
		`, company, startDate, endDate).Row().Scan(&totalDistance)
		companyStats.TotalDistance = totalDistance

		switch company {
		case "Petrol Arrows":
			var dropOffStats []struct {
				DropOffPoint  string
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
				Fee           float64
			}

			// Get aggregated data per drop-off point
			type DropOffData struct {
				DropOffPoint  string
				TotalVolume   float64
				TotalDistance float64
				Fee           float64
			}

			var dropOffData []DropOffData
			h.DB.Raw(`
				SELECT 
					t.drop_off_point,
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					fm.fee
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND t.date >= ? AND t.date <= ?
				GROUP BY t.drop_off_point, fm.fee, t.company
			`, company, startDate, endDate).Scan(&dropOffData)

			// For each drop-off point, count trips separately
			for _, data := range dropOffData {
				var containerParentCount int64
				h.DB.Raw(`
					SELECT COUNT(DISTINCT parent_trip_id) 
					FROM trips 
					WHERE company = ? 
					AND drop_off_point = ?
					AND deleted_at IS NULL
					AND date >= ? AND date <= ?
					AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
				`, company, data.DropOffPoint, startDate, endDate).Row().Scan(&containerParentCount)

				var standaloneCount int64
				h.DB.Raw(`
					SELECT COUNT(ID)
					FROM trips
					WHERE company = ? 
					AND drop_off_point = ?
					AND deleted_at IS NULL
					AND date >= ? AND date <= ?
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				`, company, data.DropOffPoint, startDate, endDate).Row().Scan(&standaloneCount)

				tripCount := containerParentCount + standaloneCount

				dropOffStats = append(dropOffStats, struct {
					DropOffPoint  string
					TotalTrips    int64
					TotalVolume   float64
					TotalDistance float64
					Fee           float64
				}{
					DropOffPoint:  data.DropOffPoint,
					TotalTrips:    tripCount,
					TotalVolume:   data.TotalVolume,
					TotalDistance: data.TotalDistance,
					Fee:           data.Fee,
				})
			}

			companyStats.Details = make([]TripStatisticsDetails, 0, len(dropOffStats))
			companyStats.TotalRevenue = 0

			for _, stat := range dropOffStats {
				revenue := stat.Fee * stat.TotalVolume / 1000

				detail := TripStatisticsDetails{
					GroupName:     stat.DropOffPoint,
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.TotalDistance,
					TotalRevenue:  revenue,
					Fee:           stat.Fee,
				}

				if !hasFinancialAccess {
					detail.TotalRevenue = 0
					detail.Fee = 0
				}

				companyStats.Details = append(companyStats.Details, detail)
				if hasFinancialAccess {
					companyStats.TotalRevenue += revenue
				}
			}

			// Calculate total trips from details
			companyStats.TotalTrips = 0
			for _, detail := range companyStats.Details {
				companyStats.TotalTrips += detail.TotalTrips
			}

		case "TAQA":
			var terminalStats []struct {
				Terminal      string
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
				DistinctCars  int64
			}

			// Get aggregated data per terminal
			type TerminalData struct {
				Terminal      string
				TotalVolume   float64
				TotalDistance float64
				DistinctCars  int64
			}

			var terminalData []TerminalData
			h.DB.Raw(`
				SELECT 
					t.terminal,
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT t.car_no_plate) as distinct_cars
				FROM trips t
				LEFT JOIN fee_mappings fm 
					ON t.company = fm.company 
					AND t.terminal = fm.terminal 
					AND t.drop_off_point = fm.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND t.date >= ? AND t.date <= ?
				GROUP BY t.terminal, t.company
			`, company, startDate, endDate).Scan(&terminalData)

			// For each terminal, count trips separately
			for _, data := range terminalData {
				var containerParentCount int64
				h.DB.Raw(`
					SELECT COUNT(DISTINCT parent_trip_id) 
					FROM trips 
					WHERE company = ? 
					AND terminal = ?
					AND deleted_at IS NULL
					AND date >= ? AND date <= ?
					AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
				`, company, data.Terminal, startDate, endDate).Row().Scan(&containerParentCount)

				var standaloneCount int64
				h.DB.Raw(`
					SELECT COUNT(ID)
					FROM trips
					WHERE company = ? 
					AND terminal = ?
					AND deleted_at IS NULL
					AND date >= ? AND date <= ?
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				`, company, data.Terminal, startDate, endDate).Row().Scan(&standaloneCount)

				tripCount := containerParentCount + standaloneCount

				terminalStats = append(terminalStats, struct {
					Terminal      string
					TotalTrips    int64
					TotalVolume   float64
					TotalDistance float64
					DistinctCars  int64
				}{
					Terminal:      data.Terminal,
					TotalTrips:    tripCount,
					TotalVolume:   data.TotalVolume,
					TotalDistance: data.TotalDistance,
					DistinctCars:  data.DistinctCars,
				})
			}

			companyStats.Details = make([]TripStatisticsDetails, 0, len(terminalStats))
			companyStats.TotalRevenue = 0
			companyStats.TotalVAT = 0
			companyStats.TotalCarRent = 0
			companyStats.TotalAmount = 0

			for _, stat := range terminalStats {
				var ratePerKm float64
				if stat.Terminal == "Alex" || stat.Terminal == "Suez" {
					ratePerKm = 44.5
				} else {
					ratePerKm = 0
				}

				baseRevenue := stat.TotalDistance * ratePerKm

				var carRentalFee float64 = 0

				var carWorkingDays []struct {
					CarNoPlate  string
					WorkingDays int64
				}

				h.DB.Raw(`
					SELECT 
						car_no_plate,
						COUNT(DISTINCT date) as working_days
					FROM trips
					WHERE company = ? 
					AND deleted_at IS NULL
					AND terminal = ?
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					AND date >= ? AND date <= ?
					GROUP BY car_no_plate
				`, company, stat.Terminal, startDate, endDate).Scan(&carWorkingDays)

				var totalCarDays int64 = 0
				for _, car := range carWorkingDays {
					var carRental float64
					if car.WorkingDays >= 28 {
						carRental = 43000.0
					} else {
						carRental = 43000.0 - (float64(28-car.WorkingDays) * 1433.0)
						if carRental < 0 {
							carRental = 0
						}
					}
					carRentalFee += carRental
					totalCarDays += car.WorkingDays
				}

				vat := (baseRevenue + carRentalFee) * 0.14
				totalRevenue := baseRevenue + carRentalFee + vat

				var distinctDays int64
				h.DB.Raw(`
					SELECT COUNT(DISTINCT date)
					FROM trips
					WHERE company = ? 
					AND deleted_at IS NULL
					AND terminal = ?
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					AND date >= ? AND date <= ?
				`, company, stat.Terminal, startDate, endDate).Row().Scan(&distinctDays)

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
					CarDays:       totalCarDays,
				}

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
					companyStats.TotalCarRent += carRentalFee
					companyStats.TotalVAT += vat
					companyStats.TotalAmount += totalRevenue
				}
			}

			// Calculate total trips from details
			companyStats.TotalTrips = 0
			for _, detail := range companyStats.Details {
				companyStats.TotalTrips += detail.TotalTrips
			}

		case "Petromin":
			groupedTrips := h.groupPetrominTripsByCapacity(company, startDate, endDate)

			var revenueDistance float64 = 0
			for _, tripGroup := range groupedTrips {
				maxDistance := h.getMaxDistanceFromTripGroup(tripGroup)
				revenueDistance += maxDistance
			}

			var allWorkingDays []struct {
				CarNoPlate string
				Date       string
			}

			h.DB.Raw(`
				SELECT DISTINCT car_no_plate, date
				FROM trips
				WHERE company = ? 
				AND deleted_at IS NULL
				AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				AND date >= ? AND date <= ?
				ORDER BY car_no_plate, date
			`, company, startDate, endDate).Scan(&allWorkingDays)

			var totalCarRentalFee float64 = float64(len(allWorkingDays)) * 2000.0

			terminalStats := make(map[string]*struct {
				Terminal        string
				TotalTrips      int64
				TotalVolume     float64
				RevenueDistance float64
				DistinctCars    map[string]bool
				WorkingDays     map[string]bool
			})

			for _, tripGroup := range groupedTrips {
				if len(tripGroup) == 0 {
					continue
				}

				terminal := tripGroup[0].Terminal
				carPlate := tripGroup[0].CarNoPlate

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

				totalTrips, totalVolume, maxDistance, _ := h.calculateGroupStats(tripGroup)

				terminalStats[terminal].TotalTrips += totalTrips
				terminalStats[terminal].TotalVolume += totalVolume
				terminalStats[terminal].RevenueDistance += maxDistance

				terminalStats[terminal].DistinctCars[carPlate] = true
				for _, trip := range tripGroup {
					terminalStats[terminal].WorkingDays[trip.Date] = true
				}
			}

			terminalCarDays := make(map[string]int)
			for terminal := range terminalStats {
				var terminalWorkingDays []struct {
					CarNoPlate string
					Date       string
				}

				h.DB.Raw(`
					SELECT DISTINCT car_no_plate, date
					FROM trips
					WHERE company = ? 
					AND deleted_at IS NULL
					AND terminal = ?
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					AND date >= ? AND date <= ?
					ORDER BY car_no_plate, date
				`, company, terminal, startDate, endDate).Scan(&terminalWorkingDays)

				terminalCarDays[terminal] = len(terminalWorkingDays)
			}

			companyStats.Details = make([]TripStatisticsDetails, 0, len(terminalStats))
			companyStats.TotalRevenue = 0
			companyStats.TotalVAT = 0
			companyStats.TotalCarRent = totalCarRentalFee
			companyStats.TotalAmount = 0
			companyStats.TotalDistance = revenueDistance

			for _, stat := range terminalStats {
				ratePerKm := 42.5
				baseRevenue := stat.RevenueDistance * ratePerKm

				carRentalFee := 0.0
				if terminalCarDays[stat.Terminal] > 0 {
					carRentalFee = float64(terminalCarDays[stat.Terminal]) * 2000.0
				}

				vat := (baseRevenue + carRentalFee) * 0.14
				totalRevenue := baseRevenue + carRentalFee + vat

				detail := TripStatisticsDetails{
					GroupName:     stat.Terminal,
					TotalTrips:    stat.TotalTrips,
					TotalVolume:   stat.TotalVolume,
					TotalDistance: stat.RevenueDistance,
					TotalRevenue:  baseRevenue,
					CarRental:     carRentalFee,
					VAT:           vat,
					TotalWithVAT:  totalRevenue,
					Fee:           ratePerKm,
					DistinctCars:  int64(len(stat.DistinctCars)),
					DistinctDays:  int64(len(stat.WorkingDays)),
					CarDays:       int64(terminalCarDays[stat.Terminal]),
				}

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

			// Calculate total trips from details
			companyStats.TotalTrips = 0
			for _, detail := range companyStats.Details {
				companyStats.TotalTrips += detail.TotalTrips
			}

		case "Watanya":
			var feeStats []struct {
				Fee           float64
				TotalTrips    int64
				TotalVolume   float64
				TotalDistance float64
			}

			// Get aggregated data per fee
			type FeeData struct {
				Fee           float64
				TotalVolume   float64
				TotalDistance float64
			}

			var feeData []FeeData
			h.DB.Raw(`
				SELECT 
					f.fee, 
					COALESCE(SUM(t.tank_capacity), 0) as total_volume, 
					COALESCE(SUM(f.distance), 0) as total_distance
				FROM trips t
				LEFT JOIN fee_mappings f 
					ON t.company = f.company 
					AND t.terminal = f.terminal 
					AND t.drop_off_point = f.drop_off_point
				WHERE t.company = ? AND t.deleted_at IS NULL
				AND t.date >= ? AND t.date <= ?
				GROUP BY f.fee
			`, company, startDate, endDate).Scan(&feeData)

			// For each fee, count trips separately
			for _, data := range feeData {
				var containerParentCount int64
				h.DB.Raw(`
					SELECT COUNT(DISTINCT t.parent_trip_id) 
					FROM trips t
					LEFT JOIN fee_mappings fm 
						ON t.company = fm.company 
						AND t.terminal = fm.terminal 
						AND t.drop_off_point = fm.drop_off_point
					WHERE t.company = ? 
					AND t.deleted_at IS NULL
					AND t.date >= ? AND t.date <= ?
					AND fm.fee = ?
					AND t.parent_trip_id IS NOT NULL AND t.parent_trip_id != 0
				`, company, startDate, endDate, data.Fee).Row().Scan(&containerParentCount)

				var standaloneCount int64
				h.DB.Raw(`
					SELECT COUNT(t.ID)
					FROM trips t
					LEFT JOIN fee_mappings fm 
						ON t.company = fm.company 
						AND t.terminal = fm.terminal 
						AND t.drop_off_point = fm.drop_off_point
					WHERE t.company = ? 
					AND t.deleted_at IS NULL
					AND t.date >= ? AND t.date <= ?
					AND fm.fee = ?
					AND (t.parent_trip_id IS NULL OR t.parent_trip_id = 0)
				`, company, startDate, endDate, data.Fee).Row().Scan(&standaloneCount)

				tripCount := containerParentCount + standaloneCount

				feeStats = append(feeStats, struct {
					Fee           float64
					TotalTrips    int64
					TotalVolume   float64
					TotalDistance float64
				}{
					Fee:           data.Fee,
					TotalTrips:    tripCount,
					TotalVolume:   data.TotalVolume,
					TotalDistance: data.TotalDistance,
				})
			}

			companyStats.Details = make([]TripStatisticsDetails, 0, len(feeStats))
			companyStats.TotalRevenue = 0
			companyStats.TotalVAT = 0
			companyStats.TotalAmount = 0

			for _, stat := range feeStats {
				var ratePerVolume float64

				switch int(stat.Fee) {
				case 1:
					ratePerVolume = 95.0
				case 2:
					ratePerVolume = 111.0
				case 3:
					ratePerVolume = 118.0
				case 4:
					ratePerVolume = 142.0
				case 5:
					ratePerVolume = 167.0
				case 6:
					ratePerVolume = 179.0
				case 7:
					ratePerVolume = 191.0
				default:
					ratePerVolume = 0
				}

				baseRevenue := stat.TotalVolume * ratePerVolume / 1000
				vat := baseRevenue * 0.14
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

			// Calculate total trips from details
			companyStats.TotalTrips = 0
			for _, detail := range companyStats.Details {
				companyStats.TotalTrips += detail.TotalTrips
			}
			sort.Slice(companyStats.Details, func(i, j int) bool {
				feeI := companyStats.Details[i].Fee
				feeJ := companyStats.Details[j].Fee

				// If one is 0 and the other isn't, 0 goes last
				if feeI == 0 && feeJ != 0 {
					return false
				}
				if feeI != 0 && feeJ == 0 {
					return true
				}

				// Otherwise sort numerically
				return feeI < feeJ
			})
		default:
			var avgFee float64
			h.DB.Raw(`
				SELECT COALESCE(AVG(fee), 0)
				FROM fee_mappings
				WHERE company = ?
			`, company).Row().Scan(&avgFee)

			if avgFee == 0 {
				avgFee = 50
			}

			revenue := companyStats.TotalVolume * avgFee

			if hasFinancialAccess {
				companyStats.TotalRevenue = revenue
			} else {
				companyStats.TotalRevenue = 0
			}

			// For default companies, count trips using split query
			var containerParentCount int64
			h.DB.Raw(`
				SELECT COUNT(DISTINCT parent_trip_id) 
				FROM trips 
				WHERE company = ? 
				AND deleted_at IS NULL
				AND date >= ? AND date <= ?
				AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
			`, company, startDate, endDate).Row().Scan(&containerParentCount)

			var standaloneCount int64
			h.DB.Raw(`
				SELECT COUNT(ID)
				FROM trips
				WHERE company = ?
				AND deleted_at IS NULL
				AND date >= ? AND date <= ?
				AND (parent_trip_id IS NULL OR parent_trip_id = 0)
			`, company, startDate, endDate).Row().Scan(&standaloneCount)

			companyStats.TotalTrips = containerParentCount + standaloneCount
		}

		routeStats := h.GetTripStatsByRoute(company, startDate, endDate, hasFinancialAccess)
		companyStats.RouteDetails = routeStats
		statistics = append(statistics, companyStats)
	}

	statsByDate := h.GetTripStatsByTime(startDate, endDate, companyFilter, hasFinancialAccess)
	carTotals := GetCarTotals(statistics)

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":            "Trip statistics retrieved successfully",
		"data":               statistics,
		"statsByDate":        statsByDate,
		"hasFinancialAccess": hasFinancialAccess,
		"carTotals":          carTotals,
	})
}

func (h *TripHandler) GetTripStatsByRoute(company, startDate, endDate string, hasFinancialAccess bool) []RouteRevenueStats {
	var routeStats []RouteRevenueStats

	switch company {
	case "Petrol Arrows":
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
				(SELECT COUNT(*) FROM (
					SELECT DISTINCT parent_trip_id
					FROM trips
					WHERE company = t.company
					AND terminal = t.terminal
					AND drop_off_point = t.drop_off_point
					AND deleted_at IS NULL
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
					UNION ALL
					SELECT ID
					FROM trips
					WHERE company = t.company
					AND terminal = t.terminal
					AND drop_off_point = t.drop_off_point
					AND deleted_at IS NULL
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				) AS counted_trips) as total_trips,
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
			GROUP BY t.terminal, t.drop_off_point, fm.fee, t.company
		`, startDate, startDate, endDate, endDate, startDate, startDate, endDate, endDate, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		for _, route := range routeData {
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
				routeStat.TotalWithVAT = revenue
			}

			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					(SELECT COUNT(*) FROM (
						SELECT DISTINCT parent_trip_id
						FROM trips
						WHERE company = ? AND deleted_at IS NULL
						AND terminal = ? AND drop_off_point = ?
						AND car_no_plate = t.car_no_plate
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
						UNION ALL
						SELECT ID
						FROM trips
						WHERE company = ? AND deleted_at IS NULL
						AND terminal = ? AND drop_off_point = ?
						AND car_no_plate = t.car_no_plate
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					) AS counted_trips) as total_trips,
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT CASE WHEN (t.parent_trip_id IS NULL OR t.parent_trip_id = 0) THEN t.date END) as working_days
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
			`, company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate,
				company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate,
				company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate).Scan(&carStats)

			for i := range carStats {
				if hasFinancialAccess {
					carStats[i].TotalRevenue = route.Fee * carStats[i].TotalVolume / 1000
					carStats[i].TotalWithVAT = carStats[i].TotalRevenue
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}

	case "TAQA":
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
				(SELECT COUNT(*) FROM (
					SELECT DISTINCT parent_trip_id
					FROM trips
					WHERE company = t.company
					AND terminal = t.terminal
					AND deleted_at IS NULL
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
					UNION ALL
					SELECT ID
					FROM trips
					WHERE company = t.company
					AND terminal = t.terminal
					AND deleted_at IS NULL
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				) AS counted_trips) as total_trips,
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
			GROUP BY t.terminal, t.company
		`, startDate, startDate, endDate, endDate, startDate, startDate, endDate, endDate, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		for _, route := range routeData {
			var ratePerKm float64
			if route.Terminal == "Alex" || route.Terminal == "Suez" {
				ratePerKm = 44.5
			} else {
				ratePerKm = 0
			}

			baseRevenue := route.TotalDistance * ratePerKm

			// CORRECTED: Calculate rental PER CAR, then sum
			var carRentalFee float64 = 0

			var carWorkingDays []struct {
				CarNoPlate  string
				WorkingDays int64
			}

			h.DB.Raw(`
				SELECT 
					car_no_plate,
					COUNT(DISTINCT date) as working_days
				FROM trips
				WHERE company = ? 
				AND deleted_at IS NULL
				AND terminal = ?
				AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
				GROUP BY car_no_plate
			`, company, route.Terminal, startDate, startDate, endDate, endDate).Scan(&carWorkingDays)

			for _, car := range carWorkingDays {
				var carRental float64
				if car.WorkingDays >= 28 {
					carRental = 43000.0
				} else {
					carRental = 43000.0 - (float64(28-car.WorkingDays) * 1433.0)
					if carRental < 0 {
						carRental = 0
					}
				}
				carRentalFee += carRental
			}

			// VAT applied AFTER adding base revenue and rental
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

			// Get car stats with per-car rental calculation
			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					(SELECT COUNT(*) FROM (
						SELECT DISTINCT parent_trip_id
						FROM trips
						WHERE company = ? AND deleted_at IS NULL
						AND terminal = ?
						AND car_no_plate = t.car_no_plate
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
						UNION ALL
						SELECT ID
						FROM trips
						WHERE company = ? AND deleted_at IS NULL
						AND terminal = ?
						AND car_no_plate = t.car_no_plate
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					) AS counted_trips) as total_trips,
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT CASE WHEN (t.parent_trip_id IS NULL OR t.parent_trip_id = 0) THEN t.date END) as working_days
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
			`, company, route.Terminal, startDate, startDate, endDate, endDate,
				company, route.Terminal, startDate, startDate, endDate, endDate,
				company, route.Terminal, startDate, startDate, endDate, endDate).Scan(&carStats)

			for i := range carStats {
				if hasFinancialAccess {
					carBaseRevenue := carStats[i].TotalDistance * ratePerKm

					// CORRECTED: Per-car rental calculation
					var carRental float64
					if carStats[i].WorkingDays >= 28 {
						carRental = 43000.0
					} else {
						carRental = 43000.0 - (float64(28-carStats[i].WorkingDays) * 1433.0)
						if carRental < 0 {
							carRental = 0
						}
					}

					// VAT applied AFTER adding base revenue and rental
					carVAT := (carBaseRevenue + carRental) * 0.14
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
		groupedTrips := h.groupPetrominTripsByCapacity(company, startDate, endDate)

		terminalGroups := make(map[string][][]Models.TripStruct)
		for _, tripGroup := range groupedTrips {
			if len(tripGroup) > 0 {
				terminal := tripGroup[0].Terminal
				terminalGroups[terminal] = append(terminalGroups[terminal], tripGroup)
			}
		}

		for terminal, groups := range terminalGroups {
			var totalTrips int64 = 0
			var totalVolume float64 = 0
			var revenueTotalDistance float64 = 0
			var distinctCars = make(map[string]bool)
			var workingDaysSet = make(map[string]bool)

			for _, tripGroup := range groups {
				groupTrips, groupVolume, maxDistance, _ := h.calculateGroupStats(tripGroup)
				totalTrips += groupTrips
				totalVolume += groupVolume
				revenueTotalDistance += maxDistance

				for _, trip := range tripGroup {
					distinctCars[trip.CarNoPlate] = true
					workingDaysSet[trip.Date] = true
				}
			}

			var terminalWorkingDays []struct {
				CarNoPlate string
				Date       string
			}

			h.DB.Raw(`
				SELECT DISTINCT car_no_plate, date
				FROM trips
				WHERE company = ? AND deleted_at IS NULL
				AND terminal = ?
				AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
				ORDER BY car_no_plate, date
			`, company, terminal, startDate, startDate, endDate, endDate).Scan(&terminalWorkingDays)

			ratePerKm := 42.5
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
				TotalDistance: revenueTotalDistance,
				Fee:           ratePerKm,
			}

			if hasFinancialAccess {
				routeStat.TotalRevenue = baseRevenue
				routeStat.CarRental = carRentalFee
				routeStat.VAT = vat
				routeStat.TotalWithVAT = totalRevenue
			}

			var carStats []CarStats
			carStatsMap := make(map[string]*CarStats)

			for _, tripGroup := range groups {
				if len(tripGroup) == 0 {
					continue
				}

				carPlate := tripGroup[0].CarNoPlate
				groupTrips, groupVolume, maxDistance, groupWorkingDays := h.calculateGroupStats(tripGroup)

				if carStatsMap[carPlate] == nil {
					carStatsMap[carPlate] = &CarStats{
						CarNoPlate: carPlate,
					}
				}

				carStatsMap[carPlate].TotalTrips += groupTrips
				carStatsMap[carPlate].TotalVolume += groupVolume
				carStatsMap[carPlate].TotalDistance += maxDistance
				carStatsMap[carPlate].WorkingDays += groupWorkingDays
			}

			for _, carStat := range carStatsMap {
				if hasFinancialAccess {
					carBaseRevenue := carStat.TotalDistance * ratePerKm
					carRental := float64(carStat.WorkingDays) * 2000.0
					carVAT := (carBaseRevenue + carRental) * 0.14
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

		for _, route := range routeData {
			var ratePerVolume float64

			switch int(route.Fee) {
			case 1:
				ratePerVolume = 95.0
			case 2:
				ratePerVolume = 111.0
			case 3:
				ratePerVolume = 118.0
			case 4:
				ratePerVolume = 142.0
			case 5:
				ratePerVolume = 167.0
			case 6:
				ratePerVolume = 179.0
			case 7:
				ratePerVolume = 191.0
			default:
				ratePerVolume = 0
			}

			baseRevenue := route.TotalVolume * ratePerVolume / 1000
			vat := baseRevenue * 0.14
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

			for i := range carStats {
				if hasFinancialAccess {
					carBaseRevenue := carStats[i].TotalVolume * ratePerVolume / 1000
					carVAT := carBaseRevenue * 0.14
					carTotal := carBaseRevenue + carVAT

					carStats[i].TotalRevenue = carBaseRevenue
					carStats[i].VAT = carVAT
					carStats[i].TotalWithVAT = carTotal
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}
		sort.Slice(routeStats, func(i, j int) bool {
			feeI := routeStats[i].Fee
			feeJ := routeStats[j].Fee

			// If one is 0 and the other isn't, 0 goes last
			if feeI == 0 && feeJ != 0 {
				return false
			}
			if feeI != 0 && feeJ == 0 {
				return true
			}

			// Otherwise sort numerically
			return feeI < feeJ
		})

	default:
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
				(SELECT COUNT(*) FROM (
					SELECT DISTINCT parent_trip_id
					FROM trips
					WHERE company = t.company
					AND terminal = t.terminal
					AND drop_off_point = t.drop_off_point
					AND deleted_at IS NULL
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
					UNION ALL
					SELECT ID
					FROM trips
					WHERE company = t.company
					AND terminal = t.terminal
					AND drop_off_point = t.drop_off_point
					AND deleted_at IS NULL
					AND (date >= ? OR ? = '')
					AND (date <= ? OR ? = '')
					AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				) AS counted_trips) as total_trips,
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
			GROUP BY t.terminal, t.drop_off_point, t.company
		`, startDate, startDate, endDate, endDate, startDate, startDate, endDate, endDate, company, startDate, startDate, endDate, endDate).Scan(&routeData)

		for _, route := range routeData {
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
				routeStat.TotalWithVAT = revenue
			}

			var carStats []CarStats
			h.DB.Raw(`
				SELECT 
					t.car_no_plate,
					(SELECT COUNT(*) FROM (
						SELECT DISTINCT parent_trip_id
						FROM trips
						WHERE company = ? AND deleted_at IS NULL
						AND terminal = ? AND drop_off_point = ?
						AND car_no_plate = t.car_no_plate
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						AND parent_trip_id IS NOT NULL AND parent_trip_id != 0
						UNION ALL
						SELECT ID
						FROM trips
						WHERE company = ? AND deleted_at IS NULL
						AND terminal = ? AND drop_off_point = ?
						AND car_no_plate = t.car_no_plate
						AND (date >= ? OR ? = '')
						AND (date <= ? OR ? = '')
						AND (parent_trip_id IS NULL OR parent_trip_id = 0)
					) AS counted_trips) as total_trips,
					COALESCE(SUM(t.tank_capacity), 0) as total_volume,
					COALESCE(SUM(fm.distance), 0) as total_distance,
					COUNT(DISTINCT CASE WHEN (t.parent_trip_id IS NULL OR t.parent_trip_id = 0) THEN t.date END) as working_days
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
			`, company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate,
				company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate,
				company, route.Terminal, route.DropOffPoint, startDate, startDate, endDate, endDate).Scan(&carStats)

			for i := range carStats {
				if hasFinancialAccess {
					carStats[i].TotalRevenue = route.AvgFee * carStats[i].TotalVolume
					carStats[i].TotalWithVAT = carStats[i].TotalRevenue
				}
			}

			routeStat.Cars = carStats
			routeStats = append(routeStats, routeStat)
		}
	}

	return routeStats
}

func (h *TripHandler) GetWatanyaDriverAnalytics(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	user, ok := c.Locals("user").(*Models.User)
	hasFinancialAccess := ok && user.Permission >= 3

	type DriverAnalytics struct {
		DriverName      string  `json:"driver_name"`
		TotalTrips      int64   `json:"total_trips"`
		TotalDistance   float64 `json:"total_distance"`
		TotalVolume     float64 `json:"total_volume"`
		TotalFees       float64 `json:"total_fees,omitempty"`
		TotalRevenue    float64 `json:"total_revenue,omitempty"`
		TotalVAT        float64 `json:"total_vat,omitempty"`
		TotalAmount     float64 `json:"total_amount,omitempty"`
		WorkingDays     int64   `json:"working_days"`
		AvgTripsPerDay  float64 `json:"avg_trips_per_day"`
		AvgKmPerDay     float64 `json:"avg_km_per_day"`
		AvgFeesPerDay   float64 `json:"avg_fees_per_day,omitempty"`
		AvgTripsPerKm   float64 `json:"avg_trips_per_km"`
		AvgVolumePerKm  float64 `json:"avg_volume_per_km"`
		Efficiency      float64 `json:"efficiency"`
		ActivityHeatmap []struct {
			Date    string  `json:"date"`
			Count   int64   `json:"count"`
			Revenue float64 `json:"revenue"`
		} `json:"activity_heatmap"`
		RouteDistribution []struct {
			Terminal     string  `json:"terminal"`
			DropOffPoint string  `json:"drop_off_point"`
			Count        int64   `json:"count"`
			Distance     float64 `json:"distance"`
			Percent      float64 `json:"percent"`
		} `json:"route_distribution"`
	}

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
			TotalFees            float64  `json:"total_fees,omitempty"`
			TotalRevenue         float64  `json:"total_revenue,omitempty"`
			TopDrivers           []string `json:"top_drivers"`
		} `json:"global_stats"`
	}

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

	// Get driver names from ALL trips (both standalone and containers)
	baseQuery := h.DB.Model(&Models.TripStruct{}).
		Where("company = ? AND deleted_at IS NULL", "Watanya")

	if startDate != "" && endDate != "" {
		baseQuery = baseQuery.Where("date BETWEEN ? AND ?", startDate, endDate)
	}

	var driverNames []string
	if err := baseQuery.Distinct("driver_name").
		Where("driver_name IS NOT NULL AND driver_name != ''").
		Pluck("driver_name", &driverNames).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch driver names",
			"error":   err.Error(),
		})
	}

	if len(driverNames) == 0 {
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
					TopDrivers: make([]string, 0),
				},
			},
			"hasFinancialAccess": hasFinancialAccess,
		})
	}

	response := DriverAnalyticsResponse{
		Drivers: make([]DriverAnalytics, 0, len(driverNames)),
	}

	var globalTotalTrips int64 = 0
	var globalTotalDistance float64 = 0
	var globalTotalVolume float64 = 0
	var globalTotalFees float64 = 0
	var globalTotalRevenue float64 = 0
	var globalTotalWorkingDays int64 = 0
	var globalDistinctDays int64 = 0

	// Count global distinct days from ALL trips
	h.DB.Raw(`
		SELECT COUNT(DISTINCT date)
		FROM trips
		WHERE company = 'Watanya' AND deleted_at IS NULL
		AND (date >= ? OR ? = '')
		AND (date <= ? OR ? = '')
	`, startDate, startDate, endDate, endDate).Row().Scan(&globalDistinctDays)

	if globalDistinctDays == 0 {
		globalDistinctDays = 1
	}

	type DriverPerformance struct {
		Name    string
		Revenue float64
	}
	driverPerformance := make([]DriverPerformance, 0, len(driverNames))

	for _, driverName := range driverNames {
		if driverName == "" {
			continue
		}

		driverAnalytics := DriverAnalytics{
			DriverName: driverName,
		}

		// Route distribution - count unique parent_trip_id + standalone trips
		var routeDistribution []struct {
			Terminal     string  `json:"terminal"`
			DropOffPoint string  `json:"drop_off_point"`
			Count        int64   `json:"count"`
			Distance     float64 `json:"distance"`
			Percent      float64 `json:"percent"`
		}

		err := h.DB.Raw(`
			SELECT terminal, drop_off_point, COUNT(*) as count
			FROM (
				SELECT terminal, drop_off_point, parent_trip_id
				FROM trips
				WHERE company = 'Watanya' 
				AND driver_name = ? 
				AND deleted_at IS NULL
				AND parent_trip_id IS NOT NULL 
				AND parent_trip_id != 0
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
				GROUP BY terminal, drop_off_point, parent_trip_id
				UNION ALL
				SELECT terminal, drop_off_point, ID as parent_trip_id
				FROM trips
				WHERE company = 'Watanya' 
				AND driver_name = ? 
				AND deleted_at IS NULL
				AND (parent_trip_id IS NULL OR parent_trip_id = 0)
				AND (date >= ? OR ? = '')
				AND (date <= ? OR ? = '')
			) AS all_trips
			GROUP BY terminal, drop_off_point
			ORDER BY count DESC
		`, driverName, startDate, startDate, endDate, endDate,
			driverName, startDate, startDate, endDate, endDate).
			Find(&routeDistribution).Error

		if err != nil {
			log.Printf("Error fetching route distribution for driver %s: %v", driverName, err)
		}

		driverAnalytics.RouteDistribution = routeDistribution

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

		dailyRouteRevenuesMap := make(map[string]float64)
		dailyTripsMap := make(map[string]int64)

		for _, route := range routeDistribution {
			var feeMapping Models.FeeMapping
			err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
				"Watanya", route.Terminal, route.DropOffPoint).
				First(&feeMapping).Error

			if err != nil {
				log.Printf("No fee mapping found for route %s → %s", route.Terminal, route.DropOffPoint)
				continue
			}

			var feeAmount float64
			switch int64(feeMapping.Fee) {
			case 1:
				feeAmount = 95.0
			case 2:
				feeAmount = 111.0
			case 3:
				feeAmount = 118.0
			case 4:
				feeAmount = 142.0
			case 5:
				feeAmount = 167.0
			case 6:
				feeAmount = 179.0
			case 7:
				feeAmount = 191.0
			default:
				feeAmount = 0.0
			}
			feeAmount *= 1.14

			// Get daily route details
			// Count: unique parent_trip_id + standalone trips
			// Sum: ALL volumes and distances (including all containers)
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
					COALESCE(SUM(volume), 0) as volume,
					COALESCE(SUM(distance), 0) as distance
				FROM (
					SELECT 
						t.date,
						t.parent_trip_id,
						SUM(t.tank_capacity) as volume,
						SUM(? * 1) as distance
					FROM trips t
					WHERE t.company = 'Watanya' 
					AND t.driver_name = ? 
					AND t.terminal = ? 
					AND t.drop_off_point = ? 
					AND t.deleted_at IS NULL
					AND t.parent_trip_id IS NOT NULL 
					AND t.parent_trip_id != 0
					AND (t.date >= ? OR ? = '')
					AND (t.date <= ? OR ? = '')
					GROUP BY t.date, t.parent_trip_id
					UNION ALL
					SELECT 
						t.date,
						t.ID as parent_trip_id,
						t.tank_capacity as volume,
						? * 1 as distance
					FROM trips t
					WHERE t.company = 'Watanya' 
					AND t.driver_name = ? 
					AND t.terminal = ? 
					AND t.drop_off_point = ? 
					AND t.deleted_at IS NULL
					AND (t.parent_trip_id IS NULL OR t.parent_trip_id = 0)
					AND (t.date >= ? OR ? = '')
					AND (t.date <= ? OR ? = '')
				) AS combined_trips
				GROUP BY date
			`, feeMapping.Distance, driverName, route.Terminal, route.DropOffPoint,
				startDate, startDate, endDate, endDate,
				feeMapping.Distance, driverName, route.Terminal, route.DropOffPoint,
				startDate, startDate, endDate, endDate).
				Find(&dailyRouteDetails).Error

			if err != nil {
				log.Printf("Error fetching daily route details: %v", err)
				continue
			}

			for _, daily := range dailyRouteDetails {
				// Calculate route revenue based on volume (all containers)
				routeRevenue := (feeAmount * daily.Volume) / 1000.0

				dailyRouteRevenuesMap[daily.Date] += routeRevenue
				dailyTripsMap[daily.Date] += daily.Count
				totalTrips += daily.Count
				totalVolume += daily.Volume
				totalDistance += daily.Distance
				totalFees += routeRevenue
				totalRevenue += routeRevenue
			}
		}

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

			if dailyTripsMap[date] > 0 {
				workingDays++
			}
		}

		sort.Slice(activityHeatmap, func(i, j int) bool {
			return activityHeatmap[i].Date < activityHeatmap[j].Date
		})

		driverAnalytics.ActivityHeatmap = activityHeatmap
		driverAnalytics.TotalTrips = totalTrips
		driverAnalytics.TotalDistance = totalDistance
		driverAnalytics.TotalVolume = totalVolume
		driverAnalytics.TotalFees = totalFees
		driverAnalytics.TotalRevenue = totalRevenue
		driverAnalytics.WorkingDays = workingDays

		if workingDays > 0 {
			driverAnalytics.AvgTripsPerDay = float64(totalTrips) / float64(workingDays)
			driverAnalytics.AvgKmPerDay = totalDistance / float64(workingDays)
			driverAnalytics.AvgFeesPerDay = totalFees / float64(workingDays)
		}

		if totalDistance > 0 {
			driverAnalytics.AvgTripsPerKm = float64(totalTrips) / totalDistance
			driverAnalytics.AvgVolumePerKm = totalVolume / totalDistance
		}

		globalTotalTrips += totalTrips
		globalTotalDistance += totalDistance
		globalTotalVolume += totalVolume
		globalTotalFees += totalFees
		globalTotalRevenue += totalRevenue
		globalTotalWorkingDays += workingDays

		driverPerformance = append(driverPerformance, DriverPerformance{
			Name:    driverName,
			Revenue: totalFees,
		})

		if !hasFinancialAccess {
			driverAnalytics.TotalFees = 0
			driverAnalytics.TotalRevenue = 0
			driverAnalytics.TotalVAT = 0
			driverAnalytics.TotalAmount = 0
			driverAnalytics.AvgFeesPerDay = 0
		}

		response.Drivers = append(response.Drivers, driverAnalytics)
	}

	driverCount := len(response.Drivers)
	if driverCount > 0 {
		response.GlobalStats.AvgTripsPerDriver = float64(globalTotalTrips) / float64(driverCount)
		response.GlobalStats.AvgDistancePerDriver = globalTotalDistance / float64(driverCount)
	}

	if globalTotalWorkingDays > 0 {
		response.GlobalStats.AvgTripsPerDay = float64(globalTotalTrips) / float64(globalTotalWorkingDays)
		response.GlobalStats.AvgRevenuePerDay = globalTotalFees / float64(globalTotalWorkingDays)
	} else {
		response.GlobalStats.AvgTripsPerDay = 0
		response.GlobalStats.AvgRevenuePerDay = 0
	}

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

	globalAvgRevenuePerDay := response.GlobalStats.AvgRevenuePerDay
	for i := range response.Drivers {
		if response.Drivers[i].TotalFees > 0 && globalAvgRevenuePerDay > 0 && response.Drivers[i].WorkingDays > 0 {
			driverDailyRevenue := response.Drivers[i].TotalFees / float64(response.Drivers[i].WorkingDays)
			response.Drivers[i].Efficiency = driverDailyRevenue / globalAvgRevenuePerDay
		} else {
			response.Drivers[i].Efficiency = 0
		}
	}

	sort.Slice(driverPerformance, func(i, j int) bool {
		return driverPerformance[i].Revenue > driverPerformance[j].Revenue
	})

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

// GetWatanyaTripsReport generates an Excel report for Watanya trips within a date range// Data structures for grouping
// GetWatanyaTripsReport generates a modern Excel report for Watanya trips
func (h *TripHandler) GetWatanyaTripsReport(c *fiber.Ctx) error {
	var req DateRangeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse request body",
		})
	}

	if req.StartDate == "" || req.EndDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Start date and end date are required",
		})
	}

	var trips []Models.TripStruct
	if err := h.DB.Where("company = ? AND date BETWEEN ? AND ?",
		"Watanya", req.StartDate, req.EndDate).Find(&trips).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error: " + err.Error(),
		})
	}

	// Fee index to amount mapping
	feeIndexToAmount := map[int]float64{
		1:  95.0,
		2:  111.0,
		3:  118.0,
		4:  142.0,
		5:  167.0,
		6:  179.0,
		7:  191.0,
		8:  214.0,
		9:  238.0,
		10: 262.0,
		11: 286.0,
		12: 310.0,
		13: 334.0,
		14: 358.0,
		15: 382.0,
	}

	// Get fee mappings
	var feeMappings []Models.FeeMapping
	if err := h.DB.Where("company = ?", "Watanya").Find(&feeMappings).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve fee mappings: " + err.Error(),
		})
	}

	feeMappingMap := make(map[string]Models.FeeMapping)
	for _, m := range feeMappings {
		feeMappingMap[m.Terminal+"|"+m.DropOffPoint] = m
	}

	// Build hierarchical structure: DropOff -> Terminal -> Trips
	dropOffData := make(map[string]map[string][]Models.TripStruct)
	routeFeeInfo := make(map[string]struct {
		Distance  float64
		FeeIndex  int
		ActualFee float64
	})

	for _, trip := range trips {
		key := trip.Terminal + "|" + trip.DropOffPoint
		feeMapping, exists := feeMappingMap[key]
		if !exists {
			continue
		}

		actualFee := feeIndexToAmount[int(feeMapping.Fee)]
		routeFeeInfo[key] = struct {
			Distance  float64
			FeeIndex  int
			ActualFee float64
		}{feeMapping.Distance, int(feeMapping.Fee), actualFee}

		if dropOffData[trip.DropOffPoint] == nil {
			dropOffData[trip.DropOffPoint] = make(map[string][]Models.TripStruct)
		}
		dropOffData[trip.DropOffPoint][trip.Terminal] = append(
			dropOffData[trip.DropOffPoint][trip.Terminal], trip)
	}

	// Build DropOffGroups with row calculations
	type RouteTable struct {
		Terminal string
		DropOff  string
		Trips    []Models.TripStruct
		FeeInfo  struct {
			Distance  float64
			FeeIndex  int
			ActualFee float64
		}
		RowCount int
	}

	type DropOffGroup struct {
		DropOffPoint string
		Tables       []RouteTable
		TotalRows    int
	}

	var dropOffGroups []DropOffGroup
	const (
		tableOverhead   = 3 // title + header + total row
		interTableSpace = 2
	)

	for dropOff, terminals := range dropOffData {
		group := DropOffGroup{DropOffPoint: dropOff}

		terminalNames := make([]string, 0, len(terminals))
		for t := range terminals {
			terminalNames = append(terminalNames, t)
		}
		sort.Strings(terminalNames)

		for _, terminal := range terminalNames {
			tripList := terminals[terminal]
			key := terminal + "|" + dropOff
			feeInfo := routeFeeInfo[key]

			table := RouteTable{
				Terminal: terminal,
				DropOff:  dropOff,
				Trips:    tripList,
				RowCount: len(tripList) + tableOverhead,
			}
			table.FeeInfo.Distance = feeInfo.Distance
			table.FeeInfo.FeeIndex = feeInfo.FeeIndex
			table.FeeInfo.ActualFee = feeInfo.ActualFee

			group.Tables = append(group.Tables, table)
		}

		// Calculate total rows for this group
		group.TotalRows = 0
		for _, t := range group.Tables {
			group.TotalRows += t.RowCount
		}
		if len(group.Tables) > 1 {
			group.TotalRows += (len(group.Tables) - 1) * interTableSpace
		}

		dropOffGroups = append(dropOffGroups, group)
	}

	sort.Slice(dropOffGroups, func(i, j int) bool {
		return dropOffGroups[i].DropOffPoint < dropOffGroups[j].DropOffPoint
	})

	// Build summaries for summary sheet - now grouped by drop-off
	type WatanyaReportSummary struct {
		ID            int
		DropOffPoint  string
		Terminal      string
		TripCount     int
		TotalCapacity int
		Distance      float64
		Fee           int
		ActualFee     float64
		Cost          float64
		IsSubtotal    bool // Flag to mark subtotal rows
	}

	var summaries []WatanyaReportSummary
	idCounter := 1

	for _, group := range dropOffGroups {
		var groupTripCount, groupCapacity int
		var groupCost float64

		for _, table := range group.Tables {
			var totalCapacity int
			for _, trip := range table.Trips {
				totalCapacity += trip.TankCapacity
			}
			cost := float64(totalCapacity) * table.FeeInfo.ActualFee / 1000.0

			summaries = append(summaries, WatanyaReportSummary{
				ID:            idCounter,
				DropOffPoint:  table.DropOff,
				Terminal:      table.Terminal,
				TripCount:     len(table.Trips),
				TotalCapacity: totalCapacity,
				Distance:      table.FeeInfo.Distance,
				Fee:           table.FeeInfo.FeeIndex,
				ActualFee:     table.FeeInfo.ActualFee,
				Cost:          cost,
				IsSubtotal:    false,
			})
			idCounter++

			// Accumulate for subtotal
			groupTripCount += len(table.Trips)
			groupCapacity += totalCapacity
			groupCost += cost
		}

		// Add subtotal row for this drop-off point
		summaries = append(summaries, WatanyaReportSummary{
			DropOffPoint:  group.DropOffPoint,
			TripCount:     groupTripCount,
			TotalCapacity: groupCapacity,
			Cost:          groupCost,
			IsSubtotal:    true,
		})
	}

	// Create Excel file
	f := excelize.NewFile()

	// =====================================================
	// COLOR PALETTE - Professional Blue Theme
	// =====================================================
	colors := struct {
		HeaderBg   string
		HeaderText string
		AltRow     string
		Border     string
		White      string
		TextDark   string
		TotalBg    string
		AccentBlue string
		SubtotalBg string
	}{
		HeaderBg:   "2C5F7C",
		HeaderText: "FFFFFF",
		AltRow:     "F8FAFB",
		Border:     "D5DBDE",
		White:      "FFFFFF",
		TextDark:   "2C3E50",
		TotalBg:    "E8F1F5",
		AccentBlue: "4A90A4",
		SubtotalBg: "D4E6ED", // Slightly darker than TotalBg for subtotals
	}

	// =====================================================
	// SUMMARY SHEET
	// =====================================================
	summarySheetName := "Summary"
	f.SetSheetName("Sheet1", summarySheetName)

	// Title style - larger font
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   20,
			Color:  colors.HeaderBg,
			Family: "Cairo",
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})

	// Subtitle style - larger font
	subtitleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Size:   14,
			Color:  "7F8C8D",
			Family: "Cairo",
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})

	// Header style - larger font
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   13,
			Color:  colors.HeaderText,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.HeaderBg},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
			WrapText:   true,
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colors.HeaderBg, Style: 2},
		},
	})

	// Data row style - larger font
	dataStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Size:   12,
			Color:  colors.TextDark,
			Family: "Cairo",
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colors.Border, Style: 1},
		},
	})

	// Alternate row style - larger font
	dataAltStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Size:   12,
			Color:  colors.TextDark,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.AltRow},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colors.Border, Style: 1},
		},
	})

	// Subtotal row style - larger font
	subtotalStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   12,
			Color:  colors.AccentBlue,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.SubtotalBg},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "top", Color: colors.AccentBlue, Style: 1},
			{Type: "bottom", Color: colors.AccentBlue, Style: 2},
		},
	})

	// Grand total row style - larger font
	totalStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   13,
			Color:  colors.HeaderText,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.HeaderBg},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "top", Color: colors.HeaderBg, Style: 2},
			{Type: "bottom", Color: colors.HeaderBg, Style: 2},
		},
	})

	// Row 1: Title
	f.MergeCell(summarySheetName, "A1", "I1")
	f.SetCellValue(summarySheetName, "A1", "Watanya Trips Report")
	f.SetCellStyle(summarySheetName, "A1", "I1", titleStyle)
	f.SetRowHeight(summarySheetName, 1, 32)

	// Row 2: Date range
	f.MergeCell(summarySheetName, "A2", "I2")
	f.SetCellValue(summarySheetName, "A2", fmt.Sprintf("%s  —  %s", req.StartDate, req.EndDate))
	f.SetCellStyle(summarySheetName, "A2", "I2", subtitleStyle)
	f.SetRowHeight(summarySheetName, 2, 24)

	// Row 3: Spacer
	f.SetRowHeight(summarySheetName, 3, 12)

	// Row 4: Headers
	summaryHeaders := []string{"#", "Drop-off Point", "Terminal", "Trips", "Capacity (L)", "Distance", "Fee Idx", "Fee Amt", "Total Cost"}
	for i, h := range summaryHeaders {
		f.SetCellValue(summarySheetName, fmt.Sprintf("%c4", 'A'+i), h)
	}
	f.SetCellStyle(summarySheetName, "A4", "I4", headerStyle)
	f.SetRowHeight(summarySheetName, 4, 28)

	// Track column max widths for auto-fit
	colMaxWidths := make(map[int]float64)
	// Initialize with header widths
	for i, h := range summaryHeaders {
		colMaxWidths[i] = float64(len(h)) * 1.3 // Factor for font size 13
	}

	// Track station positions for page breaks
	type SummaryStationPosition struct {
		StartRow int
		EndRow   int
		DropOff  string
	}
	var stationPositions []SummaryStationPosition
	var currentStationStart int

	// Data rows with subtotals
	currentRow := 5
	dataRowIndex := 0
	currentDropOff := ""

	for _, s := range summaries {
		if s.IsSubtotal {
			// Subtotal row
			subtotalText := fmt.Sprintf("Subtotal: %s", s.DropOffPoint)
			f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", currentRow), subtotalText)
			f.SetCellValue(summarySheetName, fmt.Sprintf("D%d", currentRow), s.TripCount)
			f.SetCellValue(summarySheetName, fmt.Sprintf("E%d", currentRow), s.TotalCapacity)
			f.SetCellValue(summarySheetName, fmt.Sprintf("I%d", currentRow), s.Cost)
			f.SetCellStyle(summarySheetName, fmt.Sprintf("A%d", currentRow), fmt.Sprintf("I%d", currentRow), subtotalStyle)
			f.SetRowHeight(summarySheetName, currentRow, 26)

			// Update max width for subtotal text
			subtotalWidth := float64(len(subtotalText)) * 1.2
			if subtotalWidth > colMaxWidths[1] {
				colMaxWidths[1] = subtotalWidth
			}

			// Record station position
			stationPositions = append(stationPositions, SummaryStationPosition{
				StartRow: currentStationStart,
				EndRow:   currentRow,
				DropOff:  s.DropOffPoint,
			})

			dataRowIndex = 0
			currentDropOff = ""
		} else {
			// Track station start
			if currentDropOff != s.DropOffPoint {
				currentDropOff = s.DropOffPoint
				currentStationStart = currentRow
			}

			// Regular data row
			isAlt := dataRowIndex%2 == 1

			f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", currentRow), s.ID)
			f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", currentRow), s.DropOffPoint)
			f.SetCellValue(summarySheetName, fmt.Sprintf("C%d", currentRow), s.Terminal)
			f.SetCellValue(summarySheetName, fmt.Sprintf("D%d", currentRow), s.TripCount)
			f.SetCellValue(summarySheetName, fmt.Sprintf("E%d", currentRow), s.TotalCapacity)
			f.SetCellValue(summarySheetName, fmt.Sprintf("F%d", currentRow), s.Distance)
			f.SetCellValue(summarySheetName, fmt.Sprintf("G%d", currentRow), s.Fee)
			f.SetCellValue(summarySheetName, fmt.Sprintf("H%d", currentRow), s.ActualFee)
			f.SetCellValue(summarySheetName, fmt.Sprintf("I%d", currentRow), s.Cost)

			// Track max widths
			rowValues := []string{
				fmt.Sprintf("%d", s.ID),
				s.DropOffPoint,
				s.Terminal,
				fmt.Sprintf("%d", s.TripCount),
				fmt.Sprintf("%d", s.TotalCapacity),
				fmt.Sprintf("%.1f", s.Distance),
				fmt.Sprintf("%d", s.Fee),
				fmt.Sprintf("%.1f", s.ActualFee),
				fmt.Sprintf("%.2f", s.Cost),
			}
			for i, val := range rowValues {
				width := float64(len(val)) * 1.2 // Factor for font size 12
				if width > colMaxWidths[i] {
					colMaxWidths[i] = width
				}
			}

			if isAlt {
				f.SetCellStyle(summarySheetName, fmt.Sprintf("A%d", currentRow), fmt.Sprintf("I%d", currentRow), dataAltStyle)
			} else {
				f.SetCellStyle(summarySheetName, fmt.Sprintf("A%d", currentRow), fmt.Sprintf("I%d", currentRow), dataStyle)
			}
			f.SetRowHeight(summarySheetName, currentRow, 24)
			dataRowIndex++
		}
		currentRow++
	}

	// Grand Total row
	totalRow := currentRow
	f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", totalRow), "GRAND TOTAL")

	// Calculate grand totals from subtotal rows
	var grandTripCount, grandCapacity int
	var grandCost float64
	for _, s := range summaries {
		if s.IsSubtotal {
			grandTripCount += s.TripCount
			grandCapacity += s.TotalCapacity
			grandCost += s.Cost
		}
	}

	f.SetCellValue(summarySheetName, fmt.Sprintf("D%d", totalRow), grandTripCount)
	f.SetCellValue(summarySheetName, fmt.Sprintf("E%d", totalRow), grandCapacity)
	f.SetCellValue(summarySheetName, fmt.Sprintf("I%d", totalRow), grandCost)
	f.SetCellStyle(summarySheetName, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("I%d", totalRow), totalStyle)
	f.SetRowHeight(summarySheetName, totalRow, 28)

	// Update max widths for grand total
	grandTotalValues := []string{
		"",
		"GRAND TOTAL",
		"",
		fmt.Sprintf("%d", grandTripCount),
		fmt.Sprintf("%d", grandCapacity),
		"",
		"",
		"",
		fmt.Sprintf("%.2f", grandCost),
	}
	for i, val := range grandTotalValues {
		if val != "" {
			width := float64(len(val)) * 1.3
			if width > colMaxWidths[i] {
				colMaxWidths[i] = width
			}
		}
	}

	// Apply auto-fit column widths with minimum and padding
	colLetters := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
	minWidths := []float64{5, 18, 16, 8, 14, 10, 9, 10, 12}
	for i, col := range colLetters {
		width := colMaxWidths[i] + 3 // Add padding
		if width < minWidths[i] {
			width = minWidths[i]
		}
		f.SetColWidth(summarySheetName, col, col, width)
	}

	// =====================================================
	// SUMMARY SHEET PAGE BREAKS - Keep stations together
	// =====================================================
	const summaryPageLimit = 50
	var summaryPageBreaks []int
	summaryPageStart := 1 // Account for title, subtitle, spacer, header rows

	for i := 0; i < len(stationPositions); i++ {
		stationPos := stationPositions[i]
		rowsUsedIfAdded := stationPos.EndRow - summaryPageStart + 1

		if rowsUsedIfAdded > summaryPageLimit && stationPos.StartRow > summaryPageStart {
			// Insert page break before this station
			summaryPageBreaks = append(summaryPageBreaks, stationPos.StartRow)
			summaryPageStart = stationPos.StartRow
		}
	}

	// Insert summary sheet page breaks
	for _, row := range summaryPageBreaks {
		f.InsertPageBreak(summarySheetName, fmt.Sprintf("A%d", row))
	}

	// =====================================================
	// DETAILED SHEET (unchanged from original)
	// =====================================================
	detailedSheetName := "Trip Details"
	f.NewSheet(detailedSheetName)

	// Column widths - optimized for portrait A4
	detailColWidths := map[string]float64{
		"A": 4, "B": 11, "C": 11, "D": 17, "E": 10,
		"F": 10, "G": 9, "H": 9, "I": 10,
	}
	for col, width := range detailColWidths {
		f.SetColWidth(detailedSheetName, col, col, width)
	}

	// Table title style
	tableTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   11,
			Color:  colors.HeaderText,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.HeaderBg},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})

	// Table header style
	tableHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   9,
			Color:  colors.HeaderBg,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.TotalBg},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colors.AccentBlue, Style: 2},
		},
	})

	// Detail data style
	detailDataStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Size:   9,
			Color:  colors.TextDark,
			Family: "Cairo",
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colors.Border, Style: 1},
		},
	})

	// Detail alternate row style
	detailAltStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Size:   9,
			Color:  colors.TextDark,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.AltRow},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colors.Border, Style: 1},
		},
	})

	// Table total style
	tableTotalStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   9,
			Color:  colors.HeaderBg,
			Family: "Cairo",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colors.TotalBg},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "top", Color: colors.AccentBlue, Style: 2},
		},
	})

	tripHeaders := []string{"#", "Date", "Receipt", "Driver", "Plate", "Capacity", "Mileage", "Fee", "Cost"}

	// Track positions for page breaks
	type GroupPosition struct {
		StartRow      int
		EndRow        int
		EndRowWithGap int
		DropOff       string
	}
	var groupPositions []GroupPosition

	detailRow := 1

	for gi, group := range dropOffGroups {
		groupStartRow := detailRow

		for ti, table := range group.Tables {
			// Title row
			title := fmt.Sprintf("%s  →  %s", table.Terminal, table.DropOff)
			f.SetCellValue(detailedSheetName, fmt.Sprintf("A%d", detailRow), title)
			f.MergeCell(detailedSheetName, fmt.Sprintf("A%d", detailRow), fmt.Sprintf("I%d", detailRow))
			f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", detailRow), fmt.Sprintf("I%d", detailRow), tableTitleStyle)
			f.SetRowHeight(detailedSheetName, detailRow, 22)
			detailRow++

			// Header row
			for i, h := range tripHeaders {
				f.SetCellValue(detailedSheetName, fmt.Sprintf("%c%d", 'A'+i, detailRow), h)
			}
			f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", detailRow), fmt.Sprintf("I%d", detailRow), tableHeaderStyle)
			f.SetRowHeight(detailedSheetName, detailRow, 18)
			detailRow++

			// Data rows
			dataStartRow := detailRow
			for i, trip := range table.Trips {
				tripCost := float64(trip.TankCapacity) * table.FeeInfo.ActualFee / 1000.0
				isAlt := i%2 == 1

				f.SetCellValue(detailedSheetName, fmt.Sprintf("A%d", detailRow), i+1)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("B%d", detailRow), trip.Date)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("C%d", detailRow), trip.ReceiptNo)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("D%d", detailRow), trip.DriverName)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("E%d", detailRow), trip.CarNoPlate)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("F%d", detailRow), trip.TankCapacity)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("G%d", detailRow), trip.Mileage)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("H%d", detailRow), table.FeeInfo.ActualFee)
				f.SetCellValue(detailedSheetName, fmt.Sprintf("I%d", detailRow), tripCost)

				if isAlt {
					f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", detailRow), fmt.Sprintf("I%d", detailRow), detailAltStyle)
				} else {
					f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", detailRow), fmt.Sprintf("I%d", detailRow), detailDataStyle)
				}
				f.SetRowHeight(detailedSheetName, detailRow, 16)
				detailRow++
			}

			// Total row
			f.SetCellValue(detailedSheetName, fmt.Sprintf("B%d", detailRow), "TOTAL")
			f.SetCellFormula(detailedSheetName, fmt.Sprintf("F%d", detailRow), fmt.Sprintf("SUM(F%d:F%d)", dataStartRow, detailRow-1))
			f.SetCellFormula(detailedSheetName, fmt.Sprintf("G%d", detailRow), fmt.Sprintf("SUM(G%d:G%d)", dataStartRow, detailRow-1))
			f.SetCellFormula(detailedSheetName, fmt.Sprintf("I%d", detailRow), fmt.Sprintf("SUM(I%d:I%d)", dataStartRow, detailRow-1))
			f.SetCellStyle(detailedSheetName, fmt.Sprintf("A%d", detailRow), fmt.Sprintf("I%d", detailRow), tableTotalStyle)
			f.SetRowHeight(detailedSheetName, detailRow, 18)
			detailRow++

			if ti < len(group.Tables)-1 {
				detailRow += interTableSpace
			}
		}

		groupEndRow := detailRow - 1

		if gi < len(dropOffGroups)-1 {
			detailRow += 3
		}

		groupPositions = append(groupPositions, GroupPosition{
			StartRow:      groupStartRow,
			EndRow:        groupEndRow,
			EndRowWithGap: detailRow - 1,
			DropOff:       group.DropOffPoint,
		})
	}

	// Page breaks
	const pageLimit = 50
	var pageBreaks []int
	currentPageStart := 1

	for i := 0; i < len(groupPositions); i++ {
		groupPos := groupPositions[i]
		rowsUsedIfAdded := groupPos.EndRow - currentPageStart + 1

		if rowsUsedIfAdded > pageLimit {
			pageBreaks = append(pageBreaks, groupPos.StartRow)
			currentPageStart = groupPos.StartRow
		}
	}

	for _, row := range pageBreaks {
		f.InsertPageBreak(detailedSheetName, fmt.Sprintf("A%d", row))
	}

	// Print settings
	orientation := "portrait"
	paperSize := 9
	f.SetPageLayout(detailedSheetName, &excelize.PageLayoutOptions{
		Orientation: &orientation,
		Size:        &paperSize,
	})

	marginLeft, marginRight := 0.3, 0.3
	marginTop, marginBottom := 0.4, 0.4
	f.SetPageMargins(detailedSheetName, &excelize.PageLayoutMarginsOptions{
		Left:   &marginLeft,
		Right:  &marginRight,
		Top:    &marginTop,
		Bottom: &marginBottom,
	})

	f.SetPageLayout(summarySheetName, &excelize.PageLayoutOptions{
		Orientation: &orientation,
		Size:        &paperSize,
	})
	f.SetPageMargins(summarySheetName, &excelize.PageLayoutMarginsOptions{
		Left:   &marginLeft,
		Right:  &marginRight,
		Top:    &marginTop,
		Bottom: &marginBottom,
	})

	idx, _ := f.GetSheetIndex(summarySheetName)
	f.SetActiveSheet(idx)

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("watanya_report_%s.xlsx", timestamp)
	filePath := filepath.Join(os.TempDir(), filename)

	if err := f.SaveAs(filePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate Excel report: " + err.Error(),
		})
	}

	return c.SendFile(filePath, true)
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
	osrmURL := fmt.Sprintf("http://localhost:5000/route/v1/driving/%f,%f;%f,%f?overview=full&steps=false",
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
	// 			To:      []string{"redacted@example.com", "redacted@example.com"},
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

// Helper function to apply receipt status filter - FIXED for any order
func (h *TripHandler) applyReceiptStatusFilter(query *gorm.DB, receiptStatus string) *gorm.DB {
	switch receiptStatus {
	case "pending":
		// Trips with no receipt steps at all
		query = query.Where("NOT EXISTS (SELECT 1 FROM receipt_steps WHERE receipt_steps.trip_id = trips.id)")

	case "in_garage":
		// Trips where the LATEST step is Garage
		// This means either: only has garage, OR has both but garage is more recent
		query = query.Where(`EXISTS (
            SELECT 1 FROM receipt_steps rs1
            WHERE rs1.trip_id = trips.id 
            AND rs1.location = 'Garage'
            AND NOT EXISTS (
                SELECT 1 FROM receipt_steps rs2
                WHERE rs2.trip_id = trips.id
                AND rs2.received_at > rs1.received_at
            )
        )`)

	case "in_office":
		// Trips where the LATEST step is Office
		// This means either: only has office, OR has both but office is more recent
		query = query.Where(`EXISTS (
            SELECT 1 FROM receipt_steps rs1
            WHERE rs1.trip_id = trips.id 
            AND rs1.location = 'Office'
            AND NOT EXISTS (
                SELECT 1 FROM receipt_steps rs2
                WHERE rs2.trip_id = trips.id
                AND rs2.received_at > rs1.received_at
            )
        )`)
	}
	return query
}

// Helper function to fetch trips with all their containers - FIXED
func (h *TripHandler) fetchTripsWithContainers(query *gorm.DB, limit, offset int) ([]Models.TripStruct, error) {
	// First, get the paginated trips
	var paginatedTrips []Models.TripStruct
	err := query.Limit(limit).Offset(offset).Find(&paginatedTrips).Error
	if err != nil {
		return nil, err
	}

	if len(paginatedTrips) == 0 {
		return paginatedTrips, nil
	}

	// Collect all parent IDs from containers in the paginated results
	parentIDs := make(map[uint]bool)
	for _, trip := range paginatedTrips {
		if trip.ParentTripID != nil && *trip.ParentTripID > 0 {
			parentIDs[*trip.ParentTripID] = true
		}
	}

	// If we have containers, fetch ALL siblings for those parents
	if len(parentIDs) > 0 {
		parentIDList := make([]uint, 0, len(parentIDs))
		for id := range parentIDs {
			parentIDList = append(parentIDList, id)
		}

		// Fetch all containers that belong to these parent IDs WITH ParentTrip and ReceiptBatch
		var allContainers []Models.TripStruct
		h.DB.Preload("ReceiptSteps").
			Preload("ParentTrip.ReceiptBatch.Receipts").
			Preload("ParentTrip.ReceiptBatch.Driver").
			Where("parent_trip_id IN ?", parentIDList).
			Find(&allContainers)

		// Create a map to track which trips we already have
		tripMap := make(map[uint]Models.TripStruct)
		for _, trip := range paginatedTrips {
			tripMap[trip.ID] = trip
		}

		// Add all containers (including ones not in original pagination)
		for _, container := range allContainers {
			tripMap[container.ID] = container
		}

		// Convert back to slice
		finalTrips := make([]Models.TripStruct, 0, len(tripMap))
		for _, trip := range tripMap {
			finalTrips = append(finalTrips, trip)
		}

		return finalTrips, nil
	}

	return paginatedTrips, nil
}

func (h *TripHandler) GetDriverTrips(c *fiber.Ctx) error {
	// ✅ Get driver from context (set by VerifyDriver middleware)
	driver, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Driver not found in context",
		})
	}

	// Get pagination parameters
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	// Optional filters
	searchTerm := c.Query("search", "")
	dateFrom := c.Query("date_from", "")
	dateTo := c.Query("date_to", "")
	company := c.Query("company", "")

	// ✅ Find current trip by checking active session
	var currentTrip *Models.ParentTrip
	var currentTripID *uint
	var activeSession *Models.DriverSession

	session, err := utils.GetActiveSession(h.DB, driver.ID)
	if err == nil {
		// Driver has active session - find linked ParentTrip
		activeSession = session

		var trip Models.ParentTrip
		if err := h.DB.Where("session_id = ?", session.ID).
			Preload("ContainerTrips", func(db *gorm.DB) *gorm.DB {
				return db.Order("receipt_no ASC").Preload("ReceiptSteps")
			}).First(&trip).Error; err == nil {

			currentTripID = &trip.ID

			// Collect location names for landmarks
			locationNames := make(map[string]bool)
			for j := range trip.ContainerTrips {
				if trip.ContainerTrips[j].LocationName != "" {
					locationNames[trip.ContainerTrips[j].LocationName] = true
				}
			}

			// Fetch landmarks
			var landmarks []Models.LandMark
			if len(locationNames) > 0 {
				names := make([]string, 0, len(locationNames))
				for name := range locationNames {
					names = append(names, name)
				}
				h.DB.Where("name IN ?", names).Find(&landmarks)
			}

			// Create landmark map
			landmarkMap := make(map[string]Models.LandMark)
			for _, landmark := range landmarks {
				landmarkMap[landmark.Name] = landmark
			}

			// Calculate trip details
			var totalRevenue float64
			var calculatedTankCapacity int

			for j := range trip.ContainerTrips {
				containerTrip := &trip.ContainerTrips[j]
				calculatedTankCapacity += containerTrip.TankCapacity

				// Get fee mapping
				var mapping Models.FeeMapping
				h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
					containerTrip.Company, containerTrip.Terminal, containerTrip.DropOffPoint).
					First(&mapping)

				if mapping.ID > 0 {
					containerTrip.Distance = mapping.Distance
					containerTrip.Fee = mapping.Fee
					totalRevenue += mapping.Fee

					// Populate location from fee mapping
					containerTrip.Location = Models.LandMark{
						Name:      containerTrip.DropOffPoint,
						Latitude:  fmt.Sprintf("%f", mapping.Latitude),
						Longitude: fmt.Sprintf("%f", mapping.Longitude),
					}
				}

				// Assign landmark if exists
				if landmark, exists := landmarkMap[containerTrip.LocationName]; exists {
					containerTrip.Location = landmark
				}
			}

			trip.TankCapacity = calculatedTankCapacity
			trip.TotalRevenue = totalRevenue
			trip.TotalContainers = len(trip.ContainerTrips)
			currentTrip = &trip
		}
	}

	// ✅ Create base query for historical trips using driver.ID directly
	query := h.DB.Model(&Models.ParentTrip{}).
		Where("driver_id = ?", driver.ID).
		Order("date DESC").
		Preload("ContainerTrips", func(db *gorm.DB) *gorm.DB {
			return db.Order("receipt_no ASC").Preload("ReceiptSteps")
		})

	// Exclude current trip from history
	if currentTripID != nil {
		query = query.Where("id != ?", *currentTripID)
	}

	// Apply search filter
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		query = query.Where("car_no_plate LIKE ? OR terminal LIKE ? OR date LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern)
	}

	// Apply date range filter
	if dateFrom != "" {
		query = query.Where("date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("date <= ?", dateTo)
	}

	// Apply company filter
	if company != "" {
		query = query.Where("company = ?", company)
	}

	// ✅ Count total records using driver.ID
	var total int64
	countQuery := h.DB.Model(&Models.ParentTrip{}).Where("driver_id = ?", driver.ID)

	// Exclude current trip from count
	if currentTripID != nil {
		countQuery = countQuery.Where("id != ?", *currentTripID)
	}

	// Reapply filters for counting
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		countQuery = countQuery.Where("car_no_plate LIKE ? OR terminal LIKE ? OR date LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern)
	}

	if dateFrom != "" {
		countQuery = countQuery.Where("date >= ?", dateFrom)
	}
	if dateTo != "" {
		countQuery = countQuery.Where("date <= ?", dateTo)
	}
	if company != "" {
		countQuery = countQuery.Where("company = ?", company)
	}
	countQuery.Count(&total)

	// Fetch parent trips with pagination
	var parentTrips []Models.ParentTrip
	if err := query.Limit(limit).Offset(offset).Find(&parentTrips).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch driver trips",
			"error":   err.Error(),
		})
	}

	// Collect all unique location names and terminals for batch fetching
	locationNames := make(map[string]bool)
	terminalNames := make(map[string]bool)

	for i := range parentTrips {
		terminalNames[parentTrips[i].Terminal] = true
		for j := range parentTrips[i].ContainerTrips {
			if parentTrips[i].ContainerTrips[j].LocationName != "" {
				locationNames[parentTrips[i].ContainerTrips[j].LocationName] = true
			}
		}
	}

	// Fetch all landmarks in one query
	var landmarks []Models.LandMark
	if len(locationNames) > 0 {
		names := make([]string, 0, len(locationNames))
		for name := range locationNames {
			names = append(names, name)
		}
		h.DB.Where("name IN ?", names).Find(&landmarks)
	}

	// Fetch all terminals in one query
	var terminals []Models.Terminal
	if len(terminalNames) > 0 {
		names := make([]string, 0, len(terminalNames))
		for name := range terminalNames {
			names = append(names, name)
		}
		h.DB.Where("name IN ?", names).Find(&terminals)
	}

	// Create lookup maps
	landmarkMap := make(map[string]Models.LandMark)
	for _, landmark := range landmarks {
		landmarkMap[landmark.Name] = landmark
	}

	terminalMap := make(map[string]Models.Terminal)
	for _, terminal := range terminals {
		terminalMap[terminal.Name] = terminal
	}

	// Enrich each parent trip with calculated data
	for i := range parentTrips {
		var totalRevenue float64
		totalContainers := len(parentTrips[i].ContainerTrips)

		var calculatedTankCapacity int
		for j := range parentTrips[i].ContainerTrips {
			trip := &parentTrips[i].ContainerTrips[j]
			calculatedTankCapacity += trip.TankCapacity

			// Get fee mapping
			var mapping Models.FeeMapping
			h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
				trip.Company, trip.Terminal, trip.DropOffPoint).First(&mapping)

			if mapping.ID > 0 {
				trip.Distance = mapping.Distance
				trip.Fee = mapping.Fee
				totalRevenue += mapping.Fee

				// Populate location from fee mapping
				trip.Location = Models.LandMark{
					Name:      trip.DropOffPoint,
					Latitude:  fmt.Sprintf("%f", mapping.Latitude),
					Longitude: fmt.Sprintf("%f", mapping.Longitude),
				}
			}

			// Assign landmark if exists
			if landmark, exists := landmarkMap[trip.LocationName]; exists {
				trip.Location = landmark
			}
		}

		parentTrips[i].TankCapacity = calculatedTankCapacity
		parentTrips[i].TotalRevenue = totalRevenue
		parentTrips[i].TotalContainers = totalContainers
	}

	// Build response
	response := fiber.Map{
		"message": "Driver trips retrieved successfully",
		"data":    parentTrips,
		"meta": fiber.Map{
			"total": total,
			"page":  page,
			"limit": limit,
			"pages": (total + int64(limit) - 1) / int64(limit),
		},
	}

	// Add current trip info if exists
	if currentTrip != nil {
		response["current_trip"] = fiber.Map{
			"current_trip_id": currentTripID,
			"trip_details":    currentTrip,
			"session_id":      activeSession.ID,
			"session_start":   activeSession.StartTime,
		}
	} else {
		response["current_trip"] = fiber.Map{
			"current_trip_id": nil,
			"trip_details":    nil,
			"message":         "No active trip",
		}
	}

	return c.Status(http.StatusOK).JSON(response)
}

func (h *TripHandler) GetAllTrips(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	searchTerm := c.Query("search", "")
	missingData := c.Query("missing_data", "")
	receiptStatus := c.Query("receipt_status", "")

	// Create base query with proper sorting and preload ParentTrip with ReceiptBatch
	query := h.DB.Model(&Models.TripStruct{}).
		Order("date DESC, receipt_no DESC").
		Preload("ReceiptSteps").
		Preload("ParentTrip.ReceiptBatch.Receipts").
		Preload("ParentTrip.ReceiptBatch.Driver")

	// Add search condition
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		query = query.Where("car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	// Add missing data filter
	if missingData != "" {
		switch missingData {
		case "driver":
			query = query.Where("driver_name = ?", "غير مسجل")
		case "route":
			query = query.Where("drop_off_point = ?", "غير مسجل")
		case "any":
			query = query.Where("driver_name = ? OR drop_off_point = ?", "غير مسجل", "غير مسجل")
		}
	}

	// Apply receipt status filter BEFORE counting and pagination
	if receiptStatus != "" {
		query = h.applyReceiptStatusFilter(query, receiptStatus)
	}

	// Count total records AFTER all filters are applied
	var total int64
	countQuery := h.DB.Model(&Models.TripStruct{})

	// Reapply all filters for counting
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		countQuery = countQuery.Where("car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}
	if missingData != "" {
		switch missingData {
		case "driver":
			countQuery = countQuery.Where("driver_name = ?", "غير مسجل")
		case "route":
			countQuery = countQuery.Where("drop_off_point = ?", "غير مسجل")
		case "any":
			countQuery = countQuery.Where("driver_name = ? OR drop_off_point = ?", "غير مسجل", "غير مسجل")
		}
	}
	if receiptStatus != "" {
		countQuery = h.applyReceiptStatusFilter(countQuery, receiptStatus)
	}
	countQuery.Count(&total)

	// Fetch trips with all their containers
	trips, err := h.fetchTripsWithContainers(query, limit, offset)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   err.Error(),
		})
	}

	// Enrich trip data with fee mapping details
	for i := range trips {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint).First(&mapping)

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

// GetTripsByCompany - FIXED
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

	searchTerm := c.Query("search", "")
	missingData := c.Query("missing_data", "")
	receiptStatus := c.Query("receipt_status", "")

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	query := h.DB.Model(&Models.TripStruct{}).
		Where("company = ?", company).
		Preload("ReceiptSteps").
		Order("date DESC, receipt_no DESC")

	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		query = query.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	if missingData != "" {
		switch missingData {
		case "driver":
			query = query.Where("driver_name = ?", "غير مسجل")
		case "route":
			query = query.Where("drop_off_point = ?", "غير مسجل")
		case "any":
			query = query.Where("driver_name = ? OR drop_off_point = ?", "غير مسجل", "غير مسجل")
		}
	}

	if receiptStatus != "" {
		query = h.applyReceiptStatusFilter(query, receiptStatus)
	}

	// Count with same filters
	var total int64
	countQuery := h.DB.Model(&Models.TripStruct{}).Where("company = ?", company)
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		countQuery = countQuery.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}
	if missingData != "" {
		switch missingData {
		case "driver":
			countQuery = countQuery.Where("driver_name = ?", "غير مسجل")
		case "route":
			countQuery = countQuery.Where("drop_off_point = ?", "غير مسجل")
		case "any":
			countQuery = countQuery.Where("driver_name = ? OR drop_off_point = ?", "غير مسجل", "غير مسجل")
		}
	}
	if receiptStatus != "" {
		countQuery = h.applyReceiptStatusFilter(countQuery, receiptStatus)
	}
	countQuery.Count(&total)

	trips, err := h.fetchTripsWithContainers(query, limit, offset)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   err.Error(),
		})
	}

	for i := range trips {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint).First(&mapping)

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

// GetTripsByDate - FIXED
func (h *TripHandler) GetTripsByDate(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	if startDate == "" || endDate == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Start date and end date are required",
		})
	}

	searchTerm := c.Query("search", "")
	company := c.Query("company")
	missingData := c.Query("missing_data", "")
	receiptStatus := c.Query("receipt_status", "")

	company, err := url.QueryUnescape(company)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid company name format",
			"error":   err.Error(),
		})
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	offset := (page - 1) * limit

	query := h.DB.Model(&Models.TripStruct{}).
		Preload("ReceiptSteps").
		Where("date >= ? AND date <= ?", startDate, endDate).
		Order("date DESC, receipt_no DESC")

	if company != "" {
		query = query.Where("company = ?", company)
	}

	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		query = query.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	if missingData != "" {
		switch missingData {
		case "driver":
			query = query.Where("driver_name = ?", "غير مسجل")
		case "route":
			query = query.Where("drop_off_point = ?", "غير مسجل")
		case "any":
			query = query.Where("driver_name = ? OR drop_off_point = ?", "غير مسجل", "غير مسجل")
		}
	}

	if receiptStatus != "" {
		query = h.applyReceiptStatusFilter(query, receiptStatus)
	}

	// Count with same filters
	var total int64
	countQuery := h.DB.Model(&Models.TripStruct{}).Where("date >= ? AND date <= ?", startDate, endDate)
	if company != "" {
		countQuery = countQuery.Where("company = ?", company)
	}
	if searchTerm != "" {
		searchPattern := "%" + searchTerm + "%"
		countQuery = countQuery.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}
	if missingData != "" {
		switch missingData {
		case "driver":
			countQuery = countQuery.Where("driver_name = ?", "غير مسجل")
		case "route":
			countQuery = countQuery.Where("drop_off_point = ?", "غير مسجل")
		case "any":
			countQuery = countQuery.Where("driver_name = ? OR drop_off_point = ?", "غير مسجل", "غير مسجل")
		}
	}
	if receiptStatus != "" {
		countQuery = h.applyReceiptStatusFilter(countQuery, receiptStatus)
	}
	countQuery.Count(&total)

	trips, err := h.fetchTripsWithContainers(query, limit, offset)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   err.Error(),
		})
	}

	for i := range trips {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint).First(&mapping)

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

type DuplicateInfo struct {
	ReceiptNo           string                   `json:"receipt_no"`
	ExistingContainerID uint                     `json:"existing_container_id"`
	ExistingParent      *DuplicateParentInfo     `json:"existing_parent,omitempty"`
	ExistingStandalone  *DuplicateStandaloneInfo `json:"existing_standalone,omitempty"`
}

type DuplicateParentInfo struct {
	ID           uint   `json:"id"`
	CarNoPlate   string `json:"car_no_plate"`
	DriverName   string `json:"driver_name"`
	Company      string `json:"company"`
	Terminal     string `json:"terminal"`
	Date         string `json:"date"`
	DropOffPoint string `json:"drop_off_point"`
	TankCapacity int    `json:"tank_capacity"`
	GasType      string `json:"gas_type"`
}

type DuplicateStandaloneInfo struct {
	ID           uint   `json:"id"`
	CarNoPlate   string `json:"car_no_plate"`
	DriverName   string `json:"driver_name"`
	Company      string `json:"company"`
	Terminal     string `json:"terminal"`
	Date         string `json:"date"`
	DropOffPoint string `json:"drop_off_point"`
	TankCapacity int    `json:"tank_capacity"`
	GasType      string `json:"gas_type"`
}

// Helper function to check for duplicate receipts
func (h *TripHandler) checkDuplicateReceipts(receiptNumbers []string, excludeTripID *uint) ([]DuplicateInfo, error) {
	if len(receiptNumbers) == 0 {
		return nil, nil
	}

	var existingTrips []Models.TripStruct
	query := h.DB.Where("receipt_no IN ?", receiptNumbers)

	// If updating, exclude containers from the current parent trip
	if excludeTripID != nil {
		query = query.Where("parent_trip_id IS NULL OR parent_trip_id != ?", *excludeTripID)
	}

	if err := query.Find(&existingTrips).Error; err != nil {
		return nil, err
	}

	if len(existingTrips) == 0 {
		return nil, nil
	}

	duplicates := make([]DuplicateInfo, 0)

	for _, trip := range existingTrips {
		duplicate := DuplicateInfo{
			ReceiptNo:           trip.ReceiptNo,
			ExistingContainerID: trip.ID,
		}

		// Check if this trip has a parent
		if trip.ParentTripID != nil && *trip.ParentTripID != 0 {
			// Fetch parent trip data
			var parent Models.ParentTrip
			if err := h.DB.First(&parent, *trip.ParentTripID).Error; err == nil {
				duplicate.ExistingParent = &DuplicateParentInfo{
					ID:           parent.ID,
					CarNoPlate:   parent.CarNoPlate,
					DriverName:   parent.DriverName,
					Company:      parent.Company,
					Terminal:     parent.Terminal,
					Date:         parent.Date,
					DropOffPoint: trip.DropOffPoint,
					TankCapacity: trip.TankCapacity,
					GasType:      trip.GasType,
				}
			}
		} else {
			// Legacy standalone trip
			duplicate.ExistingStandalone = &DuplicateStandaloneInfo{
				ID:           trip.ID,
				CarNoPlate:   trip.CarNoPlate,
				DriverName:   trip.DriverName,
				Company:      trip.Company,
				Terminal:     trip.Terminal,
				Date:         trip.Date,
				DropOffPoint: trip.DropOffPoint,
				TankCapacity: trip.TankCapacity,
				GasType:      trip.GasType,
			}
		}

		duplicates = append(duplicates, duplicate)
	}

	return duplicates, nil
}

func (h *TripHandler) CreateMultiContainerTrip(c *fiber.Ctx) error {
	var input struct {
		ParentTrip       Models.ParentTrip   `json:"parent_trip"`
		Containers       []Models.TripStruct `json:"containers"`
		UpdateContainers bool                `json:"update_containers"`
		ReceiptBatchID   *uint               `json:"receipt_batch_id"`
		SetAsCurrentTrip bool                `json:"set_as_current_trip"`
		ForceCreate      bool                `json:"force_create"` // NEW: Flag to bypass duplicate check
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	// Validation
	if len(input.Containers) == 0 || len(input.Containers) > 4 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Must have between 1 and 4 containers",
		})
	}

	if input.ParentTrip.Company == "" || input.ParentTrip.Terminal == "" || input.ParentTrip.Date == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Parent trip must have company, terminal, and date",
		})
	}

	// NEW: Check for duplicate receipts unless force_create is true
	if !input.ForceCreate {
		receiptNumbers := make([]string, len(input.Containers))
		for i, container := range input.Containers {
			receiptNumbers[i] = container.ReceiptNo
		}

		duplicates, err := h.checkDuplicateReceipts(receiptNumbers, nil)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to check for duplicates",
				"error":   err.Error(),
			})
		}

		if len(duplicates) > 0 {
			// Return conflict with duplicate details
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"message":    "Duplicate receipt numbers found",
				"duplicates": duplicates,
				"new_data": fiber.Map{
					"car_no_plate": input.ParentTrip.CarNoPlate,
					"driver_name":  input.ParentTrip.DriverName,
					"company":      input.ParentTrip.Company,
					"terminal":     input.ParentTrip.Terminal,
					"date":         input.ParentTrip.Date,
					"containers":   input.Containers,
				},
			})
		}
	}

	// Log if this is a forced creation
	if input.ForceCreate {
		log.Printf("⚠️  FORCED CREATION: User bypassed duplicate check for receipt numbers: %v",
			func() []string {
				receipts := make([]string, len(input.Containers))
				for i, c := range input.Containers {
					receipts[i] = c.ReceiptNo
				}
				return receipts
			}())
	}

	// Validate all containers and get their mappings
	for i, container := range input.Containers {
		if container.DropOffPoint == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Container %d missing drop_off_point", i+1),
			})
		}
		if container.ReceiptNo == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Container %d missing receipt_no", i+1),
			})
		}
		if container.TankCapacity == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Container %d missing tank_capacity", i+1),
			})
		}

		if container.DropOffPoint == "غير مسجل" {
			continue
		}

		// Verify mapping exists
		var mapping Models.FeeMapping
		err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			input.ParentTrip.Company, input.ParentTrip.Terminal, container.DropOffPoint).
			First(&mapping).Error

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"message": fmt.Sprintf("Invalid mapping for container %d: %s to %s",
						i+1, input.ParentTrip.Terminal, container.DropOffPoint),
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to validate mapping",
				"error":   err.Error(),
			})
		}
	}

	// Begin transaction
	tx := h.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	authorUser := c.Locals("user").(*Models.User)
	input.ParentTrip.Author = authorUser.Name

	// Create parent trip
	if err := tx.Create(&input.ParentTrip).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to create parent trip",
			"error":   err.Error(),
		})
	}

	// Create each container trip
	createdContainers := make([]Models.TripStruct, 0, len(input.Containers))
	for i := range input.Containers {
		// Copy common fields from parent
		input.Containers[i].ParentTripID = &input.ParentTrip.ID
		input.Containers[i].CarID = input.ParentTrip.CarID
		input.Containers[i].DriverID = input.ParentTrip.DriverID
		input.Containers[i].CarNoPlate = input.ParentTrip.CarNoPlate
		input.Containers[i].DriverName = input.ParentTrip.DriverName
		input.Containers[i].Transporter = input.ParentTrip.Transporter
		input.Containers[i].Company = input.ParentTrip.Company
		input.Containers[i].Terminal = input.ParentTrip.Terminal
		input.Containers[i].Date = input.ParentTrip.Date

		// Get fee mapping for this container
		var mapping Models.FeeMapping
		tx.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			input.Containers[i].Company, input.Containers[i].Terminal, input.Containers[i].DropOffPoint).
			First(&mapping)

		// Set mileage and revenue from mapping
		input.Containers[i].Mileage = mapping.Distance
		input.Containers[i].Revenue = mapping.Fee

		// Set calculated fields for response (not stored)
		input.Containers[i].Distance = mapping.Distance
		input.Containers[i].Fee = mapping.Fee

		if err := tx.Create(&input.Containers[i]).Error; err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": fmt.Sprintf("Failed to create container %d", i+1),
				"error":   err.Error(),
			})
		}

		createdContainers = append(createdContainers, input.Containers[i])
	}

	// Update car's driver assignment
	if input.ParentTrip.DriverID != 0 {
		if err := tx.Model(&Models.Car{}).Where("id = ?", input.ParentTrip.CarID).
			Update("driver_id", input.ParentTrip.DriverID).Error; err != nil {
			log.Println("Warning: Failed to update car driver assignment:", err)
		}
	}

	// Link receipt batch to PARENT trip if provided
	if input.ReceiptBatchID != nil {
		var batch Models.ReceiptBatch
		if err := tx.First(&batch, *input.ReceiptBatchID).Error; err != nil {
			tx.Rollback()
			if err == gorm.ErrRecordNotFound {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"message": "Receipt batch not found",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to fetch receipt batch",
				"error":   err.Error(),
			})
		}

		// Check if batch is still pending
		if batch.Status != "pending" {
			tx.Rollback()
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "Receipt batch is already " + batch.Status,
			})
		}

		// Update batch status and link to PARENT trip
		batch.Status = "assigned"
		batch.AssignedToTripID = &input.ParentTrip.ID
		if err := tx.Save(&batch).Error; err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to update receipt batch",
				"error":   err.Error(),
			})
		}
	}

	// Set as current trip by linking to driver session
	if input.SetAsCurrentTrip {
		if input.ParentTrip.DriverID == 0 {
			log.Println("⚠️  Cannot set as current trip: no driver assigned. Trip will be created without current status.")
		} else {
			var driver Models.Driver
			if err := tx.First(&driver, input.ParentTrip.DriverID).Error; err != nil {
				log.Printf("⚠️  Cannot set as current trip: driver %d not found. Trip will be created without current status.",
					input.ParentTrip.DriverID)
			} else {
				session, err := utils.GetActiveSession(tx, input.ParentTrip.DriverID)

				if err != nil {
					log.Printf("No active session for driver %d, attempting to create new session", input.ParentTrip.DriverID)

					var car Models.Car
					if err := tx.First(&car, input.ParentTrip.CarID).Error; err != nil {
						log.Printf("⚠️  Cannot create session: car %d not found. Trip will be created without current status.",
							input.ParentTrip.CarID)
					} else if car.TabletID == nil {
						log.Printf("⚠️  Cannot create session: car %d has no tablet assigned. Trip will be created without current status.",
							car.ID)
					} else {
						session, err = utils.CreateSession(tx, input.ParentTrip.DriverID, car.ID, *car.TabletID)
						if err != nil {
							log.Printf("⚠️  Failed to create session for driver %d: %v. Trip will be created without current status.",
								input.ParentTrip.DriverID, err)
							session = nil
						} else {
							log.Printf("✅ Created new session %d for driver %d", session.ID, input.ParentTrip.DriverID)
						}
					}
				} else {
					log.Printf("Found existing active session %d for driver %d", session.ID, input.ParentTrip.DriverID)
				}

				if session != nil {
					var existingTrip Models.ParentTrip
					if err := tx.Where("session_id = ?", session.ID).First(&existingTrip).Error; err == nil {
						existingTrip.SessionID = nil
						if err := tx.Save(&existingTrip).Error; err != nil {
							log.Printf("⚠️  Warning: Failed to unlink old trip %d: %v", existingTrip.ID, err)
						} else {
							log.Printf("✅ Unlinked old ParentTrip %d from session %d (moved to history)",
								existingTrip.ID, session.ID)
						}
					}

					input.ParentTrip.SessionID = &session.ID
					if err := tx.Save(&input.ParentTrip).Error; err != nil {
						log.Printf("⚠️  Warning: Failed to link trip to session: %v. Trip will be created without current status.", err)
						input.ParentTrip.SessionID = nil
					} else {
						log.Printf("✅ Linked ParentTrip %d to session %d (set as current trip)",
							input.ParentTrip.ID, session.ID)
					}
				}
			}
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to commit transaction",
			"error":   err.Error(),
		})
	}

	wasSetAsCurrent := input.SetAsCurrentTrip && input.ParentTrip.SessionID != nil

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":         "Multi-container trip created successfully",
		"parent_trip":     input.ParentTrip,
		"containers":      createdContainers,
		"container_count": len(createdContainers),
		"is_current_trip": wasSetAsCurrent,
		"forced_creation": input.ForceCreate,
	})
}

// Helper function to provide user-friendly notes
func getSetAsCurrentNote(requested bool, success bool, driverID uint) string {
	if !requested {
		return ""
	}
	if success {
		return "Trip successfully set as current for the driver"
	}
	if driverID == 0 {
		return "Trip created but not set as current (no driver assigned)"
	}
	return "Trip created but could not be set as current (check logs for details)"
}

// GetContainersByParent retrieves all container trips for a given parent trip
func (h *TripHandler) GetContainersByParent(c *fiber.Ctx) error {
	parentID := c.Params("parent_id")

	if parentID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Parent trip ID is required",
		})
	}

	// Convert to uint
	id, err := strconv.ParseUint(parentID, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid parent trip ID",
			"error":   err.Error(),
		})
	}

	// Get parent trip
	var parentTrip Models.ParentTrip
	if err := h.DB.First(&parentTrip, uint(id)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Parent trip not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch parent trip",
			"error":   err.Error(),
		})
	}

	// Get all container trips
	var containers []Models.TripStruct
	if err := h.DB.Where("parent_trip_id = ?", uint(id)).
		Order("receipt_no ASC").
		Find(&containers).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch container trips",
			"error":   err.Error(),
		})
	}

	// Enrich with fee mapping data
	for i := range containers {
		var mapping Models.FeeMapping
		h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			containers[i].Company, containers[i].Terminal, containers[i].DropOffPoint).
			First(&mapping)

		if mapping.ID > 0 {
			containers[i].Distance = mapping.Distance
			containers[i].Fee = mapping.Fee
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":         "Containers retrieved successfully",
		"parent_trip":     parentTrip,
		"containers":      containers,
		"container_count": len(containers),
	})
}

// UpdateParentTrip updates parent trip information and optionally its containers
// UpdateParentTrip updates parent trip information and optionally its containers
// Modified UpdateParentTrip function

func (h *TripHandler) UpdateParentTrip(c *fiber.Ctx) error {
	parentID := c.Params("parent_id")

	if parentID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Parent trip ID is required",
		})
	}

	// Convert to uint
	id, err := strconv.ParseUint(parentID, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid parent trip ID",
			"error":   err.Error(),
		})
	}

	var input struct {
		ParentTrip       Models.ParentTrip   `json:"parent_trip"`
		Containers       []Models.TripStruct `json:"containers,omitempty"`
		UpdateContainers bool                `json:"update_containers"`
		ForceUpdate      bool                `json:"force_update"` // NEW: Flag to bypass duplicate check
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	// Check if parent trip exists
	var existingParent Models.ParentTrip
	if err := h.DB.First(&existingParent, uint(id)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Parent trip not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch parent trip",
			"error":   err.Error(),
		})
	}

	// NEW: Check for duplicate receipts in updated/new containers unless force_update is true
	if !input.ForceUpdate && input.UpdateContainers && len(input.Containers) > 0 {
		receiptNumbers := make([]string, 0)
		for _, container := range input.Containers {
			if container.ReceiptNo != "" {
				receiptNumbers = append(receiptNumbers, container.ReceiptNo)
			}
		}

		if len(receiptNumbers) > 0 {
			parentIDUint := uint(id)
			duplicates, err := h.checkDuplicateReceipts(receiptNumbers, &parentIDUint)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"message": "Failed to check for duplicates",
					"error":   err.Error(),
				})
			}

			if len(duplicates) > 0 {
				// Build new_data structure showing what's being updated
				newData := fiber.Map{
					"car_no_plate": input.ParentTrip.CarNoPlate,
					"driver_name":  input.ParentTrip.DriverName,
					"company":      input.ParentTrip.Company,
					"terminal":     input.ParentTrip.Terminal,
					"date":         input.ParentTrip.Date,
					"containers":   input.Containers,
				}

				// Use existing values if new ones are empty
				if input.ParentTrip.CarNoPlate == "" {
					newData["car_no_plate"] = existingParent.CarNoPlate
				}
				if input.ParentTrip.DriverName == "" {
					newData["driver_name"] = existingParent.DriverName
				}
				if input.ParentTrip.Company == "" {
					newData["company"] = existingParent.Company
				}
				if input.ParentTrip.Terminal == "" {
					newData["terminal"] = existingParent.Terminal
				}
				if input.ParentTrip.Date == "" {
					newData["date"] = existingParent.Date
				}

				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"message":    "Duplicate receipt numbers found",
					"duplicates": duplicates,
					"new_data":   newData,
				})
			}
		}
	}

	// Log if this is a forced update
	if input.ForceUpdate {
		log.Printf("⚠️  FORCED UPDATE: User bypassed duplicate check for parent trip ID: %d", id)
	}

	// Begin transaction
	tx := h.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Track if any parent fields were updated
	parentFieldsUpdated := false
	terminalOrCompanyChanged := false

	// Update parent trip fields
	if input.ParentTrip.CarID != 0 {
		existingParent.CarID = input.ParentTrip.CarID
		parentFieldsUpdated = true
	}
	if input.ParentTrip.DriverID != 0 {
		existingParent.DriverID = input.ParentTrip.DriverID
		parentFieldsUpdated = true
	}
	if input.ParentTrip.CarNoPlate != "" {
		existingParent.CarNoPlate = input.ParentTrip.CarNoPlate
		parentFieldsUpdated = true
	}
	if input.ParentTrip.DriverName != "" {
		existingParent.DriverID = input.ParentTrip.DriverID
		existingParent.DriverName = input.ParentTrip.DriverName
		parentFieldsUpdated = true
	}
	if input.ParentTrip.Company != "" && input.ParentTrip.Company != existingParent.Company {
		existingParent.Company = input.ParentTrip.Company
		parentFieldsUpdated = true
		terminalOrCompanyChanged = true
	}
	if input.ParentTrip.Terminal != "" && input.ParentTrip.Terminal != existingParent.Terminal {
		existingParent.Terminal = input.ParentTrip.Terminal
		parentFieldsUpdated = true
		terminalOrCompanyChanged = true
	}
	if input.ParentTrip.Date != "" {
		existingParent.Date = input.ParentTrip.Date
		parentFieldsUpdated = true
	}

	overWriterUser := c.Locals("user").(*Models.User)
	existingParent.Overwriter = overWriterUser.Name

	if err := tx.Save(&existingParent).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update parent trip",
			"error":   err.Error(),
		})
	}

	// ALWAYS update containers with parent data if parent fields were updated
	if parentFieldsUpdated {
		updateData := map[string]interface{}{
			"car_id":       existingParent.CarID,
			"driver_id":    existingParent.DriverID,
			"car_no_plate": existingParent.CarNoPlate,
			"driver_name":  existingParent.DriverName,
			"company":      existingParent.Company,
			"terminal":     existingParent.Terminal,
			"date":         existingParent.Date,
		}

		if err := tx.Model(&Models.TripStruct{}).
			Where("parent_trip_id = ?", uint(id)).
			Updates(updateData).Error; err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to update container trips",
				"error":   err.Error(),
			})
		}

		// If terminal or company changed, recalculate fees and distances for all containers
		if terminalOrCompanyChanged {
			var containers []Models.TripStruct
			if err := tx.Where("parent_trip_id = ?", uint(id)).Find(&containers).Error; err != nil {
				tx.Rollback()
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"message": "Failed to fetch containers for recalculation",
					"error":   err.Error(),
				})
			}

			for _, container := range containers {
				var mapping Models.FeeMapping
				err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
					existingParent.Company, existingParent.Terminal, container.DropOffPoint).
					First(&mapping).Error

				if err != nil {
					if err == gorm.ErrRecordNotFound {
						tx.Rollback()
						return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
							"message": fmt.Sprintf("No mapping found for %s -> %s (container ID: %d)",
								existingParent.Terminal, container.DropOffPoint, container.ID),
						})
					}
					tx.Rollback()
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"message": "Failed to fetch fee mapping",
						"error":   err.Error(),
					})
				}

				// Update mileage and revenue
				if err := tx.Model(&Models.TripStruct{}).
					Where("id = ?", container.ID).
					Updates(map[string]interface{}{
						"mileage": mapping.Distance,
						"revenue": mapping.Fee,
					}).Error; err != nil {
					tx.Rollback()
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"message": fmt.Sprintf("Failed to update fees for container %d", container.ID),
						"error":   err.Error(),
					})
				}
			}
		}
	}

	// Update or create specific containers if provided
	if input.UpdateContainers && len(input.Containers) > 0 {
		for _, container := range input.Containers {
			if container.ID != 0 {
				// Update existing container's specific fields
				containerUpdate := map[string]interface{}{}

				if container.DropOffPoint != "" {
					containerUpdate["drop_off_point"] = container.DropOffPoint
					if container.DropOffPoint != "غير مسجل" {
						// Recalculate fee and distance for this drop-off point
						var mapping Models.FeeMapping
						err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
							existingParent.Company, existingParent.Terminal, container.DropOffPoint).
							First(&mapping).Error

						if err != nil {
							if err == gorm.ErrRecordNotFound {
								tx.Rollback()
								return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
									"message": fmt.Sprintf("No mapping found for %s -> %s",
										existingParent.Terminal, container.DropOffPoint),
								})
							}
							tx.Rollback()
							return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
								"message": "Failed to fetch fee mapping",
								"error":   err.Error(),
							})
						}

						containerUpdate["mileage"] = mapping.Distance
						containerUpdate["revenue"] = mapping.Fee
					}
				}

				if container.TankCapacity != 0 {
					containerUpdate["tank_capacity"] = container.TankCapacity
				}
				if container.GasType != "" {
					containerUpdate["gas_type"] = container.GasType
				}
				if container.ReceiptNo != "" {
					containerUpdate["receipt_no"] = container.ReceiptNo
				}

				if len(containerUpdate) > 0 {
					if err := tx.Model(&Models.TripStruct{}).
						Where("id = ? AND parent_trip_id = ?", container.ID, uint(id)).
						Updates(containerUpdate).Error; err != nil {
						tx.Rollback()
						return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
							"message": fmt.Sprintf("Failed to update container %d", container.ID),
							"error":   err.Error(),
						})
					}
				}
			} else {
				// Validate new container has required fields
				if container.DropOffPoint == "" {
					tx.Rollback()
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"message": "New container missing drop_off_point",
					})
				}
				if container.ReceiptNo == "" {
					tx.Rollback()
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"message": "New container missing receipt_no",
					})
				}
				if container.TankCapacity == 0 {
					tx.Rollback()
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"message": "New container missing tank_capacity",
					})
				}

				if container.DropOffPoint == "غير مسجل" {
					continue
				}

				// Get fee mapping for new container
				var mapping Models.FeeMapping
				err := h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
					existingParent.Company, existingParent.Terminal, container.DropOffPoint).
					First(&mapping).Error

				if err != nil {
					if err == gorm.ErrRecordNotFound {
						tx.Rollback()
						return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
							"message": fmt.Sprintf("No mapping found for %s -> %s",
								existingParent.Terminal, container.DropOffPoint),
						})
					}
					tx.Rollback()
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"message": "Failed to validate mapping",
						"error":   err.Error(),
					})
				}

				// Create new container with ALL fields
				parentIDUint := uint(id)
				newContainer := Models.TripStruct{
					ParentTripID: &parentIDUint,
					CarID:        existingParent.CarID,
					DriverID:     existingParent.DriverID,
					CarNoPlate:   existingParent.CarNoPlate,
					DriverName:   existingParent.DriverName,
					Company:      existingParent.Company,
					Terminal:     existingParent.Terminal,
					Date:         existingParent.Date,
					DropOffPoint: container.DropOffPoint,
					TankCapacity: container.TankCapacity,
					GasType:      container.GasType,
					ReceiptNo:    container.ReceiptNo,
					Mileage:      mapping.Distance,
					Revenue:      mapping.Fee,
				}

				if err := tx.Create(&newContainer).Error; err != nil {
					tx.Rollback()
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"message": "Failed to create new container",
						"error":   err.Error(),
					})
				}
			}
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to commit transaction",
			"error":   err.Error(),
		})
	}

	// Fetch updated containers
	var updatedContainers []Models.TripStruct
	h.DB.Where("parent_trip_id = ?", uint(id)).
		Order("receipt_no ASC").
		Find(&updatedContainers)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":       "Parent trip updated successfully",
		"parent_trip":   existingParent,
		"containers":    updatedContainers,
		"forced_update": input.ForceUpdate,
	})
}

// DeleteParentTrip deletes a parent trip and all its container trips
func (h *TripHandler) DeleteParentTrip(c *fiber.Ctx) error {
	parentID := c.Params("parent_id")

	if parentID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Parent trip ID is required",
		})
	}

	// Convert to uint
	id, err := strconv.ParseUint(parentID, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid parent trip ID",
			"error":   err.Error(),
		})
	}

	// Check if parent trip exists
	var parentTrip Models.ParentTrip
	if err := h.DB.First(&parentTrip, uint(id)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Parent trip not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch parent trip",
			"error":   err.Error(),
		})
	}

	// Begin transaction
	tx := h.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Count containers before deletion
	var containerCount int64
	tx.Model(&Models.TripStruct{}).Where("parent_trip_id = ?", uint(id)).Count(&containerCount)

	// Soft delete all container trips
	if err := tx.Where("parent_trip_id = ?", uint(id)).Delete(&Models.TripStruct{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to delete container trips",
			"error":   err.Error(),
		})
	}

	// Soft delete parent trip
	if err := tx.Delete(&parentTrip).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to delete parent trip",
			"error":   err.Error(),
		})
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to commit transaction",
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":            "Parent trip and containers deleted successfully",
		"deleted_parent":     parentTrip.ID,
		"deleted_containers": containerCount,
	})
}

func GetFullTripSummaryByParentTrip(c *fiber.Ctx) error {
	parentTripID, err := strconv.ParseUint(c.Params("parent_trip_id"), 10, 32)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid parent trip ID",
			"message": "Parent trip ID must be a valid number",
		})
	}

	// Check if parent trip exists and load it
	var parentTrip Models.ParentTrip
	if err := Models.DB.First(&parentTrip, parentTripID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error":   "Parent trip not found",
			"message": "No parent trip found with the specified ID",
		})
	}

	// CRITICAL: Validate session exists for this parent trip
	if parentTrip.SessionID == nil || *parentTrip.SessionID == 0 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error":   "No session associated with this trip",
			"message": "This trip was created before the session system was implemented or the session link is missing. Trip summary cannot be generated without a valid session.",
		})
	}

	// Verify the session exists in the database
	var session Models.DriverSession
	if err := Models.DB.First(&session, *parentTrip.SessionID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error":   "Session not found",
			"message": "The session associated with this trip no longer exists in the database.",
		})
	}

	// Get trip summary with all location pings from the session
	service := services.NewTripSummaryService(Models.DB)
	summary, err := service.GetSummaryByParentTripIDWithPings(uint(parentTripID))
	if err != nil {
		// Check if it's because summary doesn't exist yet
		if err.Error() == "summary not found for parent trip" ||
			err.Error() == "record not found" {
			return c.Status(404).JSON(fiber.Map{
				"success": false,
				"error":   "Trip summary not generated yet",
				"message": "The trip summary for this parent trip has not been generated. The worker may still be processing it, or the trip may not be complete.",
				"hint":    "Trip summaries are automatically generated 15 minutes after a trip is completed.",
			})
		}

		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to retrieve trip summary",
			"message": err.Error(),
		})
	}

	// Success - return the complete summary with pings
	return c.JSON(fiber.Map{
		"success": true,
		"data":    summary,
		"meta": fiber.Map{
			"parent_trip_id": parentTripID,
			"session_id":     *parentTrip.SessionID,
			"total_pings":    len(summary.LocationPings),
			"total_legs":     len(summary.ActualLegs),
		},
	})
}

// GetTripSummaryByID retrieves a trip summary by its direct ID (alternative endpoint)
func GetTripSummaryByID(c *fiber.Ctx) error {
	summaryID, err := strconv.ParseUint(c.Params("summary_id"), 10, 32)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid summary ID",
			"message": "Summary ID must be a valid number",
		})
	}

	// Get trip summary with pings
	service := services.NewTripSummaryService(Models.DB)
	summary, err := service.GetSummaryWithPings(uint(summaryID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error":   "Trip summary not found",
			"message": err.Error(),
		})
	}

	// Load parent trip info for context
	var parentTrip Models.ParentTrip
	if err := Models.DB.First(&parentTrip, summary.ParentTripID).Error; err == nil {
		return c.JSON(fiber.Map{
			"success": true,
			"data":    summary,
			"meta": fiber.Map{
				"parent_trip_id": summary.ParentTripID,
				"session_id":     parentTrip.SessionID,
				"total_pings":    len(summary.LocationPings),
				"total_legs":     len(summary.ActualLegs),
			},
		})
	}

	// Return without parent trip info if not found
	return c.JSON(fiber.Map{
		"success": true,
		"data":    summary,
		"meta": fiber.Map{
			"parent_trip_id": summary.ParentTripID,
			"total_pings":    len(summary.LocationPings),
			"total_legs":     len(summary.ActualLegs),
		},
	})
}

// ListTripSummaries retrieves all trip summaries with optional filters
func ListTripSummaries(c *fiber.Ctx) error {
	var summaries []Models.TripSummary

	query := Models.DB.Preload("ParentTrip")

	// Optional filters
	if companyFilter := c.Query("company"); companyFilter != "" {
		query = query.Joins("JOIN parent_trips ON parent_trips.id = trip_summaries.parent_trip_id").
			Where("parent_trips.company = ?", companyFilter)
	}

	if sessionID := c.Query("session_id"); sessionID != "" {
		sessionIDUint, err := strconv.ParseUint(sessionID, 10, 32)
		if err == nil {
			query = query.Joins("JOIN parent_trips ON parent_trips.id = trip_summaries.parent_trip_id").
				Where("parent_trips.session_id = ?", sessionIDUint)
		}
	}

	// Pagination
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset := (page - 1) * limit

	// Get total count
	var total int64
	query.Model(&Models.TripSummary{}).Count(&total)

	// Get results
	if err := query.Limit(limit).Offset(offset).Order("created_at DESC").Find(&summaries).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to retrieve trip summaries",
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    summaries,
		"meta": fiber.Map{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		},
	})
}

// ManuallyGenerateTripSummary allows manual triggering of summary generation
// Useful for testing or regenerating summaries
func ManuallyGenerateTripSummary(c *fiber.Ctx) error {
	parentTripID, err := strconv.ParseUint(c.Params("parent_trip_id"), 10, 32)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid parent trip ID",
		})
	}

	// Check if parent trip exists
	var parentTrip Models.ParentTrip
	if err := Models.DB.First(&parentTrip, parentTripID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error":   "Parent trip not found",
		})
	}

	// Validate session exists
	if parentTrip.SessionID == nil || *parentTrip.SessionID == 0 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error":   "No session associated with this trip",
			"message": "Cannot generate summary for trips without a session",
		})
	}

	// Check if summary already exists
	var existingSummary Models.TripSummary
	err = Models.DB.Where("parent_trip_id = ?", parentTripID).First(&existingSummary).Error
	if err == nil {
		// Check if user wants to force regenerate
		if c.Query("force") != "true" {
			return c.Status(409).JSON(fiber.Map{
				"success":             false,
				"error":               "Trip summary already exists",
				"message":             "Use ?force=true to regenerate",
				"existing_summary_id": existingSummary.ID,
			})
		}

		// Delete existing summary and its legs
		Models.DB.Where("trip_summary_id = ?", existingSummary.ID).Delete(&Models.TripLeg{})
		Models.DB.Delete(&existingSummary)
	}

	// Generate new summary
	service := services.NewTripSummaryService(Models.DB)
	summary, err := service.GenerateSummary(uint(parentTripID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to generate trip summary",
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Trip summary generated successfully",
		"data":    summary,
		"meta": fiber.Map{
			"parent_trip_id": parentTripID,
			"session_id":     *parentTrip.SessionID,
			"total_legs":     len(summary.ActualLegs) + len(summary.OptimalLegs),
		},
	})
}
