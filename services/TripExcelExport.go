package services

import (
	"math"
	"sort"
	"strings"
	"time"

	"Falcon/Models"
	"Falcon/report"
)

// Trip export, server-side.
//
// This replaces a browser-side ExcelJS build. Two reasons it moved:
//
//   - The client capped the fetch at 10,000 rows, so a wide filter silently
//     produced a partial spreadsheet with nothing to say so. Here the query is
//     unbounded and streams straight into the sheet.
//   - Building a workbook in the browser meant shipping every row to the client
//     and holding a second copy in JS memory just to write it back out.
//
// It now takes its look from the Watanya report (package report): title
// block, headline cards, coloured receipt flags, a live SUBTOTAL totals row,
// autofilter, frozen header, A4 print setup — and the same sheets as a PDF.

// TripExportLabels carries the already-translated column headers and titles.
//
// The server has no i18n bundle, and inventing a second set of English strings
// would let the two exports drift. The client sends what it renders.
type TripExportLabels struct {
	// Lang is the language the labels are in: "ar" lays the sheets out
	// right-to-left and prints the report's own captions in Arabic.
	Lang string `json:"lang"`

	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Sheet    string `json:"sheet"`
	Summary  string `json:"summary"`

	ReceiptNo        string `json:"receipt_no"`
	Date             string `json:"date"`
	Company          string `json:"company"`
	Terminal         string `json:"terminal"`
	DropOffPoint     string `json:"drop_off_point"`
	Vehicle          string `json:"vehicle"`
	Driver           string `json:"driver"`
	TankCapacity     string `json:"tank_capacity"`
	Distance         string `json:"distance"`
	Fee              string `json:"fee"`
	InGarage         string `json:"in_garage"`
	InOffice         string `json:"in_office"`
	Stamped          string `json:"stamped"`
	GarageReceivedBy string `json:"garage_received_by"`
	GarageDate       string `json:"garage_date"`
	OfficeReceivedBy string `json:"office_received_by"`
	OfficeDate       string `json:"office_date"`
	Remark           string `json:"remark"`

	TotalTrips    string `json:"total_trips"`
	TotalVolume   string `json:"total_volume"`
	TotalDistance string `json:"total_distance"`
	TotalFees     string `json:"total_fees"`
	StampedCount  string `json:"stamped_count"`
	Totals        string `json:"totals"`
	Yes           string `json:"yes"`
	No            string `json:"no"`
}

// withDefaults fills anything the client omitted, so a caller that sends
// nothing still gets a usable sheet rather than blank headers.
func (l TripExportLabels) withDefaults() TripExportLabels {
	def := func(v, fallback string) string {
		if strings.TrimSpace(v) == "" {
			return fallback
		}
		return v
	}
	l.Title = def(l.Title, "Trips")
	l.Subtitle = def(l.Subtitle, "Trip records")
	l.Sheet = def(l.Sheet, "Trips")
	l.Summary = def(l.Summary, "Summary")
	l.ReceiptNo = def(l.ReceiptNo, "Receipt No")
	l.Date = def(l.Date, "Date")
	l.Company = def(l.Company, "Company")
	l.Terminal = def(l.Terminal, "Terminal")
	l.DropOffPoint = def(l.DropOffPoint, "Drop-off Point")
	l.Vehicle = def(l.Vehicle, "Vehicle")
	l.Driver = def(l.Driver, "Driver")
	l.TankCapacity = def(l.TankCapacity, "Capacity (L)")
	l.Distance = def(l.Distance, "Distance (km)")
	l.Fee = def(l.Fee, "Fee")
	l.InGarage = def(l.InGarage, "In Garage")
	l.InOffice = def(l.InOffice, "In Office")
	l.Stamped = def(l.Stamped, "Stamped")
	l.GarageReceivedBy = def(l.GarageReceivedBy, "Garage Received By")
	l.GarageDate = def(l.GarageDate, "Garage Date")
	l.OfficeReceivedBy = def(l.OfficeReceivedBy, "Office Received By")
	l.OfficeDate = def(l.OfficeDate, "Office Date")
	l.Remark = def(l.Remark, "Remark")
	l.TotalTrips = def(l.TotalTrips, "Total Trips")
	l.TotalVolume = def(l.TotalVolume, "Total Volume")
	l.TotalDistance = def(l.TotalDistance, "Total Distance")
	l.TotalFees = def(l.TotalFees, "Total Fees")
	l.StampedCount = def(l.StampedCount, "Stamped")
	l.Totals = def(l.Totals, "TOTALS")
	l.Yes = def(l.Yes, "Yes")
	l.No = def(l.No, "No")
	return l
}

// BuildTripsExcel renders the workbook and returns it as bytes.
//
// `showFinancials` mirrors the permission masking used elsewhere: below
// permission 3 the fee column is omitted entirely rather than zeroed, so the
// spreadsheet does not imply every trip was free.
func BuildTripsExcel(trips []Models.TripStruct, labels TripExportLabels, showFinancials bool, meta string) ([]byte, error) {
	return BuildTripsExport(trips, labels, showFinancials, meta, false)
}

// BuildTripsExport renders the trips export as a workbook, or with asPDF as a
// PDF of both of its sheets.
//
// The layout is the Watanya report's (package report): title block,
// headline cards, a column header repeated on every printed page, A4 print
// setup with planned page breaks, running header and page-numbered footer,
// right-to-left for Arabic. The data, order, columns and permission masking
// are the export's own, unchanged; the totals row stays a live SUBTOTAL so it
// follows the autofilter.
func BuildTripsExport(trips []Models.TripStruct, labels TripExportLabels, showFinancials bool, meta string, asPDF bool) ([]byte, error) {
	l := labels.withDefaults()
	lang := report.English
	if strings.EqualFold(strings.TrimSpace(l.Lang), "ar") {
		lang = report.Arabic
	}

	cols := []report.Column{
		{Header: l.ReceiptNo, Width: 11, Kind: report.Center},
		{Header: l.Date, Width: 10.5, Kind: report.Date},
		{Header: l.Company, Width: 14, Kind: report.Text},
		{Header: l.Terminal, Width: 16, Kind: report.Text},
		{Header: l.DropOffPoint, Width: 18, Kind: report.Text},
		{Header: l.Vehicle, Width: 11, Kind: report.Center},
		{Header: l.Driver, Width: 18, Kind: report.Text},
		{Header: l.TankCapacity, Width: 10, Kind: report.Int, Total: true},
		{Header: l.Distance, Width: 10, Kind: report.Decimal, Total: true},
	}
	if showFinancials {
		cols = append(cols, report.Column{Header: l.Fee, Width: 11, Kind: report.Money, Total: true})
	}
	cols = append(cols,
		report.Column{Header: l.InGarage, Width: 8, Kind: report.Flag},
		report.Column{Header: l.InOffice, Width: 8, Kind: report.Flag},
		report.Column{Header: l.Stamped, Width: 8, Kind: report.Flag},
		report.Column{Header: l.GarageReceivedBy, Width: 14, Kind: report.Text},
		report.Column{Header: l.GarageDate, Width: 14, Kind: report.DateTime},
		report.Column{Header: l.OfficeReceivedBy, Width: 14, Kind: report.Text},
		report.Column{Header: l.OfficeDate, Width: 14, Kind: report.DateTime},
		report.Column{Header: l.Remark, Width: 22, Kind: report.Text},
	)

	// Deterministic order, matching what the client produced so downstream
	// pivots and macros keep working: date desc, then company, then receipt.
	sorted := make([]Models.TripStruct, len(trips))
	copy(sorted, trips)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Date != sorted[j].Date {
			return sorted[i].Date > sorted[j].Date
		}
		if sorted[i].Company != sorted[j].Company {
			return sorted[i].Company < sorted[j].Company
		}
		return sorted[i].ReceiptNo < sorted[j].ReceiptNo
	})

	rows := make([][]any, 0, len(sorted))
	for _, t := range sorted {
		values := []any{
			orDash(t.ReceiptNo),
			parseTripDate(t.Date),
			orDash(t.Company),
			orDash(t.Terminal),
			orDash(t.DropOffPoint),
			t.CarNoPlate,
			t.DriverName,
			t.TankCapacity,
			tripDistance(t),
		}
		if showFinancials {
			values = append(values, t.Fee)
		}
		inGarage, inOffice, stamped := hasStep(t, "Garage"), hasStep(t, "Office"), isStamped(t)
		values = append(values,
			report.YesNo{Text: yesNo(inGarage, l), On: inGarage},
			report.YesNo{Text: yesNo(inOffice, l), On: inOffice},
			report.YesNo{Text: yesNo(stamped, l), On: stamped},
			stepReceivedBy(t, "Garage"),
			stepReceivedAt(t, "Garage"),
			stepReceivedBy(t, "Office"),
			stepReceivedAt(t, "Office"),
			receiptRemark(t),
		)
		rows = append(rows, values)
	}

	stats := computeTripStats(sorted, showFinancials)
	cards := []report.Card{
		{Label: l.TotalTrips, Value: report.FormatCount(int64(stats.trips))},
		{Label: l.TotalVolume, Value: report.FormatCount(int64(math.Round(stats.capacity)))},
		{Label: l.TotalDistance, Value: report.FormatAmount(stats.distance)},
	}
	if showFinancials {
		cards = append(cards, report.Card{Label: l.TotalFees, Value: report.FormatAmount(stats.fees), Dark: true})
	}
	cards = append(cards, report.Card{Label: l.StampedCount, Value: report.FormatCount(int64(stats.stamped))})

	book := report.NewBook()
	defer book.Close()
	sheet := sanitizeSheetName(l.Sheet)
	table := report.Table{
		Lang: lang, Company: "Apex Transport",
		Title: l.Title, Subtitle: l.Subtitle, Period: meta,
		Running: joinDot(l.Title, l.Subtitle),
		Columns: cols, Cards: cards,
		Bands:      []report.Band{{Rows: rows}},
		AutoFilter: true, FreezeHeader: true, Landscape: true,
	}
	if len(rows) > 0 {
		table.TotalLabel, table.TotalMode = l.Totals, report.TotalSubtotal
	}
	if _, err := book.Table(sheet, table); err != nil {
		return nil, err
	}
	if err := writeTripSummarySheet(book, sorted, l, lang, showFinancials, meta); err != nil {
		return nil, err
	}
	if asPDF {
		return book.PDF(l.Title)
	}
	return book.XLSX()
}

type tripStats struct {
	trips, stamped           int
	capacity, distance, fees float64
}

func computeTripStats(trips []Models.TripStruct, showFinancials bool) tripStats {
	var s tripStats
	for _, t := range trips {
		s.trips++
		s.capacity += float64(t.TankCapacity)
		s.distance += tripDistance(t)
		if showFinancials {
			s.fees += t.Fee
		}
		if isStamped(t) {
			s.stamped++
		}
	}
	s.capacity, s.distance, s.fees = round2(s.capacity), round2(s.distance), round2(s.fees)
	return s
}

// writeTripSummarySheet adds a per-company breakdown.
//
// Not present in the TypeScript export. With the row cap gone an export can now
// be tens of thousands of rows, and a sheet that large is unreadable without an
// aggregate to open on.
func writeTripSummarySheet(book *report.Book, trips []Models.TripStruct, l TripExportLabels, lang report.Lang, showFinancials bool, meta string) error {
	type agg struct {
		trips    int
		capacity float64
		distance float64
		fees     float64
	}
	byCompany := map[string]*agg{}
	order := []string{}
	for _, t := range trips {
		key := t.Company
		if strings.TrimSpace(key) == "" {
			key = "—"
		}
		if _, ok := byCompany[key]; !ok {
			byCompany[key] = &agg{}
			order = append(order, key)
		}
		a := byCompany[key]
		a.trips++
		a.capacity += float64(t.TankCapacity)
		a.distance += tripDistance(t)
		a.fees += t.Fee
	}
	sort.Strings(order)

	cols := []report.Column{
		{Header: l.Company, Width: 28, Kind: report.Text},
		{Header: l.TotalTrips, Width: 14, Kind: report.Int, Total: true},
		{Header: l.TotalVolume, Width: 16, Kind: report.Decimal, Total: true},
		{Header: l.TotalDistance, Width: 16, Kind: report.Decimal, Total: true},
	}
	if showFinancials {
		cols = append(cols, report.Column{Header: l.TotalFees, Width: 16, Kind: report.Money, Total: true})
	}
	var rows [][]any
	for _, key := range order {
		a := byCompany[key]
		vals := []any{key, a.trips, round2(a.capacity), round2(a.distance)}
		if showFinancials {
			vals = append(vals, round2(a.fees))
		}
		rows = append(rows, vals)
	}
	table := report.Table{
		Lang: lang, Company: "Apex Transport",
		Title: l.Title, Subtitle: l.Summary, Period: meta,
		Running:      joinDot(l.Title, l.Summary),
		Columns:      cols,
		Bands:        []report.Band{{Rows: rows}},
		FreezeHeader: true,
	}
	if len(rows) > 0 {
		table.TotalLabel = l.Totals
	}
	_, err := book.Table(sanitizeSheetName(l.Summary), table)
	return err
}

func joinDot(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "  ·  ")
}

/* -------------------------------------------------------------------------- */
/* Helpers                                                                     */
/* -------------------------------------------------------------------------- */

func hasStep(t Models.TripStruct, location string) bool {
	for _, s := range t.ReceiptSteps {
		if s.Location == location {
			return true
		}
	}
	return false
}

func isStamped(t Models.TripStruct) bool {
	for _, s := range t.ReceiptSteps {
		if s.Stamped {
			return true
		}
	}
	return false
}

func stepReceivedBy(t Models.TripStruct, location string) string {
	for _, s := range t.ReceiptSteps {
		if s.Location == location {
			return s.ReceivedBy
		}
	}
	return ""
}

func stepReceivedAt(t Models.TripStruct, location string) interface{} {
	for _, s := range t.ReceiptSteps {
		if s.Location == location && !s.ReceivedAt.IsZero() {
			return s.ReceivedAt
		}
	}
	return time.Time{}
}

// receiptRemark flags receipt numbers that fall outside the terminal's serial
// pattern for the trip's company. An explicit override (a knowingly
// out-of-serial receipt, e.g. a replacement book) is labelled as such; an
// unchecked mismatch is called out; trips with no applicable pattern
// (ReceiptValid == nil) get no remark.
func receiptRemark(t Models.TripStruct) string {
	if t.ReceiptOverride {
		return "overridden - out of terminal serialization"
	}
	if t.ReceiptValid != nil && !*t.ReceiptValid {
		return "receipt out of serialization"
	}
	return ""
}

// tripDistance prefers the recorded mileage and falls back to the planned
// distance, matching how the table and the client export read it.
func tripDistance(t Models.TripStruct) float64 {
	if t.Mileage > 0 {
		return t.Mileage
	}
	return t.Distance
}

// parseTripDate turns the stored YYYY-MM-DD text into a real date cell, so
// Excel sorts and filters it as a date rather than a string.
func parseTripDate(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	if d, err := time.Parse("2006-01-02", s); err == nil {
		return d
	}
	return s
}

func yesNo(v bool, l TripExportLabels) string {
	if v {
		return l.Yes
	}
	return l.No
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Excel rejects these in a sheet name and caps it at 31 characters.
func sanitizeSheetName(s string) string {
	replacer := strings.NewReplacer(":", " ", "\\", " ", "/", " ", "?", " ", "*", " ", "[", "(", "]", ")")
	out := strings.TrimSpace(replacer.Replace(s))
	if out == "" {
		out = "Sheet1"
	}
	r := []rune(out)
	if len(r) > 31 {
		out = string(r[:31])
	}
	return out
}
