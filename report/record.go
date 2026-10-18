package report

import (
	"github.com/xuri/excelize/v2"
)

// sheetRecord is what a sheetWriter wrote, kept so the same sheet can also be
// drawn as a PDF: every cell's value and format, merges, row heights, column
// widths, page breaks and the print setup. The PDF is rendered from this
// record, never from a second layout, so its figures, rows and page breaks
// are the workbook's by construction.
type sheetRecord struct {
	name      string
	title     string // document title for the PDF
	rtl       bool
	landscape bool
	widths    map[int]float64 // column index (1-based) → width in Excel units
	heights   map[int]float64
	cells     map[cellKey]*recCell
	merges    map[cellKey]cellKey // top-left → bottom-right
	covered   map[cellKey]bool    // cells inside a merge, other than its top-left
	breaks    []int               // first row of each page after the first
	print     printOpts
	header    [2]hfText // running header (pages 2+): start side, end side
	footer    [2]hfText // footer (every page): start side, end side; "&P" = page number
}

type cellKey struct{ col, row int }

type recCell struct {
	value any        // nil = empty
	spans []textSpan // rich text, instead of value
	f     cellFmt
}

type hfText struct {
	text  string
	bold  bool
	size  float64
	color string
}

func newRecord(name string, rtl bool) *sheetRecord {
	return &sheetRecord{
		name: name, rtl: rtl,
		widths: map[int]float64{}, heights: map[int]float64{},
		cells:  map[cellKey]*recCell{},
		merges: map[cellKey]cellKey{}, covered: map[cellKey]bool{},
	}
}

func keyOf(ref string) cellKey {
	c, r, _ := excelize.CellNameToCoordinates(ref)
	return cellKey{c, r}
}

func (s *sheetRecord) at(k cellKey) *recCell {
	c := s.cells[k]
	if c == nil {
		c = &recCell{}
		s.cells[k] = c
	}
	return c
}

func (s *sheetRecord) merge(from, to string) {
	a, b := keyOf(from), keyOf(to)
	if a == b {
		return
	}
	s.merges[a] = b
	for col := a.col; col <= b.col; col++ {
		for row := a.row; row <= b.row; row++ {
			if (cellKey{col, row}) != a {
				s.covered[cellKey{col, row}] = true
			}
		}
	}
}

func (s *sheetRecord) paint(from, to string, f cellFmt) {
	a, b := keyOf(from), keyOf(to)
	for col := a.col; col <= b.col; col++ {
		for row := a.row; row <= b.row; row++ {
			s.at(cellKey{col, row}).f = f
		}
	}
}

func (s *sheetRecord) lastCol() int {
	n := 0
	for c := range s.widths {
		n = max(n, c)
	}
	return n
}
