// Package report writes the print-ready sheets of the Watanya trips report
// with excelize (v2.7+, Go 1.21+):
//
//   - WriteSummarySheet: title block, KPI cards, a by-terminal table and the
//     summary by drop-off point with subtotals and a grand total.
//   - WriteDetailsSheet: one section per route (terminal → drop-off point)
//     listing its trips, with a total per route.
//
// Both sheets print on A4 portrait. The code places the page breaks itself:
// a drop-off point (Summary) or a route (Trip Details) is never split across
// pages, except a route longer than a whole page, which continues on the next
// page under a repeated header. Pages are balanced so they end at about the
// same height.
//
// Set Report.Lang to Arabic for Arabic headers and labels and a right-to-left
// layout. The Cairo font must be installed wherever the file is opened or
// printed.
package report

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Lang selects the language of the headers and labels and the sheet direction.
type Lang int

const (
	English Lang = iota // left-to-right
	Arabic              // right-to-left
)

// Report holds what both sheets need: Groups feed the Summary sheet and
// Sections feed the Trip Details sheet.
type Report struct {
	Lang       Lang
	Company    string    // e.g. "Apex Transport"
	Title      string    // e.g. "Watanya Trips Report" or "تقرير رحلات وطنية"
	Period     string    // e.g. "September 2026"; derived from the trip dates when empty
	FirstTrip  time.Time // first and last trip date; derived from Sections when zero
	LastTrip   time.Time
	PreparedOn time.Time // zero means today

	Groups   []SummaryGroup // Summary sheet
	Sections []TripSection  // Trip Details sheet

	// SubtotalSingleLines also prints a Summary subtotal under drop-off points
	// that have a single route (skipped by default: it would repeat the line).
	SubtotalSingleLines bool
}

// SummaryGroup is one drop-off point with its routes.
type SummaryGroup struct {
	DropOff string
	Lines   []SummaryLine
}

// SummaryLine is one route (terminal → drop-off point) in the Summary.
type SummaryLine struct {
	No       int // line number; Trip Details sections show the same number
	Terminal string
	Trips    int
	Capacity float64 // litres
	Distance float64 // km
	FeeIndex int     // distance band
	FeeRate  float64 // EGP per 1,000 L
	Amount   float64 // EGP
}

// TripSection is one route in Trip Details.
type TripSection struct {
	No       int // matches SummaryLine.No; 0 numbers the sections in order
	Terminal string
	DropOff  string
	Trips    []Trip
}

// Trip is one delivery.
type Trip struct {
	Date     time.Time
	Receipt  string
	Driver   string
	Plate    string
	Capacity float64 // litres
	Distance float64 // km
	FeeRate  float64 // EGP per 1,000 L
	Cost     float64 // EGP
}

// withDefaults fills the derived fields on a copy of the report.
func (rep Report) withDefaults() Report {
	if rep.FirstTrip.IsZero() && rep.LastTrip.IsZero() {
		for _, s := range rep.Sections {
			for _, t := range s.Trips {
				if t.Date.IsZero() {
					continue
				}
				if rep.FirstTrip.IsZero() || t.Date.Before(rep.FirstTrip) {
					rep.FirstTrip = t.Date
				}
				if t.Date.After(rep.LastTrip) {
					rep.LastTrip = t.Date
				}
			}
		}
	}
	if rep.Period == "" && !rep.FirstTrip.IsZero() {
		rep.Period = monthYear(rep.Lang, rep.FirstTrip)
		if !rep.LastTrip.IsZero() && monthYear(rep.Lang, rep.LastTrip) != rep.Period {
			rep.Period += " – " + monthYear(rep.Lang, rep.LastTrip)
		}
	}
	if rep.PreparedOn.IsZero() {
		rep.PreparedOn = time.Now()
	}
	return rep
}

// ---------------------------------------------------------------------------
// Look & page geometry

const (
	fontFamily = "Cairo"

	clrNavy   = "1D3B4F" // headers, totals, title
	clrAccent = "2C6E86" // company line, total labels
	clrBar    = "4A90A4" // KPI accent bars
	clrInk    = "1F2A33" // body text
	clrMuted  = "6B7A86" // secondary text
	clrSoft   = "B9CCD6" // secondary text on navy
	clrRule   = "D5DDE3" // hairlines
	clrTint   = "F1F5F7" // KPI cards, total rows
	clrHead2  = "E6EDF1" // light header bands
	clrWhite  = "FFFFFF"

	pageW, pageH       = 595.28, 841.89 // A4 in points
	marginSide         = 0.4            // inches
	marginTopBottom    = 0.7            // inches
	marginHeaderFooter = 0.3            // inches
	widthSlack         = 0.93           // spare width: LibreOffice measures columns ~6% wider than Excel
	heightSafety       = 0.95           // spare height so Excel never inserts an automatic break
)

// Built-in Excel number formats (left-aligned or centred cells).
const (
	fmtInt = 1 // 0
)

// Right-aligned numbers (and the headers above them) take their right padding
// from the number format ("_)" = a space as wide as ")") rather than from cell
// indent, which LibreOffice drops on numbers in some rows. Numbers stay
// right-aligned in both languages so decimals line up.
const (
	padThou  = "#,##0_)"
	padMoney = "#,##0.00_)"
	padPct   = "0.0%_)"
	padText  = "@_)"
	fmtDate  = `dd\/mm\/yyyy`
)

// colSpec is a column letter and its width in Excel character units
// (default Calibri 11 font: 7 px per unit).
type colSpec struct {
	col   string
	width float64
}

// ---------------------------------------------------------------------------
// Page planning

// planBlock is a unit the page planner places in order.
type planBlock struct {
	h         float64 // height in points
	breakable bool    // a page may start with this block
	carry     float64 // extra height when a page starts here (a repeated heading)
}

// planPages places blocks over the fewest pages that hold them, then spreads
// them so every page ends at about the same height (minimises the sum of
// squared free space per page). Page 1 has capFirst points, later pages
// capRest. It returns the index of the first block on each page.
//
// One pass of dynamic programming over (pages, cost) compared in that order:
// both add up page by page, so the best plan for blocks [0, j) extends a best
// plan for some [0, i).
func planPages(blocks []planBlock, capFirst, capRest float64) ([]int, error) {
	n := len(blocks)
	pre := make([]float64, n+1)
	for i, b := range blocks {
		pre[i+1] = pre[i] + b.h
	}
	type plan struct {
		pages int
		cost  float64
		from  int
	}
	const none = math.MaxInt
	dp := make([]plan, n+1)
	for j := 1; j <= n; j++ {
		dp[j] = plan{pages: none}
	}
	capMax := max(capFirst, capRest)
	for j := 1; j <= n; j++ {
		for i := j - 1; i >= 0; i-- {
			seg := pre[j] - pre[i]
			if seg > capMax {
				break // earlier starts only add height
			}
			if dp[i].pages == none || (i > 0 && !blocks[i].breakable) {
				continue
			}
			limit := capFirst // a page starting at block 0 is page 1
			if i > 0 {
				limit = capRest
				seg += blocks[i].carry
			}
			if seg > limit {
				continue
			}
			free := limit - seg
			c := plan{dp[i].pages + 1, dp[i].cost + free*free, i}
			if c.pages < dp[j].pages || (c.pages == dp[j].pages && c.cost < dp[j].cost) {
				dp[j] = c
			}
		}
	}
	if dp[n].pages == none {
		return nil, errors.New("report: a block does not fit on one page")
	}
	starts := make([]int, dp[n].pages)
	for k, j := len(starts)-1, n; k >= 0; k-- {
		starts[k] = dp[j].from
		j = dp[j].from
	}
	return starts, nil
}

// bodyHeight is the printable height of a page in sheet points at scale.
func bodyHeight(scale int) float64 {
	return (pageH - 2*marginTopBottom*72) / (float64(scale) / 100) * heightSafety
}

// ---------------------------------------------------------------------------
// Cell formats

// cellFmt describes a cell style; identical formats share one excelize style.
// Horizontal alignment may be "start"/"end" (left/right in English, mirrored
// in Arabic) or a physical "left"/"right"/"center".
type cellFmt struct {
	size          float64
	isBold        bool
	fontColor     string
	bg            string
	h, v          string
	indent        int
	numFmt        int
	customFmt     string
	top, bottom   string // border colours, "" = none
	topW, bottomW int    // border styles: 1 thin, 2 medium
	side          string // left and right border colour
	sideW         int
	shrinkFit     bool
}

func txt(size float64) cellFmt { return cellFmt{size: size, fontColor: clrInk, v: "center"} }

func (c cellFmt) sz(s float64) cellFmt               { c.size = s; return c }
func (c cellFmt) bold() cellFmt                      { c.isBold = true; return c }
func (c cellFmt) color(x string) cellFmt             { c.fontColor = x; return c }
func (c cellFmt) fill(x string) cellFmt              { c.bg = x; return c }
func (c cellFmt) start(indent int) cellFmt           { c.h, c.indent = "start", indent; return c }
func (c cellFmt) end(indent int) cellFmt             { c.h, c.indent = "end", indent; return c }
func (c cellFmt) right(indent int) cellFmt           { c.h, c.indent = "right", indent; return c }
func (c cellFmt) center() cellFmt                    { c.h, c.indent = "center", 0; return c }
func (c cellFmt) valign(v string) cellFmt            { c.v = v; return c }
func (c cellFmt) format(id int) cellFmt              { c.numFmt = id; return c }
func (c cellFmt) custom(f string) cellFmt            { c.customFmt = f; return c }
func (c cellFmt) num(f string) cellFmt               { c.h, c.indent, c.customFmt = "right", 0, f; return c }
func (c cellFmt) shrink() cellFmt                    { c.shrinkFit = true; return c }
func (c cellFmt) topRule(x string, w int) cellFmt    { c.top, c.topW = x, w; return c }
func (c cellFmt) bottomRule(x string, w int) cellFmt { c.bottom, c.bottomW = x, w; return c }
func (c cellFmt) sideRules(x string, w int) cellFmt  { c.side, c.sideW = x, w; return c }

// ---------------------------------------------------------------------------
// Sheet writer (records the first error; later calls become no-ops)

type sheetWriter struct {
	f       *excelize.File
	sheet   string
	rtl     bool
	styles  map[cellFmt]int
	heights map[int]float64
	err     error
}

func newWriter(f *excelize.File, sheet string, lang Lang) (*sheetWriter, error) {
	if idx, _ := f.GetSheetIndex(sheet); idx < 0 {
		if _, err := f.NewSheet(sheet); err != nil {
			return nil, err
		}
	}
	return &sheetWriter{
		f: f, sheet: sheet, rtl: lang == Arabic,
		styles: map[cellFmt]int{}, heights: map[int]float64{},
	}, nil
}

func (w *sheetWriter) styleID(c cellFmt) int {
	if id, ok := w.styles[c]; ok {
		return id
	}
	h := c.h
	switch {
	case h == "start" && w.rtl, h == "end" && !w.rtl:
		h = "right"
	case h == "start", h == "end":
		h = "left"
	}
	st := &excelize.Style{
		Font:      &excelize.Font{Family: fontFamily, Size: c.size, Bold: c.isBold, Color: c.fontColor},
		Alignment: &excelize.Alignment{Horizontal: h, Vertical: c.v, Indent: c.indent, ShrinkToFit: c.shrinkFit},
		NumFmt:    c.numFmt,
	}
	if c.customFmt != "" {
		custom := c.customFmt
		st.CustomNumFmt = &custom
	}
	if c.bg != "" {
		st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{c.bg}}
	}
	if c.top != "" {
		st.Border = append(st.Border, excelize.Border{Type: "top", Color: c.top, Style: c.topW})
	}
	if c.bottom != "" {
		st.Border = append(st.Border, excelize.Border{Type: "bottom", Color: c.bottom, Style: c.bottomW})
	}
	if c.side != "" {
		st.Border = append(st.Border,
			excelize.Border{Type: "left", Color: c.side, Style: c.sideW},
			excelize.Border{Type: "right", Color: c.side, Style: c.sideW})
	}
	id, err := w.f.NewStyle(st)
	if err != nil && w.err == nil {
		w.err = err
	}
	w.styles[c] = id
	return id
}

// cell writes v (nil = leave empty) into from, merging from:to when to is set
// and differs, and styles the whole range.
func (w *sheetWriter) cell(from, to string, v any, c cellFmt) {
	if w.err != nil {
		return
	}
	if to == "" {
		to = from
	}
	if from != to {
		if w.err = w.f.MergeCell(w.sheet, from, to); w.err != nil {
			return
		}
	}
	if v != nil {
		if w.err = w.f.SetCellValue(w.sheet, from, v); w.err != nil {
			return
		}
	}
	w.paint(from, to, c)
}

// rich writes rich text (runs with their own fonts) into from, merging
// from:to, and styles the range (alignment, fill, borders).
func (w *sheetWriter) rich(from, to string, runs []excelize.RichTextRun, c cellFmt) {
	if w.err != nil {
		return
	}
	if from != to {
		if w.err = w.f.MergeCell(w.sheet, from, to); w.err != nil {
			return
		}
	}
	if w.err = w.f.SetCellRichText(w.sheet, from, runs); w.err != nil {
		return
	}
	w.paint(from, to, c)
}

// paint styles a range without merging it.
func (w *sheetWriter) paint(from, to string, c cellFmt) {
	id := w.styleID(c)
	if w.err == nil {
		w.err = w.f.SetCellStyle(w.sheet, from, to, id)
	}
}

func (w *sheetWriter) height(row int, h float64) {
	w.heights[row] = h
	if w.err == nil {
		w.err = w.f.SetRowHeight(w.sheet, row, h)
	}
}

// columns sets the column widths and returns the print scale (percent) that
// fits them across an A4 portrait page.
func (w *sheetWriter) columns(cols []colSpec) int {
	var px float64
	for _, c := range cols {
		if w.err == nil {
			w.err = w.f.SetColWidth(w.sheet, c.col, c.col, c.width)
		}
		px += math.Floor(c.width*7 + 0.5)
	}
	scale := int(math.Floor((pageW - 2*marginSide*72) * widthSlack / (px * 0.75) * 100))
	return max(10, min(100, scale))
}

// pageBreak starts a new printed page at row.
func (w *sheetWriter) pageBreak(row int) {
	if w.err == nil {
		w.err = w.f.InsertPageBreak(w.sheet, ref("A", row))
	}
}

// frontHeight sums the heights of rows 1..last.
func (w *sheetWriter) frontHeight(last int) float64 {
	var h float64
	for row := 1; row <= last; row++ {
		h += w.heights[row]
	}
	return h
}

// ---------------------------------------------------------------------------
// Shared blocks

// titleBlock writes rows 1–6 (company and preparation date, title, subtitle
// with the period and a note, accent rule) and returns the next free row.
func (w *sheetWriter) titleBlock(rep Report, subtitle, note string) int {
	t := rep.Lang.texts()
	w.height(1, 16)
	w.cell("A1", "F1", strings.ToUpper(rep.Company), txt(9).bold().color(clrAccent).start(0))
	w.cell("G1", "I1", fmt.Sprintf(t.prepared, formatDate(rep.Lang, rep.PreparedOn)), txt(8.5).color(clrMuted).end(0))

	w.height(2, 34)
	w.cell("A2", "I2", rep.Title, txt(22).bold().color(clrNavy).start(0))

	w.height(3, 20)
	w.cell("A3", "F3", joinDot(subtitle, rep.Period), txt(11).color(clrMuted).start(0))
	if note != "" {
		w.cell("G3", "I3", note, txt(9).color(clrMuted).end(0).shrink())
	}

	w.height(4, 6)
	w.height(5, 2.25) // accent rule
	w.paint("A5", "I5", txt(10).fill(clrNavy))
	w.height(6, 14)
	return 7
}

type printOpts struct {
	scale    int
	titleRow int // row repeated at the top of every page; 0 = none
	lastRow  int
	pages    int
	running  string // end-side text of the running header (pages 2+)
}

func (w *sheetWriter) printSetup(rep Report, o printOpts) {
	if w.err != nil {
		return
	}
	f, sheet, t := w.f, w.sheet, rep.Lang.texts()
	size, orientation, adjust, first := 9, "portrait", uint(o.scale), uint(1) // 9 = A4

	// Header/footer sections are physical (&L = left), so mirror them in RTL.
	hf := func(startText, endText string) string {
		if w.rtl {
			startText, endText = endText, startText
		}
		return "&L" + startText + "&R" + endText
	}
	style := func(bold bool, size int, color string) string {
		weight := "Regular"
		if bold {
			weight = "Bold"
		}
		return fmt.Sprintf(`&"%s,%s"&%d&K%s`, fontFamily, weight, size, color)
	}
	running := hf(
		style(true, 9, clrAccent)+hfEscape(strings.ToUpper(rep.Company)),
		style(false, 9, clrMuted)+hfEscape(o.running))
	// "of N" is written as a number: &N would count every sheet when the
	// whole workbook is printed.
	footer := hf(
		style(false, 8, clrMuted)+hfEscape(fmt.Sprintf(t.prepared, formatDate(rep.Lang, rep.PreparedOn))),
		style(false, 8, clrMuted)+fmt.Sprintf(t.pageOf, o.pages))

	steps := []func() error{
		func() error {
			return f.SetPageLayout(sheet, &excelize.PageLayoutOptions{
				Size: &size, Orientation: &orientation, AdjustTo: &adjust, FirstPageNumber: &first,
			})
		},
		func() error {
			// Fit-to-page must be off, otherwise Excel ignores the manual breaks.
			return f.SetSheetProps(sheet, &excelize.SheetPropsOptions{FitToPage: ptr(false)})
		},
		func() error {
			return f.SetPageMargins(sheet, &excelize.PageLayoutMarginsOptions{
				Left: ptr(marginSide), Right: ptr(marginSide),
				Top: ptr(marginTopBottom), Bottom: ptr(marginTopBottom),
				Header: ptr(marginHeaderFooter), Footer: ptr(marginHeaderFooter),
				Horizontally: ptr(true),
			})
		},
		func() error {
			return f.SetHeaderFooter(sheet, &excelize.HeaderFooterOptions{
				DifferentFirst: true, // page 1 has the title block instead of the running header
				OddHeader:      running,
				OddFooter:      footer,
				FirstFooter:    footer,
			})
		},
		func() error {
			return f.SetSheetView(sheet, 0, &excelize.ViewOptions{ShowGridLines: ptr(false), RightToLeft: ptr(w.rtl)})
		},
		func() error {
			q := quoteSheet(sheet)
			names := []excelize.DefinedName{
				{Name: "_xlnm.Print_Area", RefersTo: fmt.Sprintf("%s!$A$1:$I$%d", q, o.lastRow), Scope: sheet},
			}
			if o.titleRow > 0 {
				names = append(names, excelize.DefinedName{
					Name: "_xlnm.Print_Titles", RefersTo: fmt.Sprintf("%s!$%d:$%d", q, o.titleRow, o.titleRow), Scope: sheet,
				})
			}
			for _, dn := range names {
				_ = f.DeleteDefinedName(&excelize.DefinedName{Name: dn.Name, Scope: sheet}) // re-runs
				if err := f.SetDefinedName(&dn); err != nil {
					return err
				}
			}
			return nil
		},
	}
	for _, step := range steps {
		if w.err = step(); w.err != nil {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Small helpers

func ref(col string, row int) string { return col + strconv.Itoa(row) }

func ptr[T any](v T) *T { return &v }

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// quoteSheet quotes a sheet name for use in a formula reference.
func quoteSheet(name string) string { return "'" + strings.ReplaceAll(name, "'", "''") + "'" }

// hfEscape escapes text for an Excel header/footer string.
func hfEscape(s string) string { return strings.ReplaceAll(s, "&", "&&") }

// joinDot joins the non-empty parts with a spaced middle dot.
func joinDot(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "  ·  ")
}

// digits formats an integer with thousands separators: 1234567 → "1,234,567".
func digits(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return sign + b.String()
}

// money formats an amount with separators and two decimals: 4290.4 → "4,290.40".
func money(x float64) string {
	cents := int64(math.Round(x * 100))
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%s.%02d", sign, digits(cents/100), cents%100)
}
