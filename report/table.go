package report

// Table sheets: the report's look for any list — title block, optional
// headline cards, a column header repeated on every printed page, rows
// optionally grouped under numbered bands, a totals row and footnotes, with
// the same page planning, print setup, labels, right-to-left handling and PDF
// rendering as the Watanya sheets. The trips export and the receipt-piles
// workbook are built from it.

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/xuri/excelize/v2"
)

// ColKind sets a column's alignment and number format.
type ColKind int

const (
	Text     ColKind = iota // start-aligned text
	Center                  // centred text
	Int                     // 1,234
	Decimal                 // 1,234.50
	Money                   // 1,234.50
	Date                    // 18/10/2026
	DateTime                // 18/10/2026 14:05
	Flag                    // a YesNo value: coloured yes/no
	Tick                    // an empty box to tick by hand
)

// Column is one table column.
type Column struct {
	Header string
	Width  float64 // Excel character units
	Kind   ColKind
	Total  bool // summed in the totals row
}

// Card is one headline figure above the table.
type Card struct {
	Label, Value, Note string
	Dark               bool
}

// YesNo is a yes/no cell for a Flag column: Text prints, On picks the colour.
type YesNo struct {
	Text string
	On   bool
}

// Formula is a live formula cell. Expr gets the cell's row; Value is what the
// formula evaluates to when the file is written, which the PDF prints.
type Formula struct {
	Expr  func(row int) string
	Value any
}

// Band groups rows under a numbered heading. A band with no Title prints its
// rows with no heading (a plain list).
type Band struct {
	No    int
	Title string
	Note  string
	Rows  [][]any // one value per column; nil = empty
}

// TotalMode picks how the totals row is computed.
type TotalMode int

const (
	TotalStatic   TotalMode = iota // fixed numbers
	TotalSum                       // =SUM(...)
	TotalSubtotal                  // =SUBTOTAL(109,...): follows the autofilter
)

// Table is a whole table sheet.
type Table struct {
	Lang       Lang
	Company    string
	Title      string
	Subtitle   string // under the title, before the period
	Period     string
	Note       string // end side of the subtitle line
	Running    string // running header text on pages 2+; defaults to title · period
	PreparedOn time.Time

	Columns []Column
	Cards   []Card
	Bands   []Band

	TotalLabel string // "" = no totals row
	TotalMode  TotalMode
	Footnotes  []string

	AutoFilter   bool
	FreezeHeader bool
	Landscape    bool
}

// TableLayout says where the rows landed, for formulas in other sheets.
type TableLayout struct {
	HeaderRow, FirstRow, LastRow, TotalRow int
	Pages                                  int
}

const (
	hTblHeader = 24.0
	hTblBand   = 22.0
	hTblRow    = 19.0
	hTblGap    = 8.0
	hTblTotal  = 24.0
	hTblFoot   = 18.0
)

// Table writes a table sheet into the book.
func (b *Book) Table(sheet string, t Table) (TableLayout, error) {
	if err := b.claim(sheet); err != nil {
		return TableLayout{}, err
	}
	lay, rec, err := writeTable(b.File, sheet, t)
	if err == nil {
		b.keep(rec)
	}
	return lay, err
}

func writeTable(f *excelize.File, sheet string, t Table) (TableLayout, *sheetRecord, error) {
	var lay TableLayout
	if len(t.Columns) < 4 {
		return lay, nil, errors.New("table: needs at least four columns")
	}
	rep := Report{Lang: t.Lang, Company: t.Company, Title: t.Title, Period: t.Period, PreparedOn: t.PreparedOn}
	if rep.PreparedOn.IsZero() {
		rep.PreparedOn = time.Now()
	}
	w, err := newWriter(f, sheet, t.Lang)
	if err != nil {
		return lay, nil, err
	}
	w.landscape = t.Landscape
	n := len(t.Columns)
	w.lastCol = colName(n)
	tx := t.Lang.texts()

	specs := make([]colSpec, n)
	for i, c := range t.Columns {
		specs[i] = colSpec{colName(i + 1), c.Width}
	}
	scale := w.columns(specs)

	r := w.titleBlock(rep, t.Subtitle, t.Note)
	if len(t.Cards) > 0 {
		r = w.tableCards(r, t.Cards)
	}
	hdr := r
	w.tableHeader(hdr, t.Columns)
	lay.HeaderRow = hdr
	r = hdr + 1
	lay.FirstRow = r

	// --- plan the pages ----------------------------------------------------
	body := w.bodyHeight(scale)
	capFirst, capRest := body-w.frontHeight(hdr), body-hTblHeader
	type span struct{ band, from, to int }
	var spans []span
	var blocks []planBlock
	for i, bd := range t.Bands {
		rows := len(bd.Rows)
		head := 0.0
		if bd.Title != "" {
			head = hTblBand
		}
		tail := 0.0
		if i < len(t.Bands)-1 && bd.Title != "" {
			tail = hTblGap
		}
		whole := head + float64(rows)*hTblRow + tail
		if bd.Title != "" && (whole <= capRest && (len(blocks) > 0 || whole <= capFirst) || rows < 2*minSplitTrips) {
			spans = append(spans, span{i, 0, rows})
			blocks = append(blocks, planBlock{h: whole, breakable: true})
			continue
		}
		// A plain list, or a band longer than a page: one block per row, a
		// band never split closer than minSplitTrips rows from either end.
		first := 0
		if bd.Title != "" {
			first = minSplitTrips
			spans = append(spans, span{i, 0, first})
			blocks = append(blocks, planBlock{h: head + float64(first)*hTblRow, breakable: true})
		}
		for k := first; k < rows; k++ {
			b := planBlock{h: hTblRow, breakable: true}
			if bd.Title != "" {
				b.breakable = rows-k >= minSplitTrips
				b.carry = hTblBand
			}
			if k == rows-1 {
				b.h += tail
			}
			spans = append(spans, span{i, k, k + 1})
			blocks = append(blocks, b)
		}
	}
	tailH := 0.0
	if t.TotalLabel != "" {
		tailH += hTblGap + hTblTotal
	}
	if len(t.Footnotes) > 0 {
		tailH += hTblGap + float64(len(t.Footnotes))*hTblFoot
	}
	if len(blocks) == 0 {
		blocks = append(blocks, planBlock{h: hTblRow, breakable: true})
	}
	blocks[len(blocks)-1].h += tailH
	starts, err := planPages(blocks, capFirst, capRest)
	if err != nil {
		return lay, nil, err
	}
	newPage := map[int]bool{}
	for _, s := range starts[1:] {
		newPage[s] = true
	}

	// --- rows ----------------------------------------------------------------
	sums := make([]float64, n)
	for bi, sp := range spans {
		bd := t.Bands[sp.band]
		if newPage[bi] {
			w.pageBreak(r)
		}
		if bd.Title != "" && (sp.from == 0 || newPage[bi]) {
			r = w.tableBand(r, tx, bd, sp.from > 0)
		}
		for k := sp.from; k < sp.to; k++ {
			w.tableRow(r, t.Columns, bd.Rows[k], sums)
			r++
		}
		if sp.to == len(bd.Rows) && bd.Title != "" && sp.band < len(t.Bands)-1 {
			w.height(r, hTblGap)
			r++
		}
	}
	lay.LastRow = r - 1
	if lay.LastRow < lay.FirstRow {
		lay.LastRow = lay.FirstRow
	}

	if t.TotalLabel != "" {
		w.height(r, hTblGap)
		r++
		lay.TotalRow = r
		w.tableTotal(r, t, lay, sums)
		r++
	}
	if len(t.Footnotes) > 0 {
		w.height(r, hTblGap)
		r++
		for _, note := range t.Footnotes {
			w.height(r, hTblFoot)
			w.cell(ref("A", r), ref(w.lastCol, r), note, txt(8.5).color(clrMuted).start(0).shrink())
			r++
		}
	}

	if t.AutoFilter && lay.LastRow >= lay.FirstRow && w.err == nil {
		w.err = f.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", hdr, w.lastCol, lay.LastRow), nil)
	}
	if t.FreezeHeader && w.err == nil {
		w.err = f.SetPanes(sheet, &excelize.Panes{
			Freeze: true, YSplit: hdr, TopLeftCell: fmt.Sprintf("A%d", hdr+1), ActivePane: "bottomLeft",
		})
	}

	running := t.Running
	if running == "" {
		running = joinDot(t.Title, t.Period)
	}
	w.printSetup(rep, printOpts{scale: scale, titleRow: hdr, lastRow: r - 1, pages: len(starts), running: running})
	if w.err != nil {
		return lay, nil, w.err
	}
	lay.Pages = len(starts)
	return lay, w.rec, nil
}

func colName(n int) string {
	s, _ := excelize.ColumnNumberToName(n)
	return s
}

// tableCards writes headline cards across the columns (as the Summary's KPI
// cards) and returns the next free row.
func (w *sheetWriter) tableCards(r int, cards []Card) int {
	n, _ := excelize.ColumnNameToNumber(w.lastCol)
	k := min(len(cards), n)
	w.height(r, 20)
	w.height(r+1, 30)
	w.height(r+2, 20)
	for i := 0; i < k; i++ {
		c := cards[i]
		from, to := colName(i*n/k+1), colName((i+1)*n/k)
		bg, label, value, note := clrTint, clrMuted, clrNavy, clrMuted
		if c.Dark {
			bg, label, value, note = clrNavy, clrSoft, clrWhite, clrSoft
		}
		card := txt(8).fill(bg).sideRules(clrWhite, 2)
		w.cell(ref(from, r), ref(to, r), c.Label, card.bold().color(label).start(1).valign("bottom").topRule(clrBar, 2).shrink())
		w.cell(ref(from, r+1), ref(to, r+1), c.Value, card.sz(18).bold().color(value).start(1).shrink())
		w.cell(ref(from, r+2), ref(to, r+2), c.Note, card.sz(8.5).color(note).start(1).valign("top").shrink())
	}
	w.height(r+3, 18)
	return r + 4
}

func (c Column) headFmt() cellFmt {
	h := txt(8.5).bold().color(clrWhite).fill(clrNavy).shrink()
	switch c.Kind {
	case Text:
		return h.start(1)
	case Int, Decimal, Money:
		return h.num(padText)
	default:
		return h.center()
	}
}

func (w *sheetWriter) tableHeader(r int, cols []Column) {
	w.height(r, hTblHeader)
	for i, c := range cols {
		w.cell(ref(colName(i+1), r), "", c.Header, c.headFmt())
	}
}

// tableBand writes a band heading: number, title, and a note at the end side.
func (w *sheetWriter) tableBand(r int, t *texts, b Band, continued bool) int {
	band := txt(11).bold().color(clrNavy).fill(clrHead2)
	note := b.Note
	if continued {
		note = joinDot(t.cont, note)
	}
	w.height(r, hTblBand)
	w.cell(ref("A", r), "", b.No, txt(10).bold().color(clrWhite).fill(clrNavy).center().format(fmtInt))
	w.cell(ref("B", r), ref(w.colAt(2), r), b.Title, band.start(1).shrink())
	w.cell(ref(w.colAt(1), r), ref(w.lastCol, r), note, txt(9).color(clrMuted).fill(clrHead2).end(1).shrink())
	return r + 1
}

func (c Column) cellFmt() cellFmt {
	row := txt(9.5).bottomRule(clrRule, 1)
	switch c.Kind {
	case Text:
		return row.start(1).shrink()
	case Center:
		return row.center().shrink()
	case Int:
		return row.num(padThou)
	case Decimal, Money:
		return row.num(padMoney)
	case Date:
		return row.center().custom(fmtDate)
	case DateTime:
		return row.center().custom(fmtDateTime)
	case Tick:
		return txt(12).bold().color(clrNavy).center().bottomRule(clrRule, 1).sideRules(clrRule, 1)
	default:
		return row.center()
	}
}

const fmtDateTime = `dd\/mm\/yyyy hh:mm`

func (w *sheetWriter) tableRow(r int, cols []Column, vals []any, sums []float64) {
	w.height(r, hTblRow)
	for i, c := range cols {
		var v any
		if i < len(vals) {
			v = vals[i]
		}
		cf := c.cellFmt()
		at := ref(colName(i+1), r)
		switch x := v.(type) {
		case YesNo:
			fl := txt(9.5).bottomRule(clrRule, 1).center()
			if x.On {
				fl = fl.bold().color(clrAccent)
			} else {
				fl = fl.color(clrMuted)
			}
			w.cell(at, "", x.Text, fl)
			continue
		case Formula:
			w.formula(at, x.Expr(r), x.Value, cf)
			v = x.Value
		case time.Time:
			if x.IsZero() {
				w.cell(at, "", nil, cf)
				continue
			}
			w.cell(at, "", x, cf)
		default:
			w.cell(at, "", v, cf)
		}
		if c.Total {
			sums[i] += toFloat(v)
		}
	}
}

// formula writes a live formula, recording the value it evaluates to now.
func (w *sheetWriter) formula(at, expr string, value any, c cellFmt) {
	if w.err != nil {
		return
	}
	if w.err = w.f.SetCellFormula(w.sheet, at, expr); w.err != nil {
		return
	}
	w.rec.at(keyOf(at)).value = formulaValue{expr: expr, value: value}
	w.paint(at, at, c)
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

func (w *sheetWriter) tableTotal(r int, t Table, lay TableLayout, sums []float64) {
	g := txt(10.5).bold().color(clrWhite).fill(clrNavy)
	w.height(r, hTblTotal)
	first := -1
	for i, c := range t.Columns {
		if c.Total {
			first = i
			break
		}
	}
	labelTo := colName(max(1, first))
	if first <= 0 {
		labelTo = w.colAt(1)
	}
	w.cell(ref("A", r), ref(labelTo, r), t.TotalLabel, g.start(1))
	for i, c := range t.Columns {
		if i < first || first < 0 {
			continue
		}
		at := ref(colName(i+1), r)
		cf := g.num(padThou)
		if c.Kind == Decimal || c.Kind == Money {
			cf = g.num(padMoney)
		}
		if !c.Total {
			w.paint(at, at, g)
			continue
		}
		val := sums[i]
		if c.Kind == Int {
			val = math.Round(val)
		} else {
			val = round2(val)
		}
		col := colName(i + 1)
		rng := fmt.Sprintf("%s%d:%s%d", col, lay.FirstRow, col, lay.LastRow)
		switch t.TotalMode {
		case TotalSum:
			w.formula(at, "SUM("+rng+")", val, cf)
		case TotalSubtotal:
			w.formula(at, "SUBTOTAL(109,"+rng+")", val, cf)
		default:
			w.cell(at, "", val, cf)
		}
	}
}

// FormatCount formats a whole number the way the cards print it: 1,234,567.
func FormatCount(n int64) string { return digits(n) }

// FormatAmount formats an amount with two decimals: 4,290.40.
func FormatAmount(x float64) string { return money(x) }
