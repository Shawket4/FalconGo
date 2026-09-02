package Controllers

import (
	"archive/zip"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"testing"

	"Falcon/Models"

	"github.com/xuri/excelize/v2"
)

func wtFakeTrips(n int) []Models.TripStruct {
	names := []string{"العياط", "بنها", "اسيوط", "شبين الكوم", "طنطا", "دمنهور", "المنصورة", "قنا"}
	terms := []string{"مستودع طنطا", "مستودع القاهرة", "مستودع اسيوط"}
	rnd := rand.New(rand.NewSource(7))
	var out []Models.TripStruct
	for i := 0; i < n; i++ {
		out = append(out, Models.TripStruct{
			Company:      "Watanya",
			DropOffPoint: names[rnd.Intn(len(names))],
			Terminal:     terms[rnd.Intn(len(terms))],
			Date:         fmt.Sprintf("2026-07-%02d", 1+rnd.Intn(28)),
			ReceiptNo:    fmt.Sprintf("R%05d", i),
			DriverName:   "سائق تجريبي",
			CarNoPlate:   "ا ب ج 123",
			TankCapacity: 20000,
			Mileage:      float64(100 + rnd.Intn(400)),
		})
	}
	return out
}

// brkRows returns the manual row-break positions of one sheet's XML.
func brkRows(t *testing.T, body string) []int {
	t.Helper()
	var rows []int
	for _, m := range regexp.MustCompile(`<brk id="(\d+)"`).FindAllStringSubmatch(body, -1) {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		rows = append(rows, n)
	}
	sort.Ints(rows)
	return rows
}

// The report must not emit a printed page holding nothing but separator rows,
// and no manual page may be taller than the A4 sheet it is printed on.
func TestWatanyaReportPagination(t *testing.T) {
	var mappings []Models.FeeMapping
	for _, d := range []string{"العياط", "بنها", "اسيوط", "شبين الكوم", "طنطا", "دمنهور", "المنصورة", "قنا"} {
		for _, term := range []string{"مستودع طنطا", "مستودع القاهرة", "مستودع اسيوط"} {
			mappings = append(mappings, Models.FeeMapping{
				Company: "Watanya", Terminal: term, DropOffPoint: d, Distance: 120, Fee: 5,
			})
		}
	}
	groups, summaries := wtBuildGroups(wtFakeTrips(600), mappings)
	path, err := wtRenderExcel(groups, summaries, DateRangeRequest{StartDate: "2026-07-01", EndDate: "2026-07-31"})
	if err != nil {
		t.Fatal(err)
	}

	xl, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer xl.Close()

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	sheetXML := map[string]string{}
	var wbXML string
	for _, fh := range zr.File {
		rc, _ := fh.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		switch {
		case fh.Name == "xl/workbook.xml":
			wbXML = string(b)
		case strings.HasPrefix(fh.Name, "xl/worksheets/sheet"):
			sheetXML[fh.Name] = string(b)
		}
	}

	if !strings.Contains(wbXML, `name="_xlnm.Print_Titles">Summary!$1:$4`) {
		t.Error("summary sheet is missing repeating print titles")
	}

	for i, sheet := range []string{"Summary", "Trip Details"} {
		name := fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		body := sheetXML[name]
		if !strings.Contains(body, `fitToPage="true"`) || !strings.Contains(body, `fitToWidth="1"`) {
			t.Errorf("%s: fit-to-one-page-wide not set", sheet)
		}

		rows, err := xl.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		heights := make([]float64, len(rows)+1)
		for r := 1; r <= len(rows); r++ {
			h, _ := xl.GetRowHeight(sheet, r)
			heights[r] = h
		}
		nonEmpty := func(r int) bool {
			if r-1 >= len(rows) {
				return false
			}
			for _, c := range rows[r-1] {
				if strings.TrimSpace(c) != "" {
					return true
				}
			}
			return false
		}

		// Walk the pages the manual breaks cut, plus the tail page.
		bounds := append(brkRows(t, body), len(rows)+1)
		start := 1
		titlePt := 0.0
		if sheet == "Summary" {
			for r := 1; r <= 4; r++ {
				titlePt += heights[r]
			}
		}
		for pageNo, end := range bounds {
			var pt float64
			content := false
			for r := start; r < end; r++ {
				pt += heights[r]
				if nonEmpty(r) {
					content = true
				}
			}
			if start > 1 {
				pt += titlePt // print titles are reprinted on every page
			}
			if !content {
				t.Errorf("%s: page %d (rows %d-%d) holds no content — a blank sheet of paper",
					sheet, pageNo+1, start, end-1)
			}
			if pt > wtUsablePagePt+0.5 {
				t.Errorf("%s: page %d (rows %d-%d) is %.0fpt tall, over the %.0fpt A4 page",
					sheet, pageNo+1, start, end-1, pt, wtUsablePagePt)
			}
			start = end
		}
	}
}
