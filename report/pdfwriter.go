package report

// A small PDF 1.7 writer: filled rectangles, lines and shaped glyph runs, with
// the fonts embedded whole as TrueType CID fonts (Identity-H, glyph IDs as
// character codes) and a ToUnicode map so the text can be searched and copied.
// It writes only what the report renderer draws.

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/go-text/typesetting/font"
)

type pdfPage struct {
	w, h    float64
	content bytes.Buffer
}

type pdfDoc struct {
	title string
	fonts []*pdfFont
	pages []*pdfPage
}

func newPDFDoc(title string, fonts ...*pdfFont) *pdfDoc {
	d := &pdfDoc{title: title, fonts: fonts}
	for i, f := range fonts {
		f.resource = fmt.Sprintf("F%d", i+1)
	}
	return d
}

func (d *pdfDoc) addPage(w, h float64) *pdfPage {
	p := &pdfPage{w: w, h: h}
	d.pages = append(d.pages, p)
	return p
}

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

func rgb(hex string) string {
	if len(hex) != 6 {
		return "0 0 0"
	}
	v, _ := strconv.ParseUint(hex, 16, 32)
	return fmt.Sprintf("%s %s %s", num(float64(v>>16&255)/255), num(float64(v>>8&255)/255), num(float64(v&255)/255))
}

// Coordinates below are top-left based (y grows downward); the page flips them.

func (p *pdfPage) fillRect(x, y, w, h float64, color string) {
	fmt.Fprintf(&p.content, "%s rg %s %s %s %s re f\n", rgb(color), num(x), num(p.h-y-h), num(w), num(h))
}

func (p *pdfPage) line(x1, y1, x2, y2, width float64, color string) {
	fmt.Fprintf(&p.content, "%s RG %s w %s %s m %s %s l S\n", rgb(color), num(width),
		num(x1), num(p.h-y1), num(x2), num(p.h-y2))
}

// text draws a shaped line with its origin (left end of the baseline) at x, y.
func (p *pdfPage) text(l shapedLine, x, y float64) {
	if len(l.glyphs) == 0 {
		return
	}
	// Drawn tick marks first, outside the text object.
	for _, g := range l.glyphs {
		if !g.check {
			continue
		}
		gx, gy, sz := x+g.x, y-g.y, g.size
		fmt.Fprintf(&p.content, "q %s RG %s w 1 J 1 j %s %s m %s %s l %s %s l S Q\n",
			rgb(g.color), num(sz*0.09),
			num(gx+sz*0.12), num(p.h-(gy-sz*0.33)),
			num(gx+sz*0.32), num(p.h-(gy-sz*0.10)),
			num(gx+sz*0.70), num(p.h-(gy-sz*0.68)))
	}
	p.content.WriteString("BT\n")
	var curFont *pdfFont
	curSize, curColor := -1.0, ""
	for _, g := range l.glyphs {
		if g.check {
			continue
		}
		if g.font != curFont || g.size != curSize {
			fmt.Fprintf(&p.content, "/%s %s Tf\n", g.font.resource, num(g.size))
			curFont, curSize = g.font, g.size
		}
		if g.color != curColor {
			fmt.Fprintf(&p.content, "%s rg\n", rgb(g.color))
			curColor = g.color
		}
		fmt.Fprintf(&p.content, "1 0 0 1 %s %s Tm <%04X> Tj\n", num(x+g.x), num(p.h-(y-g.y)), uint16(g.gid))
	}
	p.content.WriteString("ET\n")
}

// ---------------------------------------------------------------------------

type pdfOut struct {
	buf     bytes.Buffer
	offsets []int
}

func (o *pdfOut) obj(id int, body string) {
	for len(o.offsets) <= id {
		o.offsets = append(o.offsets, 0)
	}
	o.offsets[id] = o.buf.Len()
	fmt.Fprintf(&o.buf, "%d 0 obj\n%s\nendobj\n", id, body)
}

func (o *pdfOut) stream(id int, dict string, data []byte, compress bool) {
	for len(o.offsets) <= id {
		o.offsets = append(o.offsets, 0)
	}
	o.offsets[id] = o.buf.Len()
	filter := ""
	if compress {
		var z bytes.Buffer
		zw, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
		zw.Write(data)
		zw.Close()
		data = z.Bytes()
		filter = " /Filter /FlateDecode"
	}
	fmt.Fprintf(&o.buf, "%d 0 obj\n<< %s%s /Length %d >>\nstream\n", id, dict, filter, len(data))
	o.buf.Write(data)
	o.buf.WriteString("\nendstream\nendobj\n")
}

func pdfString(s string) string {
	// UTF-16BE with BOM, hex-encoded: safe for any text.
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}
	b.WriteString(">")
	return b.String()
}

// write serialises the document.
func (d *pdfDoc) write(w io.Writer) error {
	o := &pdfOut{}
	o.buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")

	// Object ids: 1 catalog, 2 pages, 3 info, then 5 per font, then 2 per page.
	next := 4
	fontIDs := make([]int, len(d.fonts))
	for i := range d.fonts {
		fontIDs[i] = next
		next += 5
	}
	pageIDs := make([]int, len(d.pages))
	for i := range d.pages {
		pageIDs[i] = next
		next += 2
	}

	o.obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	var kids []string
	for _, id := range pageIDs {
		kids = append(kids, fmt.Sprintf("%d 0 R", id))
	}
	o.obj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(d.pages)))
	o.obj(3, fmt.Sprintf("<< /Title %s /Producer (Falcon report) >>", pdfString(d.title)))

	var fontRes []string
	for i, f := range d.fonts {
		id := fontIDs[i]
		fontRes = append(fontRes, fmt.Sprintf("/%s %d 0 R", f.resource, id))
		scale := 1000 / f.upem
		gids := make([]int, 0, len(f.used))
		for g := range f.used {
			gids = append(gids, int(g))
		}
		sort.Ints(gids)
		var widths strings.Builder
		for _, g := range gids {
			adv := float64(f.face.HorizontalAdvance(font.GID(g)))
			fmt.Fprintf(&widths, "%d [%s] ", g, num(adv*scale))
		}
		// Type0 font
		o.obj(id, fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
			f.name, id+1, id+4))
		// CIDFontType2
		o.obj(id+1, fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /CIDToGIDMap /Identity /DW 0 /W [%s] >>",
			f.name, id+2, widths.String()))
		asc, desc := f.ascent*scale, f.descent*scale
		o.obj(id+2, fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags 32 /FontBBox [-600 %s 1600 %s] /ItalicAngle 0 /Ascent %s /Descent %s /CapHeight %s /StemV 80 /FontFile2 %d 0 R >>",
			f.name, num(-desc), num(asc), num(asc), num(-desc), num(asc*0.7), id+3))
		o.stream(id+3, fmt.Sprintf("/Length1 %d", len(f.raw)), f.raw, true)
		o.stream(id+4, "", toUnicodeCMap(f, gids), true)
	}

	for i, p := range d.pages {
		id := pageIDs[i]
		o.obj(id, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /Font << %s >> >> /Contents %d 0 R >>",
			num(p.w), num(p.h), strings.Join(fontRes, " "), id+1))
		o.stream(id+1, "", p.content.Bytes(), true)
	}

	xref := o.buf.Len()
	fmt.Fprintf(&o.buf, "xref\n0 %d\n0000000000 65535 f \n", len(o.offsets))
	for _, off := range o.offsets[1:] {
		fmt.Fprintf(&o.buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&o.buf, "trailer\n<< /Size %d /Root 1 0 R /Info 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(o.offsets), xref)
	_, err := w.Write(o.buf.Bytes())
	return err
}

func toUnicodeCMap(f *pdfFont, gids []int) []byte {
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
		"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	var entries []string
	for _, g := range gids {
		rs := f.used[font.GID(g)]
		if len(rs) == 0 {
			continue
		}
		var hex strings.Builder
		for _, u := range utf16.Encode(rs) {
			fmt.Fprintf(&hex, "%04X", u)
		}
		entries = append(entries, fmt.Sprintf("<%04X> <%s>", g, hex.String()))
	}
	for len(entries) > 0 {
		n := min(100, len(entries))
		fmt.Fprintf(&b, "%d beginbfchar\n%s\nendbfchar\n", n, strings.Join(entries[:n], "\n"))
		entries = entries[n:]
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.Bytes()
}
