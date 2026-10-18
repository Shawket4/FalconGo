package services

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Falcon/Models"
)

// exportFixtureTrips is a representative trips export: several companies,
// receipt steps in every state, mileage and planned-distance fallbacks,
// receipt-serial remarks, blank fields, Arabic names. Deterministic, so the
// old and new builders can be compared on the same input.
func exportFixtureTrips() []Models.TripStruct {
	companies := []string{"Watanya", "TAQA", "Petrol Arrows", ""}
	terminals := []string{"بدر", "TAQA Suez Terminal", "هايكستب", "مسطرد"}
	drops := []string{"طنطا", "العياط", "بنها", "دمنهور", "", "شبين الكوم", "قنا"}
	drivers := []string{"محمد عبد الله", "أحمد سيد", "غير مسجل", "خالد فتحي"}
	plates := []string{"ن ق ط 4821", "ر س م 1934", "ع ب د 7710"}
	rnd := rand.New(rand.NewSource(20261018))
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	f := false
	var out []Models.TripStruct
	for i := 0; i < 90; i++ {
		t := Models.TripStruct{
			ReceiptNo:    fmt.Sprintf("%d", 610000+rnd.Intn(9000)),
			Date:         fmt.Sprintf("2026-09-%02d", 1+rnd.Intn(30)),
			Company:      companies[rnd.Intn(len(companies))],
			Terminal:     terminals[rnd.Intn(len(terminals))],
			DropOffPoint: drops[rnd.Intn(len(drops))],
			CarNoPlate:   plates[rnd.Intn(len(plates))],
			DriverName:   drivers[rnd.Intn(len(drivers))],
			TankCapacity: []int{19000, 20000, 24000, 38000, 42000}[rnd.Intn(5)],
			Fee:          float64(rnd.Intn(800000)) / 100,
		}
		if i%3 == 0 {
			t.Distance = float64(rnd.Intn(50000)) / 100
		} else {
			t.Mileage = float64(rnd.Intn(50000)) / 100
		}
		at := base.Add(time.Duration(rnd.Intn(30*24)) * time.Hour)
		switch i % 4 {
		case 1:
			t.ReceiptSteps = []Models.ReceiptStep{{Location: "Garage", ReceivedBy: "Shady", ReceivedAt: at}}
		case 2:
			t.ReceiptSteps = []Models.ReceiptStep{
				{Location: "Garage", ReceivedBy: "Shady", ReceivedAt: at},
				{Location: "Office", ReceivedBy: "Mahmoud", ReceivedAt: at.Add(5 * time.Hour), Stamped: true},
			}
		case 3:
			t.ReceiptSteps = []Models.ReceiptStep{{Location: "Office", ReceivedBy: "مها", ReceivedAt: at, Stamped: i%8 == 3}}
		}
		if i%11 == 0 {
			t.ReceiptValid = &f
		}
		if i%17 == 0 {
			t.ReceiptOverride = true
		}
		if i == 5 {
			t.ReceiptNo = ""
		}
		out = append(out, t)
	}
	return out
}

func exportFixturePlan() PilePlan {
	var trips []Models.TripStruct
	rnd := rand.New(rand.NewSource(7))
	for _, name := range []string{"العياط", "عين شمس", "بلبيس 2", "اسيوط الغربي1", "حلوان 1",
		"دجله بالميز", "6 اكتوبر", "جمصه", "روض الفرج 1", "الشيخ زايد 2", "وادي حوف",
		"بنها", "طنطا", "دمنهور", "المنصورة", "قنا", "سوهاج", "الفيوم", "كفر الشيخ", "مدينة نصر"} {
		for _, t := range tripsFor(name, 2+rnd.Intn(14)) {
			t.CarNoPlate = []string{"ن ق ط 4821", "ر س م 1934"}[rnd.Intn(2)]
			t.Terminal = []string{"بدر", "هايكستب", "مسطرد"}[rnd.Intn(3)]
			trips = append(trips, t)
		}
	}
	trips = append(trips, Models.TripStruct{Company: "Watanya", DropOffPoint: "بنها", Date: "2026-07-03"}) // no receipt
	plan := BuildPilePlan(trips, ModeBalanced, 0)
	plan.StartDate, plan.EndDate = "2026-07-01", "2026-07-31"
	return plan
}

// EXPORT_FIXTURE_OUT=/dir writes the fixture exports there (old and new
// builders share these entry points, so the same test runs on both).
func TestWriteExportFixtures(t *testing.T) {
	dir := os.Getenv("EXPORT_FIXTURE_OUT")
	if dir == "" {
		t.Skip("EXPORT_FIXTURE_OUT not set")
	}
	trips, err := BuildTripsExcel(exportFixtureTrips(), TripExportLabels{}, true, "90 trips · fixture")
	if err != nil {
		t.Fatal(err)
	}
	piles, err := RenderPilePlanExcel(exportFixturePlan())
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "trips.xlsx"), trips, 0o644)
	os.WriteFile(filepath.Join(dir, "piles.xlsx"), piles, 0o644)
}
