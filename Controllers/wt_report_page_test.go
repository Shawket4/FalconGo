package Controllers

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"Falcon/Models"
	"Falcon/report"

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
	return wtStore(out, wtFakeMappings())
}

func wtFakeMappings() []Models.FeeMapping {
	var mappings []Models.FeeMapping
	for _, d := range []string{"العياط", "بنها", "اسيوط", "شبين الكوم", "طنطا", "دمنهور", "المنصورة", "قنا"} {
		for _, term := range []string{"مستودع طنطا", "مستودع القاهرة", "مستودع اسيوط"} {
			mappings = append(mappings, Models.FeeMapping{
				Company: "Watanya", Terminal: term, DropOffPoint: d, Distance: 120, Fee: 5,
			})
		}
	}
	return mappings
}

// wtRender builds and renders a report and opens the result.
func wtRender(t *testing.T, trips []Models.TripStruct, maps []Models.FeeMapping, lang report.Lang) (*excelize.File, string, report.Report) {
	t.Helper()
	rep := wtBuildReport(trips, maps, lang)
	path, err := wtRenderReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	xl, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { xl.Close() })
	return xl, path, rep
}

// pageStarts returns the first row of every page after the first, from the
// manual row breaks of one sheet's XML. <brk id="N"> is a break after row N.
func pageStarts(body string) []int {
	var rows []int
	for _, m := range regexp.MustCompile(`<brk id="(\d+)"`).FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[1])
		rows = append(rows, n+1)
	}
	sort.Ints(rows)
	return rows
}

func sheetXMLs(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string]string{}
	for _, fh := range zr.File {
		if strings.HasPrefix(fh.Name, "xl/worksheets/sheet") {
			rc, _ := fh.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			out[fh.Name] = string(b)
		}
	}
	return out
}

// printTitleRow returns the row a sheet repeats at the top of every page.
func printTitleRow(t *testing.T, xl *excelize.File, sheet string) int {
	t.Helper()
	for _, dn := range xl.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Titles" && dn.Scope == sheet {
			m := regexp.MustCompile(`\$(\d+):\$\d+$`).FindStringSubmatch(dn.RefersTo)
			if m != nil {
				n, _ := strconv.Atoi(m[1])
				return n
			}
		}
	}
	t.Fatalf("%s: no print titles", sheet)
	return 0
}

// Every printed page must hold content, fit the A4 sheet at the sheet's print
// scale (so Excel never adds an automatic break of its own), and start at a
// drop-off point (Summary) or a route band (Trip Details) — never mid-group.
func TestWatanyaReportPagination(t *testing.T) {
	fixTrips, fixMaps := wtFixture()
	inputs := []struct {
		name  string
		trips []Models.TripStruct
		maps  []Models.FeeMapping
	}{
		{"random600", wtFakeTrips(600), wtFakeMappings()},
		{"fixture", fixTrips, fixMaps},
	}
	for _, in := range inputs {
		for _, lang := range []report.Lang{report.English, report.Arabic} {
			t.Run(fmt.Sprintf("%s/lang%d", in.name, lang), func(t *testing.T) {
				xl, path, rep := wtRender(t, in.trips, in.maps, lang)
				xmls := sheetXMLs(t, path)
				dropOffs := map[string]bool{}
				for _, g := range rep.Groups {
					dropOffs[g.DropOff] = true
				}
				summary, details := wtSheetNames(lang)
				for i, sheet := range []string{summary, details} {
					body := xmls[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)]
					if strings.Contains(body, `fitToPage="true"`) || strings.Contains(body, `fitToPage="1"`) {
						t.Errorf("%s: fit-to-page is on, Excel would ignore the manual breaks", sheet)
					}
					layout, err := xl.GetPageLayout(sheet)
					if err != nil || layout.AdjustTo == nil {
						t.Fatalf("%s: no print scale: %v", sheet, err)
					}
					margins, err := xl.GetPageMargins(sheet)
					if err != nil {
						t.Fatal(err)
					}
					limit := (841.89 - (*margins.Top+*margins.Bottom)*72) / (float64(*layout.AdjustTo) / 100)

					rows, err := xl.GetRows(sheet)
					if err != nil {
						t.Fatal(err)
					}
					heights := make([]float64, len(rows)+2)
					for r := 1; r <= len(rows)+1; r++ {
						heights[r], _ = xl.GetRowHeight(sheet, r)
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
					titleRow := printTitleRow(t, xl, sheet)

					breaks := pageStarts(body)
					if sheet == details && len(breaks) == 0 {
						t.Errorf("%s: no manual page breaks on a multi-page report", sheet)
					}
					bounds := append(breaks, len(rows)+1)
					start := 1
					for pageNo, end := range bounds {
						var pt float64
						content := false
						for r := start; r < end; r++ {
							pt += heights[r]
							content = content || nonEmpty(r)
						}
						if start > 1 {
							pt += heights[titleRow] // print titles are reprinted on every page
							b, _ := xl.GetCellValue(sheet, fmt.Sprintf("B%d", start))
							if !dropOffs[strings.TrimSpace(b)] {
								t.Errorf("%s: page %d starts at row %d with %q, not a drop-off point",
									sheet, pageNo+1, start, b)
							}
						}
						if !content {
							t.Errorf("%s: page %d (rows %d-%d) holds no content", sheet, pageNo+1, start, end-1)
						}
						if pt > limit+0.5 {
							t.Errorf("%s: page %d (rows %d-%d) is %.0fpt tall, over the %.0fpt page",
								sheet, pageNo+1, start, end-1, pt, limit)
						}
						start = end
					}
				}
			})
		}
	}
}

// The totals the sheets print must be the fixture's own sums, with the
// unmapped route left out, and the Arabic build must carry Arabic labels and
// a right-to-left view.
func TestWatanyaReportContent(t *testing.T) {
	trips, maps := wtFixture()
	priced := map[string]float64{}
	for _, m := range maps {
		priced[m.Terminal+"|"+m.DropOffPoint] = wtSeedBandRates[int(m.Fee)]
	}
	var wantTrips, wantCap int
	var wantAmount float64
	for _, tr := range trips {
		rate, ok := priced[tr.Terminal+"|"+tr.DropOffPoint]
		if !ok {
			continue
		}
		wantTrips++
		wantCap += tr.TankCapacity
		wantAmount += float64(tr.TankCapacity) * rate / 1000
	}

	for _, lang := range []report.Lang{report.English, report.Arabic} {
		xl, _, _ := wtRender(t, trips, maps, lang)
		summary, details := wtSheetNames(lang)
		if got := xl.GetSheetList(); len(got) != 2 || got[0] != summary || got[1] != details {
			t.Fatalf("sheets = %v, want [%s %s]", got, summary, details)
		}

		grand, total, terminalLabel := "Grand total", "Total", "Terminal"
		if lang == report.Arabic {
			grand, total, terminalLabel = "الإجمالي العام", "الإجمالي", "المستودع"
		}

		// Summary: the grand total row.
		rows, _ := xl.GetRows(summary, excelize.Options{RawCellValue: true})
		found := false
		for _, r := range rows {
			if len(r) > 8 && r[0] == grand {
				found = true
				gotTrips, _ := strconv.Atoi(r[3])
				gotCap, _ := strconv.ParseFloat(r[4], 64)
				gotAmt, _ := strconv.ParseFloat(r[8], 64)
				if gotTrips != wantTrips || int(gotCap) != wantCap || math.Abs(gotAmt-wantAmount) > 0.006 {
					t.Errorf("lang %d grand total = %d trips, %.0f L, %.2f EGP; want %d, %d, %.2f",
						lang, gotTrips, gotCap, gotAmt, wantTrips, wantCap, wantAmount)
				}
			}
		}
		if !found {
			t.Errorf("lang %d: no grand total row on %s", lang, summary)
		}

		// Trip Details: one row per priced trip, route totals adding up.
		rows, _ = xl.GetRows(details, excelize.Options{RawCellValue: true})
		var tripRows int
		var routeSum float64
		for _, r := range rows {
			if len(r) < 9 {
				continue
			}
			if r[0] == total {
				v, _ := strconv.ParseFloat(r[8], 64)
				routeSum += v
			} else if r[2] != "" && r[3] != "" && r[0] != "" && r[0] != "#" && r[0] != "م" {
				tripRows++
			}
		}
		if tripRows != wantTrips {
			t.Errorf("lang %d: %d trip rows on %s, want %d", lang, tripRows, details, wantTrips)
		}
		if math.Abs(routeSum-wantAmount) > 0.01*float64(len(maps)) {
			t.Errorf("lang %d: route totals add to %.2f, want %.2f", lang, routeSum, wantAmount)
		}

		for _, sheet := range []string{summary, details} {
			all, _ := xl.GetRows(sheet)
			var flat strings.Builder
			for _, r := range all {
				flat.WriteString(strings.Join(r, "|"))
			}
			if strings.Contains(flat.String(), "سوهاج") {
				t.Errorf("%s: unmapped route printed", sheet)
			}
			if !strings.Contains(flat.String(), terminalLabel) {
				t.Errorf("%s: missing %q label", sheet, terminalLabel)
			}
			rtl, err := xl.GetSheetView(sheet, 0)
			if err != nil {
				t.Fatal(err)
			}
			if (lang == report.Arabic) != (rtl.RightToLeft != nil && *rtl.RightToLeft) {
				t.Errorf("%s: right-to-left = %v", sheet, rtl.RightToLeft)
			}
		}
	}
}

// No priced trips: nothing to report, which the handler answers with a 404.
func TestWatanyaReportEmpty(t *testing.T) {
	rep := wtBuildReport(nil, wtFakeMappings(), report.English)
	if len(rep.Groups) != 0 || len(rep.Sections) != 0 {
		t.Fatalf("empty input gave %d groups", len(rep.Groups))
	}
	trips, _ := wtFixture()
	if rep := wtBuildReport(trips, nil, report.English); len(rep.Groups) != 0 {
		t.Fatal("trips with no fee mappings should not be reported")
	}
}

func TestWtParseDate(t *testing.T) {
	for in, want := range map[string]string{
		"2026-09-05":           "2026-09-05",
		"2026-09-05T00:00:00Z": "2026-09-05",
		" 2026-09-05 ":         "2026-09-05",
		"05/09/2026":           "0001-01-01",
		"":                     "0001-01-01",
	} {
		if got := wtParseDate(in).Format("2006-01-02"); got != want {
			t.Errorf("wtParseDate(%q) = %s, want %s", in, got, want)
		}
	}
}

// WT_REPORT_OUT=/some/dir go test ./Controllers -run TestWatanyaReportWriteFixture
// writes the fixture report there for inspection: the workbook in both
// languages, and each PDF part in both languages under pdf/.
func TestWatanyaReportWriteFixture(t *testing.T) {
	dir := os.Getenv("WT_REPORT_OUT")
	if dir == "" {
		t.Skip("WT_REPORT_OUT not set")
	}
	if err := os.MkdirAll(filepath.Join(dir, "pdf"), 0o755); err != nil {
		t.Fatal(err)
	}
	trips, maps := wtFixture()
	for _, c := range []struct {
		lang report.Lang
		tag  string
	}{{report.English, "en"}, {report.Arabic, "ar"}} {
		rep := wtBuildReport(trips, maps, c.lang)
		rep.Period = wtPeriod(c.lang, "2026-09-01", "2026-09-30")
		path, err := wtRenderReport(rep)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(path)
		if err := os.WriteFile(filepath.Join(dir, "new_"+c.tag+".xlsx"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{"summary", "details"} {
			pdf, err := wtRenderPDF(rep, part)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "pdf", part+"_"+c.tag+".pdf"), pdf, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWtPeriod(t *testing.T) {
	for _, c := range []struct {
		lang             report.Lang
		start, end, want string
	}{
		{report.English, "2026-08-01", "2026-08-31", "August 2026"},
		{report.English, "2026-08-01", "2026-08-15", "1–15 Aug 2026"},
		{report.Arabic, "2026-08-01", "2026-08-31", "أغسطس 2026"},
		{report.English, "nonsense", "2026-01-03", "nonsense – 2026-01-03"},
	} {
		if got := wtPeriod(c.lang, c.start, c.end); got != c.want {
			t.Errorf("wtPeriod(%q, %q) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
}
