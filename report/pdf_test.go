package report

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// testReport is a month of trips over a few routes, one long enough to run
// over a page, with Arabic names.
func testReport(lang Lang) Report {
	routes := []struct {
		dropOff, terminal string
		trips             int
		rate              float64
		fee               int
		dist              float64
	}{
		{"اسيوط", "TAQA Suez Terminal", 7, 393.8, 14, 498},
		{"اسيوط", "الفيوم", 5, 314.6, 11, 312},
		{"بنها", "بدر", 22, 129.8, 3, 71},
		{"طنطا", "بدر", 70, 183.7, 5, 118},
		{"قنا", "تعاون مسطرد", 2, 420.2, 15, 702},
	}
	rep := Report{Lang: lang, Company: "Apex Transport", Title: "Watanya Trips Report",
		Period: "September 2026", PreparedOn: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)}
	if lang == Arabic {
		rep.Title, rep.Period = "تقرير رحلات وطنية", "سبتمبر 2026"
	}
	groups := map[string]int{}
	caps := []float64{19000, 20000, 24000, 38000}
	for i, r := range routes {
		sec := TripSection{No: i + 1, Terminal: r.terminal, DropOff: r.dropOff}
		var capSum float64
		for k := 0; k < r.trips; k++ {
			c := caps[(k*7+i)%4]
			capSum += c
			sec.Trips = append(sec.Trips, Trip{
				Date: time.Date(2026, 9, 1+k%30, 0, 0, 0, 0, time.UTC), Receipt: fmt.Sprint(610000 + i*1000 + k),
				Driver: "محمد عبد الله", Plate: "ن ق ط 4821", Capacity: c, Distance: r.dist*2 + float64(k%9),
				FeeRate: r.rate, Cost: c * r.rate / 1000,
			})
		}
		rep.Sections = append(rep.Sections, sec)
		line := SummaryLine{No: i + 1, Terminal: r.terminal, Trips: r.trips, Capacity: capSum,
			Distance: r.dist, FeeIndex: r.fee, FeeRate: r.rate, Amount: capSum * r.rate / 1000}
		gi, ok := groups[r.dropOff]
		if !ok {
			gi = len(rep.Groups)
			groups[r.dropOff] = gi
			rep.Groups = append(rep.Groups, SummaryGroup{DropOff: r.dropOff})
		}
		rep.Groups[gi].Lines = append(rep.Groups[gi].Lines, line)
	}
	return rep
}

func buildTestBook(t *testing.T, lang Lang) (*Book, []string) {
	t.Helper()
	b := NewBook()
	names := []string{"Summary", "Trip Details"}
	if lang == Arabic {
		names = []string{"الملخص", "تفاصيل الرحلات"}
	}
	rep := testReport(lang)
	if _, err := b.Summary(names[0], rep); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Details(names[1], rep); err != nil {
		t.Fatal(err)
	}
	return b, names
}

// Every cell the PDF draws reads exactly as the workbook displays it, and
// every page the workbook plans is a page of the PDF.
func TestPDFMatchesWorkbook(t *testing.T) {
	for _, lang := range []Lang{English, Arabic} {
		b, names := buildTestBook(t, lang)
		data, err := b.XLSX()
		if err != nil {
			t.Fatal(err)
		}
		xl, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			rec := b.sheet[name]
			pdf, drawn, err := renderPDFDrawn("t", []*sheetRecord{rec})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(pdf, []byte("%PDF-1.7")) || !bytes.Contains(pdf, []byte("/FontFile2")) {
				t.Fatalf("%s: not a PDF with an embedded TrueType font", name)
			}
			if got := len(drawn); got != rec.print.pages {
				t.Errorf("%s: PDF has %d pages, the workbook plans %d", name, got, rec.print.pages)
			}
			if got := bytes.Count(pdf, []byte("/Type /Page ")); got != rec.print.pages {
				t.Errorf("%s: %d page objects, want %d", name, got, rec.print.pages)
			}
			seen := map[string]int{}
			for _, p := range drawn {
				for _, s := range p.texts {
					seen[s]++
				}
			}
			checked := 0
			for k, c := range rec.cells {
				if c.value == nil || rec.covered[k] {
					continue
				}
				ref, _ := excelize.CoordinatesToCellName(k.col, k.row)
				want, err := xl.GetCellValue(name, ref)
				if err != nil {
					t.Fatal(err)
				}
				want = strings.TrimSpace(want)
				got, _ := formatCell(c.value, c.f)
				if got != want {
					t.Errorf("%s!%s: PDF prints %q, workbook shows %q", name, ref, got, want)
				}
				if want != "" && seen[want] == 0 {
					t.Errorf("%s!%s: %q never drawn", name, ref, want)
				}
				checked++
			}
			if checked < 50 {
				t.Errorf("%s: only %d cells checked", name, checked)
			}
		}
		xl.Close()
		b.Close()
	}
}

// With poppler installed, the PDF's own text layer carries the totals too.
func TestPDFTextLayer(t *testing.T) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed")
	}
	b, names := buildTestBook(t, English)
	defer b.Close()
	pdf, err := b.PDF("t", names[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s.pdf")
	os.WriteFile(path, pdf, 0o644)
	out, err := exec.Command(bin, "-layout", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, g := range testReport(English).Groups {
		for _, l := range g.Lines {
			total += l.Amount
		}
	}
	for _, want := range []string{money(total), "Grand total", "Watanya Trips Report", "Page 1 of 1"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("text layer lacks %q", want)
		}
	}
}

func TestRangeLabel(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	for _, c := range []struct {
		lang       Lang
		a, b, want string
	}{
		{English, "2026-09-01", "2026-09-30", "September 2026"},
		{English, "2026-02-01", "2026-02-28", "February 2026"},
		{English, "2026-09-01", "2026-09-15", "1–15 Sep 2026"},
		{English, "2026-08-20", "2026-09-05", "20 Aug – 5 Sep 2026"},
		{English, "2026-08-01", "2026-09-30", "August 2026 – September 2026"},
		{English, "2025-12-28", "2026-01-03", "28 Dec 2025 – 3 Jan 2026"},
		{Arabic, "2026-09-01", "2026-09-30", "سبتمبر 2026"},
		{Arabic, "2026-09-01", "2026-09-15", "من 1 إلى 15 سبتمبر 2026"},
	} {
		if got := RangeLabel(c.lang, d(c.a), d(c.b)); got != c.want {
			t.Errorf("RangeLabel(%s, %s) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	for _, c := range []struct {
		v          float64
		code, want string
	}{
		{1234567, padThou, "1,234,567"},
		{4290.4, padMoney, "4,290.40"},
		{0.53067, padPct, "53.1%"},
		{14, "0", "14"},
		{2.5, "", "2.5"},
		{-1234.5, padMoney, "-1,234.50"},
	} {
		if got := formatNumber(c.v, c.code); got != c.want {
			t.Errorf("formatNumber(%v, %q) = %q, want %q", c.v, c.code, got, c.want)
		}
	}
}
