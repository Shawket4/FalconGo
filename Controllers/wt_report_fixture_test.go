package Controllers

import (
	"fmt"
	"math/rand"
	"testing"

	"Falcon/Models"
	"Falcon/testutil"
)

// wtFixture is a representative month of Watanya work: real terminal names,
// Arabic drop-off points, drop-offs served from one terminal and from
// several, one route long enough to run past a printed page, mixed tanker
// sizes, and a handful of trips on a route with no fee mapping (which the
// report must leave out). Deterministic, so two generators can be compared
// on exactly the same input.
func wtFixture() ([]Models.TripStruct, []Models.FeeMapping) {
	type route struct {
		terminal, dropOff string
		distance          float64
		fee               float64
		trips             int
	}
	routes := []route{
		{"بدر", "العياط", 96, 4, 14},
		{"تعاون مسطرد", "العياط", 112, 5, 6},
		{"الفيوم", "العياط", 64, 3, 3},
		{"بدر", "بنها", 71, 3, 22},
		{"تعاون مسطرد", "بنها", 38, 1, 9},
		{"هايكستب", "شبين الكوم", 104, 4, 11},
		{"TAQA Suez Terminal", "اسيوط", 498, 14, 7},
		{"الفيوم", "اسيوط", 312, 11, 5},
		{"بدر", "طنطا", 118, 5, 64}, // longer than one printed page
		{"مصر البترول بور سعيد", "دمنهور", 236, 8, 4},
		{"هايكستب", "دمنهور", 171, 7, 12},
		{"مصر البترول بور سعيد", "المنصورة", 141, 6, 18},
		{"تعاون مسطرد", "قنا", 702, 15, 2},
		{"بدر", "الزقازيق", 83, 3, 1},
	}
	drivers := []string{"محمد عبد الله", "أحمد سيد", "محمود حسن", "مصطفى إبراهيم", "خالد فتحي", "سامح عادل"}
	plates := []string{"ن ق ط 4821", "ر س م 1934", "ع ب د 7710", "ص ل ح 3056", "م ج د 6402"}
	capacities := []int{19000, 20000, 24000, 38000}

	rnd := rand.New(rand.NewSource(20260901))
	var trips []Models.TripStruct
	var mappings []Models.FeeMapping
	receipt := 610000
	for _, r := range routes {
		mappings = append(mappings, Models.FeeMapping{
			Company: "Watanya", Terminal: r.terminal, DropOffPoint: r.dropOff,
			Distance: r.distance, Fee: r.fee,
		})
		for i := 0; i < r.trips; i++ {
			receipt += 1 + rnd.Intn(40)
			trips = append(trips, Models.TripStruct{
				Company:      "Watanya",
				Terminal:     r.terminal,
				DropOffPoint: r.dropOff,
				Date:         fmt.Sprintf("2026-09-%02d", 1+rnd.Intn(30)),
				ReceiptNo:    fmt.Sprintf("%d", receipt),
				DriverName:   drivers[rnd.Intn(len(drivers))],
				CarNoPlate:   plates[rnd.Intn(len(plates))],
				TankCapacity: capacities[rnd.Intn(len(capacities))],
				Mileage:      r.distance*2 + float64(rnd.Intn(30)),
			})
		}
	}
	trips = wtStore(trips, mappings)
	// No fee mapping for this route: excluded from the report.
	for i := 0; i < 3; i++ {
		trips = append(trips, Models.TripStruct{
			Company: "Watanya", Terminal: "بدر", DropOffPoint: "سوهاج",
			Date: "2026-09-15", ReceiptNo: fmt.Sprintf("99%04d", i),
			DriverName: drivers[0], CarNoPlate: plates[0], TankCapacity: 38000, Mileage: 900,
		})
	}
	return trips, mappings
}

// wtSeedBandRates is the Watanya band table the pricing seed stores
// (apex-rust's WATANYA_BAND_RATES); TestWatanyaSeedBandsMatchFixtures pins
// the two together. The report reads stored prices, so its fixtures carry
// what the engine stores.
var wtSeedBandRates = map[int]float64{
	1: 104.5, 2: 122.1, 3: 129.8, 4: 156.2, 5: 183.7,
	6: 196.9, 7: 210.1, 8: 235.4, 9: 261.8, 10: 288.2,
	11: 314.6, 12: 341.0, 13: 367.4, 14: 393.8, 15: 420.2,
}

// wtStore sets each mapped trip's stored base revenue as the pricing engine
// would: tank_capacity x band rate / 1,000.
func wtStore(trips []Models.TripStruct, maps []Models.FeeMapping) []Models.TripStruct {
	band := map[string]int{}
	for _, m := range maps {
		band[m.Terminal+"|"+m.DropOffPoint] = int(m.Fee)
	}
	for i := range trips {
		if b, ok := band[trips[i].Terminal+"|"+trips[i].DropOffPoint]; ok {
			v := float64(trips[i].TankCapacity) * wtSeedBandRates[b] / 1000
			trips[i].BaseRevenue = &v
		}
	}
	return trips
}

// TestWatanyaSeedBandsMatchFixtures: the fixtures' band table is the one the
// pricing seed stores, so the report tests price as production does.
func TestWatanyaSeedBandsMatchFixtures(t *testing.T) {
	db := testutil.TripSchema(t)
	var bands []struct {
		Band int
		Rate float64
	}
	db.Raw(`SELECT b.band, b.rate_per_1000l::float8 AS rate FROM pricing_bands b
              JOIN pricing_rules r ON r.id = b.rule_id
             WHERE r.company = 'Watanya' AND r.effective_from = '2000-01-01'`).Scan(&bands)
	if len(bands) != len(wtSeedBandRates) {
		t.Fatalf("seed has %d bands, fixtures %d", len(bands), len(wtSeedBandRates))
	}
	for _, b := range bands {
		if wtSeedBandRates[b.Band] != b.Rate {
			t.Errorf("band %d: seed %.2f, fixtures %.2f", b.Band, b.Rate, wtSeedBandRates[b.Band])
		}
	}
}
