package report

import (
	"fmt"
	"sort"
	"strings"
)

// FitIssue is a cell whose text does not sit inside its cell.
type FitIssue struct {
	Target  string // "pdf" (Cairo) or "xlsx" (the workbook font)
	Sheet   string
	Cell    string
	Text    string
	Problem string
}

func (i FitIssue) String() string {
	return fmt.Sprintf("%s %s!%s %q: %s", i.Target, i.Sheet, i.Cell, i.Text, i.Problem)
}

const (
	minShrink = 0.80 // shrink-to-fit may take text down to 80% of its size
	fitGap    = 5.0  // points kept clear between text and a filled neighbour
)

// CheckFit measures every cell of every sheet in the book against its cell:
// with Cairo as the PDF draws it, and — when its regular and bold font files
// are given — with the workbook's own font (Tahoma), as Excel lays it out.
//
// It reports numbers that do not fit (Excel prints ####), shrink-to-fit text
// that would shrink below 80%, text that runs into a filled neighbouring cell
// or is cut off inside a merged range.
func (b *Book) CheckFit(xlsxRegular, xlsxBold []byte) ([]FitIssue, error) {
	type target struct {
		name string
		e    *textEngine
	}
	pdfE, err := newTextEngine()
	if err != nil {
		return nil, err
	}
	targets := []target{{"pdf", pdfE}}
	if xlsxRegular != nil && xlsxBold != nil {
		xe, err := newTextEngineFonts("xlsx-regular", xlsxRegular, "xlsx-bold", xlsxBold)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target{"xlsx", xe})
	}
	var out []FitIssue
	for _, name := range b.order {
		rec := b.sheet[name]
		for _, t := range targets {
			out = append(out, checkSheetFit(t.name, t.e, rec)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func checkSheetFit(target string, e *textEngine, rec *sheetRecord) []FitIssue {
	var out []FitIssue
	ncols := rec.lastCol()
	filled := func(col, row int) bool {
		if col < 1 || col > ncols {
			return false
		}
		k := cellKey{col, row}
		for c := col; c >= 1 && rec.covered[cellKey{c, row}]; c-- {
			k = cellKey{c - 1, row}
		}
		cell := rec.cells[k]
		return cell != nil && (cell.value != nil || cell.spans != nil)
	}
	for k, cell := range rec.cells {
		if rec.covered[k] || (cell.value == nil && cell.spans == nil) {
			continue
		}
		f := cell.f
		spans := cell.spans
		isNumber := false
		if spans == nil {
			text, num := formatCell(cell.value, f)
			if text == "" {
				continue
			}
			isNumber = num
			spans = []textSpan{{text: text, size: f.size, bold: f.isBold, color: f.fontColor}}
		}
		var plain strings.Builder
		for _, s := range spans {
			plain.WriteString(s.text)
		}
		text := plain.String()
		line := e.layout(spans, !isNumber && isRTLText(text, rec.rtl))

		endK := k
		if m, ok := rec.merges[k]; ok {
			endK = m
		}
		var cw float64
		for c := k.col; c <= endK.col; c++ {
			cw += colPoints(rec.widths[c])
		}
		halign := physicalAlign(f.h, rec.rtl, isNumber)
		avail := cw - 2*cellPad - float64(f.indent)*indentPt
		if strings.HasSuffix(f.customFmt, "_)") {
			avail -= 0.33 * f.size
		}
		// The neighbour on the side the text grows towards.
		next := endK.col + 1 // text grows right (left-aligned in LTR)
		if (halign == "right") != rec.rtl {
			next = k.col - 1
		}
		if halign == "center" {
			next = 0
		}
		// Column numbers run right to left in an RTL sheet, so "grows left"
		// on paper means a higher column number there; the arithmetic above
		// already mirrors that.
		neighbour := next > 0 && filled(next, k.row)
		need := line.width
		if neighbour && !isNumber {
			need += fitGap
		}
		ref := colName(k.col) + fmt.Sprint(k.row)
		issue := func(p string) {
			out = append(out, FitIssue{target, rec.name, ref, text, p})
		}
		switch {
		case need <= avail:
		case isNumber:
			issue(fmt.Sprintf("number needs %.0fpt, cell has %.0fpt", need, avail))
		case f.shrinkFit:
			if sc := avail / need; sc < minShrink {
				issue(fmt.Sprintf("shrinks to %.0f%%", sc*100))
			}
		case endK != k:
			issue(fmt.Sprintf("cut off: needs %.0fpt, merged cell has %.0fpt", need, avail))
		case neighbour:
			issue(fmt.Sprintf("runs into the next cell: needs %.0fpt, has %.0fpt", need, avail))
		}
	}
	return out
}

// physicalAlign resolves a format's alignment to left/right/center.
func physicalAlign(h string, rtl, isNumber bool) string {
	switch {
	case h == "start" && rtl, h == "end" && !rtl:
		return "right"
	case h == "start", h == "end":
		return "left"
	case h == "":
		if isNumber != rtl {
			return "right"
		}
		return "left"
	}
	return h
}
