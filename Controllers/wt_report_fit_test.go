package Controllers

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"Falcon/Models"
	"Falcon/report"
)

// The longest real terminal names (Models/setup.go) and drop-off / driver
// names at least as long as the longest the dashboard holds, with month
// totals at the top of the real range: tens of millions of litres and
// millions of pounds.
var (
	longTerminals = []string{"مصر البترول بور سعيد", "انابيب البترول بدر", "TAQA Alex Terminal", "TAQA Suez Terminal", "موبيل بور سعيد", "تعاون مسطرد"}
	longDropOffs  = []string{"مدينة السادس من أكتوبر 2", "كفر الشيخ - سيدي سالم", "العاشر من رمضان 3", "اسيوط الغربي1", "شبين الكوم", "روض الفرج 1", "الشيخ زايد 2"}
	longDrivers   = []string{"عبد الرحمن محمد عبد العزيز", "محمد عبد الله", "مصطفى إبراهيم عبد الحميد"}
)

func wtStressFixture() ([]Models.TripStruct, []Models.FeeMapping) {
	var trips []Models.TripStruct
	var maps []Models.FeeMapping
	receipt := 6100000
	for i, d := range longDropOffs {
		for j, term := range longTerminals {
			if (i+j)%3 == 2 {
				continue
			}
			maps = append(maps, Models.FeeMapping{Company: "Watanya", Terminal: term, DropOffPoint: d, Distance: float64(98 + 101*j), Fee: float64(1 + (i+j)%15)})
			n := 3 + (i*7+j*5)%40
			if i == 0 && j == 0 {
				n = 260 // one very heavy route
			}
			for k := 0; k < n; k++ {
				receipt += 7
				trips = append(trips, Models.TripStruct{
					Company: "Watanya", Terminal: term, DropOffPoint: d,
					Date: fmt.Sprintf("2026-09-%02d", 1+k%30), ReceiptNo: fmt.Sprint(receipt),
					DriverName: longDrivers[k%len(longDrivers)], CarNoPlate: "ص ل ح 3056",
					TankCapacity: 42000, Mileage: 1234.5,
				})
			}
		}
	}
	return trips, maps
}

// tahomaFonts reads Tahoma from $TAHOMA_DIR (tahoma.ttf, tahomabd.ttf); nil
// when it is not set, and then only the PDF is measured.
func tahomaFonts(t *testing.T) ([]byte, []byte) {
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

// No text in the Watanya report runs into its neighbour, is cut off, or
// shrinks below 80%, with the longest names — in the PDF, and in the
// workbook when Tahoma is available to measure with.
func TestWatanyaReportFitsLongestNames(t *testing.T) {
	reg, bold := tahomaFonts(t)
	trips, maps := wtStressFixture()
	for _, lang := range []report.Lang{report.English, report.Arabic} {
		rep := wtBuildReport(trips, maps, lang)
		rep.Period = wtPeriod(lang, "2026-09-01", "2026-09-30")
		book, err := wtBuildBook(rep)
		if err != nil {
			t.Fatal(err)
		}
		issues, err := book.CheckFit(reg, bold)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range issues {
			t.Errorf("lang %d: %s", lang, i)
		}
		if out := os.Getenv("WT_REPORT_OUT"); out != "" {
			tag := map[report.Lang]string{report.English: "en", report.Arabic: "ar"}[lang]
			os.MkdirAll(filepath.Join(out, "stress"), 0o755)
			x, _ := book.XLSX()
			os.WriteFile(filepath.Join(out, "stress", "watanya_"+tag+".xlsx"), x, 0o644)
			for _, part := range []string{"summary", "details"} {
				p, err := wtRenderPDF(rep, part)
				if err != nil {
					t.Fatal(err)
				}
				os.WriteFile(filepath.Join(out, "stress", "watanya_"+part+"_"+tag+".pdf"), p, 0o644)
			}
		}
		book.Close()
	}
}
