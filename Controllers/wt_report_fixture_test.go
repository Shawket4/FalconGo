package Controllers

import (
	"fmt"
	"math/rand"

	"Falcon/Models"
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
