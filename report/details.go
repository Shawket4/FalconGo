package report

import (
	"errors"
	"fmt"
	"math"

	"github.com/xuri/excelize/v2"
)

var detailCols = []colSpec{
	{"A", 4.5},  // #
	{"B", 12},   // date
	{"C", 13},   // receipt
	{"D", 27},   // driver
	{"E", 13.5}, // plate
	{"F", 10},   // capacity
	{"G", 11},   // distance
	{"H", 8.5},  // fee rate
	{"I", 11.5}, // cost
}

const (
	hDetHeader  = 24.0 // column labels, repeated on every page
	hRouteBand  = 22.0 // route band: number, drop-off point, terminal
	hTrip       = 19.0 // one trip
	hRouteTotal = 20.0 // route total
	hRouteGap   = 10.0 // space after a route

	minSplitTrips = 3 // a route split across pages keeps at least this many trips on each side
)

// WriteDetailsSheet writes the styled, print-ready Trip Details sheet into f
// and returns the number of printed pages. Call it on a new, empty sheet; the
// sheet is created if it does not exist.
func WriteDetailsSheet(f *excelize.File, sheet string, rep Report) (int, error) {
	rep = rep.withDefaults()
	var secs []TripSection
	trips := 0
	for i, s := range rep.Sections {
		if len(s.Trips) == 0 {
			continue
		}
		if s.No == 0 {
			s.No = i + 1
		}
		secs = append(secs, s)
		trips += len(s.Trips)
	}
	if len(secs) == 0 {
		return 0, errors.New("details: no trips")
	}
	w, err := newWriter(f, sheet, rep.Lang)
	if err != nil {
		return 0, err
	}
	t := rep.Lang.texts()
	scale := w.columns(detailCols)
	r := w.titleBlock(rep, t.detailsTitle, fmt.Sprintf(t.stats, digits(int64(trips)), len(secs)))
	hdrRow := w.detailsHeader(r, t)
	r = hdrRow + 1

	// --- plan the pages ----------------------------------------------------------
	// A route that fits on a page is one block. A longer route becomes its
	// opening block plus one block per further trip, so the planner can pick
	// the split; a page that starts mid-route repeats the route band.
	body := bodyHeight(scale)
	capFirst, capRest := body-w.frontHeight(hdrRow), body-hDetHeader
	tail := hRouteTotal + hRouteGap
	type span struct {
		sec, from, to int
	}
	var spans []span
	var blocks []planBlock
	for i, s := range secs {
		n := len(s.Trips)
		whole := hRouteBand + float64(n)*hTrip + tail
		// The first route also splits when it cannot fit under the title block.
		fits := whole <= capRest && (len(blocks) > 0 || whole <= capFirst)
		if fits || n < 2*minSplitTrips {
			spans = append(spans, span{i, 0, n})
			blocks = append(blocks, planBlock{h: whole, breakable: true})
			continue
		}
		spans = append(spans, span{i, 0, minSplitTrips})
		blocks = append(blocks, planBlock{h: hRouteBand + minSplitTrips*hTrip, breakable: true})
		for k := minSplitTrips; k < n; k++ {
			b := planBlock{h: hTrip, breakable: n-k >= minSplitTrips, carry: hRouteBand}
			if k == n-1 {
				b.h += tail
			}
			spans = append(spans, span{i, k, k + 1})
			blocks = append(blocks, b)
		}
	}
	starts, err := planPages(blocks, capFirst, capRest)
	if err != nil {
		return 0, err
	}
	newPage := map[int]bool{}
	for _, s := range starts[1:] {
		newPage[s] = true
	}

	// --- rows ------------------------------------------------------------------
	for bi, sp := range spans {
		s := secs[sp.sec]
		switch {
		case newPage[bi]:
			w.pageBreak(r)
			r = w.routeBand(r, t, s, sp.from > 0)
		case sp.from == 0:
			r = w.routeBand(r, t, s, false)
		}
		r = w.tripRows(r, s, sp.from, sp.to)
		if sp.to == len(s.Trips) {
			r = w.routeTotal(r, t, s)
		}
	}

	w.printSetup(rep, printOpts{
		scale: scale, titleRow: hdrRow, lastRow: r - 2, pages: len(starts), // r-1 is the trailing gap
		running: joinDot(rep.Title, t.detailsTitle, rep.Period),
	})
	if w.err != nil {
		return 0, w.err
	}
	return len(starts), nil
}

// detailsHeader writes the column labels (repeated at the top of every
// printed page) and returns their row number.
func (w *sheetWriter) detailsHeader(r int, t *texts) int {
	h := txt(8.5).bold().color(clrWhite).fill(clrNavy)
	w.height(r, hDetHeader)
	labels := []struct {
		col, text string
		f         cellFmt
	}{
		{"A", t.hNo, h.center()},
		{"B", t.hDate, h.center()},
		{"C", t.hReceipt, h.center()},
		{"D", t.hDriver, h.right(1)},
		{"E", t.hPlate, h.center()},
		{"F", t.hCapacity, h.num(padText)},
		{"G", t.hDistanceKm, h.num(padText)},
		{"H", t.hFeeRate, h.num(padText)},
		{"I", t.hCost, h.num(padText)},
	}
	for _, l := range labels {
		w.cell(ref(l.col, r), "", l.text, l.f.shrink())
	}
	return r
}

// routeBand writes the route's heading: number, drop-off point, terminal and
// trip count ("continued" when the route carries over from the previous page).
func (w *sheetWriter) routeBand(r int, t *texts, s TripSection, continued bool) int {
	band := txt(11).bold().color(clrNavy).fill(clrHead2)
	count := t.tripCount(len(s.Trips))
	if continued {
		count = joinDot(t.cont, count)
	}
	w.height(r, hRouteBand)
	w.cell(ref("A", r), "", s.No, txt(10).bold().color(clrWhite).fill(clrNavy).center().format(fmtInt))
	w.cell(ref("B", r), ref("D", r), s.DropOff, band.start(1).shrink())
	w.rich(ref("E", r), ref("G", r), []excelize.RichTextRun{
		{Text: t.terminalLabel, Font: &excelize.Font{Family: fontFamily, Size: 9, Color: clrMuted}},
		{Text: s.Terminal, Font: &excelize.Font{Family: fontFamily, Size: 9.5, Bold: true, Color: clrInk}},
	}, band.start(1))
	w.cell(ref("H", r), ref("I", r), count, txt(9).color(clrMuted).fill(clrHead2).end(1).shrink())
	return r + 1
}

// tripRows writes trips [from, to) of a route.
func (w *sheetWriter) tripRows(r int, s TripSection, from, to int) int {
	row := txt(10).bottomRule(clrRule, 1)
	for i := from; i < to; i++ {
		tr := s.Trips[i]
		w.height(r, hTrip)
		w.cell(ref("A", r), "", i+1, row.sz(9).color(clrMuted).center().format(fmtInt))
		var date any
		if !tr.Date.IsZero() {
			date = tr.Date
		}
		w.cell(ref("B", r), "", date, row.center().custom(fmtDate))
		w.cell(ref("C", r), "", tr.Receipt, row.center().shrink())
		w.cell(ref("D", r), "", tr.Driver, row.right(1).shrink())
		w.cell(ref("E", r), "", tr.Plate, row.center().shrink())
		w.cell(ref("F", r), "", math.Round(tr.Capacity), row.num(padThou))
		w.cell(ref("G", r), "", tr.Distance, row.num(padThou))
		w.cell(ref("H", r), "", tr.FeeRate, row.num(padMoney))
		w.cell(ref("I", r), "", round2(tr.Cost), row.num(padMoney))
		r++
	}
	return r
}

// routeTotal writes the route's total row and the gap after it.
func (w *sheetWriter) routeTotal(r int, t *texts, s TripSection) int {
	var capacity, distance, cost float64
	for _, tr := range s.Trips {
		capacity += tr.Capacity
		distance += tr.Distance
		cost += tr.Cost
	}
	tot := txt(10).bold().color(clrNavy).fill(clrTint)
	w.height(r, hRouteTotal)
	w.cell(ref("A", r), ref("E", r), t.total, tot.sz(9).color(clrAccent).start(1))
	w.cell(ref("F", r), "", math.Round(capacity), tot.num(padThou))
	w.cell(ref("G", r), "", distance, tot.num(padThou))
	w.paint(ref("H", r), ref("H", r), tot)
	w.cell(ref("I", r), "", round2(cost), tot.num(padMoney))
	w.height(r+1, hRouteGap)
	return r + 2
}
