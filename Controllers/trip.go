package Controllers

import (
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/pricing"
	"Falcon/report"
	"Falcon/services"
	"Falcon/utils"
	"context"
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
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/datatypes"
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

// ─────────────────────────────────────────────────────────────────────────────
// Watanya trips report
//
// The workbook itself is laid out by package Falcon/report (Summary and Trip
// Details sheets, A4 portrait, its own page planning). This file only fetches
// the trips with their stored prices and hands them over in report.Report form.
//
// The report is a customer statement and has always billed the base amount,
// excluding VAT (VAT goes on the invoice): Amount and Cost are the trips'
// stored base_revenue, and the fee rate printed is the one they were priced at.
// ─────────────────────────────────────────────────────────────────────────────

// DateRangeRequest is the JSON body for all date-filtered endpoints.
type DateRangeRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// wtReportRequest is the export body. All but the dates are optional:
//
//	lang:   "ar" gives Arabic labels and a right-to-left layout; else English.
//	format: "xlsx" (default) — one workbook, Summary and Trip Details sheets;
//	        "pdf"  — one part of it as a PDF with the Cairo font embedded.
//	part:   with format "pdf": "summary" or "details".
type wtReportRequest struct {
	DateRangeRequest
	Lang   string `json:"lang"`
	Format string `json:"format"`
	Part   string `json:"part"`
}

const (
	wtCompany = "Apex Transport"
	wtTitleEN = "Watanya Trips Report"
	wtTitleAR = "تقرير رحلات وطنية"

	// wtEmptyRangeCode tells the dashboard the range simply has nothing to
	// bill, as opposed to a failure.
	wtEmptyRangeCode = "WATANYA_EMPTY_RANGE"
)

func (h *TripHandler) GetWatanyaTripsReport(c *fiber.Ctx) error {
	// ── 1. Parse request ─────────────────────────────────────────────────────
	var req wtReportRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Cannot parse request body"})
	}
	if err := req.normalise(); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// ── 2. Fetch data ─────────────────────────────────────────────────────────
	var trips []Models.TripStruct
	if err := h.DB.Where("company = ? AND date BETWEEN ? AND ?",
		"Watanya", req.StartDate, req.EndDate).Find(&trips).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error: " + err.Error()})
	}

	var feeMappings []Models.FeeMapping
	if err := h.DB.Where("company = ?", "Watanya").Find(&feeMappings).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve fee mappings: " + err.Error()})
	}

	return wtRespond(c, req, trips, feeMappings)
}

// normalise checks the dates are present and canonicalises lang, format and part.
func (req *wtReportRequest) normalise() error {
	if req.StartDate == "" || req.EndDate == "" {
		return fmt.Errorf("Start date and end date are required")
	}
	req.Lang = strings.ToLower(strings.TrimSpace(req.Lang))
	if req.Lang != "ar" {
		req.Lang = "en"
	}
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	req.Part = strings.ToLower(strings.TrimSpace(req.Part))
	switch req.Format {
	case "", "xlsx":
		req.Format = "xlsx"
	case "pdf":
		if req.Part != "summary" && req.Part != "details" {
			return fmt.Errorf(`part must be "summary" or "details" for a PDF`)
		}
	default:
		return fmt.Errorf(`format must be "xlsx" or "pdf"`)
	}
	return nil
}

// wtRespond builds and sends the report for already-fetched data.
func wtRespond(c *fiber.Ctx, req wtReportRequest, trips []Models.TripStruct, feeMappings []Models.FeeMapping) error {
	lang := report.English
	if req.Lang == "ar" {
		lang = report.Arabic
	}
	format, part := req.Format, req.Part

	// ── 3. Build the report ──────────────────────────────────────────────────
	rep := wtBuildReport(trips, feeMappings, lang)
	rep.Period = wtPeriod(lang, req.StartDate, req.EndDate)
	if len(rep.Groups) == 0 {
		// The report package refuses an empty workbook; tell the user why.
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
			"error": wtEmptyRangeMessage(lang, rep.Period),
			"code":  wtEmptyRangeCode,
		})
	}

	// ── 4. Render ────────────────────────────────────────────────────────────
	if format == "pdf" {
		data, err := wtRenderPDF(rep, part)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		filename := fmt.Sprintf("watanya_%s_%s_%s.pdf", part,
			strings.ReplaceAll(req.StartDate, "-", ""), strings.ReplaceAll(req.EndDate, "-", ""))
		c.Set("Content-Type", "application/pdf")
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		c.Set("Access-Control-Expose-Headers", "Content-Disposition")
		c.Set("Cache-Control", "no-store")
		return c.Send(data)
	}
	filePath, err := wtRenderReport(rep)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendFile(filePath, true)
}

// wtPeriod is the header's period: the requested range, in the report's style.
func wtPeriod(lang report.Lang, start, end string) string {
	from, errFrom := time.Parse("2006-01-02", start)
	to, errTo := time.Parse("2006-01-02", end)
	if errFrom != nil || errTo != nil {
		return start + " – " + end
	}
	return report.RangeLabel(lang, from, to)
}

func wtEmptyRangeMessage(lang report.Lang, period string) string {
	if lang == report.Arabic {
		return "لا توجد رحلات وطنية مسعّرة في الفترة " + period
	}
	return "No priced Watanya trips in " + period
}

// wtBuildReport organises trips into the DropOff→Terminal hierarchy and fills
// both halves of the report: Groups (Summary) and Sections (Trip Details), one
// section per summary line with the same number.
//
// Trips with no fee mapping for their Terminal↔DropOff route, or that the
// pricing engine could not price (a pricing_error, no stored base revenue),
// are left out, as unmapped trips always were: an unpriced route is a
// data-entry gap, not a zero bill.
func wtBuildReport(
	trips []Models.TripStruct,
	feeMappings []Models.FeeMapping,
	lang report.Lang,
) report.Report {
	rep := report.Report{Lang: lang, Company: wtCompany, Title: wtTitleEN}
	if lang == report.Arabic {
		rep.Title = wtTitleAR
	}

	// Index fee mappings by "Terminal|DropOffPoint"
	feeByKey := make(map[string]Models.FeeMapping, len(feeMappings))
	for _, m := range feeMappings {
		feeByKey[m.Terminal+"|"+m.DropOffPoint] = m
	}

	// Bucket trips: DropOff → Terminal → []Trip
	buckets := make(map[string]map[string][]Models.TripStruct)
	for _, trip := range trips {
		if _, ok := feeByKey[trip.Terminal+"|"+trip.DropOffPoint]; !ok || trip.BaseRevenue == nil {
			continue // skip trips with no fee mapping or no stored price
		}
		if buckets[trip.DropOffPoint] == nil {
			buckets[trip.DropOffPoint] = make(map[string][]Models.TripStruct)
		}
		buckets[trip.DropOffPoint][trip.Terminal] = append(
			buckets[trip.DropOffPoint][trip.Terminal], trip)
	}

	dropOffNames := make([]string, 0, len(buckets))
	for d := range buckets {
		dropOffNames = append(dropOffNames, d)
	}
	// Same order as the receipt piles screen — see services.SortPileOrderDropOffs.
	services.SortPileOrderDropOffs(dropOffNames)

	no := 1
	for _, dropOff := range dropOffNames {
		terminals := buckets[dropOff]
		terminalNames := make([]string, 0, len(terminals))
		for t := range terminals {
			terminalNames = append(terminalNames, t)
		}
		services.SortReportNames(terminalNames)

		group := report.SummaryGroup{DropOff: dropOff}
		for _, terminal := range terminalNames {
			tripList := terminals[terminal]
			fm := feeByKey[terminal+"|"+dropOff]

			// Sort by date, then receipt number.
			services.SortReportTrips(tripList)

			section := report.TripSection{No: no, Terminal: terminal, DropOff: dropOff}
			var totalCapacity int
			var amount float64
			for _, t := range tripList {
				cost := *t.BaseRevenue
				totalCapacity += t.TankCapacity
				amount += cost
				section.Trips = append(section.Trips, report.Trip{
					Date:     wtParseDate(t.Date),
					Receipt:  t.ReceiptNo,
					Driver:   t.DriverName,
					Plate:    t.CarNoPlate,
					Capacity: float64(t.TankCapacity),
					Distance: t.Mileage,
					FeeRate:  services.RatePer1000L(cost, float64(t.TankCapacity)),
					Cost:     cost,
				})
			}

			group.Lines = append(group.Lines, report.SummaryLine{
				No:       no,
				Terminal: terminal,
				Trips:    len(tripList),
				Capacity: float64(totalCapacity),
				Distance: fm.Distance,
				FeeIndex: int(fm.Fee),
				FeeRate:  services.RatePer1000L(amount, float64(totalCapacity)),
				Amount:   amount,
			})
			rep.Sections = append(rep.Sections, section)
			no++
		}
		rep.Groups = append(rep.Groups, group)
	}
	return rep
}

// wtParseDate reads a trip date. Dates are stored as YYYY-MM-DD text; a
// longer timestamp is cut to its date. Anything else gives the zero time,
// which the report prints as an empty cell.
func wtParseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if len(s) > 10 {
		s = s[:10]
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return d
}

// wtSheetNames returns the Summary and Trip Details sheet names for lang.
func wtSheetNames(lang report.Lang) (summary, details string) {
	if lang == report.Arabic {
		return "الملخص", "تفاصيل الرحلات"
	}
	return "Summary", "Trip Details"
}

// wtBuildBook writes both sheets into a new workbook.
func wtBuildBook(rep report.Report) (*report.Book, error) {
	book := report.NewBook()
	summaryName, detailsName := wtSheetNames(rep.Lang)
	if _, err := book.Summary(summaryName, rep); err != nil {
		book.Close()
		return nil, fmt.Errorf("failed to write summary sheet: %w", err)
	}
	if _, err := book.Details(detailsName, rep); err != nil {
		book.Close()
		return nil, fmt.Errorf("failed to write trip details sheet: %w", err)
	}
	return book, nil
}

// wtRenderReport writes the workbook to a temp file and returns its path.
func wtRenderReport(rep report.Report) (string, error) {
	book, err := wtBuildBook(rep)
	if err != nil {
		return "", err
	}
	defer book.Close()
	data, err := book.XLSX()
	if err != nil {
		return "", err
	}
	timestamp := time.Now().Format("20060102_150405")
	filePath := filepath.Join(os.TempDir(), fmt.Sprintf("watanya_report_%s.xlsx", timestamp))
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to save report: %w", err)
	}
	return filePath, nil
}

// wtRenderPDF draws one part of the report — "summary" or "details" — as a
// PDF, from the very sheet the workbook carries.
func wtRenderPDF(rep report.Report, part string) ([]byte, error) {
	book, err := wtBuildBook(rep)
	if err != nil {
		return nil, err
	}
	defer book.Close()
	summaryName, detailsName := wtSheetNames(rep.Lang)
	sheet := summaryName
	if part == "details" {
		sheet = detailsName
	}
	return book.PDF(rep.Title, sheet)
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

	trip.ReceiptValid = services.ReceiptValidity(h.DB, trip.TerminalID, trip.Company, trip.ReceiptNo)

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trip retrieved successfully",
		"data":    trip,
	})
}

type RouteResult struct {
	OSRMData      map[string]interface{} `json:"osrm_data"`
	GoogleMapsURL string                 `json:"googe_maps_url"`
}

func getRouteFromOSRM(ctx context.Context, startLat, startLng, endLat, endLng float64) (*RouteResult, error) {
	// Through etit-proxy, which owns the routing rules. This used to build its
	// own http://localhost:5000 URL with no `exclude`, so the route drawn on
	// the trip map could follow time-gated roads the audit's own routing
	// avoids — the picture disagreed with the number beside it.
	distanceKm, durationMin, geometry, err := services.RouteWithGeometryCtx(ctx,
		startLat, startLng, endLat, endLng, true)
	if err != nil {
		return nil, err
	}

	// Rebuilt in OSRM's shape because generateGoogleMapsURLFromOSRM and the
	// trip-details handler both read it that way: metres and seconds, with the
	// polyline under routes[0].geometry.
	result := map[string]interface{}{
		"code": "Ok",
		"routes": []interface{}{
			map[string]interface{}{
				"distance": distanceKm * 1000,
				"duration": durationMin * 60,
				"geometry": geometry,
			},
		},
	}

	return &RouteResult{
		OSRMData:      result,
		GoogleMapsURL: generateGoogleMapsURLFromOSRM(result),
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
	// Pin comes from drop_off_points; fee_mappings no longer stores one.
	_ = Models.HydrateFeeMappingCoord(h.DB, &mapping)

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
			osrmData, err := getRouteFromOSRM(c.UserContext(), Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
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

	oldTripDate := existingTrip.Date
	// What the trip was priced from, to tell whether this edit re-prices it.
	priceInputs := func(t Models.TripStruct) string {
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d",
			t.Company, t.Terminal, t.DropOffPoint, t.TankCapacity, t.Date, t.CarNoPlate, t.CarID)
	}
	oldPriceInputs := priceInputs(existingTrip)
	oldMonth, oldMonthOK := pricing.MonthOf(existingTrip.Company, existingTrip.Date)

	// Parse the update data
	updatedTrip := new(Models.TripStruct)
	if err := c.BodyParser(updatedTrip); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	// Optional FK-era fields. Parsed separately with pointers so that an
	// absent field is distinguishable from a false/zero one -- the legacy
	// string-only payload keeps working unchanged.
	var aux struct {
		TerminalID      *uint `json:"terminal_id"`
		ReceiptOverride *bool `json:"receipt_override"`
	}
	_ = c.BodyParser(&aux)

	// A terminal picked by id wins over the terminal string: validate it
	// against the company allowlist and drive the legacy string from the
	// canonical name.
	if aux.TerminalID != nil {
		var terminal Models.Terminal
		if err := h.DB.First(&terminal, *aux.TerminalID).Error; err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"message": "Terminal not found",
			})
		}
		company := existingTrip.Company
		if updatedTrip.Company != "" {
			company = updatedTrip.Company
		}
		allowed, err := terminalAllowedForCompany(h.DB, terminal.ID, company)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to validate terminal",
				"error":   err.Error(),
			})
		}
		if !allowed {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Terminal %s is not allowed for company %s", terminal.Name, company),
			})
		}
		updatedTrip.TerminalID = aux.TerminalID
		updatedTrip.Terminal = terminal.Name
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
		// tank_capacity and gas_type are derived from the compartments a drop
		// was loaded from. This single-trip path knows nothing about them, so
		// letting it write either would leave the row contradicting itself.
		// Compartment-planned trips are edited through the parent trip.
		if len(existingTrip.Compartments) > 0 && string(existingTrip.Compartments) != "null" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "This trip's volume comes from its compartments. Edit it from the trip's compartment plan.",
			})
		}
		existingTrip.TankCapacity = updatedTrip.TankCapacity
	}
	if updatedTrip.Company != "" {
		existingTrip.Company = updatedTrip.Company
	}
	if updatedTrip.Terminal != "" {
		existingTrip.Terminal = updatedTrip.Terminal
	}
	if updatedTrip.TerminalID != nil {
		existingTrip.TerminalID = updatedTrip.TerminalID
	} else if terminalChanged {
		// Legacy string-only rename: keep the FK in sync by name (nil when
		// the string matches no canonical terminal).
		if terminal := resolveTerminalByName(h.DB, existingTrip.Terminal); terminal != nil {
			existingTrip.TerminalID = &terminal.ID
		} else {
			existingTrip.TerminalID = nil
		}
	}
	if aux.ReceiptOverride != nil {
		existingTrip.ReceiptOverride = *aux.ReceiptOverride
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
		if len(existingTrip.Compartments) > 0 && string(existingTrip.Compartments) != "null" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "This trip's gas type comes from its compartments. Edit it from the trip's compartment plan.",
			})
		}
		existingTrip.GasType = updatedTrip.GasType
	}
	if updatedTrip.Date != "" {
		existingTrip.Date = updatedTrip.Date
	}

	// For numerical fields, check explicitly to allow setting to zero.
	//
	// Revenue is not one of them any more: it is owned by the pricing engine
	// and computed from the rule effective on the trip's date. A revenue in
	// the body (and the old X-Update-Revenue header) is ignored; a wrong
	// figure is corrected by fixing its inputs or a rule, then re-pricing
	// (POST /api/pricing/reprice), never by typing a number in.
	if c.Body() != nil {
		if c.Get("X-Update-Mileage") != "" {
			existingTrip.Mileage = updatedTrip.Mileage
		} else if updatedTrip.Mileage != 0 {
			existingTrip.Mileage = updatedTrip.Mileage
		}
	}

	if updatedTrip.ReceiptNo != "" {
		existingTrip.ReceiptNo = updatedTrip.ReceiptNo
	}

	// Save the updated trip and, when anything it is priced from changed (or
	// it was never priced), re-price it — with its logical trip, since a
	// container's change can move which sibling holds the furthest drop —
	// and recompute the truck-month it left as well as the one it is in.
	repriced := priceInputs(existingTrip) != oldPriceInputs ||
		existingTrip.PricedAt == nil || existingTrip.PricingError != nil
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit(pricing.Columns...).Save(&existingTrip).Error; err != nil {
			return err
		}
		if !repriced {
			return nil
		}
		var extra []pricing.MonthRef
		if oldMonthOK {
			extra = append(extra, oldMonth)
		}
		if _, err := pricing.PriceTrips(tx, []uint{existingTrip.ID}, extra...); err != nil {
			return err
		}
		trips := []Models.TripStruct{existingTrip}
		if err := pricing.Hydrate(tx, trips); err != nil {
			return err
		}
		existingTrip = trips[0]
		return nil
	})
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update trip",
			"error":   err.Error(),
		})
	}

	services.NotifyTripAudit(oldTripDate, existingTrip.Date)

	// Refresh fee mapping data
	var mapping Models.FeeMapping
	h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
		existingTrip.Company, existingTrip.Terminal, existingTrip.DropOffPoint).First(&mapping)

	if mapping.ID > 0 {
		existingTrip.Distance = mapping.Distance
		existingTrip.Fee = mapping.Fee
	}
	// Pin comes from drop_off_points; fee_mappings no longer stores one.
	_ = Models.HydrateFeeMappingCoord(h.DB, &mapping)
	existingTrip.ReceiptValid = services.ReceiptValidity(h.DB, existingTrip.TerminalID, existingTrip.Company, existingTrip.ReceiptNo)
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
			osrmData, err := getRouteFromOSRM(c.UserContext(), Terminal.Latitude, Terminal.Longitude, mapping.Latitude, mapping.Longitude)
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

	// Perform soft delete (GORM default with DeletedAt field), then re-price
	// the rest of its logical trip and recompute its truck-month without it.
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&trip).Error; err != nil {
			return err
		}
		_, err := pricing.PriceTrips(tx, []uint{trip.ID})
		return err
	})
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to delete trip",
			"error":   err.Error(),
		})
	}

	services.NotifyTripAudit(trip.Date)

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

			// Calculate trip details. No money: the driver app shows none
			// (and StripMoney scrubs the route besides).
			var calculatedTankCapacity int

			for j := range trip.ContainerTrips {
				containerTrip := &trip.ContainerTrips[j]
				calculatedTankCapacity += containerTrip.TankCapacity

				// Get fee mapping
				var mapping Models.FeeMapping
				h.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
					containerTrip.Company, containerTrip.Terminal, containerTrip.DropOffPoint).
					First(&mapping)

				// Pin comes from drop_off_points; fee_mappings no longer stores
				// one. Must run BEFORE the coordinates are read below.
				_ = Models.HydrateFeeMappingCoord(h.DB, &mapping)

				if mapping.ID > 0 {
					containerTrip.Distance = mapping.Distance

					// Populate location from the canonical pin
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

				// Populate location from the canonical pin
				_ = Models.HydrateFeeMappingCoord(h.DB, &mapping)
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
			query = query.Where("driver_name = ?", unregisteredDropOff)
		case "route":
			query = query.Where("drop_off_point = ?", unregisteredDropOff)
		case "any":
			query = query.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
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
			countQuery = countQuery.Where("driver_name = ?", unregisteredDropOff)
		case "route":
			countQuery = countQuery.Where("drop_off_point = ?", unregisteredDropOff)
		case "any":
			countQuery = countQuery.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
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

	services.EnrichReceiptValidity(h.DB, trips)

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
			query = query.Where("driver_name = ?", unregisteredDropOff)
		case "route":
			query = query.Where("drop_off_point = ?", unregisteredDropOff)
		case "any":
			query = query.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
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
			countQuery = countQuery.Where("driver_name = ?", unregisteredDropOff)
		case "route":
			countQuery = countQuery.Where("drop_off_point = ?", unregisteredDropOff)
		case "any":
			countQuery = countQuery.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
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

	services.EnrichReceiptValidity(h.DB, trips)

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
			query = query.Where("driver_name = ?", unregisteredDropOff)
		case "route":
			query = query.Where("drop_off_point = ?", unregisteredDropOff)
		case "any":
			query = query.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
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
			countQuery = countQuery.Where("driver_name = ?", unregisteredDropOff)
		case "route":
			countQuery = countQuery.Where("drop_off_point = ?", unregisteredDropOff)
		case "any":
			countQuery = countQuery.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
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

	services.EnrichReceiptValidity(h.DB, trips)

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

// enforceCapacityMatch compares the summed container capacities against the
// car's registered tank capacity. On mismatch, admins (permission >= 4) get
// the trip through with a logged override; everyone else is rejected with a
// 400. Returns handled=true when a response has already been written — the
// caller must then return the accompanying error.
func (h *TripHandler) enforceCapacityMatch(
	c *fiber.Ctx,
	carID uint,
	carNoPlate string,
	containers []Models.TripStruct,
) (bool, error) {
	if carID == 0 {
		return false, nil
	}

	var car Models.Car
	if err := h.DB.First(&car, carID).Error; err != nil || car.TankCapacity <= 0 {
		// Unknown car or no registered capacity — nothing to enforce
		return false, nil
	}

	totalCapacity := 0
	for _, container := range containers {
		totalCapacity += container.TankCapacity
	}
	if totalCapacity == car.TankCapacity {
		return false, nil
	}

	user, ok := c.Locals("user").(*Models.User)
	if !ok || user.Permission < 4 {
		return true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": fmt.Sprintf(
				"Total container capacity (%d L) doesn't match the truck's tank capacity (%d L)",
				totalCapacity, car.TankCapacity),
		})
	}

	log.Printf("⚠️  CAPACITY OVERRIDE: %s saved a trip for %s with %d L against a registered capacity of %d L",
		user.Name, carNoPlate, totalCapacity, car.TankCapacity)
	return false, nil
}

// gasTypeOrder is the canonical product ordering, mirroring GAS_TYPE_VALUES in
// the web client's trip form. Derived gas_type strings are built in this order
// so the server and the client always produce byte-identical values.
var gasTypeOrder = []string{"80", "92", "95", "diesel"}

// deriveGasTypeSet renders the distinct products of a drop's compartments as
// the comma-separated form the gas_type column has always used ("80,92").
// Products outside the known set are kept, appended after the known ones in
// sorted order, so an unrecognised value is never silently dropped.
func deriveGasTypeSet(compartments []Models.TripCompartment) string {
	seen := make(map[string]bool, len(compartments))
	for _, compartment := range compartments {
		gas := strings.ToLower(strings.TrimSpace(compartment.GasType))
		if gas != "" {
			seen[gas] = true
		}
	}

	ordered := make([]string, 0, len(seen))
	for _, gas := range gasTypeOrder {
		if seen[gas] {
			ordered = append(ordered, gas)
			delete(seen, gas)
		}
	}
	extras := make([]string, 0, len(seen))
	for gas := range seen {
		extras = append(extras, gas)
	}
	sort.Strings(extras)

	return strings.Join(append(ordered, extras...), ",")
}

// compartmentPlan is the server's canonical reading of a multi-container
// payload's compartment assignment. Applied is false when the payload carries
// no compartment data at all (a legacy client, or a trip recorded before
// compartment planning existed) — the caller then falls back to the old
// whole-truck capacity check.
type compartmentPlan struct {
	Applied      bool
	PerContainer [][]Models.TripCompartment
}

// TankCapacity is the drop's volume as the server computes it: the sum of the
// compartments it was loaded from. The client's own figure is never trusted.
func (p compartmentPlan) TankCapacity(index int) int {
	total := 0
	for _, compartment := range p.PerContainer[index] {
		total += compartment.Volume
	}
	return total
}

// GasType is the drop's product set as the server computes it.
func (p compartmentPlan) GasType(index int) string {
	return deriveGasTypeSet(p.PerContainer[index])
}

// Marshal renders a drop's compartments back to storage form, from the parsed
// values rather than the client's bytes.
func (p compartmentPlan) Marshal(index int) datatypes.JSON {
	encoded, err := json.Marshal(p.PerContainer[index])
	if err != nil {
		return nil
	}
	return datatypes.JSON(encoded)
}

// mergeContainersForValidation builds the state a parent trip will be left in
// once an update is applied. The update path merges field-by-field and leaves
// containers absent from the payload untouched, so validating the payload
// alone would let a caller shrink a trip past every capacity and coverage rule
// simply by sending fewer containers. Validating the merged result closes that.
//
// The returned map points each payload container at its position in the merged
// set, so the caller can read back the volumes and products the server derived.
func mergeContainersForValidation(
	existing []Models.TripStruct,
	input []Models.TripStruct,
) ([]Models.TripStruct, map[int]int) {
	inputByID := make(map[uint]int, len(input))
	for i, container := range input {
		if container.ID != 0 {
			inputByID[container.ID] = i
		}
	}

	effective := make([]Models.TripStruct, 0, len(existing)+len(input))
	inputToEffective := make(map[int]int, len(input))

	for _, stored := range existing {
		i, updated := inputByID[stored.ID]
		if !updated {
			effective = append(effective, stored)
			continue
		}
		// Mirror the field-presence merge the update block performs below.
		merged := stored
		if input[i].DropOffPoint != "" {
			merged.DropOffPoint = input[i].DropOffPoint
		}
		if input[i].TankCapacity != 0 {
			merged.TankCapacity = input[i].TankCapacity
		}
		if input[i].GasType != "" {
			merged.GasType = input[i].GasType
		}
		if input[i].ReceiptNo != "" {
			merged.ReceiptNo = input[i].ReceiptNo
		}
		if len(input[i].Compartments) > 0 {
			merged.Compartments = input[i].Compartments
		}
		inputToEffective[i] = len(effective)
		effective = append(effective, merged)
	}

	for i, container := range input {
		if container.ID == 0 {
			inputToEffective[i] = len(effective)
			effective = append(effective, container)
		}
	}

	return effective, inputToEffective
}

// storedPlan is the compartment state already committed for a trip. It exists
// so that re-submitting what is already saved is never treated as a fresh
// override: without it, one permission-4 override would lock the trip to
// permission-4 editors forever, because the form hydrates the stored plan and
// sends it straight back on the next unrelated edit.
type storedPlan struct {
	volumes    map[int]int
	assigned   map[int]bool
	hasRecords bool
}

func loadStoredPlan(containers []Models.TripStruct) storedPlan {
	previous := storedPlan{volumes: map[int]int{}, assigned: map[int]bool{}}
	for _, container := range containers {
		if len(container.Compartments) == 0 || string(container.Compartments) == "null" {
			continue
		}
		var compartments []Models.TripCompartment
		if err := json.Unmarshal(container.Compartments, &compartments); err != nil {
			continue
		}
		previous.hasRecords = true
		for _, compartment := range compartments {
			previous.volumes[compartment.Index] = compartment.Volume
			previous.assigned[compartment.Index] = true
		}
	}
	return previous
}

// enforceCompartmentAssignment validates that a multi-container payload
// describes a loading plan the truck can actually carry: real compartments, no
// compartment loaded twice, volumes matching the registered layout, and every
// compartment accounted for.
//
// The split between hard failures and overridable ones is deliberate.
// Malformed JSON, an out-of-range index, a repeated compartment and a
// tank_capacity that contradicts its own compartments are internal
// inconsistencies — no one may save those, because they would leave the row
// unreadable. A volume that differs from the registered one, or a compartment
// count that doesn't cover the truck, are business judgements about an unusual
// load: permission >= 4 may commit those, and each one is logged.
//
// Returns handled=true when a response has already been written — the caller
// must then return the accompanying error.
func (h *TripHandler) enforceCompartmentAssignment(
	c *fiber.Ctx,
	carID uint,
	carNoPlate string,
	containers []Models.TripStruct,
	previous storedPlan,
) (compartmentPlan, bool, error) {
	var plan compartmentPlan

	if carID == 0 {
		return plan, false, nil
	}

	var car Models.Car
	if err := h.DB.First(&car, carID).Error; err != nil {
		return plan, false, nil
	}
	layout, err := car.CompartmentList()
	if err != nil || len(layout) == 0 {
		// The car has no registered compartment layout — there is nothing to
		// validate against, so the old whole-truck capacity rule still applies.
		return plan, false, nil
	}

	carrying := 0
	for _, container := range containers {
		if len(container.Compartments) > 0 && string(container.Compartments) != "null" {
			carrying++
		}
	}
	if carrying == 0 {
		// Legacy payload. This branch is what keeps every trip recorded before
		// this feature editable — without it the coverage rule below would
		// reject them all.
		return plan, false, nil
	}
	if carrying != len(containers) {
		return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Some drops have compartments assigned and others don't. Assign compartments to every drop, or to none.",
		})
	}

	user, _ := c.Locals("user").(*Models.User)
	canOverride := user != nil && user.Permission >= 4
	userName := "unknown user"
	if user != nil {
		userName = user.Name
	}

	plan.Applied = true
	plan.PerContainer = make([][]Models.TripCompartment, len(containers))
	claimedBy := make(map[int]int, len(layout))

	for i, container := range containers {
		var compartments []Models.TripCompartment
		if err := json.Unmarshal(container.Compartments, &compartments); err != nil {
			return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Drop %d has an unreadable compartment assignment", i+1),
			})
		}
		if len(compartments) == 0 {
			return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Drop %d has no compartments assigned", i+1),
			})
		}

		for _, compartment := range compartments {
			if compartment.Index < 0 || compartment.Index >= len(layout) {
				return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"message": fmt.Sprintf(
						"Drop %d refers to compartment %d, but %s has %d compartments",
						i+1, compartment.Index+1, carNoPlate, len(layout)),
				})
			}
			if previous, taken := claimedBy[compartment.Index]; taken {
				return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"message": fmt.Sprintf(
						"Compartment %d is assigned to both drop %d and drop %d",
						compartment.Index+1, previous+1, i+1),
				})
			}
			claimedBy[compartment.Index] = i

			// Already committed at this volume — the caller is not changing
			// anything, so the override was made once and is not re-litigated.
			alreadyStored := previous.hasRecords &&
				previous.volumes[compartment.Index] == compartment.Volume
			if compartment.Volume != layout[compartment.Index] && !alreadyStored {
				if !canOverride {
					return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"message": fmt.Sprintf(
							"Compartment %d of %s holds %d L, not %d L",
							compartment.Index+1, carNoPlate,
							layout[compartment.Index], compartment.Volume),
					})
				}
				log.Printf("⚠️  COMPARTMENT VOLUME OVERRIDE: %s loaded compartment %d of %s with %d L against a registered %d L",
					userName, compartment.Index+1, carNoPlate,
					compartment.Volume, layout[compartment.Index])
			}
		}

		plan.PerContainer[i] = compartments

		// The client's own derived fields must agree with its compartments.
		// Disagreement means the two halves of the payload describe different
		// loads, which no permission level should be able to store.
		if container.TankCapacity != plan.TankCapacity(i) {
			return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf(
					"Drop %d reports %d L but its compartments hold %d L",
					i+1, container.TankCapacity, plan.TankCapacity(i)),
			})
		}
		if container.GasType != plan.GasType(i) {
			return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf(
					"Drop %d reports gas type %q but its compartments hold %q",
					i+1, container.GasType, plan.GasType(i)),
			})
		}
	}

	if len(claimedBy) != len(layout) {
		unassigned := make([]string, 0, len(layout))
		// Same reasoning as the volume rule: leaving a compartment out that the
		// saved trip already left out is not a new override.
		newlyUnassigned := false
		for index := range layout {
			if _, taken := claimedBy[index]; !taken {
				unassigned = append(unassigned, strconv.Itoa(index+1))
				if !previous.hasRecords || previous.assigned[index] {
					newlyUnassigned = true
				}
			}
		}
		if !canOverride && newlyUnassigned {
			return plan, true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf(
					"Compartment(s) %s of %s aren't assigned to any drop",
					strings.Join(unassigned, ", "), carNoPlate),
			})
		}
		log.Printf("⚠️  COMPARTMENT COVERAGE OVERRIDE: %s saved a trip for %s leaving compartment(s) %s unassigned",
			userName, carNoPlate, strings.Join(unassigned, ", "))
	}

	return plan, false, nil
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

	if input.ParentTrip.Company == "" || input.ParentTrip.Date == "" ||
		(input.ParentTrip.Terminal == "" && input.ParentTrip.TerminalID == nil) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Parent trip must have company, terminal, and date",
		})
	}

	// Resolve the canonical terminal. A terminal_id (new clients) wins: it is
	// validated against the company allowlist and the legacy terminal string
	// is set to the canonical name. A string-only payload (backwards compat)
	// behaves exactly as before, with the FK resolved by name best-effort.
	if input.ParentTrip.TerminalID != nil {
		var terminal Models.Terminal
		if err := h.DB.First(&terminal, *input.ParentTrip.TerminalID).Error; err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "Terminal not found",
			})
		}
		allowed, err := terminalAllowedForCompany(h.DB, terminal.ID, input.ParentTrip.Company)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to validate terminal",
				"error":   err.Error(),
			})
		}
		if !allowed {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Terminal %s is not allowed for company %s",
					terminal.Name, input.ParentTrip.Company),
			})
		}
		input.ParentTrip.Terminal = terminal.Name
	} else if terminal := resolveTerminalByName(h.DB, input.ParentTrip.Terminal); terminal != nil {
		input.ParentTrip.TerminalID = &terminal.ID
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

		if container.DropOffPoint == unregisteredDropOff {
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

	// The loading plan must fit the truck: real compartments, none loaded
	// twice, every one accounted for. Permission-4 admins may override the
	// volume and coverage rules (logged inside the helper). A payload with no
	// compartment data at all falls back to the older whole-truck capacity
	// rule, so clients that predate compartment planning keep working.
	plan, handled, err := h.enforceCompartmentAssignment(
		c, input.ParentTrip.CarID, input.ParentTrip.CarNoPlate, input.Containers,
		storedPlan{},
	)
	if handled {
		return err
	}
	if !plan.Applied {
		if handled, err := h.enforceCapacityMatch(
			c, input.ParentTrip.CarID, input.ParentTrip.CarNoPlate, input.Containers,
		); handled {
			return err
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
		input.Containers[i].TerminalID = input.ParentTrip.TerminalID
		input.Containers[i].Date = input.ParentTrip.Date

		// Store the compartments the server parsed, and the volume and product
		// set it derived from them, rather than the bytes and figures the
		// client sent.
		if plan.Applied {
			input.Containers[i].Compartments = plan.Marshal(i)
			input.Containers[i].TankCapacity = plan.TankCapacity(i)
			input.Containers[i].GasType = plan.GasType(i)
		}

		// Get fee mapping for this container
		var mapping Models.FeeMapping
		tx.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			input.Containers[i].Company, input.Containers[i].Terminal, input.Containers[i].DropOffPoint).
			First(&mapping)

		// Mileage is the planned distance. Revenue is priced below, once
		// every container exists: a per-km trip bills its furthest drop.
		input.Containers[i].Mileage = mapping.Distance

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

	// Price the containers under the rule effective on the trip date and
	// recompute the truck-month. An unpriceable container (unregistered
	// drop-off, no rule for the company, unknown band) is stored with a
	// pricing_error rather than failing the trip.
	if _, err := pricing.PriceParent(tx, input.ParentTrip.ID); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to price the trip",
			"error":   err.Error(),
		})
	}
	if err := pricing.Hydrate(tx, createdContainers); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to load the trip's pricing",
			"error":   err.Error(),
		})
	}

	// Update car's driver assignment
	if input.ParentTrip.DriverID != 0 {
		if err := tx.Model(&Models.Car{}).Where("id = ?", input.ParentTrip.CarID).
			Update("driver_id", input.ParentTrip.DriverID).Error; err != nil {
			log.Println("Warning: Failed to update car driver assignment:", err)
			Observability.ReportJobFailure("trip_write", "car_driver_assignment", err)
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
							Observability.ReportJobFailure("trip_write", "unlink_old_trip", err)
						} else {
							log.Printf("✅ Unlinked old ParentTrip %d from session %d (moved to history)",
								existingTrip.ID, session.ID)
						}
					}

					input.ParentTrip.SessionID = &session.ID
					if err := tx.Save(&input.ParentTrip).Error; err != nil {
						log.Printf("⚠️  Warning: Failed to link trip to session: %v. Trip will be created without current status.", err)
						Observability.ReportJobFailure("trip_write", "link_trip_session", err)
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

	services.NotifyTripAudit(input.ParentTrip.Date)

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

	services.EnrichReceiptValidity(h.DB, containers)

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

	oldParentDate := existingParent.Date

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

	// A terminal picked by id wins over the terminal string: validate it
	// against the company allowlist and drive the legacy string from the
	// canonical name. String-only payloads keep working unchanged.
	if input.ParentTrip.TerminalID != nil {
		var terminal Models.Terminal
		if err := h.DB.First(&terminal, *input.ParentTrip.TerminalID).Error; err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "Terminal not found",
			})
		}
		company := existingParent.Company
		if input.ParentTrip.Company != "" {
			company = input.ParentTrip.Company
		}
		allowed, err := terminalAllowedForCompany(h.DB, terminal.ID, company)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to validate terminal",
				"error":   err.Error(),
			})
		}
		if !allowed {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": fmt.Sprintf("Terminal %s is not allowed for company %s", terminal.Name, company),
			})
		}
		input.ParentTrip.Terminal = terminal.Name
	}

	// Same rules as creation, but applied to the state the trip will be left
	// in — the stored containers merged with the payload — rather than to the
	// payload alone, which a caller could trim to slip past them.
	var plan compartmentPlan
	var inputToEffective map[int]int
	if input.UpdateContainers && len(input.Containers) > 0 {
		capacityCarID := input.ParentTrip.CarID
		capacityCarPlate := input.ParentTrip.CarNoPlate
		if capacityCarID == 0 {
			capacityCarID = existingParent.CarID
			capacityCarPlate = existingParent.CarNoPlate
		}

		var storedContainers []Models.TripStruct
		if err := h.DB.Where("parent_trip_id = ?", uint(id)).
			Order("id").Find(&storedContainers).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to load the trip's containers",
				"error":   err.Error(),
			})
		}
		effective, mapping := mergeContainersForValidation(storedContainers, input.Containers)
		inputToEffective = mapping

		var handled bool
		var err error
		plan, handled, err = h.enforceCompartmentAssignment(
			c, capacityCarID, capacityCarPlate, effective,
			loadStoredPlan(storedContainers),
		)
		if handled {
			return err
		}
		if !plan.Applied {
			if handled, err := h.enforceCapacityMatch(
				c, capacityCarID, capacityCarPlate, effective,
			); handled {
				return err
			}
		}
	}

	// The months the containers sit in before the edit: a date, company or
	// car change moves them out, and the month they leave must lose them.
	oldMonths, err := pricing.MonthsOfParent(h.DB, uint(id))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to load the trip's pricing",
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
		// The ID is taken above, and only when one is sent: a rename that
		// carries driver_name alone used to zero driver_id on the parent and,
		// through updateData below, on every container.
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
		if input.ParentTrip.TerminalID == nil {
			// Legacy string-only rename: keep the FK in sync by name.
			if terminal := resolveTerminalByName(h.DB, existingParent.Terminal); terminal != nil {
				existingParent.TerminalID = &terminal.ID
			} else {
				existingParent.TerminalID = nil
			}
		}
	}
	if input.ParentTrip.TerminalID != nil &&
		(existingParent.TerminalID == nil || *existingParent.TerminalID != *input.ParentTrip.TerminalID) {
		existingParent.TerminalID = input.ParentTrip.TerminalID
		parentFieldsUpdated = true
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
			"terminal_id":  existingParent.TerminalID,
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

			// The drop-off to price against is the one the caller is SENDING,
			// not the one currently stored.
			//
			// This block runs before the per-container updates below, so
			// reading DropOffPoint off the database row gives the pre-edit
			// value. A request that changes the terminal AND corrects a
			// drop-off in the same call was rejected against the old
			// drop-off — the very edit that would have made it valid is
			// applied thirty lines further down.
			incoming := map[uint]string{}
			if input.UpdateContainers {
				for _, ic := range input.Containers {
					if ic.ID != 0 && ic.DropOffPoint != "" {
						incoming[ic.ID] = ic.DropOffPoint
					}
				}
			}

			for _, container := range containers {
				dropOff := container.DropOffPoint
				if sent, ok := incoming[container.ID]; ok {
					dropOff = sent
				}

				// Same guard the per-container block below uses. Without it
				// the two paths disagreed: one rejected the unregistered
				// placeholder, the other deliberately skipped it.
				if dropOff == unregisteredDropOff {
					continue
				}

				var mapping Models.FeeMapping
				err := tx.Where("company = ? AND terminal = ? AND drop_off_point = ?",
					existingParent.Company, existingParent.Terminal, dropOff).
					First(&mapping).Error

				if err != nil {
					if err == gorm.ErrRecordNotFound {
						tx.Rollback()
						reportMissingFeeMapping(existingParent.Company, existingParent.Terminal, dropOff)
						return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
							"message": fmt.Sprintf("No mapping found for %s -> %s (container ID: %d)",
								existingParent.Terminal, dropOff, container.ID),
						})
					}
					tx.Rollback()
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"message": "Failed to fetch fee mapping",
						"error":   err.Error(),
					})
				}

				// Update mileage; revenue is re-priced below.
				if err := tx.Model(&Models.TripStruct{}).
					Where("id = ?", container.ID).
					Updates(map[string]interface{}{
						"mileage": mapping.Distance,
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
		for containerIndex, container := range input.Containers {
			if container.ID != 0 {
				// Update existing container's specific fields
				containerUpdate := map[string]interface{}{}

				if container.DropOffPoint != "" {
					containerUpdate["drop_off_point"] = container.DropOffPoint
					if container.DropOffPoint != unregisteredDropOff {
						// Recalculate fee and distance for this drop-off point.
						// Read through tx, not h.DB: this transaction has
						// already written rows these figures depend on.
						var mapping Models.FeeMapping
						err := tx.Where("company = ? AND terminal = ? AND drop_off_point = ?",
							existingParent.Company, existingParent.Terminal, container.DropOffPoint).
							First(&mapping).Error

						if err != nil {
							if err == gorm.ErrRecordNotFound {
								tx.Rollback()
								reportMissingFeeMapping(existingParent.Company, existingParent.Terminal, container.DropOffPoint)
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
				// Derived from the compartments the server parsed, so they
				// overwrite whatever the client reported for these two.
				if plan.Applied {
					effectiveIndex := inputToEffective[containerIndex]
					containerUpdate["compartments"] = plan.Marshal(effectiveIndex)
					containerUpdate["tank_capacity"] = plan.TankCapacity(effectiveIndex)
					containerUpdate["gas_type"] = plan.GasType(effectiveIndex)
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

				// Get fee mapping for new container. The unregistered
				// placeholder is not a place, so it has no mapping: it is
				// created unpriced, exactly as CreateMultiContainerTrip
				// creates it. It used to be skipped here, so the container was
				// silently dropped while the response still said 200.
				var mapping Models.FeeMapping
				if container.DropOffPoint != unregisteredDropOff {
					err := tx.Where("company = ? AND terminal = ? AND drop_off_point = ?",
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
				}

				// Create new container with ALL fields
				parentIDUint := uint(id)
				newContainer := Models.TripStruct{
					ParentTripID:    &parentIDUint,
					CarID:           existingParent.CarID,
					DriverID:        existingParent.DriverID,
					CarNoPlate:      existingParent.CarNoPlate,
					DriverName:      existingParent.DriverName,
					Company:         existingParent.Company,
					Terminal:        existingParent.Terminal,
					TerminalID:      existingParent.TerminalID,
					Date:            existingParent.Date,
					DropOffPoint:    container.DropOffPoint,
					TankCapacity:    container.TankCapacity,
					GasType:         container.GasType,
					ReceiptNo:       container.ReceiptNo,
					ReceiptOverride: container.ReceiptOverride,
					Mileage:         mapping.Distance,
				}

				if plan.Applied {
					effectiveIndex := inputToEffective[containerIndex]
					newContainer.Compartments = plan.Marshal(effectiveIndex)
					newContainer.TankCapacity = plan.TankCapacity(effectiveIndex)
					newContainer.GasType = plan.GasType(effectiveIndex)
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

	// Re-price every container (the furthest drop may have moved) and
	// recompute the months they left and the months they are in.
	if parentFieldsUpdated || (input.UpdateContainers && len(input.Containers) > 0) {
		if _, err := pricing.PriceParent(tx, uint(id), oldMonths...); err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to price the trip",
				"error":   err.Error(),
			})
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to commit transaction",
			"error":   err.Error(),
		})
	}

	services.NotifyTripAudit(oldParentDate, existingParent.Date)

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
	var containerIDs []uint
	tx.Model(&Models.TripStruct{}).Where("parent_trip_id = ?", uint(id)).Pluck("id", &containerIDs)

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

	// The containers' truck-months lose their days.
	if _, err := pricing.PriceTrips(tx, containerIDs); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to recompute the trip's truck-month",
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

	services.NotifyTripAudit(parentTrip.Date)

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
