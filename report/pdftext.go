package report

// Text for the PDF renderer: bidi segmentation, script itemisation and
// OpenType shaping (go-text/typesetting, a pure-Go HarfBuzz port), so Arabic
// comes out joined, with its ligatures and marks, in right-to-left order, and
// mixed Arabic/Latin/number strings are ordered per the Unicode bidi rules.
//
// Glyphs are addressed by glyph ID, not by character: Cairo, like most modern
// Arabic fonts, has no presentation-form code points in its cmap, so a writer
// that maps characters to glyphs cannot draw joined Arabic with it.

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/Cairo-Regular.ttf
var cairoRegular []byte

//go:embed fonts/Cairo-Bold.ttf
var cairoBold []byte

// pdfFont is one embedded font file and the glyphs a document used from it.
type pdfFont struct {
	name     string // PostScript name, also the PDF BaseFont
	raw      []byte
	face     *font.Face
	upem     float64
	ascent   float64 // font units, positive
	descent  float64 // font units, positive (below the baseline)
	used     map[font.GID][]rune
	resource string // /F1, /F2 inside the PDF
}

func loadPDFFont(name string, raw []byte) (*pdfFont, error) {
	face, err := font.ParseTTF(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	ext, _ := face.FontHExtents()
	return &pdfFont{
		name: name, raw: raw, face: face,
		upem:   float64(face.Upem()),
		ascent: float64(ext.Ascender), descent: -float64(ext.Descender),
		used: map[font.GID][]rune{},
	}, nil
}

// singleFace satisfies shaping.Fontmap with one face for every rune.
type singleFace struct{ f *font.Face }

func (s singleFace) ResolveFace(rune) *font.Face { return s.f }

// textEngine shapes text with the two Cairo weights. Not safe for concurrent
// use; each PDF gets its own.
type textEngine struct {
	regular, bold *pdfFont
	shaper        shaping.HarfbuzzShaper
	seg           shaping.Segmenter
}

func newTextEngine() (*textEngine, error) {
	return newTextEngineFonts("Cairo-Regular", cairoRegular, "Cairo-Bold", cairoBold)
}

// newTextEngineFonts shapes with any regular/bold pair (the fit check uses
// it to measure with the workbook's font). Each engine gets its own faces
// because the used-glyph sets are per document.
func newTextEngineFonts(regName string, reg []byte, boldName string, bold []byte) (*textEngine, error) {
	r, err := loadPDFFont(regName, reg)
	if err != nil {
		return nil, err
	}
	b, err := loadPDFFont(boldName, bold)
	if err != nil {
		return nil, err
	}
	return &textEngine{regular: r, bold: b}, nil
}

// textSpan is a run of text in one style.
type textSpan struct {
	text  string
	size  float64 // points
	bold  bool
	color string // RRGGBB
}

// placedGlyph is a glyph positioned relative to the start of the line, on
// the baseline, in points.
type placedGlyph struct {
	font  *pdfFont
	gid   font.GID
	x, y  float64
	size  float64
	color string
	check bool    // a tick mark Cairo has no glyph for; drawn as strokes
	adv   float64 // advance, for drawn marks
}

// isTick reports a check-mark character; Cairo has none, so the renderer
// draws it.
func isTick(r rune) bool { return r == '✓' || r == '✔' || r == '☑' }

// shapedLine is one line of text laid out in visual (left-to-right) order.
type shapedLine struct {
	glyphs []placedGlyph
	width  float64
	maxPt  float64 // largest font size on the line
}

// isRTLText reports the paragraph direction from the first strong character,
// which is how Excel resolves a cell's "context" reading order. Text with no
// strong character (numbers, dates) takes the sheet direction.
func isRTLText(s string, sheetRTL bool) bool {
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Arabic, unicode.Hebrew, unicode.Syriac, unicode.Thaana):
			return true
		case unicode.IsLetter(r):
			return false
		}
	}
	return sheetRTL
}

// layout shapes spans as one line. rtl is the paragraph direction.
func (e *textEngine) layout(spans []textSpan, rtl bool) shapedLine {
	var runes []rune
	var spanOf []int
	for i, s := range spans {
		for _, r := range s.text {
			runes = append(runes, r)
			spanOf = append(spanOf, i)
		}
	}
	var line shapedLine
	if len(runes) == 0 {
		return line
	}
	for _, s := range spans {
		line.maxPt = max(line.maxPt, s.size)
	}

	dir := di.DirectionLTR
	if rtl {
		dir = di.DirectionRTL
	}
	input := shaping.Input{
		Text: runes, RunStart: 0, RunEnd: len(runes),
		Direction: dir, Face: e.regular.face, Size: fixed.I(12),
	}
	runs := e.seg.Split(input, singleFace{e.regular.face})

	// Each bidi/script run, cut again at style boundaries, shaped piece by
	// piece. Pieces of a right-to-left run are laid out in reverse.
	type piece struct {
		out  shaping.Output
		font *pdfFont
		span textSpan
		rtl  bool
	}
	type runPieces struct {
		pieces []piece
		rtl    bool
	}
	var logical []runPieces
	for _, run := range runs {
		runRTL := run.Direction.Progression() == di.TowardTopLeft
		var rp runPieces
		rp.rtl = runRTL
		for a := run.RunStart; a < run.RunEnd; {
			b := a
			for b < run.RunEnd && spanOf[b] == spanOf[a] {
				b++
			}
			sp := spans[spanOf[a]]
			f := e.regular
			if sp.bold {
				f = e.bold
			}
			in := run
			in.RunStart, in.RunEnd = a, b
			in.Face = f.face
			in.Size = fixed.Int26_6(sp.size * 64)
			out := e.shaper.Shape(in)
			// Remember which characters each glyph stands for (ToUnicode).
			for _, g := range out.Glyphs {
				if _, ok := f.used[g.GlyphID]; ok {
					continue
				}
				end := min(g.ClusterIndex+g.RuneCount, len(runes))
				if g.ClusterIndex < end {
					f.used[g.GlyphID] = append([]rune(nil), runes[g.ClusterIndex:end]...)
				} else {
					f.used[g.GlyphID] = nil
				}
			}
			rp.pieces = append(rp.pieces, piece{out, f, sp, runRTL})
			a = b
		}
		if runRTL {
			for i, j := 0, len(rp.pieces)-1; i < j; i, j = i+1, j-1 {
				rp.pieces[i], rp.pieces[j] = rp.pieces[j], rp.pieces[i]
			}
		}
		logical = append(logical, rp)
	}

	// Visual order of the runs (UAX #9 rule L2 for the two levels a cell
	// produces): in a paragraph of one direction, each maximal sequence of
	// runs of the other direction keeps its internal order reversed back, and
	// the whole line is reversed for a right-to-left paragraph.
	n := len(logical)
	visual := make([]int, n)
	for i := range visual {
		visual[i] = i
	}
	if rtl {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			visual[i], visual[j] = visual[j], visual[i]
		}
		// visual now lists runs right-to-left reversed; restore the internal
		// order of each left-to-right sequence.
		for i := 0; i < n; {
			if !logical[visual[i]].rtl {
				j := i
				for j < n && !logical[visual[j]].rtl {
					j++
				}
				for a, b := i, j-1; a < b; a, b = a+1, b-1 {
					visual[a], visual[b] = visual[b], visual[a]
				}
				i = j
				continue
			}
			i++
		}
	} else {
		for i := 0; i < n; {
			if logical[visual[i]].rtl {
				j := i
				for j < n && logical[visual[j]].rtl {
					j++
				}
				for a, b := i, j-1; a < b; a, b = a+1, b-1 {
					visual[a], visual[b] = visual[b], visual[a]
				}
				i = j
				continue
			}
			i++
		}
	}

	x := 0.0
	for _, ri := range visual {
		for _, p := range logical[ri].pieces {
			for _, g := range p.out.Glyphs {
				pg := placedGlyph{
					font: p.font, gid: g.GlyphID,
					x: x + f26(g.XOffset), y: f26(g.YOffset),
					size: p.span.size, color: p.span.color,
				}
				adv := f26(g.Advance)
				if g.GlyphID == 0 && g.ClusterIndex < len(runes) && isTick(runes[g.ClusterIndex]) {
					pg.check, adv = true, 0.8*p.span.size
					pg.adv = adv
				}
				line.glyphs = append(line.glyphs, pg)
				x += adv
			}
		}
	}
	line.width = x
	return line
}

func f26(v fixed.Int26_6) float64 { return float64(v) / 64 }

// plainText reports a single-style line's text, for tests and debugging.
func (l shapedLine) String() string {
	var b strings.Builder
	for _, g := range l.glyphs {
		b.WriteString(string(g.font.used[g.gid]))
	}
	return b.String()
}
