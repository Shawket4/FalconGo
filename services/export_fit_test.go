package services

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Falcon/Models"
	"Falcon/report"
)

var (
	fitTerminals = []string{"مصر البترول بور سعيد", "انابيب البترول بدر", "TAQA Alex Terminal", "TAQA Suez Terminal"}
	fitDropOffs  = []string{"مدينة السادس من أكتوبر 2", "كفر الشيخ - سيدي سالم", "العاشر من رمضان 3", "اسيوط الغربي1"}
	fitDrivers   = []string{"عبد الرحمن محمد عبد العزيز", "مصطفى إبراهيم عبد الحميد"}
	fitCompanies = []string{"Petrol Arrows", "Watanya", "TAQA"}
)

// fitStressTrips: the longest names and the largest figures a month holds.
func fitStressTrips() []Models.TripStruct {
	at := time.Date(2026, 9, 3, 14, 5, 0, 0, time.UTC)
	f := false
	var out []Models.TripStruct
	for i := 0; i < 1200; i++ {
		t := Models.TripStruct{
			ReceiptNo: fmt.Sprint(61000000 + i*7), Date: fmt.Sprintf("2026-09-%02d", 1+i%30),
			Company: fitCompanies[i%3], Terminal: fitTerminals[i%4], DropOffPoint: fitDropOffs[i%4],
			CarNoPlate: "ص ل ح 3056", DriverName: fitDrivers[i%2], TankCapacity: 42000,
			Mileage: 480.5, Fee: 24999.99,
			ReceiptSteps: []Models.ReceiptStep{
				{Location: "Garage", ReceivedBy: "عبد الرحمن محمد", ReceivedAt: at},
				{Location: "Office", ReceivedBy: "Mahmoud Hassan", ReceivedAt: at, Stamped: true},
			},
		}
		if i%5 == 0 {
			t.ReceiptValid = &f
		}
		if i%7 == 0 {
			t.ReceiptOverride = true
		}
		out = append(out, t)
	}
	return out
}

func fitStressPlan() PilePlan {
	var trips []Models.TripStruct
	for i, name := range append(fitDropOffs, "الشيخ زايد 2", "روض الفرج 1", "دجله بالميز", "6 اكتوبر") {
		for _, t := range tripsFor(name, 30+i*9) {
			t.Terminal = fitTerminals[i%4]
			t.CarNoPlate = "ص ل ح 3056"
			t.ReceiptNo = fmt.Sprintf("%08d", 61000000+len(trips))
			trips = append(trips, t)
		}
	}
	plan := BuildPilePlan(trips, ModeBalanced, 0)
	plan.StartDate, plan.EndDate = "2026-07-01", "2026-07-31"
	return plan
}

func tahomaForFit(t *testing.T) ([]byte, []byte) {
	dir := os.Getenv("TAHOMA_DIR")
	if dir == "" {
		t.Log("TAHOMA_DIR not set: measuring the PDF only")
		return nil, nil
	}
	r, err1 := os.ReadFile(filepath.Join(dir, "tahoma.ttf"))
	b, err2 := os.ReadFile(filepath.Join(dir, "tahomabd.ttf"))
	if err1 != nil || err2 != nil {
		t.Fatalf("TAHOMA_DIR: %v %v", err1, err2)
	}
	return r, b
}

func checkFit(t *testing.T, name string, book *report.Book, reg, bold []byte) {
	t.Helper()
	issues, err := book.CheckFit(reg, bold)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Errorf("%s: %s", name, i)
	}
}

// No text in the trips or piles exports runs into its neighbour, is cut off
// or shrinks below 80% with the longest names and largest figures.
func TestExportsFitLongestNames(t *testing.T) {
	reg, bold := tahomaForFit(t)
	for _, c := range []struct {
		tag    string
		labels TripExportLabels
	}{{"en", TripExportLabels{}}, {"ar", arTripLabels}} {
		for _, fin := range []bool{true, false} {
			book, err := buildTripsBook(fitStressTrips(), c.labels, fin, "1,200 trips · 2026-09-01 → 2026-09-30 · exported 18 Oct 2026 14:05")
			if err != nil {
				t.Fatal(err)
			}
			checkFit(t, fmt.Sprintf("trips %s fees=%v", c.tag, fin), book, reg, bold)
			book.Close()
		}
		book, err := buildPilesBook(fitStressPlan(), c.tag)
		if err != nil {
			t.Fatal(err)
		}
		checkFit(t, "piles "+c.tag, book, reg, bold)
		book.Close()
	}
}
