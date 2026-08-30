package services

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"Falcon/Models"

	"github.com/xuri/excelize/v2"
)

// Oil-change export, server-side.
//
// Replaces a browser-side ExcelJS build, and changes its shape while moving:
// the workbook now opens on a master sheet holding the LATEST change per
// vehicle — the fleet status board, which is the question the sheet is
// normally opened to answer — and carries one further sheet per vehicle with
// that truck's full history, newest first.
//
// The old client build could only ever produce one or the other: the fleet
// screen exported the latest-per-vehicle projection it already had in memory,
// and the history screen exported a single truck. Getting the whole fleet's
// history meant opening every vehicle and exporting it by hand.
//
// Styling is deliberately the trip export's (newTripStyles), because the two
// workbooks land in the same inbox and a second visual language would only
// say "a different person wrote this".

// OilExportLabels carries already-translated headers, as TripExportLabels does
// and for the same reason: the server has no i18n bundle, and a second set of
// English strings here would drift from the ones the screen renders.
type OilExportLabels struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Master   string `json:"master"`

	CarPlate         string `json:"car_plate"`
	Date             string `json:"date"`
	Supervisor       string `json:"supervisor"`
	Driver           string `json:"driver"`
	OdometerAtChange string `json:"odometer_at_change"`
	CurrentOdometer  string `json:"current_odometer"`
	KmUsed           string `json:"km_used"`
	KmRemaining      string `json:"km_remaining"`
	Interval         string `json:"interval"`
	Status           string `json:"status"`
	Cost             string `json:"cost"`
	Filters          string `json:"filters"`
	OilFilter        string `json:"oil_filter"`
	FuelFilter       string `json:"fuel_filter"`
	WaterFilter      string `json:"water_filter"`

	TotalVehicles string `json:"total_vehicles"`
	TotalCost     string `json:"total_cost"`
	StatusGood    string `json:"status_good"`
	StatusWarning string `json:"status_warning"`
	StatusCrit    string `json:"status_critical"`
	Totals        string `json:"totals"`
	Yes           string `json:"yes"`
	No            string `json:"no"`
}

func (l OilExportLabels) withDefaults() OilExportLabels {
	def := func(v, fallback string) string {
		if strings.TrimSpace(v) == "" {
			return fallback
		}
		return v
	}
	l.Title = def(l.Title, "Oil Changes")
	l.Subtitle = def(l.Subtitle, "Fleet oil-change records")
	l.Master = def(l.Master, "Fleet")
	l.CarPlate = def(l.CarPlate, "Vehicle")
	l.Date = def(l.Date, "Date")
	l.Supervisor = def(l.Supervisor, "Supervisor")
	l.Driver = def(l.Driver, "Driver")
	l.OdometerAtChange = def(l.OdometerAtChange, "Odometer at Change")
	l.CurrentOdometer = def(l.CurrentOdometer, "Current Odometer")
	l.KmUsed = def(l.KmUsed, "km Since")
	l.KmRemaining = def(l.KmRemaining, "km Remaining")
	l.Interval = def(l.Interval, "Interval (km)")
	l.Status = def(l.Status, "Status")
	l.Cost = def(l.Cost, "Cost")
	l.Filters = def(l.Filters, "Filters")
	l.OilFilter = def(l.OilFilter, "Oil Filter")
	l.FuelFilter = def(l.FuelFilter, "Fuel Filter")
	l.WaterFilter = def(l.WaterFilter, "Water Separator")
	l.TotalVehicles = def(l.TotalVehicles, "Vehicles")
	l.TotalCost = def(l.TotalCost, "Total Cost")
	l.StatusGood = def(l.StatusGood, "Good")
	l.StatusWarning = def(l.StatusWarning, "Warning")
	l.StatusCrit = def(l.StatusCrit, "Critical")
	l.Totals = def(l.Totals, "TOTALS")
	l.Yes = def(l.Yes, "Yes")
	l.No = def(l.No, "No")
	return l
}

// Kilometres of remaining service life at which a change becomes warning /
// critical. These MUST match OIL_CHANGE_THRESHOLDS in the frontend's
// entities/oil-change/schemas.ts — the screen and the sheet disagreeing about
// which trucks are due is worse than either being wrong.
const (
	oilCriticalKm = 1500.0
	oilWarningKm  = 3000.0
)

func oilStatus(kmRemaining float64, l OilExportLabels) string {
	switch {
	case kmRemaining <= oilCriticalKm:
		return l.StatusCrit
	case kmRemaining <= oilWarningKm:
		return l.StatusWarning
	default:
		return l.StatusGood
	}
}

// kmUsed is clamped at zero: a current odometer below the reading at the change
// is a data-entry error, and a negative "distance driven" would flow into
// kmRemaining and report the truck as freshly serviced.
func oilKmUsed(o Models.OilChange) float64 {
	if d := o.CurrentOdometer - o.OdometerAtChange; d > 0 {
		return d
	}
	return 0
}

// BuildOilChangesExcel renders the master sheet plus one sheet per vehicle.
func BuildOilChangesExcel(records []Models.OilChange, labels OilExportLabels, meta string) ([]byte, error) {
	l := labels.withDefaults()

	f := excelize.NewFile()
	styles, err := newTripStyles(f)
	if err != nil {
		return nil, err
	}

	// Latest per vehicle, by highest ID rather than latest date: two changes
	// can share a date, and IDs are monotonic per insert. Same rule as
	// selectLatestPerCar on the client, so the master sheet and the fleet
	// screen never name a different record as current.
	byPlate := map[string][]Models.OilChange{}
	for _, r := range records {
		byPlate[r.CarNoPlate] = append(byPlate[r.CarNoPlate], r)
	}
	plates := make([]string, 0, len(byPlate))
	for p := range byPlate {
		plates = append(plates, p)
	}
	sort.Strings(plates)

	latest := make([]Models.OilChange, 0, len(plates))
	for _, p := range plates {
		rows := byPlate[p]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Date > rows[j].Date })
		best := rows[0]
		for _, r := range rows {
			if r.ID > best.ID {
				best = r
			}
		}
		latest = append(latest, best)
		byPlate[p] = rows
	}

	master := sanitizeSheetName(l.Master)
	idx, err := f.NewSheet(master)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	if master != "Sheet1" {
		_ = f.DeleteSheet("Sheet1")
	}

	if err := writeOilSheet(f, styles, master, latest, l, l.Title, l.Subtitle, meta, true); err != nil {
		return nil, err
	}

	// One sheet per vehicle, in the master sheet's order so the tab strip reads
	// the same way the first sheet does.
	used := map[string]bool{strings.ToLower(master): true}
	for _, p := range plates {
		name := uniqueSheetName(p, used)
		if _, err := f.NewSheet(name); err != nil {
			return nil, err
		}
		history := byPlate[p]
		sub := fmt.Sprintf("%s — %s", l.Subtitle, p)
		if err := writeOilSheet(f, styles, name, history, l, p, sub, meta, false); err != nil {
			return nil, err
		}
	}

	f.SetActiveSheet(idx)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// uniqueSheetName keeps Excel happy: 31 characters, no reserved punctuation,
// and no duplicate names. Two plates can sanitize to the same string once
// they're truncated, so collisions get a numeric suffix rather than silently
// overwriting a truck's history.
func uniqueSheetName(plate string, used map[string]bool) string {
	base := sanitizeSheetName(plate)
	name := base
	for i := 2; used[strings.ToLower(name)]; i++ {
		suffix := " (" + strconv.Itoa(i) + ")"
		r := []rune(base)
		limit := 31 - len([]rune(suffix))
		if len(r) > limit {
			name = string(r[:limit]) + suffix
		} else {
			name = base + suffix
		}
	}
	used[strings.ToLower(name)] = true
	return name
}

// writeOilSheet lays out one sheet: banner, stat pills, frozen header, rows,
// totals. `withVehicle` drops the plate column on the per-vehicle sheets,
// where every row carries the same value and the sheet is already named for it.
func writeOilSheet(
	f *excelize.File,
	styles *tripStyles,
	sheet string,
	rows []Models.OilChange,
	l OilExportLabels,
	title, subtitle, meta string,
	withVehicle bool,
) error {
	cols := []tripColumn{}
	if withVehicle {
		cols = append(cols, tripColumn{l.CarPlate, 16, "text", false})
	}
	cols = append(cols,
		tripColumn{l.Date, 13, "date", false},
		tripColumn{l.Supervisor, 18, "text", false},
		tripColumn{l.Driver, 26, "text", false},
		tripColumn{l.OdometerAtChange, 18, "int", false},
		tripColumn{l.CurrentOdometer, 18, "int", false},
		tripColumn{l.KmUsed, 13, "int", false},
		tripColumn{l.KmRemaining, 15, "int", false},
		tripColumn{l.Interval, 14, "int", false},
		tripColumn{l.Status, 12, "text", false},
		tripColumn{l.OilFilter, 12, "flag", false},
		tripColumn{l.FuelFilter, 12, "flag", false},
		tripColumn{l.WaterFilter, 16, "flag", false},
		tripColumn{l.Cost, 14, "money", true},
	)

	lastCol, _ := excelize.ColumnNumberToName(len(cols))

	_ = f.MergeCell(sheet, "A1", fmt.Sprintf("%s1", lastCol))
	_ = f.MergeCell(sheet, "A2", fmt.Sprintf("%s2", lastCol))
	_ = f.MergeCell(sheet, "A3", fmt.Sprintf("%s3", lastCol))
	_ = f.SetCellValue(sheet, "A1", title)
	_ = f.SetCellValue(sheet, "A2", subtitle)
	_ = f.SetCellValue(sheet, "A3", meta)
	_ = f.SetCellStyle(sheet, "A1", fmt.Sprintf("%s1", lastCol), styles.title)
	_ = f.SetCellStyle(sheet, "A2", fmt.Sprintf("%s2", lastCol), styles.subtitle)
	_ = f.SetCellStyle(sheet, "A3", fmt.Sprintf("%s3", lastCol), styles.meta)
	_ = f.SetRowHeight(sheet, 1, 38)
	_ = f.SetRowHeight(sheet, 2, 22)
	_ = f.SetRowHeight(sheet, 3, 20)
	_ = f.SetRowHeight(sheet, 4, 8)

	stats := computeOilStats(rows, l)
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

	const headerRow = 7
	for i, c := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, headerRow)
		_ = f.SetCellValue(sheet, cell, c.header)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, colName, colName, c.width)
	}
	_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", headerRow), fmt.Sprintf("%s%d", lastCol, headerRow), styles.header)
	_ = f.SetRowHeight(sheet, headerRow, 34)

	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1),
		ActivePane:  "bottomLeft",
	})
	_ = f.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", headerRow, lastCol, headerRow), nil)

	row := headerRow + 1
	for _, o := range rows {
		striped := (row-headerRow)%2 == 0
		used := oilKmUsed(o)
		remaining := o.Mileage - used

		values := []interface{}{}
		if withVehicle {
			values = append(values, o.CarNoPlate)
		}
		values = append(values,
			parseTripDate(o.Date),
			orDash(o.SuperVisor),
			orDash(o.DriverName),
			o.OdometerAtChange,
			o.CurrentOdometer,
			used,
			remaining,
			o.Mileage,
			oilStatus(remaining, l),
			boolLabel(o.OilFilterChanged, l),
			boolLabel(o.FuelFilterChanged, l),
			boolLabel(o.WaterFilterChanged, l),
			o.Cost,
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

		// The three filter columns are scanned, not read, so they take the same
		// on/off colouring the receipt flags get in the trip export. They sit
		// immediately before the cost column.
		flagStart := len(values) - 4
		for offset, on := range []bool{o.OilFilterChanged, o.FuelFilterChanged, o.WaterFilterChanged} {
			cell, _ := excelize.CoordinatesToCellName(flagStart+offset+1, row)
			_ = f.SetCellStyle(sheet, cell, cell, styles.flag(on, striped))
		}

		_ = f.SetRowHeight(sheet, row, 20)
		row++
	}

	if len(rows) > 0 {
		totalRow := row
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), l.Totals)
		for i, c := range cols {
			cell, _ := excelize.CoordinatesToCellName(i+1, totalRow)
			if c.total {
				colName, _ := excelize.ColumnNumberToName(i + 1)
				_ = f.SetCellFormula(sheet, cell,
					fmt.Sprintf("SUBTOTAL(109,%s%d:%s%d)", colName, headerRow+1, colName, totalRow-1))
			} else if i > 0 {
				_ = f.SetCellValue(sheet, cell, "")
			}
			_ = f.SetCellStyle(sheet, cell, cell, styles.totalFor(c.kind))
		}
		_ = f.SetRowHeight(sheet, totalRow, 28)
	}

	return nil
}

func boolLabel(v bool, l OilExportLabels) string {
	if v {
		return l.Yes
	}
	return l.No
}

func computeOilStats(rows []Models.OilChange, l OilExportLabels) []tripStat {
	var cost float64
	good, warn, crit := 0, 0, 0
	plates := map[string]bool{}
	for _, o := range rows {
		cost += o.Cost
		plates[o.CarNoPlate] = true
		switch remaining := o.Mileage - oilKmUsed(o); {
		case remaining <= oilCriticalKm:
			crit++
		case remaining <= oilWarningKm:
			warn++
		default:
			good++
		}
	}
	return []tripStat{
		{l.TotalVehicles, len(plates), false},
		{l.StatusGood, good, false},
		{l.StatusWarning, warn, false},
		{l.StatusCrit, crit, false},
		{l.TotalCost, cost, true},
	}
}

// OilExportMeta is the generated-at line under the banner.
func OilExportMeta(count, vehicles int) string {
	return fmt.Sprintf("%s · %d records · %d vehicles",
		time.Now().Format("2 Jan 2006 15:04"), count, vehicles)
}
