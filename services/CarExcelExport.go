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

// Cars export, server-side.
//
// The screen shows three dated papers per vehicle and colours each by how long
// it has left; this puts the same thing in a workbook, sorted so whatever
// lapses first is at the top. Styling reuses the trip export's palette, as the
// oil-change export does — the three land in the same inbox.
//
// The expiry window is NOT defined here. It arrives with the request, because
// the frontend owns that rule (DOCUMENT_EXPIRY_WARNING_DAYS in
// entities/car/expiry.ts) and a second copy in Go would be one more thing to
// keep in step with the screen and with apex-rust.

type CarExportLabels struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Sheet    string `json:"sheet"`

	Plate       string `json:"plate"`
	CarType     string `json:"car_type"`
	Driver      string `json:"driver"`
	Transporter string `json:"transporter"`
	Capacity    string `json:"capacity"`
	License     string `json:"license"`
	Calibration string `json:"calibration"`
	TankLicense string `json:"tank_license"`
	DaysLeft    string `json:"days_left"`
	Status      string `json:"status"`

	StatusValid    string `json:"status_valid"`
	StatusExpiring string `json:"status_expiring"`
	StatusExpired  string `json:"status_expired"`
	StatusMissing  string `json:"status_missing"`

	TotalCars     string `json:"total_cars"`
	TotalAssigned string `json:"total_assigned"`
	Totals        string `json:"totals"`
	NoDriver      string `json:"no_driver"`
}

func (l CarExportLabels) withDefaults() CarExportLabels {
	def := func(v, fallback string) string {
		if strings.TrimSpace(v) == "" {
			return fallback
		}
		return v
	}
	l.Title = def(l.Title, "Cars")
	l.Subtitle = def(l.Subtitle, "Fleet vehicles and documents")
	l.Sheet = def(l.Sheet, "Cars")
	l.Plate = def(l.Plate, "Plate")
	l.CarType = def(l.CarType, "Type")
	l.Driver = def(l.Driver, "Driver")
	l.Transporter = def(l.Transporter, "Transporter")
	l.Capacity = def(l.Capacity, "Capacity (L)")
	l.License = def(l.License, "Licence")
	l.Calibration = def(l.Calibration, "Calibration")
	l.TankLicense = def(l.TankLicense, "Tank licence")
	l.DaysLeft = def(l.DaysLeft, "Days left")
	l.Status = def(l.Status, "Status")
	l.StatusValid = def(l.StatusValid, "Valid")
	l.StatusExpiring = def(l.StatusExpiring, "Expiring soon")
	l.StatusExpired = def(l.StatusExpired, "Expired")
	l.StatusMissing = def(l.StatusMissing, "Not recorded")
	l.TotalCars = def(l.TotalCars, "Vehicles")
	l.TotalAssigned = def(l.TotalAssigned, "With a driver")
	l.Totals = def(l.Totals, "TOTALS")
	l.NoDriver = def(l.NoDriver, "No driver")
	return l
}

// Document states, ordered so the worst wins when a vehicle's papers are
// reduced to one status. `missing` ranks below `expiring`: an unrecorded date
// is a gap in the paperwork, not a lapsed licence, and calling it expired puts
// trucks on an overdue list nobody can act on. Mirrors entities/car/expiry.ts.
const (
	docValid = iota
	docMissing
	docExpiring
	docExpired
)

// docDaysLeft parses one of the TEXT date columns. Legacy rows carry an empty
// string, and some carry text that was never a date, so a failed parse is a
// normal outcome rather than an error worth surfacing.
func docDaysLeft(value string, today time.Time) (int, bool) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, false
	}
	var parsed time.Time
	var err error
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		parsed, err = time.Parse(layout, v)
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, false
	}
	// Whole days between dates, not instants: a licence expiring today is not
	// half-expired by lunchtime.
	a := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b).Hours() / 24), true
}

func docState(value string, horizonDays int, today time.Time) int {
	days, ok := docDaysLeft(value, today)
	if !ok {
		return docMissing
	}
	if days < 0 {
		return docExpired
	}
	if days <= horizonDays {
		return docExpiring
	}
	return docValid
}

// Only tankers carry a tank licence, so a van without one is not missing a
// paper — it was never required to have one.
func tankLicenseApplies(c Models.Car) bool {
	return c.CarType == "Trailer" || c.CarType == "Truck"
}

func carWorstState(c Models.Car, horizonDays int, today time.Time) int {
	worst := docState(c.LicenseExpirationDate, horizonDays, today)
	if s := docState(c.CalibrationExpirationDate, horizonDays, today); s > worst {
		worst = s
	}
	if tankLicenseApplies(c) {
		if s := docState(c.TankLicenseExpirationDate, horizonDays, today); s > worst {
			worst = s
		}
	}
	return worst
}

func carStatusLabel(state int, l CarExportLabels) string {
	switch state {
	case docExpired:
		return l.StatusExpired
	case docExpiring:
		return l.StatusExpiring
	case docMissing:
		return l.StatusMissing
	default:
		return l.StatusValid
	}
}

// BuildCarsExcel renders the fleet sheet.
func BuildCarsExcel(cars []Models.Car, labels CarExportLabels, horizonDays int, meta string) ([]byte, error) {
	l := labels.withDefaults()
	today := time.Now()

	f := excelize.NewFile()
	styles, err := newTripStyles(f)
	if err != nil {
		return nil, err
	}
	severity, err := newOilSeverityStyles(f)
	if err != nil {
		return nil, err
	}

	sheet := sanitizeSheetName(l.Sheet)
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	if sheet != "Sheet1" {
		_ = f.DeleteSheet("Sheet1")
	}

	// Worst paperwork first, then soonest deadline: the sheet is opened to find
	// what needs renewing, so it should not have to be sorted before it is read.
	sorted := make([]Models.Car, len(cars))
	copy(sorted, cars)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := carWorstState(sorted[i], horizonDays, today), carWorstState(sorted[j], horizonDays, today)
		if si != sj {
			return si > sj
		}
		di, oki := soonestDeadline(sorted[i], today)
		dj, okj := soonestDeadline(sorted[j], today)
		if oki != okj {
			return oki
		}
		if di != dj {
			return di < dj
		}
		return sorted[i].CarNoPlate < sorted[j].CarNoPlate
	})

	cols := []tripColumn{
		{l.Plate, 16, "text", false},
		{l.CarType, 12, "text", false},
		{l.Driver, 26, "text", false},
		{l.Transporter, 16, "text", false},
		{l.Capacity, 14, "int", true},
		{l.License, 14, "date", false},
		{l.DaysLeft, 11, "int", false},
		{l.Calibration, 14, "date", false},
		{l.DaysLeft, 11, "int", false},
		{l.TankLicense, 14, "date", false},
		{l.DaysLeft, 11, "int", false},
		{l.Status, 14, "text", false},
	}
	lastCol, _ := excelize.ColumnNumberToName(len(cols))

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

	stats := computeCarStats(sorted, l, horizonDays, today)
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
		sc, _ := excelize.ColumnNumberToName(start)
		ec, _ := excelize.ColumnNumberToName(end)
		_ = f.MergeCell(sheet, fmt.Sprintf("%s5", sc), fmt.Sprintf("%s5", ec))
		_ = f.MergeCell(sheet, fmt.Sprintf("%s6", sc), fmt.Sprintf("%s6", ec))
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s5", sc), s.label)
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s6", sc), s.value)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s5", sc), fmt.Sprintf("%s5", ec), styles.statLabel)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s6", sc), fmt.Sprintf("%s6", ec), styles.statValue)
	}
	_ = f.SetRowHeight(sheet, 5, 18)
	_ = f.SetRowHeight(sheet, 6, 30)

	const headerRow = 7
	for i, c := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, headerRow)
		_ = f.SetCellValue(sheet, cell, c.header)
		name, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, name, name, c.width)
	}
	_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", headerRow), fmt.Sprintf("%s%d", lastCol, headerRow), styles.header)
	_ = f.SetRowHeight(sheet, headerRow, 34)
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1), ActivePane: "bottomLeft",
	})
	_ = f.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", headerRow, lastCol, headerRow), nil)

	row := headerRow + 1
	for _, c := range sorted {
		striped := (row-headerRow)%2 == 0
		si := 0
		if striped {
			si = 1
		}

		driver := l.NoDriver
		if c.Driver != nil && strings.TrimSpace(c.Driver.Name) != "" {
			driver = c.Driver.Name
		}

		type docCell struct {
			value   string
			applies bool
		}
		docs := []docCell{
			{c.LicenseExpirationDate, true},
			{c.CalibrationExpirationDate, true},
			{c.TankLicenseExpirationDate, tankLicenseApplies(c)},
		}

		values := []interface{}{
			c.CarNoPlate, orDash(c.CarType), driver, orDash(c.Transporter), c.TankCapacity,
		}
		for _, d := range docs {
			if !d.applies {
				values = append(values, "", "")
				continue
			}
			days, ok := docDaysLeft(d.value, today)
			if !ok {
				values = append(values, "", "")
				continue
			}
			values = append(values, parseTripDate(d.value), days)
		}
		worst := carWorstState(c, horizonDays, today)
		values = append(values, carStatusLabel(worst, l))

		for i, v := range values {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			_ = f.SetCellValue(sheet, cell, v)
			_ = f.SetCellStyle(sheet, cell, cell, styles.cell(cols[i].kind, striped))
		}

		// Each date/days pair takes the colour of its own state, so a row with
		// one lapsed paper does not read as though all three had gone.
		for i, d := range docs {
			if !d.applies {
				continue
			}
			state := docState(d.value, horizonDays, today)
			if state == docMissing || state == docValid {
				continue
			}
			sev := 1
			if state == docExpired {
				sev = 2
			}
			dateCol := 6 + i*2
			for _, col := range []int{dateCol, dateCol + 1} {
				cell, _ := excelize.CoordinatesToCellName(col, row)
				_ = f.SetCellStyle(sheet, cell, cell, severity.number[sev][si])
			}
		}
		statusCell, _ := excelize.CoordinatesToCellName(len(cols), row)
		switch worst {
		case docExpired:
			_ = f.SetCellStyle(sheet, statusCell, statusCell, severity.text[2][si])
		case docExpiring:
			_ = f.SetCellStyle(sheet, statusCell, statusCell, severity.text[1][si])
		}

		_ = f.SetRowHeight(sheet, row, 20)
		row++
	}

	if len(sorted) > 0 {
		totalRow := row
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), l.Totals)
		for i, c := range cols {
			cell, _ := excelize.CoordinatesToCellName(i+1, totalRow)
			if c.total {
				name, _ := excelize.ColumnNumberToName(i + 1)
				_ = f.SetCellFormula(sheet, cell,
					fmt.Sprintf("SUBTOTAL(109,%s%d:%s%d)", name, headerRow+1, name, totalRow-1))
			} else if i > 0 {
				_ = f.SetCellValue(sheet, cell, "")
			}
			_ = f.SetCellStyle(sheet, cell, cell, styles.totalFor(c.kind))
		}
		_ = f.SetRowHeight(sheet, totalRow, 28)
	}

	f.SetActiveSheet(idx)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// soonestDeadline is the nearest days-left across a vehicle's applicable
// papers; the bool is false when it has no readable date at all.
func soonestDeadline(c Models.Car, today time.Time) (int, bool) {
	best, found := 0, false
	consider := func(v string) {
		if d, ok := docDaysLeft(v, today); ok && (!found || d < best) {
			best, found = d, true
		}
	}
	consider(c.LicenseExpirationDate)
	consider(c.CalibrationExpirationDate)
	if tankLicenseApplies(c) {
		consider(c.TankLicenseExpirationDate)
	}
	return best, found
}

func computeCarStats(cars []Models.Car, l CarExportLabels, horizonDays int, today time.Time) []tripStat {
	assigned, expiring, expired := 0, 0, 0
	for _, c := range cars {
		if c.DriverID != nil {
			assigned++
		}
		switch carWorstState(c, horizonDays, today) {
		case docExpired:
			expired++
		case docExpiring:
			expiring++
		}
	}
	return []tripStat{
		{l.TotalCars, len(cars), false},
		{l.TotalAssigned, assigned, false},
		{l.StatusExpiring, expiring, false},
		{l.StatusExpired, expired, false},
	}
}
