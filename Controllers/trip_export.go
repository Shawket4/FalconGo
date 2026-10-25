package Controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"Falcon/Models"
	"Falcon/middleware"
	"Falcon/services"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ExportTripsExcel streams an .xlsx of every trip matching the current filters.
//
// Replaces a browser-side ExcelJS build. Two things it fixes beyond moving the
// work off the client:
//
//   - No row cap. The client fetched with limit=10000 and built the sheet from
//     whatever came back, so a wide filter produced a partial spreadsheet with
//     nothing indicating rows were missing. That is the worst kind of wrong: it
//     looks complete.
//   - No N+1 on fee mappings. The list endpoint issues one query per trip to
//     resolve distance and fee, which is tolerable for a page of 10 and not for
//     an unbounded export. Every mapping is loaded once into a map here.
//
// The response body is the file and nothing else, with Content-Disposition set
// so the browser saves it directly.
func (h *TripHandler) ExportTripsExcel(c *fiber.Ctx) error {
	// Below permission 3 the file carries no fee or revenue columns at all.
	hasFinancialAccess := middleware.MoneyVisible(c)

	// Labels come from the client so both exports read identically and there is
	// no second set of English strings on the server to drift.
	var labels services.TripExportLabels
	if err := c.BodyParser(&labels); err != nil {
		// A malformed or absent body is not fatal -- the builder fills defaults.
		labels = services.TripExportLabels{}
	}

	query := h.DB.Model(&Models.TripStruct{}).
		Order("date DESC, receipt_no DESC").
		Preload("ReceiptSteps")

	query = h.applyTripExportFilters(query, c)

	var trips []Models.TripStruct
	if err := query.Find(&trips).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   err.Error(),
		})
	}

	if len(trips) == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "No trips match the selected filters",
		})
	}

	enrichTripFees(h.DB, trips)
	// Receipt-number serial validation drives the remark column.
	services.EnrichReceiptValidity(h.DB, trips)

	meta := buildExportMeta(c, len(trips))

	// lang normally travels with the labels (it is the language they are
	// in); the query string is accepted too.
	if labels.Lang == "" {
		labels.Lang = c.Query("lang", "")
	}
	asPDF := strings.EqualFold(c.Query("format", ""), "pdf")
	data, err := services.BuildTripsExport(trips, labels, hasFinancialAccess, meta, asPDF)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to build the spreadsheet",
			"error":   err.Error(),
		})
	}

	ext, ctype := "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	if asPDF {
		ext, ctype = "pdf", "application/pdf"
	}
	filename := fmt.Sprintf("trips-%s.%s", time.Now().Format("2006-01-02"), ext)

	c.Set("Content-Type", ctype)
	// Both forms: filename for older clients, filename* for anything non-ASCII.
	c.Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.PathEscape(filename)))
	// The dashboard reads this to name the download; without it the header is
	// invisible to the browser's fetch on a cross-origin response.
	c.Set("Access-Control-Expose-Headers", "Content-Disposition")
	c.Set("Cache-Control", "no-store")

	return c.Status(http.StatusOK).Send(data)
}

// applyTripExportFilters mirrors the list endpoints exactly.
//
// The client resolves filters across three different URLs (/trips, /trips/date,
// /trips/company/:company); the export accepts them all on one endpoint so the
// exported set cannot diverge from the table the user is looking at.
func (h *TripHandler) applyTripExportFilters(query *gorm.DB, c *fiber.Ctx) *gorm.DB {
	if search := c.Query("search", ""); search != "" {
		p := "%" + search + "%"
		query = query.Where(
			"car_no_plate LIKE ? OR driver_name LIKE ? OR drop_off_point LIKE ? OR terminal LIKE ? OR date LIKE ? OR receipt_no LIKE ? OR CAST(tank_capacity AS TEXT) LIKE ?",
			p, p, p, p, p, p, p)
	}

	switch c.Query("missing_data", "") {
	case "driver":
		query = query.Where("driver_name = ?", unregisteredDropOff)
	case "route":
		query = query.Where("drop_off_point = ?", unregisteredDropOff)
	case "any":
		query = query.Where("driver_name = ? OR drop_off_point = ?", unregisteredDropOff, unregisteredDropOff)
	}

	if company := c.Query("company", ""); company != "" {
		query = query.Where("company = ?", company)
	}

	// Receipt status: pending / in_garage / in_office.
	//
	// This calls the LIST endpoint's own filter rather than restating the SQL.
	// The predicates are not obvious -- "in garage" means the most recent
	// receipt step is Garage, not merely that a Garage step exists -- and a
	// second copy would drift from the page it is supposed to mirror.
	//
	// It was missing entirely: the client has always sent receipt_status, the
	// export ignored it, and buildExportMeta printed "receipts: pending" in the
	// banner regardless. So filtering the page to pending and exporting gave
	// you every trip in the range, under a heading claiming otherwise.
	if rs := c.Query("receipt_status", ""); rs != "" {
		query = h.applyReceiptStatusFilter(query, rs)
	}

	// `date` is stored as YYYY-MM-DD text, which compares correctly as a string
	// in that format -- the same comparison the date endpoint uses.
	start, end := c.Query("start_date", ""), c.Query("end_date", "")
	if start != "" && end != "" {
		query = query.Where("date BETWEEN ? AND ?", start, end)
	} else if start != "" {
		query = query.Where("date >= ?", start)
	} else if end != "" {
		query = query.Where("date <= ?", end)
	}

	return query
}

// enrichTripFees resolves distance and fee for every trip in ONE query.
//
// Distance and Fee are `gorm:"-"` -- derived from fee_mappings rather than
// stored -- so they have to be filled in after loading. The list endpoint does
// this with a query per trip; at export size that is thousands of round trips
// for a table that fits comfortably in memory.
func enrichTripFees(db *gorm.DB, trips []Models.TripStruct) {
	var mappings []Models.FeeMapping
	db.Find(&mappings)

	key := func(company, terminal, dropOff string) string {
		return company + "\x00" + terminal + "\x00" + dropOff
	}

	index := make(map[string]Models.FeeMapping, len(mappings))
	for _, m := range mappings {
		index[key(m.Company, m.Terminal, m.DropOffPoint)] = m
	}

	for i := range trips {
		m, ok := index[key(trips[i].Company, trips[i].Terminal, trips[i].DropOffPoint)]
		if !ok {
			continue
		}
		trips[i].Distance = m.Distance
		trips[i].Fee = m.Fee
	}
}

// buildExportMeta describes what the file actually contains, so a spreadsheet
// that outlives the conversation still says which filters produced it.
func buildExportMeta(c *fiber.Ctx, count int) string {
	parts := []string{fmt.Sprintf("%d trips", count)}

	if start, end := c.Query("start_date", ""), c.Query("end_date", ""); start != "" || end != "" {
		switch {
		case start != "" && end != "":
			parts = append(parts, fmt.Sprintf("%s → %s", start, end))
		case start != "":
			parts = append(parts, "from "+start)
		default:
			parts = append(parts, "until "+end)
		}
	}
	if company := c.Query("company", ""); company != "" {
		parts = append(parts, company)
	}
	if search := c.Query("search", ""); search != "" {
		parts = append(parts, fmt.Sprintf("search %q", search))
	}
	if md := c.Query("missing_data", ""); md != "" {
		parts = append(parts, "missing: "+md)
	}
	if rs := c.Query("receipt_status", ""); rs != "" {
		parts = append(parts, "receipts: "+rs)
	}

	parts = append(parts, "exported "+time.Now().Format("02 Jan 2006 15:04"))
	return strings.Join(parts, " · ")
}
