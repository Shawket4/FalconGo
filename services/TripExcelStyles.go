package services

import "github.com/xuri/excelize/v2"

// Cell styles for the trip export, built once per workbook.
//
// excelize returns an integer style id per NewStyle call and reuses are cheap,
// but creating one per cell across tens of thousands of rows is not -- it
// bloats the file and the write. Everything is created up front and indexed.

type tripStyles struct {
	bannerTitle    int
	bannerSub      int
	bannerMeta     int
	statLabel      int
	statValue      int
	statValueMoney int
	header         int

	// [kind][even] -> style id
	body map[string][2]int
	// [on][even]
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
	var err error

	mk := func(st *excelize.Style) (int, error) { return f.NewStyle(st) }

	if s.bannerTitle, err = mk(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 16, Color: colWhite},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{colBrand}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center", Indent: 1},
	}); err != nil {
		return nil, err
	}
	if s.bannerSub, err = mk(&excelize.Style{
		Font:      &excelize.Font{Size: 11, Color: colWhite},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{colBrandLight}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center", Indent: 1},
	}); err != nil {
		return nil, err
	}
	if s.bannerMeta, err = mk(&excelize.Style{
		Font:      &excelize.Font{Size: 9, Italic: true, Color: colMuted},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center", Indent: 1},
	}); err != nil {
		return nil, err
	}

	if s.statLabel, err = mk(&excelize.Style{
		Font:      &excelize.Font{Size: 9, Color: colMuted},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	}); err != nil {
		return nil, err
	}
	if s.statValue, err = mk(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Size: 13, Color: colBrand},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		CustomNumFmt: strPtr(fmtInt),
	}); err != nil {
		return nil, err
	}
	if s.statValueMoney, err = mk(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Size: 13, Color: colBrand},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		CustomNumFmt: strPtr(fmtMoney),
	}); err != nil {
		return nil, err
	}

	if s.header, err = mk(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: colWhite},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{colBrand}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thinBorder(),
	}); err != nil {
		return nil, err
	}

	// Body styles: one per (kind, striped) pair.
	kinds := map[string]string{
		"text": "", "flag": "",
		"int": fmtInt, "num": fmtNum, "money": fmtMoney,
		"date": fmtDate, "datetime": fmtDateTime,
	}
	for kind, numFmt := range kinds {
		var pair [2]int
		for i, even := range []bool{false, true} {
			st := &excelize.Style{
				Font:      &excelize.Font{Size: 10, Color: colText},
				Border:    thinBorder(),
				Alignment: &excelize.Alignment{Vertical: "center", Horizontal: alignFor(kind)},
			}
			if numFmt != "" {
				st.CustomNumFmt = strPtr(numFmt)
			}
			if even {
				st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{colRowEven}}
			}
			id, err := mk(st)
			if err != nil {
				return nil, err
			}
			pair[i] = id
		}
		s.body[kind] = pair
	}

	for oi, on := range []bool{false, true} {
		for ei, even := range []bool{false, true} {
			colour := colMuted
			bold := false
			if on {
				colour = colGreen
				bold = true
			}
			st := &excelize.Style{
				Font:      &excelize.Font{Size: 10, Color: colour, Bold: bold},
				Border:    thinBorder(),
				Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "center"},
			}
			if even {
				st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{colRowEven}}
			}
			id, err := mk(st)
			if err != nil {
				return nil, err
			}
			s.flags[oi][ei] = id
		}
	}

	totalBase := func(numFmt string) *excelize.Style {
		st := &excelize.Style{
			Font:      &excelize.Font{Bold: true, Size: 10, Color: colWhite},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{colViolet}},
			Border:    thinBorder(),
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "right"},
		}
		if numFmt != "" {
			st.CustomNumFmt = strPtr(numFmt)
		}
		return st
	}
	if s.totalText, err = mk(totalBase("")); err != nil {
		return nil, err
	}
	if s.totalInt, err = mk(totalBase(fmtInt)); err != nil {
		return nil, err
	}
	if s.totalNum, err = mk(totalBase(fmtNum)); err != nil {
		return nil, err
	}
	if s.totalMoney, err = mk(totalBase(fmtMoney)); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *tripStyles) cell(kind string, even bool) int {
	pair, ok := s.body[kind]
	if !ok {
		pair = s.body["text"]
	}
	if even {
		return pair[1]
	}
	return pair[0]
}

func (s *tripStyles) flag(on, even bool) int {
	oi, ei := 0, 0
	if on {
		oi = 1
	}
	if even {
		ei = 1
	}
	return s.flags[oi][ei]
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

// Numbers right, flags centre, everything else left -- the convention the
// TypeScript export used and what makes a wide sheet scannable.
func alignFor(kind string) string {
	switch kind {
	case "int", "num", "money":
		return "right"
	case "flag", "date", "datetime":
		return "center"
	default:
		return "left"
	}
}

func thinBorder() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: colBorder, Style: 1},
		{Type: "right", Color: colBorder, Style: 1},
		{Type: "top", Color: colBorder, Style: 1},
		{Type: "bottom", Color: colBorder, Style: 1},
	}
}

func strPtr(s string) *string { return &s }
