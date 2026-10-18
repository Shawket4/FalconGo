package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func testTable(lang Lang) Table {
	t := Table{
		Lang: lang, Company: "Apex Transport", Title: "Trips", Subtitle: "Trip records",
		Period: "September 2026", PreparedOn: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC),
		Columns: []Column{
			{Header: "Receipt", Width: 11, Kind: Center},
			{Header: "Date", Width: 11, Kind: Date},
			{Header: "Drop-off", Width: 18, Kind: Text},
			{Header: "Capacity (L)", Width: 11, Kind: Int, Total: true},
			{Header: "Distance", Width: 10, Kind: Decimal, Total: true},
			{Header: "Fee", Width: 11, Kind: Money, Total: true},
			{Header: "Stamped", Width: 8, Kind: Flag},
			{Header: "Received", Width: 15, Kind: DateTime},
			{Header: "✓", Width: 5, Kind: Tick},
			{Header: "Double", Width: 10, Kind: Int, Total: true},
		},
		Cards:      []Card{{Label: "TRIPS", Value: "120"}, {Label: "FEES", Value: "1,234.50", Dark: true}},
		TotalLabel: "Total", TotalMode: TotalSubtotal,
		Footnotes:  []string{"A note under the table"},
		AutoFilter: true, FreezeHeader: true,
	}
	for b := 1; b <= 4; b++ {
		band := Band{No: b, Title: fmt.Sprintf("طنطا %d", b), Note: "عدد الرحلات"}
		n := 5 + b*12 // the last band runs over a page
		for k := 0; k < n; k++ {
			band.Rows = append(band.Rows, []any{
				fmt.Sprint(610000 + b*100 + k), time.Date(2026, 9, 1+k%30, 0, 0, 0, 0, time.UTC), "العياط",
				20000 + k*1000, 120.5 + float64(k), 1234.567 + float64(k),
				YesNo{Text: map[bool]string{true: "Yes", false: "No"}[k%2 == 0], On: k%2 == 0},
				time.Date(2026, 9, 2, 14, 5, 0, 0, time.UTC), nil,
				Formula{Expr: func(row int) string { return fmt.Sprintf("D%d*2", row) }, Value: (20000 + k*1000) * 2},
			})
		}
		t.Bands = append(t.Bands, band)
	}
	return t
}

// A table sheet's PDF prints every cell as the workbook displays it, on the
// pages the workbook plans; formulas print the value they were written with.
func TestTablePDFMatchesWorkbook(t *testing.T) {
	for _, lang := range []Lang{English, Arabic} {
		b := NewBook()
		lay, err := b.Table("Trips", testTable(lang))
		if err != nil {
			t.Fatal(err)
		}
		if lay.Pages < 2 {
			t.Fatalf("want a multi-page table, got %d", lay.Pages)
		}
		data, _ := b.XLSX()
		xl, _ := excelize.OpenReader(bytes.NewReader(data))
		rec := b.sheet["Trips"]
		_, drawn, err := renderPDFDrawn("t", []*sheetRecord{rec})
		if err != nil {
			t.Fatal(err)
		}
		if len(drawn) != lay.Pages {
			t.Errorf("PDF pages %d, planned %d", len(drawn), lay.Pages)
		}
		seen := map[string]bool{}
		for _, p := range drawn {
			for _, s := range p.texts {
				seen[s] = true
			}
		}
		formulas := 0
		for k, c := range rec.cells {
			if c.value == nil || rec.covered[k] {
				continue
			}
			ref, _ := excelize.CoordinatesToCellName(k.col, k.row)
			got, _ := formatCell(c.value, c.f)
			if fv, ok := c.value.(formulaValue); ok {
				formulas++
				formula, _ := xl.GetCellFormula("Trips", ref)
				if formula != fv.expr {
					t.Errorf("%s: formula %q, recorded %q", ref, formula, fv.expr)
				}
			} else {
				want, _ := xl.GetCellValue("Trips", ref)
				if strings.TrimSpace(want) != got {
					t.Errorf("%s: PDF %q, workbook %q", ref, got, want)
				}
			}
			if got != "" && !seen[got] {
				t.Errorf("%s: %q never drawn", ref, got)
			}
		}
		if formulas < 60 {
			t.Errorf("only %d formula cells", formulas)
		}
		// The totals row sums the data rows.
		var cap int
		for _, bd := range testTable(lang).Bands {
			for _, r := range bd.Rows {
				cap += r[3].(int)
			}
		}
		f, _ := xl.GetCellFormula("Trips", fmt.Sprintf("D%d", lay.TotalRow))
		if f != fmt.Sprintf("SUBTOTAL(109,D%d:D%d)", lay.FirstRow, lay.LastRow) {
			t.Errorf("totals formula %q", f)
		}
		if !seen[FormatCount(int64(cap))] {
			t.Errorf("total capacity %s not printed", FormatCount(int64(cap)))
		}
		xl.Close()
		b.Close()
	}
}
