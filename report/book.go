package report

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Book is a workbook whose sheets are written by this package and can be
// saved as .xlsx or drawn as a PDF of any of its sheets.
type Book struct {
	File   *excelize.File
	sheet  map[string]*sheetRecord
	order  []string
	first  bool   // the default "Sheet1" has not been claimed yet
	active string // the sheet the workbook opens on; "" = the first
}

// SetActive makes the workbook open on the named sheet.
func (b *Book) SetActive(sheet string) { b.active = sheet }

// NewBook starts an empty workbook.
func NewBook() *Book {
	return &Book{File: excelize.NewFile(), sheet: map[string]*sheetRecord{}, first: true}
}

// claim names the next sheet: the first one takes over the default Sheet1.
func (b *Book) claim(name string) error {
	if b.first {
		b.first = false
		return b.File.SetSheetName("Sheet1", name)
	}
	return nil
}

func (b *Book) keep(rec *sheetRecord) {
	b.sheet[rec.name] = rec
	b.order = append(b.order, rec.name)
}

// Summary writes the Watanya Summary sheet; it returns the printed pages.
func (b *Book) Summary(sheet string, rep Report) (int, error) {
	if err := b.claim(sheet); err != nil {
		return 0, err
	}
	n, rec, err := writeSummarySheet(b.File, sheet, rep)
	if err == nil {
		b.keep(rec)
	}
	return n, err
}

// Details writes the Watanya Trip Details sheet; it returns the printed pages.
func (b *Book) Details(sheet string, rep Report) (int, error) {
	if err := b.claim(sheet); err != nil {
		return 0, err
	}
	n, rec, err := writeDetailsSheet(b.File, sheet, rep)
	if err == nil {
		b.keep(rec)
	}
	return n, err
}

// XLSX returns the workbook bytes, opening on the active sheet.
func (b *Book) XLSX() ([]byte, error) {
	idx := 0
	if b.active != "" {
		if i, err := b.File.GetSheetIndex(b.active); err == nil && i >= 0 {
			idx = i
		}
	}
	b.File.SetActiveSheet(idx)
	var buf bytes.Buffer
	if err := b.File.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PDF draws the named sheets (all of them when none are named), in order,
// into one PDF with Cairo embedded.
func (b *Book) PDF(title string, sheets ...string) ([]byte, error) {
	if len(sheets) == 0 {
		sheets = b.order
	}
	var recs []*sheetRecord
	for _, s := range sheets {
		rec, ok := b.sheet[s]
		if !ok {
			return nil, fmt.Errorf("report: no sheet %q in this book", s)
		}
		recs = append(recs, rec)
	}
	return renderPDF(title, recs)
}

// Close releases the workbook.
func (b *Book) Close() error { return b.File.Close() }
