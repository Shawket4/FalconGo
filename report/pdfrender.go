package report

// PDF rendering of recorded sheets. Geometry follows the workbook's own print
// setup: A4 in the sheet's orientation, the same margins, columns centred
// horizontally, the sheet's print scale, its manual page breaks, the column
// header row repeated on every later page, the running header on pages 2+
// and the "Page X of N" footer on every page.

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// drawnPage is the text the renderer placed on one page, in drawing order.
// Tests use it to compare the PDF's figures with the workbook's.
type drawnPage struct {
	sheet string
	texts []string
}

func renderPDF(title string, recs []*sheetRecord) ([]byte, error) {
	b, _, err := renderPDFDrawn(title, recs)
	return b, err
}

func renderPDFDrawn(title string, recs []*sheetRecord) ([]byte, []drawnPage, error) {
	e, err := newTextEngine()
	if err != nil {
		return nil, nil, err
	}
	doc := newPDFDoc(title, e.regular, e.bold)
	var drawn []drawnPage
	for _, rec := range recs {
		drawn = append(drawn, renderSheet(doc, e, rec)...)
	}
	var buf bytes.Buffer
	if err := doc.write(&buf); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), drawn, nil
}

// colPoints is a column's width in points, using the same pixel rounding the
// print scale was computed with.
func colPoints(width float64) float64 { return math.Floor(width*7+0.5) * 0.75 }

const (
	cellPad     = 1.5  // points of padding inside a cell
	indentPt    = 6.75 // one Excel indent level
	thinRule    = 0.6
	mediumRule  = 1.5
	hfMarginPts = marginHeaderFooter * 72
)

func renderSheet(doc *pdfDoc, e *textEngine, rec *sheetRecord) []drawnPage {
	pw, ph := pageW, pageH
	if rec.landscape {
		pw, ph = pageH, pageW
	}
	scale := float64(rec.print.scale) / 100
	if scale <= 0 {
		scale = 1
	}
	ncols := rec.lastCol()
	colX := make([]float64, ncols+1) // colX[c] = right edge of column c
	for c := 1; c <= ncols; c++ {
		colX[c] = colX[c-1] + colPoints(rec.widths[c])
	}
	total := colX[ncols]
	side := marginSide * 72
	x0 := side + (pw-2*side-total*scale)/2
	top := marginTopBottom * 72

	lastRow := rec.print.lastRow
	starts := append([]int{1}, rec.breaks...)
	var out []drawnPage
	for pi, start := range starts {
		end := lastRow
		if pi+1 < len(starts) {
			end = starts[pi+1] - 1
		}
		page := doc.addPage(pw, ph)
		dp := drawnPage{sheet: rec.name}
		var rows []int
		if pi > 0 && rec.print.titleRow > 0 {
			rows = append(rows, rec.print.titleRow)
		}
		for r := start; r <= end; r++ {
			rows = append(rows, r)
		}

		// Map a sheet rectangle to the page.
		place := func(x, y, w float64) (float64, float64, float64) {
			if rec.rtl {
				x = total - x - w
			}
			return x0 + x*scale, top + y*scale, w * scale
		}

		y := 0.0
		for _, r := range rows {
			h := rec.heights[r]
			if h == 0 {
				h = 15
			}
			// Three passes so fills never cover rules and rules never cover text.
			for pass := 0; pass < 3; pass++ {
				for c := 1; c <= ncols; c++ {
					k := cellKey{c, r}
					if rec.covered[k] {
						continue
					}
					cell := rec.cells[k]
					if cell == nil {
						continue
					}
					endK := k
					if m, ok := rec.merges[k]; ok {
						endK = m
					}
					cw := colX[endK.col] - colX[c-1]
					chh := h
					for rr := r + 1; rr <= endK.row; rr++ {
						chh += rec.heights[rr]
					}
					px, py, pwid := place(colX[c-1], y, cw)
					phh := chh * scale
					f := cell.f
					switch pass {
					case 0:
						if f.bg != "" {
							page.fillRect(px, py, pwid, phh, f.bg)
						}
					case 1:
						if f.top != "" {
							page.line(px, py, px+pwid, py, ruleWidth(f.topW)*scale, f.top)
						}
						if f.bottom != "" {
							page.line(px, py+phh, px+pwid, py+phh, ruleWidth(f.bottomW)*scale, f.bottom)
						}
						if f.side != "" {
							lw := ruleWidth(f.sideW) * scale
							page.line(px, py, px, py+phh, lw, f.side)
							page.line(px+pwid, py, px+pwid, py+phh, lw, f.side)
						}
					case 2:
						if s := drawCell(page, e, rec, cell, px, py, pwid, phh, scale); s != "" {
							dp.texts = append(dp.texts, s)
						}
					}
				}
			}
			y += h
		}

		// Running header (pages 2+) and footer, mirrored for right-to-left.
		pageNo := strconv.Itoa(pi + 1)
		if pi > 0 {
			drawHF(page, e, rec.header, rec.rtl, side, pw-side, hfMarginPts, true, pageNo)
		}
		drawHF(page, e, rec.footer, rec.rtl, side, pw-side, ph-hfMarginPts, false, pageNo)
		for _, t := range rec.footer {
			dp.texts = append(dp.texts, strings.ReplaceAll(t.text, "&P", pageNo))
		}
		out = append(out, dp)
	}
	return out
}

func ruleWidth(style int) float64 {
	if style >= 2 {
		return mediumRule
	}
	return thinRule
}

// drawHF draws a header or footer pair: start text at the reading start,
// end text at the reading end. y is the header's top or the footer's bottom.
func drawHF(page *pdfPage, e *textEngine, pair [2]hfText, rtl bool, left, right, y float64, isHeader bool, pageNo string) {
	for i, t := range pair {
		if t.text == "" {
			continue
		}
		text := strings.ReplaceAll(t.text, "&P", pageNo)
		l := e.layout([]textSpan{{text: text, size: t.size, bold: t.bold, color: t.color}}, isRTLText(text, rtl))
		atStart := i == 0
		var x float64
		if atStart != rtl { // start side in LTR, or end side in RTL: on the left
			x = left
		} else {
			x = right - l.width
		}
		asc, desc := e.metrics(t.size)
		base := y + asc
		if !isHeader {
			base = y - desc
		}
		page.text(l, x, base)
	}
}

// metrics returns Cairo's ascent and descent in points at size.
func (e *textEngine) metrics(size float64) (float64, float64) {
	f := e.regular
	return f.ascent / f.upem * size, f.descent / f.upem * size
}

// drawCell draws a cell's text inside its rectangle and returns the text.
func drawCell(page *pdfPage, e *textEngine, rec *sheetRecord, cell *recCell, x, y, w, h, scale float64) string {
	f := cell.f
	spans := cell.spans
	isNumber := false
	if spans == nil {
		if cell.value == nil {
			return ""
		}
		text, numeric := formatCell(cell.value, f)
		if text == "" {
			return ""
		}
		isNumber = numeric
		spans = []textSpan{{text: text, size: f.size, bold: f.isBold, color: f.fontColor}}
	}
	var plain strings.Builder
	for _, s := range spans {
		plain.WriteString(s.text)
	}
	text := plain.String()

	// Physical alignment, as the workbook resolves it.
	halign := f.h
	switch {
	case halign == "start" && rec.rtl, halign == "end" && !rec.rtl:
		halign = "right"
	case halign == "start", halign == "end":
		halign = "left"
	case halign == "":
		if isNumber != rec.rtl {
			halign = "right"
		} else {
			halign = "left"
		}
	}

	padL, padR := cellPad, cellPad
	ind := float64(f.indent) * indentPt
	switch halign {
	case "left":
		padL += ind
	case "right":
		padR += ind
	}
	if strings.HasSuffix(f.customFmt, "_)") {
		padR += 0.33 * f.size
	}
	avail := w/scale - padL - padR

	// A formatted number or date reads left to right in any sheet, as in
	// Excel; text takes its direction from its first strong character.
	rtl := !isNumber && isRTLText(text, rec.rtl)
	scaled := make([]textSpan, len(spans))
	copy(scaled, spans)
	line := e.layout(scaled, rtl)
	if f.shrinkFit && line.width > avail-fitGap && line.width > 0 {
		// Leave a little air so shrunk text never touches the next cell.
		k := (avail - fitGap) / line.width
		for i := range scaled {
			scaled[i].size *= k
		}
		line = e.layout(scaled, rtl)
	}

	var lx float64 // in sheet points, from the cell's left edge
	switch halign {
	case "right":
		lx = w/scale - padR - line.width
	case "center":
		lx = (w/scale - line.width) / 2
	default:
		lx = padL
	}
	asc, desc := e.metrics(line.maxPt)
	hh := h / scale
	var base float64
	switch f.v {
	case "top":
		base = cellPad + asc
	case "bottom":
		base = hh - cellPad - desc
	default:
		base = hh/2 + (asc-desc)/2
	}
	// Scale the laid-out line onto the page.
	for i := range line.glyphs {
		line.glyphs[i].x *= scale
		line.glyphs[i].y *= scale
		line.glyphs[i].size *= scale
	}
	page.text(line, x+lx*scale, y+base*scale)
	return text
}

// formatCell renders a value the way its number format displays it, and says
// whether it is a number.
func formatCell(v any, f cellFmt) (string, bool) {
	code := f.customFmt
	if code == "" && f.numFmt == fmtInt {
		code = "0"
	}
	switch x := v.(type) {
	case string:
		return x, false
	case time.Time:
		if x.IsZero() {
			return "", false
		}
		return formatDateCode(x, code), true
	case int:
		return formatNumber(float64(x), code), true
	case int64:
		return formatNumber(float64(x), code), true
	case float64:
		return formatNumber(x, code), true
	case bool:
		if x {
			return "TRUE", false
		}
		return "FALSE", false
	case formulaValue:
		return formatCell(x.value, f)
	default:
		return fmt.Sprint(v), false
	}
}

func formatDateCode(t time.Time, code string) string {
	if code == "" {
		code = fmtDate
	}
	c := strings.ReplaceAll(code, `\`, "")
	r := strings.NewReplacer("yyyy", t.Format("2006"), "dd", t.Format("02"), "mm", t.Format("01"),
		"hh", t.Format("15"), "ss", t.Format("05"))
	out := r.Replace(c)
	// minutes: the second "01" after hh: was turned into the month; fix "hh:mm".
	if strings.Contains(c, "hh:mm") {
		out = strings.Replace(out, t.Format("15")+":"+t.Format("01"), t.Format("15:04"), 1)
	}
	return out
}

// formatNumber supports the codes this package writes: "0", "#,##0",
// "#,##0.00", "0.0%" (each optionally with a trailing "_)" pad), and General.
func formatNumber(v float64, code string) string {
	code = strings.TrimSuffix(code, "_)")
	if code == "" || code == "General" || code == "@" {
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	pct := strings.HasSuffix(code, "%")
	if pct {
		v *= 100
		code = strings.TrimSuffix(code, "%")
	}
	decimals := 0
	if i := strings.Index(code, "."); i >= 0 {
		decimals = len(code) - i - 1
	}
	thousands := strings.Contains(code, ",")
	p := math.Pow(10, float64(decimals))
	v = math.Round(v*p) / p // Excel rounds half away from zero, as math.Round does
	s := strconv.FormatFloat(math.Abs(v), 'f', decimals, 64)
	if thousands {
		intPart, frac := s, ""
		if i := strings.Index(s, "."); i >= 0 {
			intPart, frac = s[:i], s[i:]
		}
		n, _ := strconv.ParseInt(intPart, 10, 64)
		s = digits(n) + frac
	}
	if v < 0 {
		s = "-" + s
	}
	if pct {
		s += "%"
	}
	return s
}

// formulaValue is a formula with the value it evaluates to when written, so
// the PDF can print what the workbook will show.
type formulaValue struct {
	expr  string
	value any
}
