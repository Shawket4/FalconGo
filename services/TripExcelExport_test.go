package services

import (
	"os"
	"testing"
	"time"

	"Falcon/Models"
)

func sampleTrips() []Models.TripStruct {
	at := time.Date(2026, 8, 8, 9, 30, 0, 0, time.UTC)

	return []Models.TripStruct{
		{
			ReceiptNo: "R-1001", Date: "2026-08-08", Company: "Petrol Arrows",
			Terminal: "Mostorod", DropOffPoint: "طنطا", CarNoPlate: "ف ج م 8567",
			DriverName: "محمد عبد الحي", TankCapacity: 42000, Mileage: 128.5, Fee: 4250.75,
			ReceiptSteps: []Models.ReceiptStep{
				{Location: "Garage", ReceivedBy: "Shady", ReceivedAt: at, Stamped: false},
				{Location: "Office", ReceivedBy: "Mahmoud", ReceivedAt: at.Add(3 * time.Hour), Stamped: true},
			},
		},
		{
			ReceiptNo: "R-1002", Date: "2026-08-08", Company: "TAQA",
			Terminal: "Suez", DropOffPoint: "", CarNoPlate: "ف ا ن 7421",
			DriverName: "غير مسجل", TankCapacity: 30000, Distance: 96.25, Fee: 2100,
			// No receipt steps at all: every flag must read No rather than blank.
		},
		{
			ReceiptNo: "R-0999", Date: "2026-08-07", Company: "Watanya",
			Terminal: "Alex", DropOffPoint: "دمنهور", CarNoPlate: "ف ر ص 8461",
			DriverName: "اسلام كمال", TankCapacity: 25000, Mileage: 210, Fee: 6100.5,
			ReceiptSteps: []Models.ReceiptStep{
				{Location: "Garage", ReceivedBy: "Shady", ReceivedAt: at, Stamped: true},
			},
		},
	}
}

func TestBuildTripsExcelProducesAWorkbook(t *testing.T) {
	data, err := BuildTripsExcel(sampleTrips(), TripExportLabels{}, true, "3 trips · test")
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if len(data) < 4000 {
		t.Fatalf("workbook suspiciously small: %d bytes", len(data))
	}
	// PK zip magic -- a real xlsx, not an error page.
	if data[0] != 'P' || data[1] != 'K' {
		t.Fatalf("output is not a zip/xlsx: %q", data[:4])
	}

	if out := os.Getenv("EXPORT_TEST_OUT"); out != "" {
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatalf("could not write %s: %v", out, err)
		}
	}
}

// Below permission 3 the fee column is omitted rather than zeroed: a column of
// 0.00 reads as "these trips were free", which is worse than not showing it.
func TestFeeColumnIsAbsentWithoutFinancialAccess(t *testing.T) {
	withFees, err := BuildTripsExcel(sampleTrips(), TripExportLabels{}, true, "")
	if err != nil {
		t.Fatal(err)
	}
	withoutFees, err := BuildTripsExcel(sampleTrips(), TripExportLabels{}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutFees) >= len(withFees) {
		t.Errorf("masked export (%d bytes) should be smaller than the full one (%d)",
			len(withoutFees), len(withFees))
	}

	if out := os.Getenv("EXPORT_TEST_OUT_MASKED"); out != "" {
		_ = os.WriteFile(out, withoutFees, 0o644)
	}
}

func TestEmptyExportStillBuilds(t *testing.T) {
	// The handler refuses an empty result before reaching here, but the builder
	// must not panic on one either.
	if _, err := BuildTripsExcel(nil, TripExportLabels{}, true, ""); err != nil {
		t.Fatalf("empty export failed: %v", err)
	}
}

func TestSheetNameIsSanitised(t *testing.T) {
	cases := map[string]string{
		"Trips":                            "Trips",
		"A/B:C":                            "A B C",
		"":                                 "Sheet1",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 31 max
	}
	for in, want := range cases {
		if got := sanitizeSheetName(in); got != want {
			t.Errorf("sanitizeSheetName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Distance falls back to the planned value when no mileage was recorded, which
// is how the table and the old client export both read it.
func TestDistancePrefersMileage(t *testing.T) {
	if got := tripDistance(Models.TripStruct{Mileage: 10, Distance: 99}); got != 10 {
		t.Errorf("mileage should win, got %v", got)
	}
	if got := tripDistance(Models.TripStruct{Distance: 99}); got != 99 {
		t.Errorf("should fall back to distance, got %v", got)
	}
}
