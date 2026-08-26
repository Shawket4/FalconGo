package services

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"Falcon/Models"

	"github.com/xuri/excelize/v2"
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
// The output is intended to match or improve on the TypeScript version, not to
// be a plainer fallback: same branded banner, stat pills, frozen header,
// zebra striping, per-cell colouring for the receipt flags, and a SUM totals
// row — plus an autofilter and a second summary sheet the old one lacked.

// TripExportLabels carries the already-translated column headers and titles.
//
// The server has no i18n bundle, and inventing a second set of English strings
// would let the two exports drift. The client sends what it renders.
type TripExportLabels struct {
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

type tripColumn struct {
	header string
	width  float64
	// kind drives the number format and the totals row: text, int, num, money,
	// date, datetime, flag.
	kind  string
	total bool
}

// BuildTripsExcel renders the workbook and returns it as bytes.
//
// `showFinancials` mirrors the permission masking used elsewhere: below
// permission 3 the fee column is omitted entirely rather than zeroed, so the
// spreadsheet does not imply every trip was free.
func BuildTripsExcel(trips []Models.TripStruct, labels TripExportLabels, showFinancials bool, meta string) ([]byte, error) {
	l := labels.withDefaults()

	cols := []tripColumn{
		{l.ReceiptNo, 14, "text", false},
		{l.Date, 13, "date", false},
		{l.Company, 22, "text", false},
		{l.Terminal, 22, "text", false},
		{l.DropOffPoint, 24, "text", false},
		{l.Vehicle, 14, "text", false},
		{l.Driver, 22, "text", false},
		{l.TankCapacity, 14, "int", true},
		{l.Distance, 13, "num", true},
	}
	if showFinancials {
		cols = append(cols, tripColumn{l.Fee, 14, "money", true})
	}
	cols = append(cols,
		tripColumn{l.InGarage, 11, "flag", false},
		tripColumn{l.InOffice, 11, "flag", false},
		tripColumn{l.Stamped, 11, "flag", false},
		tripColumn{l.GarageReceivedBy, 18, "text", false},
		tripColumn{l.GarageDate, 17, "datetime", false},
		tripColumn{l.OfficeReceivedBy, 18, "text", false},
		tripColumn{l.OfficeDate, 17, "datetime", false},
		tripColumn{l.Remark, 34, "text", false},
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

	f := excelize.NewFile()
	sheet := sanitizeSheetName(l.Sheet)
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	if sheet != "Sheet1" {
		_ = f.DeleteSheet("Sheet1")
	}

	styles, err := newTripStyles(f)
	if err != nil {
		return nil, err
	}

	lastCol, _ := excelize.ColumnNumberToName(len(cols))

	// ── Banner (rows 1-3) ────────────────────────────────────────────────────
	_ = f.MergeCell(sheet, "A1", fmt.Sprintf("%s1", lastCol))
	_ = f.MergeCell(sheet, "A2", fmt.Sprintf("%s2", lastCol))
	_ = f.MergeCell(sheet, "A3", fmt.Sprintf("%s3", lastCol))
	_ = f.SetCellValue(sheet, "A1", l.Title)
	_ = f.SetCellValue(sheet, "A2", l.Subtitle)
	_ = f.SetCellValue(sheet, "A3", meta)
	_ = f.SetCellStyle(sheet, "A1", fmt.Sprintf("%s1", lastCol), styles.title)
	_ = f.SetCellStyle(sheet, "A2", fmt.Sprintf("%s2", lastCol), styles.subtitle)
	_ = f.SetCellStyle(sheet, "A3", fmt.Sprintf("%s3", lastCol), styles.meta)
	_ = f.SetRowHeight(sheet, 1, 38)
	_ = f.SetRowHeight(sheet, 2, 22)
	_ = f.SetRowHeight(sheet, 3, 20)
	_ = f.SetRowHeight(sheet, 4, 8)

	// ── Stat pills (rows 5-6) ────────────────────────────────────────────────
	stats := computeTripStats(sorted, l, showFinancials)
	perStat := len(cols) / max(1, len(stats))
	if perStat < 1 {
		perStat = 1
	}
	for i, s := range stats {
		start := i*perStat + 1
		if start > len(cols) {
			break
		}
		end := start + perStat - 1
		if end > len(cols) {
			end = len(cols)
		}
		startCol, _ := excelize.ColumnNumberToName(start)
		endCol, _ := excelize.ColumnNumberToName(end)

		_ = f.MergeCell(sheet, fmt.Sprintf("%s5", startCol), fmt.Sprintf("%s5", endCol))
		_ = f.MergeCell(sheet, fmt.Sprintf("%s6", startCol), fmt.Sprintf("%s6", endCol))
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s5", startCol), s.label)
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s6", startCol), s.value)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s5", startCol), fmt.Sprintf("%s5", endCol), styles.statLabel)

		style := styles.statValue
		if s.money {
			style = styles.statValueMoney
		}
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s6", startCol), fmt.Sprintf("%s6", endCol), style)
	}
	_ = f.SetRowHeight(sheet, 5, 18)
	_ = f.SetRowHeight(sheet, 6, 30)

	// ── Header (row 7) ───────────────────────────────────────────────────────
	const headerRow = 7
	for i, c := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, headerRow)
		_ = f.SetCellValue(sheet, cell, c.header)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, colName, colName, c.width)
	}
	_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", headerRow), fmt.Sprintf("%s%d", lastCol, headerRow), styles.header)
	_ = f.SetRowHeight(sheet, headerRow, 34)

	// Freeze everything above the first data row so the header and the KPIs stay
	// visible while scrolling thousands of rows -- the whole point of moving the
	// row cap.
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1),
		ActivePane:  "bottomLeft",
	})
	_ = f.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", headerRow, lastCol, headerRow), nil)

	// ── Data rows ────────────────────────────────────────────────────────────
	row := headerRow + 1
	for _, t := range trips2(sorted) {
		striped := (row-headerRow)%2 == 0

		values := []interface{}{
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
		inGarage := hasStep(t, "Garage")
		inOffice := hasStep(t, "Office")
		stamped := isStamped(t)
		values = append(values,
			yesNo(inGarage, l), yesNo(inOffice, l), yesNo(stamped, l),
			stepReceivedBy(t, "Garage"),
			stepReceivedAt(t, "Garage"),
			stepReceivedBy(t, "Office"),
			stepReceivedAt(t, "Office"),
			receiptRemark(t),
		)

		for i, v := range values {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			if v == nil {
				_ = f.SetCellValue(sheet, cell, "")
			} else {
				_ = f.SetCellValue(sheet, cell, v)
			}
			_ = f.SetCellStyle(sheet, cell, cell, styles.cell(cols[i].kind, striped))
		}

		// Receipt flags are colour-coded green/muted, as in the client build --
		// the column is scanned, not read. Counted from the end: 3 flags +
		// 4 receipt-step columns + the remark column.
		flagStart := len(values) - 8
		for offset, on := range []bool{inGarage, inOffice, stamped} {
			cell, _ := excelize.CoordinatesToCellName(flagStart+offset+1, row)
			_ = f.SetCellStyle(sheet, cell, cell, styles.flag(on, striped))
		}

		_ = f.SetRowHeight(sheet, row, 20)
		row++
	}

	// ── Totals row ───────────────────────────────────────────────────────────
	if len(sorted) > 0 {
		totalRow := row
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), l.Totals)
		for i, c := range cols {
			cell, _ := excelize.CoordinatesToCellName(i+1, totalRow)
			if c.total {
				colName, _ := excelize.ColumnNumberToName(i + 1)
				// A live SUM rather than a precomputed number, so the figure
				// still agrees with the rows after someone filters or deletes.
				_ = f.SetCellFormula(sheet, cell,
					fmt.Sprintf("SUBTOTAL(109,%s%d:%s%d)", colName, headerRow+1, colName, totalRow-1))
			} else if i > 0 {
				_ = f.SetCellValue(sheet, cell, "")
			}
			_ = f.SetCellStyle(sheet, cell, cell, styles.totalFor(c.kind))
		}
		_ = f.SetRowHeight(sheet, totalRow, 28)
	}

	if err := writeTripSummarySheet(f, sorted, l, showFinancials); err != nil {
		return nil, err
	}

	f.SetActiveSheet(idx)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// trips2 exists only to keep the data loop readable above.
func trips2(t []Models.TripStruct) []Models.TripStruct { return t }

type tripStat struct {
	label string
	value interface{}
	money bool
}

func computeTripStats(trips []Models.TripStruct, l TripExportLabels, showFinancials bool) []tripStat {
	var capacity, distance, fees float64
	stamped := 0
	for _, t := range trips {
		capacity += float64(t.TankCapacity)
		distance += tripDistance(t)
		fees += t.Fee
		if isStamped(t) {
			stamped++
		}
	}

	stats := []tripStat{
		{l.TotalTrips, len(trips), false},
		{l.TotalVolume, round2(capacity), false},
		{l.TotalDistance, round2(distance), false},
	}
	if showFinancials {
		stats = append(stats, tripStat{l.TotalFees, round2(fees), true})
	}
	return append(stats, tripStat{l.StampedCount, stamped, false})
}

// writeTripSummarySheet adds a per-company breakdown.
//
// Not present in the TypeScript export. With the row cap gone an export can now
// be tens of thousands of rows, and a sheet that large is unreadable without an
// aggregate to open on.
func writeTripSummarySheet(f *excelize.File, trips []Models.TripStruct, l TripExportLabels, showFinancials bool) error {
	name := sanitizeSheetName(l.Summary)
	if _, err := f.NewSheet(name); err != nil {
		return err
	}

	styles, err := newTripStyles(f)
	if err != nil {
		return err
	}

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

	headers := []string{l.Company, l.TotalTrips, l.TotalVolume, l.TotalDistance}
	if showFinancials {
		headers = append(headers, l.TotalFees)
	}
	widths := []float64{28, 14, 16, 16, 16}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(name, cell, h)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(name, colName, colName, widths[i])
	}
	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	_ = f.SetCellStyle(name, "A1", fmt.Sprintf("%s1", lastCol), styles.header)
	_ = f.SetRowHeight(name, 1, 34)

	row := 2
	for _, key := range order {
		a := byCompany[key]
		striped := row%2 == 0
		vals := []interface{}{key, a.trips, round2(a.capacity), round2(a.distance)}
		kinds := []string{"text", "int", "num", "num"}
		if showFinancials {
			vals = append(vals, round2(a.fees))
			kinds = append(kinds, "money")
		}
		for i, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			_ = f.SetCellValue(name, cell, v)
			_ = f.SetCellStyle(name, cell, cell, styles.cell(kinds[i], striped))
		}
		_ = f.SetRowHeight(name, row, 20)
		row++
	}

	_ = f.SetPanes(name, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
	return nil
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
	return nil
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
