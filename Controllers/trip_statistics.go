package Controllers

import (
	"Falcon/Models"
	"Falcon/middleware"
	"Falcon/pricing"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Trip statistics, read from stored pricing.
//
// Every money figure here is a sum of what the pricing engine stored when the
// trip was written (pricing/engine.go): a trip's base_revenue and revenue_vat,
// plus its share of the car rental in truck_month_charges. Nothing is priced
// at read time any more, so these totals, the trips list, the exports and
// apex-rust cannot disagree.
//
// A company is grouped by how it is priced — by drop-off when it is billed per
// 1,000 L at the mapping's fee, by fee band when it is billed per band, by
// terminal when it is billed per km — under the version of its rule in force at
// the end of the range. The groups and field names are the ones the dashboard
// has always read.
//
// Rental. A truck-month's rental (per car, company, terminal, month) is spread
// over the days the car worked that terminal that month, and a day's share is
// split equally between the car's rows at that terminal on that day. Any
// range that covers whole months therefore reports exactly truck_month_charges;
// a part-month reports the share of the car's billed days that fall inside it.
//
// Masking is not done here: middleware.MaskMoney on the /api/trips group
// zeroes every money field for callers below permission 3.

type TripStatisticsDetails struct {
	GroupName     string  `json:"group_name"` // Terminal, DropOffPoint, or Fee level
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue"`
	CarRental     float64 `json:"car_rental,omitempty"`
	VAT           float64 `json:"vat,omitempty"`
	TotalWithVAT  float64 `json:"total_with_vat,omitempty"`
	Fee           float64 `json:"fee,omitempty"`
	DistinctCars  int64   `json:"distinct_cars,omitempty"` // per-km companies
	DistinctDays  int64   `json:"distinct_days,omitempty"` // per-km companies
	CarDays       int64   `json:"car_days,omitempty"`      // per-km companies: distinct car-days
}

type TripRevenueDateResponse struct {
	Date           string                  `json:"date"`
	TotalTrips     int64                   `json:"total_trips"`
	TotalVolume    float64                 `json:"total_volume"`
	TotalDistance  float64                 `json:"total_distance"`
	TotalRevenue   float64                 `json:"total_revenue,omitempty"`
	CompanyDetails []CompanyRevenueDetails `json:"company_details"`
}

// CompanyRevenueDetails is one company on one date. TotalRevenue here is, as
// it always was, everything the day earned: base + rental + VAT.
type CompanyRevenueDetails struct {
	Company       string  `json:"company"`
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue,omitempty"`
	VAT           float64 `json:"vat,omitempty"`
	CarRental     float64 `json:"car_rental,omitempty"`
	TotalWithVAT  float64 `json:"total_with_vat,omitempty"`
	Fee           float64 `json:"fee,omitempty"`
	DistinctCars  int64   `json:"distinct_cars,omitempty"`
	DistinctDays  int64   `json:"distinct_days,omitempty"`
	CarDays       int64   `json:"car_days,omitempty"`
}

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

type CarStats struct {
	CarNoPlate    string  `json:"car_no_plate"`
	TotalTrips    int64   `json:"total_trips"`
	TotalVolume   float64 `json:"total_volume"`
	TotalDistance float64 `json:"total_distance"`
	TotalRevenue  float64 `json:"total_revenue,omitempty"`
	WorkingDays   int64   `json:"working_days"`
	CarRental     float64 `json:"car_rental,omitempty"`
	VAT           float64 `json:"vat,omitempty"`
	TotalWithVAT  float64 `json:"total_with_vat,omitempty"`
}

type TripStatistics struct {
	Company       string                  `json:"company"`
	TotalTrips    int64                   `json:"total_trips"`
	TotalVolume   float64                 `json:"total_volume"`
	TotalDistance float64                 `json:"total_distance"`
	TotalRevenue  float64                 `json:"total_revenue"`            // base revenue
	TotalCarRent  float64                 `json:"total_car_rent,omitempty"` // rental
	TotalVAT      float64                 `json:"total_vat,omitempty"`      // VAT on base and rental
	TotalAmount   float64                 `json:"total_amount,omitempty"`   // base + rental + VAT
	Details       []TripStatisticsDetails `json:"details,omitempty"`
	RouteDetails  []RouteRevenueStats     `json:"route_details,omitempty"`
}

type CarTotal struct {
	CarNoPlate  string  `json:"car_no_plate"`
	Liters      float64 `json:"liters"`
	Distance    float64 `json:"distance"`
	BaseRevenue float64 `json:"base_revenue"`
	VAT         float64 `json:"vat"`
	Rent        float64 `json:"rent"`
}

/* ------------------------------------------------------------------------ */
/* Stored rows                                                               */
/* ------------------------------------------------------------------------ */

// statRow is one live trip with its stored pricing and its rental share.
type statRow struct {
	ID           uint    `gorm:"column:id"`
	G            int64   `gorm:"column:g"` // logical trip: the parent, or -id for a standalone row
	Company      string  `gorm:"column:company"`
	Terminal     string  `gorm:"column:terminal"`
	DropOffPoint string  `gorm:"column:drop_off_point"`
	CarNoPlate   string  `gorm:"column:car_no_plate"`
	Date         string  `gorm:"column:date"`
	DriverName   string  `gorm:"column:driver_name"`
	TankCapacity float64 `gorm:"column:tank_capacity"`
	Mapped       bool    `gorm:"column:mapped"`
	Distance     float64 `gorm:"column:distance"` // the fee mapping's distance
	Fee          float64 `gorm:"column:fee"`      // the fee mapping's fee: EGP per 1,000 L, or a band number
	Base         float64 `gorm:"column:base"`
	VAT          float64 `gorm:"column:vat"` // VAT on Base
	PricedKm     float64 `gorm:"column:priced_km"`
	Rental       float64 `gorm:"column:rental"` // this row's share of its truck-month's rental
	RentalVAT    float64 `gorm:"column:rental_vat"`
}

// statRowsSQL: the rental share is the truck-month row's rental per day the
// car worked that terminal, divided between the car's rows there that day.
// The day partition sits inside the date filter (a day is all in or all out),
// so the shares of a whole month add up to its truck_month_charges.
const statRowsSQL = `
        WITH r AS (
            SELECT t.id,
                   COALESCE(NULLIF(t.parent_trip_id, 0), -t.id) AS g,
                   COALESCE(t.company, '')        AS company,
                   COALESCE(t.terminal, '')       AS terminal,
                   COALESCE(t.drop_off_point, '') AS drop_off_point,
                   COALESCE(t.car_no_plate, '')   AS car_no_plate,
                   COALESCE(t.date, '')           AS date,
                   COALESCE(t.driver_name, '')    AS driver_name,
                   COALESCE(t.tank_capacity, 0)::float8 AS tank_capacity,
                   fm.id IS NOT NULL                    AS mapped,
                   COALESCE(fm.distance, 0)::float8     AS distance,
                   COALESCE(fm.fee, 0)::float8          AS fee,
                   COALESCE(t.base_revenue, 0)::float8  AS base,
                   COALESCE(t.revenue_vat, 0)::float8   AS vat,
                   COALESCE(t.priced_km, 0)::float8     AS priced_km,
                   COUNT(*) OVER (PARTITION BY t.company, COALESCE(t.car_no_plate, ''),
                                               COALESCE(t.terminal, ''), t.date) AS rows_that_day
              FROM trips t
              LEFT JOIN LATERAL (
                    SELECT m.id, m.distance, m.fee FROM fee_mappings m
                     WHERE m.company = t.company AND m.terminal = t.terminal
                       AND m.drop_off_point = t.drop_off_point AND m.deleted_at IS NULL
                     ORDER BY m.id LIMIT 1
              ) fm ON true
             WHERE t.deleted_at IS NULL
               AND (? = '' OR t.date >= ?)
               AND (? = '' OR t.date <= ?)
               AND (? = '' OR t.company = ?)
        )
        SELECT r.*,
               COALESCE(tm.rental     / NULLIF(tm.days_worked, 0) / r.rows_that_day, 0)::float8 AS rental,
               COALESCE(tm.rental_vat / NULLIF(tm.days_worked, 0) / r.rows_that_day, 0)::float8 AS rental_vat
          FROM r
          LEFT JOIN truck_month_charges tm
            ON tm.company = r.company AND tm.car_no_plate = r.car_no_plate
           AND tm.terminal = r.terminal AND to_char(tm.month, 'YYYY-MM') = left(r.date, 7)
         ORDER BY r.date, r.id`

func loadStatRows(db *gorm.DB, start, end, company string) ([]statRow, error) {
	var rows []statRow
	err := db.Raw(statRowsSQL, start, start, end, end, company, company).Scan(&rows).Error
	return rows, err
}

/* ------------------------------------------------------------------------ */
/* Aggregation                                                               */
/* ------------------------------------------------------------------------ */

// money is a sum of stored figures.
type money struct{ Base, VAT, Rental, RentalVAT float64 }

func (m money) vat() float64   { return m.VAT + m.RentalVAT }
func (m money) total() float64 { return m.Base + m.Rental + m.VAT + m.RentalVAT }

type set map[string]struct{}

// bucket sums the rows of one group.
type bucket struct {
	first        statRow // a representative row: terminal, drop-off, fee
	trips        map[int64]struct{}
	volume, dist float64
	m            money
	cars, days   set
	carDays      set
	sub          *buckets // cars of a route, companies of a date
}

func (b *bucket) add(r statRow, dist float64) {
	b.trips[r.G] = struct{}{}
	b.volume += r.TankCapacity
	b.dist += dist
	b.m.Base += r.Base
	b.m.VAT += r.VAT
	b.m.Rental += r.Rental
	b.m.RentalVAT += r.RentalVAT
	b.cars[r.CarNoPlate] = struct{}{}
	b.days[r.Date] = struct{}{}
	b.carDays[r.CarNoPlate+"\x00"+r.Date] = struct{}{}
}

func (b *bucket) tripCount() int64 { return int64(len(b.trips)) }

// buckets is an insertion-ordered set of groups.
type buckets struct {
	m     map[string]*bucket
	order []string
}

func newBuckets() *buckets { return &buckets{m: map[string]*bucket{}} }

func (bs *buckets) get(key string, r statRow) *bucket {
	b, ok := bs.m[key]
	if !ok {
		b = &bucket{first: r, trips: map[int64]struct{}{}, cars: set{}, days: set{}, carDays: set{}}
		bs.m[key] = b
		bs.order = append(bs.order, key)
	}
	return b
}

func (bs *buckets) sorted() []*bucket {
	keys := append([]string(nil), bs.order...)
	sort.Strings(keys)
	out := make([]*bucket, len(keys))
	for i, k := range keys {
		out[i] = bs.m[k]
	}
	return out
}

// band is a row's Watanya-style fee band; an unmapped row is band 0.
func band(r statRow) int {
	if !r.Mapped {
		return 0
	}
	return int(r.Fee)
}

// sortBands orders by band number with band 0 (unmapped) last.
func sortBands(bs []*bucket) {
	sort.SliceStable(bs, func(i, j int) bool {
		a, b := band(bs[i].first), band(bs[j].first)
		if (a == 0) != (b == 0) {
			return b == 0
		}
		return a < b
	})
}

func feeKey(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// companyStats builds one company's statistics from its rows.
func companyStats(company string, rule pricing.CompanyRule, rows []statRow) TripStatistics {
	method := rule.Method
	// Per-km companies are billed on priced_km: the logical trip's furthest
	// drop, once. Everyone else reports the mapped distance of every row.
	dist := func(r statRow) float64 {
		if method == Models.PricingPerKm {
			return r.PricedKm
		}
		return r.Distance
	}

	stats := TripStatistics{Company: company}
	all := newBuckets()
	details, routes := newBuckets(), newBuckets()
	for _, r := range rows {
		d := dist(r)
		all.get("", r).add(r, d)

		var dk, rk string
		switch method {
		case Models.PricingPer1000LFee:
			dk = r.DropOffPoint + "\x00" + feeKey(r.Fee)
			rk = r.Terminal + "\x00" + r.DropOffPoint + "\x00" + feeKey(r.Fee)
		case Models.PricingBandPer1000L:
			dk = strconv.Itoa(band(r))
			rk = dk
		case Models.PricingPerKm:
			dk = r.Terminal
			rk = dk
		default: // no rule: nothing is priced; routes as for a fee company
			rk = r.Terminal + "\x00" + r.DropOffPoint + "\x00" + feeKey(r.Fee)
		}
		if method != "" {
			details.get(dk, r).add(r, d)
		}
		route := routes.get(rk, r)
		route.add(r, d)
		if route.sub == nil {
			route.sub = newBuckets()
		}
		route.sub.get(r.CarNoPlate, r).add(r, d)
	}

	if a, ok := all.m[""]; ok {
		stats.TotalTrips = a.tripCount()
		stats.TotalVolume = a.volume
		stats.TotalDistance = a.dist
		stats.TotalRevenue = a.m.Base
		stats.TotalCarRent = a.m.Rental
		stats.TotalVAT = a.m.vat()
		stats.TotalAmount = a.m.total()
	}

	// Details.
	ds := details.sorted()
	if method == Models.PricingBandPer1000L {
		sortBands(ds)
	}
	for _, b := range ds {
		d := TripStatisticsDetails{
			TotalTrips:    b.tripCount(),
			TotalVolume:   b.volume,
			TotalDistance: b.dist,
			TotalRevenue:  b.m.Base,
			CarRental:     b.m.Rental,
			VAT:           b.m.vat(),
			TotalWithVAT:  b.m.total(),
		}
		switch method {
		case Models.PricingPer1000LFee:
			d.GroupName, d.Fee = b.first.DropOffPoint, b.first.Fee
		case Models.PricingBandPer1000L:
			d.GroupName, d.Fee = fmt.Sprintf("Fee %d", band(b.first)), float64(band(b.first))
		case Models.PricingPerKm:
			d.GroupName, d.Fee = b.first.Terminal, rule.PerKmRate
			d.DistinctCars, d.DistinctDays, d.CarDays = int64(len(b.cars)), int64(len(b.days)), int64(len(b.carDays))
		}
		stats.Details = append(stats.Details, d)
	}
	// As before: a company's trip count is the sum of its groups' (a parent
	// trip that delivered to two groups counts in each).
	if len(stats.Details) > 0 {
		stats.TotalTrips = 0
		for _, d := range stats.Details {
			stats.TotalTrips += d.TotalTrips
		}
	}

	// Routes, each with its cars.
	rs := routes.sorted()
	if method == Models.PricingBandPer1000L {
		sortBands(rs)
	}
	for _, b := range rs {
		route := RouteRevenueStats{
			TotalTrips:    b.tripCount(),
			TotalVolume:   b.volume,
			TotalDistance: b.dist,
			TotalRevenue:  b.m.Base,
			CarRental:     b.m.Rental,
			VAT:           b.m.vat(),
			TotalWithVAT:  b.m.total(),
		}
		switch method {
		case Models.PricingBandPer1000L:
			n := band(b.first)
			route.RouteName, route.RouteType, route.FeeCategory, route.Fee =
				fmt.Sprintf("Fee Category %d", n), "fee", n, float64(n)
		case Models.PricingPerKm:
			route.RouteName, route.RouteType, route.Terminal, route.Fee =
				b.first.Terminal, "terminal", b.first.Terminal, rule.PerKmRate
		default:
			route.RouteName = fmt.Sprintf("%s to %s", b.first.Terminal, b.first.DropOffPoint)
			route.RouteType, route.Terminal, route.DropOffPoint, route.Fee =
				"terminal-dropoff", b.first.Terminal, b.first.DropOffPoint, b.first.Fee
		}
		for _, cb := range b.sub.sorted() {
			route.Cars = append(route.Cars, CarStats{
				CarNoPlate:    cb.first.CarNoPlate,
				TotalTrips:    cb.tripCount(),
				TotalVolume:   cb.volume,
				TotalDistance: cb.dist,
				TotalRevenue:  cb.m.Base,
				WorkingDays:   int64(len(cb.days)),
				CarRental:     cb.m.Rental,
				VAT:           cb.m.vat(),
				TotalWithVAT:  cb.m.total(),
			})
		}
		stats.RouteDetails = append(stats.RouteDetails, route)
	}
	return stats
}

// statsByDate is the daily breakdown, one entry per company per date.
func statsByDate(rows []statRow, rules map[string]pricing.CompanyRule) []TripRevenueDateResponse {
	dates := newBuckets()
	for _, r := range rows {
		d := r.Distance
		if rules[r.Company].Method == Models.PricingPerKm {
			d = r.PricedKm
		}
		day := dates.get(r.Date, r)
		if day.sub == nil {
			day.sub = newBuckets()
		}
		day.sub.get(r.Company, r).add(r, d)
	}
	out := make([]TripRevenueDateResponse, 0, len(dates.order))
	for _, day := range dates.sorted() {
		resp := TripRevenueDateResponse{Date: day.first.Date}
		for _, cb := range day.sub.sorted() {
			cd := CompanyRevenueDetails{
				Company:       cb.first.Company,
				TotalTrips:    cb.tripCount(),
				TotalVolume:   cb.volume,
				TotalDistance: cb.dist,
				TotalRevenue:  cb.m.total(),
				VAT:           cb.m.vat(),
				CarRental:     cb.m.Rental,
				TotalWithVAT:  cb.m.total(),
			}
			resp.TotalTrips += cd.TotalTrips
			resp.TotalVolume += cd.TotalVolume
			resp.TotalDistance += cd.TotalDistance
			resp.TotalRevenue += cd.TotalRevenue
			resp.CompanyDetails = append(resp.CompanyDetails, cd)
		}
		out = append(out, resp)
	}
	return out
}

// GetCarTotals sums each car over every company's routes.
func GetCarTotals(statistics []TripStatistics) []CarTotal {
	byCar := make(map[string]*CarTotal)
	var order []string
	for _, statistic := range statistics {
		for _, routeDetail := range statistic.RouteDetails {
			for _, car := range routeDetail.Cars {
				ct, ok := byCar[car.CarNoPlate]
				if !ok {
					ct = &CarTotal{CarNoPlate: car.CarNoPlate}
					byCar[car.CarNoPlate] = ct
					order = append(order, car.CarNoPlate)
				}
				ct.Liters += car.TotalVolume
				ct.Distance += car.TotalDistance
				ct.BaseRevenue += car.TotalRevenue
				ct.VAT += car.VAT
				ct.Rent += car.CarRental
			}
		}
	}
	sort.Strings(order)
	out := make([]CarTotal, 0, len(order))
	for _, k := range order {
		out = append(out, *byCar[k])
	}
	return out
}

// GetTripStatistics: GET /api/trips/statistics?start_date&end_date&company.
func (h *TripHandler) GetTripStatistics(c *fiber.Ctx) error {
	startDate, endDate, companyFilter := c.Query("start_date"), c.Query("end_date"), c.Query("company")

	rows, err := loadStatRows(h.DB, startDate, endDate, companyFilter)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch company statistics",
			"error":   err.Error(),
		})
	}
	rules, err := pricing.RulesAt(h.DB, endDate)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch pricing rules",
			"error":   err.Error(),
		})
	}

	byCompany := map[string][]statRow{}
	var companies []string
	for _, r := range rows {
		if _, ok := byCompany[r.Company]; !ok {
			companies = append(companies, r.Company)
		}
		byCompany[r.Company] = append(byCompany[r.Company], r)
	}
	sort.Strings(companies)

	statistics := make([]TripStatistics, 0, len(companies))
	for _, company := range companies {
		statistics = append(statistics, companyStats(company, rules[company], byCompany[company]))
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":            "Trip statistics retrieved successfully",
		"data":               statistics,
		"statsByDate":        statsByDate(rows, rules),
		"hasFinancialAccess": middleware.MoneyVisible(c),
		"carTotals":          GetCarTotals(statistics),
	})
}

/* ------------------------------------------------------------------------ */
/* Watanya driver analytics                                                  */
/* ------------------------------------------------------------------------ */

type DriverActivity struct {
	Date    string  `json:"date"`
	Count   int64   `json:"count"`
	Revenue float64 `json:"revenue"`
}

type DriverRoute struct {
	Terminal     string  `json:"terminal"`
	DropOffPoint string  `json:"drop_off_point"`
	Count        int64   `json:"count"`
	Distance     float64 `json:"distance"`
	Percent      float64 `json:"percent"`
}

type DriverAnalytics struct {
	DriverName        string           `json:"driver_name"`
	TotalTrips        int64            `json:"total_trips"`
	TotalDistance     float64          `json:"total_distance"`
	TotalVolume       float64          `json:"total_volume"`
	TotalFees         float64          `json:"total_fees,omitempty"`
	TotalRevenue      float64          `json:"total_revenue,omitempty"`
	TotalVAT          float64          `json:"total_vat,omitempty"`
	TotalAmount       float64          `json:"total_amount,omitempty"`
	WorkingDays       int64            `json:"working_days"`
	AvgTripsPerDay    float64          `json:"avg_trips_per_day"`
	AvgKmPerDay       float64          `json:"avg_km_per_day"`
	AvgFeesPerDay     float64          `json:"avg_fees_per_day,omitempty"`
	AvgTripsPerKm     float64          `json:"avg_trips_per_km"`
	AvgVolumePerKm    float64          `json:"avg_volume_per_km"`
	Efficiency        float64          `json:"efficiency"`
	ActivityHeatmap   []DriverActivity `json:"activity_heatmap"`
	RouteDistribution []DriverRoute    `json:"route_distribution"`
}

type DriverGlobalStats struct {
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
}

type DriverAnalyticsResponse struct {
	Drivers     []DriverAnalytics `json:"drivers"`
	GlobalStats DriverGlobalStats `json:"global_stats"`
}

const watanyaCompany = "Watanya"

// buildDriverAnalytics: per driver, the trips on mapped routes. A trip is one
// logical trip per route (a parent trip that delivered to two drop-offs is a
// trip on each), its distance the route's mapped distance once, and its
// revenue what its rows stored, VAT included — total_fees and total_revenue
// have always meant the VAT-inclusive figure here.
func buildDriverAnalytics(rows []statRow) DriverAnalyticsResponse {
	resp := DriverAnalyticsResponse{Drivers: []DriverAnalytics{}}

	type routeTrip struct {
		route string
		g     int64
	}
	type driverAcc struct {
		trips      map[routeTrip]struct{}
		routes     *buckets
		dayTrips   map[string]int64
		dayRevenue map[string]float64
		a          DriverAnalytics
	}
	drivers := map[string]*driverAcc{}
	var names []string
	allDays := set{}
	for _, r := range rows {
		allDays[r.Date] = struct{}{}
		if r.DriverName == "" {
			continue
		}
		d, ok := drivers[r.DriverName]
		if !ok {
			d = &driverAcc{trips: map[routeTrip]struct{}{}, routes: newBuckets(),
				dayTrips: map[string]int64{}, dayRevenue: map[string]float64{},
				a: DriverAnalytics{DriverName: r.DriverName}}
			drivers[r.DriverName] = d
			names = append(names, r.DriverName)
		}
		route := r.Terminal + "\x00" + r.DropOffPoint
		rb := d.routes.get(route, r)
		rb.trips[r.G] = struct{}{}
		if !r.Mapped {
			continue // an unmapped route has no distance or rate: counted in the distribution only
		}
		key := routeTrip{route, r.G}
		if _, seen := d.trips[key]; !seen {
			d.trips[key] = struct{}{}
			d.a.TotalTrips++
			d.a.TotalDistance += r.Distance
			d.dayTrips[r.Date]++
		}
		d.a.TotalVolume += r.TankCapacity
		d.a.TotalRevenue += r.Base + r.VAT
		d.a.TotalVAT += r.VAT
		d.dayRevenue[r.Date] += r.Base + r.VAT
	}
	sort.Strings(names)

	globalDays := int64(len(allDays))
	if globalDays == 0 {
		globalDays = 1
	}
	var g DriverGlobalStats
	var globalWorkingDays int64
	type perf struct {
		name    string
		revenue float64
	}
	var ranking []perf
	for _, name := range names {
		d := drivers[name]
		a := d.a
		a.TotalFees = a.TotalRevenue
		a.TotalAmount = a.TotalRevenue
		a.RouteDistribution = []DriverRoute{}
		for _, rb := range d.routes.sorted() {
			a.RouteDistribution = append(a.RouteDistribution, DriverRoute{
				Terminal: rb.first.Terminal, DropOffPoint: rb.first.DropOffPoint, Count: rb.tripCount(),
			})
		}
		sort.SliceStable(a.RouteDistribution, func(i, j int) bool {
			return a.RouteDistribution[i].Count > a.RouteDistribution[j].Count
		})
		a.ActivityHeatmap = []DriverActivity{}
		for date, n := range d.dayTrips {
			a.ActivityHeatmap = append(a.ActivityHeatmap, DriverActivity{Date: date, Count: n, Revenue: d.dayRevenue[date]})
			if n > 0 {
				a.WorkingDays++
			}
		}
		sort.Slice(a.ActivityHeatmap, func(i, j int) bool { return a.ActivityHeatmap[i].Date < a.ActivityHeatmap[j].Date })
		if a.WorkingDays > 0 {
			a.AvgTripsPerDay = float64(a.TotalTrips) / float64(a.WorkingDays)
			a.AvgKmPerDay = a.TotalDistance / float64(a.WorkingDays)
			a.AvgFeesPerDay = a.TotalFees / float64(a.WorkingDays)
		}
		if a.TotalDistance > 0 {
			a.AvgTripsPerKm = float64(a.TotalTrips) / a.TotalDistance
			a.AvgVolumePerKm = a.TotalVolume / a.TotalDistance
		}
		g.TotalTrips += a.TotalTrips
		g.TotalDistance += a.TotalDistance
		g.TotalVolume += a.TotalVolume
		g.TotalFees += a.TotalFees
		g.TotalRevenue += a.TotalRevenue
		globalWorkingDays += a.WorkingDays
		ranking = append(ranking, perf{name, a.TotalFees})
		resp.Drivers = append(resp.Drivers, a)
	}

	if n := len(resp.Drivers); n > 0 {
		g.AvgTripsPerDriver = float64(g.TotalTrips) / float64(n)
		g.AvgDistancePerDriver = g.TotalDistance / float64(n)
	}
	if globalWorkingDays > 0 {
		g.AvgTripsPerDay = float64(g.TotalTrips) / float64(globalWorkingDays)
		g.AvgRevenuePerDay = g.TotalFees / float64(globalWorkingDays)
	}
	g.AvgKmPerDay = g.TotalDistance / float64(globalDays)
	if g.TotalDistance > 0 {
		g.AvgVolumePerKm = g.TotalVolume / g.TotalDistance
	}
	// Efficiency: a driver's revenue per working day against the fleet's.
	for i := range resp.Drivers {
		d := &resp.Drivers[i]
		if d.TotalFees > 0 && g.AvgRevenuePerDay > 0 && d.WorkingDays > 0 {
			d.Efficiency = d.TotalFees / float64(d.WorkingDays) / g.AvgRevenuePerDay
		}
	}
	sort.SliceStable(ranking, func(i, j int) bool { return ranking[i].revenue > ranking[j].revenue })
	g.TopDrivers = make([]string, 0, 5)
	for i := 0; i < len(ranking) && i < 5; i++ {
		g.TopDrivers = append(g.TopDrivers, ranking[i].name)
	}
	resp.GlobalStats = g
	return resp
}

// GetWatanyaDriverAnalytics: GET /api/trips/watanya/driver-analytics.
func (h *TripHandler) GetWatanyaDriverAnalytics(c *fiber.Ctx) error {
	startDate, endDate := c.Query("start_date"), c.Query("end_date")
	if startDate == "" || endDate == "" { // as before: a range needs both ends
		startDate, endDate = "", ""
	}
	hasFinancialAccess := middleware.MoneyVisible(c)

	var feeMappingCount int64
	h.DB.Model(&Models.FeeMapping{}).Where("company = ?", watanyaCompany).Count(&feeMappingCount)
	if feeMappingCount == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "No fee mappings found for Watanya company",
			"error":   "Fee mappings are required for proper analytics",
		})
	}

	rows, err := loadStatRows(h.DB, startDate, endDate, watanyaCompany)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch driver analytics",
			"error":   err.Error(),
		})
	}
	resp := buildDriverAnalytics(rows)
	message := "Watanya driver analytics retrieved successfully"
	if len(resp.Drivers) == 0 {
		message = "No drivers found for the given filters"
	}
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":            message,
		"data":               resp,
		"hasFinancialAccess": hasFinancialAccess,
	})
}

/* ------------------------------------------------------------------------ */
/* Dashboard widget                                                          */
/* ------------------------------------------------------------------------ */

// GlobalStats is the dashboard widget at /api/stats/widget-data (permission 1,
// money masked below 3). It carries no fuel numbers: fuel events are
// permission 4 and up, and an aggregate is still fuel data.
type GlobalStats struct {
	TotalTrips     int64   `json:"total_trips"`
	TotalRevenue   float64 `json:"total_revenue"` // stored base revenue
	TotalMileage   float64 `json:"total_mileage"`
	TotalDistance  float64 `json:"total_distance"` // mapped distance
	UniqueCars     int64   `json:"unique_cars"`
	UniqueDrivers  int64   `json:"unique_drivers"`
	TopTransporter string  `json:"top_transporter"`
	LastUpdate     string  `json:"last_update"`
	RevenuePerKm   float64 `json:"revenue_per_km"`
}

func (h *TripHandler) GetGlobalStats(c *fiber.Ctx) error {
	stats := GlobalStats{LastUpdate: time.Now().Format(time.RFC3339)}
	var totals struct {
		Trips    int64
		Revenue  float64
		Mileage  float64
		Distance float64
		Cars     int64
		Drivers  int64
	}
	if err := h.DB.Raw(`
        SELECT COUNT(*) AS trips,
               COALESCE(SUM(t.base_revenue), 0)::float8 AS revenue,
               COALESCE(SUM(t.mileage), 0)::float8      AS mileage,
               COALESCE(SUM(fm.distance), 0)::float8    AS distance,
               COUNT(DISTINCT t.car_id)                 AS cars,
               COUNT(DISTINCT t.driver_id)              AS drivers
          FROM trips t
          LEFT JOIN LATERAL (
                SELECT m.distance FROM fee_mappings m
                 WHERE m.company = t.company AND m.terminal = t.terminal
                   AND m.drop_off_point = t.drop_off_point AND m.deleted_at IS NULL
                 ORDER BY m.id LIMIT 1
          ) fm ON true
         WHERE t.deleted_at IS NULL`).Scan(&totals).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	stats.TotalTrips, stats.TotalRevenue, stats.TotalMileage = totals.Trips, totals.Revenue, totals.Mileage
	stats.TotalDistance, stats.UniqueCars, stats.UniqueDrivers = totals.Distance, totals.Cars, totals.Drivers

	var top struct {
		Transporter string
		Count       int
	}
	h.DB.Model(&Models.TripStruct{}).
		Select("transporter, COUNT(*) as count").
		Group("transporter").Order("count DESC").Limit(1).Scan(&top)
	stats.TopTransporter = top.Transporter

	if stats.TotalDistance > 0 {
		stats.RevenuePerKm = stats.TotalRevenue / stats.TotalDistance
	}
	return c.JSON(fiber.Map{"success": true, "stats": stats})
}
