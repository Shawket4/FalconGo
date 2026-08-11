package services

import (
	"fmt"
	"os"
	"testing"
	"time"

	"Falcon/Models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Builds the export from real data.
//
// Skipped unless EXPORT_IT_DSN is set, so the normal test run stays hermetic.
// The point is scale: production holds more trips than the old client-side
// export's 10,000-row cap allowed, so this is the case that was silently
// producing an incomplete spreadsheet.
func TestBuildTripsExcelAgainstRealData(t *testing.T) {
	dsn := os.Getenv("EXPORT_IT_DSN")
	if dsn == "" {
		t.Skip("set EXPORT_IT_DSN to run the real-data export test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	var trips []Models.TripStruct
	start := time.Now()
	if err := db.Model(&Models.TripStruct{}).
		Order("date DESC, receipt_no DESC").
		Preload("ReceiptSteps").
		Find(&trips).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	fetchTook := time.Since(start)

	if len(trips) == 0 {
		t.Skip("no trips in the target database")
	}

	// One query for every mapping, not one per trip -- the list endpoint's N+1
	// is survivable for a page of ten and not for the whole table.
	start = time.Now()
	var mappings []Models.FeeMapping
	db.Find(&mappings)
	index := map[string]Models.FeeMapping{}
	for _, m := range mappings {
		index[m.Company+"\x00"+m.Terminal+"\x00"+m.DropOffPoint] = m
	}
	withFee := 0
	for i := range trips {
		if m, ok := index[trips[i].Company+"\x00"+trips[i].Terminal+"\x00"+trips[i].DropOffPoint]; ok {
			trips[i].Distance = m.Distance
			trips[i].Fee = m.Fee
			withFee++
		}
	}
	enrichTook := time.Since(start)

	start = time.Now()
	data, err := BuildTripsExcel(trips, TripExportLabels{}, true,
		fmt.Sprintf("%d trips · real data", len(trips)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	buildTook := time.Since(start)

	t.Logf("trips=%d (fees resolved for %d, %d mappings)", len(trips), withFee, len(mappings))
	t.Logf("fetch=%s enrich=%s build=%s", fetchTook, enrichTook, buildTook)
	t.Logf("workbook=%.1f KB", float64(len(data))/1024)

	if len(trips) <= 10000 {
		t.Logf("note: %d trips is within the old 10,000 client cap", len(trips))
	} else {
		t.Logf("PAST THE OLD CAP: %d trips would have been truncated to 10,000", len(trips))
	}

	if out := os.Getenv("EXPORT_IT_OUT"); out != "" {
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Logf("wrote %s", out)
	}
}
