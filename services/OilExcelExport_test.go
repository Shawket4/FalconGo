package services

import (
	"bytes"
	"testing"

	"Falcon/Models"

	"github.com/xuri/excelize/v2"
)

func oilRecord(id uint, plate, date string, atChange, current, interval float64) Models.OilChange {
	o := Models.OilChange{
		CarNoPlate:       plate,
		Date:             date,
		Mileage:          interval,
		OdometerAtChange: atChange,
		CurrentOdometer:  current,
		Cost:             5000,
	}
	o.ID = id
	return o
}

func TestOilExportOpensOnTheMasterSheetThenOnePerVehicle(t *testing.T) {
	records := []Models.OilChange{
		oilRecord(1, "AA-1", "2026-01-01", 1000, 2000, 8000),
		oilRecord(2, "AA-1", "2026-06-01", 9000, 10000, 8000),
		oilRecord(3, "BB-2", "2026-05-01", 500, 900, 8000),
	}

	data, err := BuildOilChangesExcel(records, OilExportLabels{}, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) != 3 {
		t.Fatalf("want a master sheet plus one per vehicle, got %v", sheets)
	}
	if sheets[0] != "Fleet" {
		t.Errorf("workbook should open on the master sheet, got %q", sheets[0])
	}

	// The master sheet holds one row per vehicle, and for AA-1 that must be the
	// higher ID -- not the row that merely sorts first.
	rows, err := f.GetRows("Fleet")
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	const headerRow = 7
	body := rows[headerRow:]
	// Two vehicles plus the totals row.
	if len(body) != 3 {
		t.Fatalf("want 2 vehicle rows + totals, got %d: %v", len(body), body)
	}
	// AA-1's current record is the June one, whose odometer at change is 9000.
	// GetRows returns the formatted cell, so the thousands separator is there.
	if body[0][4] != "9,000" {
		t.Errorf("master sheet should carry the latest change per vehicle, got %v", body[0])
	}
}

func TestOilExportHistorySheetKeepsEveryRecordForTheVehicle(t *testing.T) {
	records := []Models.OilChange{
		oilRecord(1, "AA-1", "2026-01-01", 1000, 2000, 8000),
		oilRecord(2, "AA-1", "2026-06-01", 9000, 10000, 8000),
	}
	data, err := BuildOilChangesExcel(records, OilExportLabels{}, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	defer f.Close()

	rows, err := f.GetRows("AA-1")
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	// Both changes plus the totals row; the plate column is dropped here
	// because the sheet is already named for it.
	if got := len(rows) - 7; got != 3 {
		t.Fatalf("want both records + totals on the vehicle sheet, got %d", got)
	}
}

func TestOilExportEmptyStillBuilds(t *testing.T) {
	if _, err := BuildOilChangesExcel(nil, OilExportLabels{}, "meta"); err != nil {
		t.Fatalf("an empty export should still produce a workbook: %v", err)
	}
}

// The two colour-coded cells are addressed by index, so this pins which
// columns they are: paint the wrong one and the sheet says a healthy truck is
// overdue, with nothing to indicate anything went wrong.
func TestOilExportColoursKmRemainingAndStatus(t *testing.T) {
	records := []Models.OilChange{
		// 7,500 km into an 8,000 km interval: 500 left, critical.
		oilRecord(1, "AA-1", "2026-01-01", 100000, 107500, 8000),
		// 1,000 km in: 7,000 left, good.
		oilRecord(2, "BB-2", "2026-01-01", 200000, 201000, 8000),
	}
	data, err := BuildOilChangesExcel(records, OilExportLabels{}, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	defer f.Close()

	// Master sheet carries the vehicle column, so km remaining is H and the
	// status word is J. Rows start at 8.
	rows, _ := f.GetRows("Fleet")
	if got := rows[7][oilColKmRemaining+1]; got != "500" {
		t.Fatalf("H8 should be the critical truck's km remaining, got %q", got)
	}
	if got := rows[8][oilColKmRemaining+1]; got != "7,000" {
		t.Fatalf("H9 should be the healthy truck's km remaining, got %q", got)
	}

	crit, err := f.GetCellStyle("Fleet", "H8")
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	good, _ := f.GetCellStyle("Fleet", "H9")
	if crit == good {
		t.Error("a critical and a healthy km-remaining cell must not share a style")
	}

	critStatus, _ := f.GetCellStyle("Fleet", "J8")
	goodStatus, _ := f.GetCellStyle("Fleet", "J9")
	if critStatus == goodStatus {
		t.Error("the status column should be colour-coded alongside km remaining")
	}
	// And the colour must follow the severity, not the row: the status cell and
	// the number beside it are the same fact.
	if crit == goodStatus || good == critStatus {
		t.Error("severity styles look transposed between the two rows")
	}
}
