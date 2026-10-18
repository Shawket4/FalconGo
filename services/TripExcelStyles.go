package services

import "github.com/xuri/excelize/v2"

// Cell styles for the trip export.
//
// The visual language was taken from the first Watanya report (since restyled
// in package report, with its own navy palette):
// Cairo throughout, a teal/slate palette, and bottom-only rules instead of a
// full grid. A boxed grid fights the eye on a wide sheet; a single hairline
// under each row gives the same row separation and reads far quieter.
//
// Everything is centred horizontally and vertically. On a sheet this wide the
// alternative -- numbers right, text left -- leaves ragged gutters between
// columns, and vertical centring is what stops a wrapped header from sitting
// awkwardly above its neighbours.
//
// Styles are built once per workbook and indexed. excelize hands back an int per
// NewStyle, and creating one per cell across ~11,000 rows bloats both the file
// and the write.

// Palette of the first Watanya report (the current one, in package report, has
// moved to navy; these two exports have not followed yet).
const (
	colHeaderBg   = "#2C5F7C"
	colHeaderText = "#FFFFFF"
	colAltRow     = "#F8FAFB"
	colBorder     = "#D5DBDE"
	colTextDark   = "#2C3E50"
	colTotalBg    = "#E8F1F5"
	colAccent     = "#4A90A4"
	colSubtotalBg = "#D4E6ED"
	colMutedText  = "#7F8C8D"
	colPositive   = "#1E8449"
)

const fontFamily = "Cairo"

type tripStyles struct {
	title    int
	subtitle int
	meta     int

	statLabel      int
	statValue      int
	statValueMoney int

	header int

	// [kind][striped] -> style id
	body map[string][2]int
	// [on][striped]
	flags [2][2]int

	totalText  int
	totalInt   int
	totalNum   int
	totalMoney int
}

const (
	fmtInt      = "#,##0"
	fmtNum      = "#,##0.00"
	fmtMoney    = `#,##0.00 "EGP"`
	fmtDate     = "dd mmm yyyy"
	fmtDateTime = "dd mmm yyyy hh:mm"
)

func newTripStyles(f *excelize.File) (*tripStyles, error) {
	s := &tripStyles{body: map[string][2]int{}}

	var firstErr error
	make := func(st *excelize.Style) int {
		id, err := f.NewStyle(st)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return id
	}

	cairo := func(size float64, bold bool, color string) *excelize.Font {
		return &excelize.Font{Size: size, Bold: bold, Color: color, Family: fontFamily}
	}
	// The single alignment used everywhere.
	center := func() *excelize.Alignment {
		return &excelize.Alignment{Horizontal: "center", Vertical: "center"}
	}
	centerWrap := func() *excelize.Alignment {
		return &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	}
	fill := func(color string) excelize.Fill {
		return excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1}
	}
	underline := func(color string, weight int) []excelize.Border {
		return []excelize.Border{{Type: "bottom", Color: color, Style: weight}}
	}

	// ── Banner ───────────────────────────────────────────────────────────────
	// Coloured text on white rather than a filled block: the Watanya report reads
	// as a document, not a dashboard widget, and at this size a solid band across
	// seventeen columns is overbearing.
	s.title = make(&excelize.Style{
		Font:      cairo(20, true, colHeaderBg),
		Alignment: center(),
	})
	s.subtitle = make(&excelize.Style{
		Font:      cairo(13, false, colMutedText),
		Alignment: center(),
	})
	s.meta = make(&excelize.Style{
		Font:      cairo(10, false, colMutedText),
		Alignment: center(),
	})

	// ── KPI pills ────────────────────────────────────────────────────────────
	s.statLabel = make(&excelize.Style{
		Font:      cairo(10, false, colMutedText),
		Fill:      fill(colTotalBg),
		Alignment: center(),
	})
	s.statValue = make(&excelize.Style{
		Font:         cairo(15, true, colHeaderBg),
		Fill:         fill(colTotalBg),
		Alignment:    center(),
		Border:       underline(colAccent, 2),
		CustomNumFmt: strPtr(fmtInt),
	})
	s.statValueMoney = make(&excelize.Style{
		Font:         cairo(15, true, colHeaderBg),
		Fill:         fill(colTotalBg),
		Alignment:    center(),
		Border:       underline(colAccent, 2),
		CustomNumFmt: strPtr(fmtMoney),
	})

	// ── Header ───────────────────────────────────────────────────────────────
	s.header = make(&excelize.Style{
		Font:      cairo(11, true, colHeaderText),
		Fill:      fill(colHeaderBg),
		Alignment: centerWrap(),
		Border:    underline(colHeaderBg, 2),
	})

	// ── Body ─────────────────────────────────────────────────────────────────
	kinds := map[string]string{
		"text": "", "flag": "",
		"int": fmtInt, "num": fmtNum, "money": fmtMoney,
		"date": fmtDate, "datetime": fmtDateTime,
	}
	for kind, numFmt := range kinds {
		var pair [2]int
		for i, striped := range []bool{false, true} {
			st := &excelize.Style{
				Font:      cairo(10, false, colTextDark),
				Alignment: center(),
				Border:    underline(colBorder, 1),
			}
			if numFmt != "" {
				st.CustomNumFmt = strPtr(numFmt)
			}
			if striped {
				st.Fill = fill(colAltRow)
			}
			pair[i] = make(st)
		}
		s.body[kind] = pair
	}

	// ── Receipt flags ────────────────────────────────────────────────────────
	// Colour carries the meaning here: the column is scanned, not read.
	for oi, on := range []bool{false, true} {
		for si, striped := range []bool{false, true} {
			colour, bold := colMutedText, false
			if on {
				colour, bold = colPositive, true
			}
			st := &excelize.Style{
				Font:      cairo(10, bold, colour),
				Alignment: center(),
				Border:    underline(colBorder, 1),
			}
			if striped {
				st.Fill = fill(colAltRow)
			}
			s.flags[oi][si] = make(st)
		}
	}

	// ── Totals ───────────────────────────────────────────────────────────────
	// A heavy rule above rather than a filled band: it closes the table without
	// competing with the header for attention.
	totalStyle := func(numFmt string) *excelize.Style {
		st := &excelize.Style{
			Font:      cairo(11, true, colHeaderBg),
			Fill:      fill(colTotalBg),
			Alignment: center(),
			Border:    []excelize.Border{{Type: "top", Color: colAccent, Style: 2}},
		}
		if numFmt != "" {
			st.CustomNumFmt = strPtr(numFmt)
		}
		return st
	}
	s.totalText = make(totalStyle(""))
	s.totalInt = make(totalStyle(fmtInt))
	s.totalNum = make(totalStyle(fmtNum))
	s.totalMoney = make(totalStyle(fmtMoney))

	if firstErr != nil {
		return nil, firstErr
	}
	return s, nil
}

func (s *tripStyles) cell(kind string, striped bool) int {
	pair, ok := s.body[kind]
	if !ok {
		pair = s.body["text"]
	}
	if striped {
		return pair[1]
	}
	return pair[0]
}

func (s *tripStyles) flag(on, striped bool) int {
	oi, si := 0, 0
	if on {
		oi = 1
	}
	if striped {
		si = 1
	}
	return s.flags[oi][si]
}

func (s *tripStyles) totalFor(kind string) int {
	switch kind {
	case "int":
		return s.totalInt
	case "num":
		return s.totalNum
	case "money":
		return s.totalMoney
	default:
		return s.totalText
	}
}

func strPtr(s string) *string { return &s }

// tripColumn and tripStat are the column and stat-pill shapes the car and oil
// exports build on (the trips export itself now uses package report).
type tripColumn struct {
	header string
	width  float64
	// kind drives the number format and the totals row: text, int, num, money,
	// date, datetime, flag.
	kind  string
	total bool
}

type tripStat struct {
	label string
	value interface{}
	money bool
}
