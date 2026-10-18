package services

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// The receipt-piles workbook.
//
// Three sheets, in the order the work is actually done:
//
//	قائمة المراجعة — one row per receipt, with a tick box. This is the sheet
//	                 that gets printed and carried to the desk.
//	الأكوام        — the drop-off names inside each pile, alphabetical.
//	الصناديق       — one row per pile: which letters, how many names, how many
//	                 sheets, and how many are ticked off so far.
//
// The tick column is the spine: the "done" figures on الصناديق are COUNTIFS
// formulas over the checklist, so ticking a row updates the box progress with
// no macro and no re-export. That is lifted straight from the hand-built
// workbook this replaces, because it is the part people actually used.
//
// The layout is right-to-left throughout — the content is Arabic, and an LTR
// sheet puts the tick column at the far end of the reader's travel.

// Sheet names, kept as constants because the COUNTIFS formulas reference them.
const (
	pileSheetChecklist = "قائمة المراجعة"
	pileSheetPiles     = "الأكوام"
	pileSheetBoxes     = "الصناديق"
)

type pileStyles struct {
	title, subtitle int
	header          int
	data, dataAlt   int
	tick            int
	num, numAlt     int
	pileTag         int
	letterTag       int
	total           int
	muted           int
}

func buildPileStyles(f *excelize.File) (pileStyles, error) {
	var s pileStyles
	var err error
	mk := func(style *excelize.Style) int {
		if err != nil {
			return 0
		}
		id, e := f.NewStyle(style)
		if e != nil {
			err = e
		}
		return id
	}

	// Same palette as the trips export (and the first Watanya report), so they
	// look like one product.
	const (
		headerBg   = "2C5F7C"
		headerText = "FFFFFF"
		altRow     = "F8FAFB"
		border     = "D5DBDE"
		textDark   = "2C3E50"
		totalBg    = "E8F1F5"
		tagBg      = "D4E6ED"
		muted      = "7F8C8D"
	)

	center := &excelize.Alignment{Horizontal: "center", Vertical: "center"}
	centerWrap := &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	cairo := func(size float64, bold bool, color string) *excelize.Font {
		return &excelize.Font{Size: size, Bold: bold, Color: color, Family: "Cairo"}
	}
	fill := func(color string) excelize.Fill {
		return excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1}
	}
	rule := []excelize.Border{{Type: "bottom", Color: border, Style: 1}}

	s.title = mk(&excelize.Style{Font: cairo(20, true, headerBg), Alignment: center})
	s.subtitle = mk(&excelize.Style{Font: cairo(13, false, muted), Alignment: center})
	s.header = mk(&excelize.Style{
		Font: cairo(13, true, headerText), Fill: fill(headerBg),
		Alignment: centerWrap, Border: []excelize.Border{{Type: "bottom", Color: headerBg, Style: 2}},
	})
	s.data = mk(&excelize.Style{Font: cairo(12, false, textDark), Alignment: center, Border: rule})
	s.dataAlt = mk(&excelize.Style{
		Font: cairo(12, false, textDark), Fill: fill(altRow), Alignment: center, Border: rule,
	})
	// The tick column is wide, bold and empty — it is written on by hand.
	s.tick = mk(&excelize.Style{
		Font: cairo(14, true, headerBg), Alignment: center, Border: rule,
	})
	s.num = mk(&excelize.Style{
		Font: cairo(12, false, textDark), Alignment: center, Border: rule, NumFmt: 1,
	})
	s.numAlt = mk(&excelize.Style{
		Font: cairo(12, false, textDark), Fill: fill(altRow), Alignment: center, Border: rule, NumFmt: 1,
	})
	s.pileTag = mk(&excelize.Style{
		Font: cairo(13, true, headerBg), Fill: fill(tagBg), Alignment: center, Border: rule,
	})
	s.letterTag = mk(&excelize.Style{
		Font: cairo(16, true, headerBg), Alignment: center, Border: rule,
	})
	s.total = mk(&excelize.Style{
		Font: cairo(13, true, textDark), Fill: fill(totalBg), Alignment: center,
		Border: []excelize.Border{{Type: "top", Color: headerBg, Style: 2}},
	})
	s.muted = mk(&excelize.Style{Font: cairo(11, false, muted), Alignment: center})

	return s, err
}

// RenderPilePlanExcel writes the workbook and hands back the bytes.
//
// Bytes rather than a temp file: the trip export already works this way, and a
// file on disk means a path to clean up on every error path plus a second copy
// of a workbook that only ever travels straight out over the wire.
func RenderPilePlanExcel(plan PilePlan) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	s, err := buildPileStyles(f)
	if err != nil {
		return nil, fmt.Errorf("build styles: %w", err)
	}

	checklistRows, err := writePileChecklistSheet(f, s, plan)
	if err != nil {
		return nil, err
	}
	if err := writePilesSheet(f, s, plan); err != nil {
		return nil, err
	}
	if err := writeBoxesSheet(f, s, plan, checklistRows); err != nil {
		return nil, err
	}

	// Open on the summary — it is the sheet that answers "how many boxes do I
	// need and what goes in them", which is the first question.
	if idx, err := f.GetSheetIndex(pileSheetBoxes); err == nil {
		f.SetActiveSheet(idx)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

// rtl turns the sheet right-to-left. Every sheet here is Arabic.
func rtl(f *excelize.File, sheet string) {
	on := true
	_ = f.SetSheetView(sheet, 0, &excelize.ViewOptions{RightToLeft: &on})
}

// titleBlock writes the two merged rows every sheet opens with and returns the
// row the header belongs on.
func titleBlock(f *excelize.File, s pileStyles, sheet, title, lastCol string, plan PilePlan) int {
	f.MergeCell(sheet, "A1", lastCol+"1")
	f.SetCellValue(sheet, "A1", title)
	f.SetCellStyle(sheet, "A1", lastCol+"1", s.title)
	f.SetRowHeight(sheet, 1, 34)

	mode := "حرف لكل كومة"
	if plan.Mode == ModeBalanced {
		mode = fmt.Sprintf("توزيع متوازن على %d صناديق", len(plan.Piles))
	}
	f.MergeCell(sheet, "A2", lastCol+"2")
	f.SetCellValue(sheet, "A2", fmt.Sprintf("%s  —  %s   ·   %s   ·   %d إيصال / %d نقطة",
		plan.StartDate, plan.EndDate, mode, plan.TotalReceipts, plan.TotalDropOffs))
	f.SetCellStyle(sheet, "A2", lastCol+"2", s.subtitle)
	f.SetRowHeight(sheet, 2, 22)

	f.SetRowHeight(sheet, 3, 10) // spacer
	return 4
}

func writeHeaders(f *excelize.File, s pileStyles, sheet string, row int, headers []string, widths []float64) {
	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		cell := fmt.Sprintf("%s%d", col, row)
		f.SetCellValue(sheet, cell, h)
		f.SetCellStyle(sheet, cell, cell, s.header)
		f.SetColWidth(sheet, col, col, widths[i])
	}
	f.SetRowHeight(sheet, row, 30)
}

// writePileChecklistSheet writes one row per receipt and returns the last data
// row, which the box sheet's COUNTIFS formulas need.
func writePileChecklistSheet(f *excelize.File, s pileStyles, plan PilePlan) (int, error) {
	sheet := pileSheetChecklist
	f.SetSheetName("Sheet1", sheet)
	rtl(f, sheet)

	headers := []string{"✓", "صندوق", "الحرف", "نقطة التسليم", "#", "رقم الإيصال", "التاريخ", "المستودع", "السيارة", "ملاحظة"}
	widths := []float64{6, 9, 8, 30, 9, 16, 13, 16, 16, 22}
	headerRow := titleBlock(f, s, sheet, "أكوام الإيصالات — وطنية", "J", plan)
	writeHeaders(f, s, sheet, headerRow, headers, widths)

	row := headerRow + 1
	// Rows come out in filing order — pile, then letter, then name, then date —
	// so the sheet can be worked top to bottom without hunting.
	for _, pile := range plan.Piles {
		for _, letter := range pile.Letters {
			for _, drop := range letter.DropOffs {
				for _, r := range drop.Receipts {
					alt := row%2 == 0
					data, num := s.data, s.num
					if alt {
						data, num = s.dataAlt, s.numAlt
					}
					f.SetCellStyle(sheet, cellAt("A", row), cellAt("A", row), s.tick)
					set := func(col string, v interface{}, style int) {
						c := cellAt(col, row)
						f.SetCellValue(sheet, c, v)
						f.SetCellStyle(sheet, c, c, style)
					}
					set("B", pile.Index, num)
					set("C", letter.Letter, data)
					set("D", drop.Name, data)
					set("E", fmt.Sprintf("%d/%d", r.Seq, r.OutOf), data)
					set("F", r.ReceiptNo, data)
					set("G", r.Date, data)
					set("H", r.Terminal, data)
					set("I", r.CarNoPlate, data)
					set("J", "", data)
					f.SetRowHeight(sheet, row, 20)
					row++
				}
			}
		}
	}
	lastRow := row - 1

	// Freeze the header and let the filer filter down to one box.
	on := true
	_ = f.SetSheetView(sheet, 0, &excelize.ViewOptions{RightToLeft: &on})
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1), ActivePane: "bottomLeft",
	})
	if lastRow >= headerRow+1 {
		_ = f.AutoFilter(sheet, fmt.Sprintf("A%d:J%d", headerRow, lastRow), nil)
	}
	return lastRow, nil
}

func writePilesSheet(f *excelize.File, s pileStyles, plan PilePlan) error {
	sheet := pileSheetPiles
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	rtl(f, sheet)

	headers := []string{"صندوق", "الحرف", "نقطة التسليم", "عدد الإيصالات", "تم ✓", "ملاحظة"}
	widths := []float64{9, 8, 34, 14, 8, 24}
	headerRow := titleBlock(f, s, sheet, "نقاط التسليم داخل كل صندوق", "F", plan)
	writeHeaders(f, s, sheet, headerRow, headers, widths)

	row := headerRow + 1
	for _, pile := range plan.Piles {
		for _, letter := range pile.Letters {
			for _, drop := range letter.DropOffs {
				alt := row%2 == 0
				data, num := s.data, s.num
				if alt {
					data, num = s.dataAlt, s.numAlt
				}
				set := func(col string, v interface{}, style int) {
					c := cellAt(col, row)
					f.SetCellValue(sheet, c, v)
					f.SetCellStyle(sheet, c, c, style)
				}
				set("A", pile.Index, s.pileTag)
				set("B", letter.Letter, s.letterTag)
				set("C", drop.Name, data)
				set("D", drop.ReceiptCount, num)
				set("E", "", s.tick)
				set("F", "", data)
				f.SetRowHeight(sheet, row, 22)
				row++
			}
		}
	}

	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1), ActivePane: "bottomLeft",
	})
	if row-1 >= headerRow+1 {
		_ = f.AutoFilter(sheet, fmt.Sprintf("A%d:F%d", headerRow, row-1), nil)
	}
	return nil
}

func writeBoxesSheet(f *excelize.File, s pileStyles, plan PilePlan, checklistLastRow int) error {
	sheet := pileSheetBoxes
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	rtl(f, sheet)

	headers := []string{"صندوق", "الحروف", "عدد النقاط", "عدد الإيصالات", "تم", "المتبقي"}
	widths := []float64{10, 30, 13, 15, 10, 12}
	headerRow := titleBlock(f, s, sheet, "الصناديق", "F", plan)
	writeHeaders(f, s, sheet, headerRow, headers, widths)

	// The tick and remaining columns are live formulas over the checklist, so
	// the operator's progress shows here without another export. The ranges are
	// absolute; the checklist's B column holds the pile number and A the tick.
	firstData := headerRow + 1
	row := firstData
	for _, pile := range plan.Piles {
		num := s.num
		if row%2 == 0 {
			num = s.numAlt
		}
		set := func(col string, v interface{}, style int) {
			c := cellAt(col, row)
			f.SetCellValue(sheet, c, v)
			f.SetCellStyle(sheet, c, c, style)
		}
		set("A", pile.Index, s.pileTag)
		set("B", pile.Label, s.letterTag)
		set("C", pile.DropOffCount, num)
		set("D", pile.ReceiptCount, num)

		done := cellAt("E", row)
		if checklistLastRow >= firstData {
			f.SetCellFormula(sheet, done, fmt.Sprintf(
				`COUNTIFS('%s'!$B$%d:$B$%d,$A%d,'%s'!$A$%d:$A$%d,"<>")`,
				pileSheetChecklist, firstData, checklistLastRow, row,
				pileSheetChecklist, firstData, checklistLastRow))
		} else {
			f.SetCellValue(sheet, done, 0)
		}
		f.SetCellStyle(sheet, done, done, num)

		left := cellAt("F", row)
		f.SetCellFormula(sheet, left, fmt.Sprintf("D%d-E%d", row, row))
		f.SetCellStyle(sheet, left, left, num)

		f.SetRowHeight(sheet, row, 26)
		row++
	}

	// Totals.
	if row > firstData {
		f.SetCellValue(sheet, cellAt("B", row), "الإجمالي")
		for _, col := range []string{"C", "D", "E", "F"} {
			f.SetCellFormula(sheet, cellAt(col, row),
				fmt.Sprintf("SUM(%s%d:%s%d)", col, firstData, col, row-1))
		}
		f.SetCellStyle(sheet, cellAt("A", row), cellAt("F", row), s.total)
		f.SetRowHeight(sheet, row, 28)
		row++
	}

	row++
	f.MergeCell(sheet, cellAt("A", row), cellAt("F", row))
	f.SetCellValue(sheet, cellAt("A", row),
		"علّم عمود ✓ في ورقة قائمة المراجعة — أعمدة «تم» و«المتبقي» تتحدث تلقائياً")
	f.SetCellStyle(sheet, cellAt("A", row), cellAt("F", row), s.muted)

	if plan.SkippedReceipts > 0 {
		row++
		f.MergeCell(sheet, cellAt("A", row), cellAt("F", row))
		f.SetCellValue(sheet, cellAt("A", row), fmt.Sprintf(
			"%d رحلة بلا رقم إيصال لم تدخل الأكوام — راجعها في صفحة الرحلات", plan.SkippedReceipts))
		f.SetCellStyle(sheet, cellAt("A", row), cellAt("F", row), s.muted)
	}

	return nil
}

func cellAt(col string, row int) string { return fmt.Sprintf("%s%d", col, row) }
