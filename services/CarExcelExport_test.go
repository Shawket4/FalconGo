package services

import (
	"bytes"
	"testing"
	"time"

	"Falcon/Models"

	"github.com/xuri/excelize/v2"
)

func car(plate, carType string, license, calibration, tank string) Models.Car {
	return Models.Car{
		CarNoPlate:                plate,
		CarType:                   carType,
		TankCapacity:              50000,
		LicenseExpirationDate:     license,
		CalibrationExpirationDate: calibration,
		TankLicenseExpirationDate: tank,
	}
}

func inDays(n int) string {
	return time.Now().AddDate(0, 0, n).Format("2006-01-02")
}

// The window arrives with the request. The same fleet exported at 30 days and
// at 90 must not report the same vehicles as expiring, or the parameter is
// decorative and the sheet has quietly kept its own rule.
func TestCarsExportObeysTheHorizonItIsGiven(t *testing.T) {
	cars := []Models.Car{
		car("AA-1", "Trailer", inDays(45), inDays(500), inDays(500)),
	}

	at30, err := BuildCarsExcel(cars, CarExportLabels{}, 30, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	at90, err := BuildCarsExcel(cars, CarExportLabels{}, 90, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	status := func(data []byte) string {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer f.Close()
		rows, _ := f.GetRows("Cars")
		return rows[7][len(rows[7])-1]
	}

	if got := status(at30); got != "Valid" {
		t.Errorf("45 days out is beyond a 30-day window, want Valid, got %q", got)
	}
	if got := status(at90); got != "Expiring soon" {
		t.Errorf("45 days out is inside a 90-day window, want Expiring soon, got %q", got)
	}
}

// A lapsed paper is inside every window, and an unrecorded one is a gap in the
// paperwork rather than a lapsed licence -- it must not be reported as expired.
func TestCarsExportSeparatesLapsedFromUnrecorded(t *testing.T) {
	cars := []Models.Car{
		car("LAPSED", "Trailer", inDays(-5), inDays(500), inDays(500)),
		car("BLANK", "Trailer", "", inDays(500), inDays(500)),
	}
	data, err := BuildCarsExcel(cars, CarExportLabels{}, 30, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	defer f.Close()
	rows, _ := f.GetRows("Cars")

	// Worst paperwork sorts first, so the lapsed vehicle leads.
	if rows[7][0] != "LAPSED" {
		t.Fatalf("expected the lapsed vehicle first, got %q", rows[7][0])
	}
	if got := rows[7][len(rows[7])-1]; got != "Expired" {
		t.Errorf("want Expired, got %q", got)
	}
	if got := rows[8][len(rows[8])-1]; got != "Not recorded" {
		t.Errorf("a blank date is not a lapsed licence, want \"Not recorded\", got %q", got)
	}
}

// A van has no tank licence to be missing, so its absence must not drag the
// vehicle's status down.
func TestCarsExportDoesNotRequireATankLicenceOfEveryVehicle(t *testing.T) {
	cars := []Models.Car{car("VAN", "Car", inDays(500), inDays(500), "")}
	data, err := BuildCarsExcel(cars, CarExportLabels{}, 30, "meta")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	defer f.Close()
	rows, _ := f.GetRows("Cars")
	if got := rows[7][len(rows[7])-1]; got != "Valid" {
		t.Errorf("want Valid, got %q", got)
	}
}
