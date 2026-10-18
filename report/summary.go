package report

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/xuri/excelize/v2"
)

// Widths fit the longest real names and month totals in the tens of millions,
// measured with Tahoma (the workbook) and Cairo (the PDF); see
// report.Book.CheckFit and the fit tests.
var summaryCols = []colSpec{
	{"A", 5},    // #
	{"B", 24.5}, // drop-off point
	{"C", 21},   // terminal
	{"D", 9},    // trips
	{"E", 13.5}, // capacity
	{"F", 10.5}, // distance
	{"G", 7.5},  // fee index
	{"H", 10},   // fee rate
	{"I", 15.5}, // amount
}

const (
	hSumLine   = 20.0 // detail line
	hSumSub    = 20.0 // subtotal row
	hSumHeader = 26.0 // repeated detail header
	hSumGap    = 6.0  // gap above the grand total
	hSumGrand  = 26.0 // grand total
)

// WriteSummarySheet writes the styled, print-ready Summary sheet into f and
// returns the number of printed pages. Call it on a new, empty sheet; the
// sheet is created if it does not exist.
func WriteSummarySheet(f *excelize.File, sheet string, rep Report) (int, error) {
	n, _, err := writeSummarySheet(f, sheet, rep)
	return n, err
}

func writeSummarySheet(f *excelize.File, sheet string, rep Report) (int, *sheetRecord, error) {
	rep = rep.withDefaults()
	var groups []SummaryGroup
	for _, g := range rep.Groups {
		if len(g.Lines) > 0 {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return 0, nil, errors.New("summary: no drop-off groups")
	}
	w, err := newWriter(f, sheet, rep.Lang)
	if err != nil {
		return 0, nil, err
	}
	t := rep.Lang.texts()
	tot := summarize(groups)
	scale := w.columns(summaryCols)

	// --- page 1 front matter -------------------------------------------------
	note := ""
	if !rep.FirstTrip.IsZero() && !rep.LastTrip.IsZero() {
		note = fmt.Sprintf(t.tripsDated, dateRange(rep.Lang, rep.FirstTrip, rep.LastTrip))
	}
	r := w.titleBlock(rep, t.summaryTitle, note)
	r = w.kpiCards(r, t, len(groups), tot)
	r = w.terminalTable(r, t, tot)
	hdrRow := w.summaryHeader(r, t)
	r = hdrRow + 1

	// --- plan the pages --------------------------------------------------------
	blocks := make([]planBlock, len(groups))
	for i, g := range groups {
		blocks[i] = planBlock{h: float64(len(g.Lines)) * hSumLine, breakable: true}
		if len(g.Lines) > 1 || rep.SubtotalSingleLines {
			blocks[i].h += hSumSub
		}
	}
	blocks[len(blocks)-1].h += hSumGap + hSumGrand // the grand total rides with the last block
	body := w.bodyHeight(scale)
	starts, err := planPages(blocks, body-w.frontHeight(hdrRow), body-hSumHeader)
	if err != nil {
		return 0, nil, err
	}
	newPage := map[int]bool{}
	for _, s := range starts[1:] {
		newPage[s] = true
	}

	// --- rows ------------------------------------------------------------------
	for gi, g := range groups {
		if newPage[gi] {
			w.pageBreak(r)
		}
		r = w.dropOffBlock(r, t, g, len(g.Lines) > 1 || rep.SubtotalSingleLines)
	}
	r = w.grandTotal(r, t, tot)

	w.printSetup(rep, printOpts{
		scale: scale, titleRow: hdrRow, lastRow: r - 1, pages: len(starts),
		running: joinDot(rep.Title, rep.Period),
	})
	if w.err != nil {
		return 0, nil, w.err
	}
	return len(starts), w.rec, nil
}

func (w *sheetWriter) kpiCards(r int, t *texts, dropOffs int, tot totals) int {
	avgL, avgAmt := 0.0, 0.0
	if tot.trips > 0 {
		avgL, avgAmt = tot.capacity/float64(tot.trips), tot.amount/float64(tot.trips)
	}
	cards := []struct {
		from, to, label, value, note string
		dark                         bool
	}{
		{"A", "B", t.kTrips, digits(int64(tot.trips)), fmt.Sprintf(t.nRoutes, tot.routes), false},
		{"C", "D", t.kCapacity, digits(int64(math.Round(tot.capacity))), fmt.Sprintf(t.nAvgLitres, digits(int64(math.Round(avgL)))), false},
		{"E", "F", t.kDropOffs, digits(int64(dropOffs)), fmt.Sprintf(t.nTerminals, len(tot.terms)), false},
		{"G", "I", t.kAmount, money(tot.amount), fmt.Sprintf(t.nAvgAmount, money(avgAmt)), true},
	}
	w.height(r, 20)
	w.height(r+1, 30)
	w.height(r+2, 20)
	for _, c := range cards {
		bg, label, value, note := clrTint, clrMuted, clrNavy, clrMuted
		if c.dark {
			bg, label, value, note = clrNavy, clrSoft, clrWhite, clrSoft
		}
		card := txt(8).fill(bg).sideRules(clrWhite, 2) // white gutters between cards
		w.cell(ref(c.from, r), ref(c.to, r), c.label, card.bold().color(label).start(1).valign("bottom").topRule(clrBar, 2))
		w.cell(ref(c.from, r+1), ref(c.to, r+1), c.value, card.sz(18).bold().color(value).start(1))
		w.cell(ref(c.from, r+2), ref(c.to, r+2), c.note, card.sz(8.5).color(note).start(1).valign("top"))
	}
	w.height(r+3, 18)
	return r + 4
}

func (w *sheetWriter) terminalTable(r int, t *texts, tot totals) int {
	w.height(r, 22)
	w.cell(ref("A", r), ref("E", r), t.byTerminal, txt(11).bold().color(clrNavy).start(0))
	r++

	head := txt(8.5).bold().color(clrNavy).fill(clrHead2).bottomRule(clrNavy, 1)
	w.height(r, 22)
	w.cell(ref("A", r), ref("C", r), t.hTerminal, head.right(1))
	w.cell(ref("D", r), "", t.hTrips, head.num(padText))
	w.cell(ref("E", r), "", t.hCapacity, head.num(padText))
	w.cell(ref("F", r), "", t.hRoutes, head.num(padText))
	w.cell(ref("G", r), ref("H", r), t.hShare, head.num(padText))
	w.cell(ref("I", r), "", t.hAmount, head.num(padText))
	r++

	row := txt(10.5).bottomRule(clrRule, 1)
	for _, s := range tot.terms {
		share := 0.0
		if tot.amount != 0 {
			share = s.amount / tot.amount
		}
		w.height(r, 19)
		w.cell(ref("A", r), ref("C", r), s.name, row.right(1).shrink())
		w.cell(ref("D", r), "", s.trips, row.num(padThou))
		w.cell(ref("E", r), "", math.Round(s.capacity), row.num(padThou))
		w.cell(ref("F", r), "", s.routes, row.num(padThou))
		w.cell(ref("G", r), ref("H", r), share, row.num(padPct))
		w.cell(ref("I", r), "", round2(s.amount), row.num(padMoney))
		r++
	}

	sum := txt(10.5).bold().color(clrNavy).topRule(clrNavy, 1)
	w.height(r, 21)
	w.cell(ref("A", r), ref("C", r), t.total, sum.right(1))
	w.cell(ref("D", r), "", tot.trips, sum.num(padThou))
	w.cell(ref("E", r), "", math.Round(tot.capacity), sum.num(padThou))
	w.cell(ref("F", r), "", tot.routes, sum.num(padThou))
	w.cell(ref("G", r), ref("H", r), 1.0, sum.num(padPct))
	w.cell(ref("I", r), "", round2(tot.amount), sum.num(padMoney))
	r++

	w.height(r, 18)
	return r + 1
}

// summaryHeader writes the section title and the column header row (the row
// that repeats on every printed page) and returns the header row number.
func (w *sheetWriter) summaryHeader(r int, t *texts) int {
	w.height(r, 22)
	w.cell(ref("A", r), ref("E", r), t.detailBy, txt(11).bold().color(clrNavy).start(0))
	w.cell(ref("F", r), ref("I", r), t.unitsNote, txt(8.5).color(clrMuted).end(0).shrink())
	r++

	h := txt(8.5).bold().color(clrWhite).fill(clrNavy)
	w.height(r, hSumHeader)
	labels := []struct {
		col, text string
		f         cellFmt
	}{
		{"A", t.hNo, h.center()},
		{"B", t.hDropOff, h.right(1)},
		{"C", t.hTerminal, h.right(1)},
		{"D", t.hTrips, h.num(padText)},
		{"E", t.hCapacity, h.num(padText)},
		{"F", t.hDistance, h.num(padText)},
		{"G", t.hFeeIdx, h.center()},
		{"H", t.hFeeRate, h.num(padText)},
		{"I", t.hAmount, h.num(padText)},
	}
	for _, l := range labels {
		w.cell(ref(l.col, r), "", l.text, l.f.shrink())
	}
	return r
}

func (w *sheetWriter) dropOffBlock(r int, t *texts, g SummaryGroup, subtotal bool) int {
	for i, l := range g.Lines {
		base := txt(10.5)
		if i == 0 {
			base = base.topRule(clrRule, 1) // hairline between drop-off points
		}
		w.height(r, hSumLine)
		w.cell(ref("A", r), "", l.No, base.sz(9).color(clrMuted).center().format(fmtInt))
		var name any
		if i == 0 {
			name = g.DropOff
		}
		w.cell(ref("B", r), "", name, base.bold().right(1).shrink())
		w.cell(ref("C", r), "", l.Terminal, base.right(1).shrink())
		w.cell(ref("D", r), "", l.Trips, base.num(padThou))
		w.cell(ref("E", r), "", math.Round(l.Capacity), base.num(padThou))
		w.cell(ref("F", r), "", l.Distance, base.num(padThou))
		w.cell(ref("G", r), "", l.FeeIndex, base.center().format(fmtInt))
		w.cell(ref("H", r), "", l.FeeRate, base.num(padMoney))
		w.cell(ref("I", r), "", round2(l.Amount), base.num(padMoney))
		r++
	}
	if !subtotal {
		return r
	}
	var trips int
	var capacity, amount float64
	for _, l := range g.Lines {
		trips += l.Trips
		capacity += l.Capacity
		amount += l.Amount
	}
	s := txt(10.5).bold().color(clrNavy).fill(clrTint)
	w.height(r, hSumSub)
	w.paint(ref("A", r), ref("B", r), txt(10.5))
	w.cell(ref("C", r), "", t.subtotal, s.sz(8.5).color(clrAccent).right(1))
	w.cell(ref("D", r), "", trips, s.num(padThou))
	w.cell(ref("E", r), "", math.Round(capacity), s.num(padThou))
	w.paint(ref("F", r), ref("H", r), s)
	w.cell(ref("I", r), "", round2(amount), s.num(padMoney))
	return r + 1
}

func (w *sheetWriter) grandTotal(r int, t *texts, tot totals) int {
	w.height(r, hSumGap)
	r++
	g := txt(11).bold().color(clrWhite).fill(clrNavy)
	w.height(r, hSumGrand)
	w.cell(ref("A", r), ref("C", r), t.grandTotal, g.start(1))
	w.cell(ref("D", r), "", tot.trips, g.num(padThou))
	w.cell(ref("E", r), "", math.Round(tot.capacity), g.num(padThou))
	w.paint(ref("F", r), ref("H", r), g)
	w.cell(ref("I", r), "", round2(tot.amount), g.num(padMoney))
	return r + 1
}

// ---------------------------------------------------------------------------
// Totals

type termStat struct {
	name             string
	trips, routes    int
	capacity, amount float64
}

type totals struct {
	trips, routes    int
	capacity, amount float64
	terms            []termStat // sorted by amount, largest first
}

func summarize(groups []SummaryGroup) totals {
	var t totals
	idx := map[string]int{}
	for _, g := range groups {
		for _, l := range g.Lines {
			t.trips += l.Trips
			t.routes++
			t.capacity += l.Capacity
			t.amount += l.Amount
			i, ok := idx[l.Terminal]
			if !ok {
				i = len(t.terms)
				idx[l.Terminal] = i
				t.terms = append(t.terms, termStat{name: l.Terminal})
			}
			s := &t.terms[i]
			s.trips += l.Trips
			s.routes++
			s.capacity += l.Capacity
			s.amount += l.Amount
		}
	}
	sort.SliceStable(t.terms, func(a, b int) bool { return t.terms[a].amount > t.terms[b].amount })
	return t
}
