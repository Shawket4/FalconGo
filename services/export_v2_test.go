package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

var arTripLabels = TripExportLabels{
	Lang: "ar", Title: "الرحلات", Subtitle: "سجل الرحلات", Sheet: "الرحلات", Summary: "الملخص",
	ReceiptNo: "رقم الإيصال", Date: "التاريخ", Company: "الشركة", Terminal: "المستودع",
	DropOffPoint: "نقطة التفريغ", Vehicle: "السيارة", Driver: "السائق", TankCapacity: "السعة (لتر)",
	Distance: "المسافة (كم)", Fee: "الرسوم", InGarage: "في الجراج", InOffice: "في المكتب", Stamped: "مختوم",
	GarageReceivedBy: "استلم في الجراج", GarageDate: "تاريخ الجراج", OfficeReceivedBy: "استلم في المكتب",
	OfficeDate: "تاريخ المكتب", Remark: "ملاحظة", TotalTrips: "عدد الرحلات", TotalVolume: "إجمالي الكمية",
	TotalDistance: "إجمالي المسافة", TotalFees: "إجمالي الرسوم", StampedCount: "مختوم", Totals: "الإجمالي",
	Yes: "نعم", No: "لا",
}

func TestTripsExportArabicAndPDF(t *testing.T) {
	data, err := BuildTripsExport(exportFixtureTrips(), arTripLabels, true, "90 رحلة", false)
	if err != nil {
		t.Fatal(err)
	}
	xl, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer xl.Close()
	if got := xl.GetSheetList(); len(got) != 2 || got[0] != "الرحلات" || got[1] != "الملخص" {
		t.Fatalf("sheets %v", got)
	}
	view, _ := xl.GetSheetView("الرحلات", 0)
	if view.RightToLeft == nil || !*view.RightToLeft {
		t.Error("Arabic trips sheet is not right-to-left")
	}
	pdf, err := BuildTripsExport(exportFixtureTrips(), arTripLabels, true, "90 رحلة", true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte("/BaseFont /Cairo-Regular")) {
		t.Error("trips PDF missing or without Cairo")
	}
	// Masked export: no fee column, in the PDF as in the workbook.
	masked, _ := BuildTripsExport(exportFixtureTrips(), TripExportLabels{}, false, "", false)
	mx, _ := excelize.OpenReader(bytes.NewReader(masked))
	defer mx.Close()
	rows, _ := mx.GetRows("Trips")
	for _, r := range rows {
		for _, c := range r {
			if c == "Fee" || c == "Total Fees" {
				t.Fatal("fee column present without financial access")
			}
		}
	}
}

func TestPilesExportEnglishAndPDF(t *testing.T) {
	data, err := RenderPilePlan(exportFixturePlan(), "en", false)
	if err != nil {
		t.Fatal(err)
	}
	xl, _ := excelize.OpenReader(bytes.NewReader(data))
	defer xl.Close()
	if got := xl.GetSheetList(); strings.Join(got, ",") != "Checklist,Piles,Boxes" {
		t.Fatalf("sheets %v", got)
	}
	if xl.GetActiveSheetIndex() != 2 {
		t.Error("workbook should open on Boxes")
	}
	rows, _ := xl.GetRows("Boxes")
	found := false
	for i := range rows {
		f, _ := xl.GetCellFormula("Boxes", "E"+strconv.Itoa(i+1))
		if strings.HasPrefix(f, "COUNTIFS('Checklist'!$B$") {
			found = true
		}
	}
	if !found {
		t.Error("no COUNTIFS over the checklist on Boxes")
	}
	pdf, err := RenderPilePlan(exportFixturePlan(), "ar", true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || bytes.Count(pdf, []byte("/Type /Page ")) < 3 {
		t.Error("piles PDF should hold the three sheets")
	}
}

// EXPORT_V2_OUT=/dir writes the new exports in both languages, as workbooks
// and PDFs, under dir/trips and dir/piles.
func TestWriteExportsV2(t *testing.T) {
	dir := os.Getenv("EXPORT_V2_OUT")
	if dir == "" {
		t.Skip("EXPORT_V2_OUT not set")
	}
	for _, sub := range []string{"trips", "piles"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}
	for _, c := range []struct {
		tag    string
		labels TripExportLabels
	}{{"en", TripExportLabels{}}, {"ar", arTripLabels}} {
		for _, pdf := range []bool{false, true} {
			b, err := BuildTripsExport(exportFixtureTrips(), c.labels, true, "90 trips · fixture", pdf)
			if err != nil {
				t.Fatal(err)
			}
			ext := map[bool]string{false: "xlsx", true: "pdf"}[pdf]
			os.WriteFile(filepath.Join(dir, "trips", "trips_"+c.tag+"."+ext), b, 0o644)
			p, err := RenderPilePlan(exportFixturePlan(), c.tag, pdf)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(dir, "piles", "piles_"+c.tag+"."+ext), p, 0o644)
		}
	}
}
