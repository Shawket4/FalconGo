package services

import (
	"fmt"
	"time"

	"Falcon/report"
)

// The receipt-piles workbook.
//
// Three sheets, in the order the work is actually done:
//
//	قائمة المراجعة (Checklist) — one row per receipt, with a tick box. This is
//	                 the sheet that gets printed and carried to the desk.
//	الأكوام (Piles) — the drop-off names inside each pile, alphabetical.
//	الصناديق (Boxes) — one row per pile: which letters, how many names, how
//	                 many sheets, and how many are ticked off so far.
//
// The tick column is the spine: the "done" figures on the Boxes sheet are
// COUNTIFS formulas over the checklist, so ticking a row updates the box
// progress with no macro and no re-export. That is lifted straight from the
// hand-built workbook this replaces, because it is the part people used.
//
// The look is the Watanya report's (package report): title block, a band per
// box, a column header repeated on every printed page, A4 print setup, and
// the same sheets as a PDF. Arabic (right-to-left) is the default; English
// is available for the same plan.

// pileTexts is every caption the workbook prints, per language.
type pileTexts struct {
	sheetChecklist, sheetPiles, sheetBoxes string
	titleChecklist, titlePiles, titleBoxes string
	checklistHeaders, pilesHeaders         []string
	boxesHeaders                           []string
	modeLetter, modeBalanced               string // modeBalanced takes the box count
	counts                                 string // receipts, drop-offs
	bandNote                               string // receipts in the box
	total                                  string
	cardBoxes, cardReceipts, cardDropOffs  string
	cardLetters                            string
	tickNote, skippedNote                  string
}

var pileTextsAR = pileTexts{
	sheetChecklist: "قائمة المراجعة", sheetPiles: "الأكوام", sheetBoxes: "الصناديق",
	titleChecklist: "أكوام الإيصالات — وطنية", titlePiles: "نقاط التسليم داخل كل صندوق", titleBoxes: "الصناديق",
	checklistHeaders: []string{"✓", "صندوق", "الحرف", "نقطة التسليم", "#", "رقم الإيصال", "التاريخ", "المستودع", "السيارة", "ملاحظة"},
	pilesHeaders:     []string{"صندوق", "الحرف", "نقطة التسليم", "عدد الإيصالات", "تم ✓", "ملاحظة"},
	boxesHeaders:     []string{"صندوق", "الحروف", "عدد النقاط", "عدد الإيصالات", "تم", "المتبقي"},
	modeLetter:       "حرف لكل كومة",
	modeBalanced:     "توزيع متوازن على %d صناديق",
	counts:           "%d إيصال / %d نقطة",
	bandNote:         "عدد الإيصالات: %d",
	total:            "الإجمالي",
	cardBoxes:        "الصناديق", cardReceipts: "الإيصالات", cardDropOffs: "نقاط التسليم", cardLetters: "الحروف",
	tickNote:    "علّم عمود ✓ في ورقة قائمة المراجعة — أعمدة «تم» و«المتبقي» تتحدث تلقائياً",
	skippedNote: "%d رحلة بلا رقم إيصال لم تدخل الأكوام — راجعها في صفحة الرحلات",
}

var pileTextsEN = pileTexts{
	sheetChecklist: "Checklist", sheetPiles: "Piles", sheetBoxes: "Boxes",
	titleChecklist: "Receipt piles — Watanya", titlePiles: "Drop-off points in each box", titleBoxes: "Boxes",
	checklistHeaders: []string{"✓", "Box", "Letter", "Drop-off point", "#", "Receipt no.", "Date", "Terminal", "Vehicle", "Note"},
	pilesHeaders:     []string{"Box", "Letter", "Drop-off point", "Receipts", "Done ✓", "Note"},
	boxesHeaders:     []string{"Box", "Letters", "Drop-off points", "Receipts", "Done", "Remaining"},
	modeLetter:       "One letter per pile",
	modeBalanced:     "Balanced over %d boxes",
	counts:           "%d receipts / %d drop-off points",
	bandNote:         "%d receipts",
	total:            "Total",
	cardBoxes:        "BOXES", cardReceipts: "RECEIPTS", cardDropOffs: "DROP-OFF POINTS", cardLetters: "LETTERS",
	tickNote:    "Tick the ✓ column on the Checklist sheet — Done and Remaining update by themselves",
	skippedNote: "%d trips with no receipt number are not in any pile — check them on the trips page",
}

// RenderPilePlanExcel writes the Arabic workbook and hands back the bytes.
func RenderPilePlanExcel(plan PilePlan) ([]byte, error) {
	return RenderPilePlan(plan, "ar", false)
}

// RenderPilePlan renders the plan as a workbook, or with asPDF as a PDF of
// its three sheets. lang is "en" or "ar" (anything else is Arabic, the
// workbook's original language).
//
// Bytes rather than a temp file: a file on disk means a path to clean up on
// every error path plus a second copy that only ever travels over the wire.
func RenderPilePlan(plan PilePlan, lang string, asPDF bool) ([]byte, error) {
	tx, rl := pileTextsAR, report.Arabic
	if lang == "en" {
		tx, rl = pileTextsEN, report.English
	}
	mode := tx.modeLetter
	if plan.Mode == ModeBalanced {
		mode = fmt.Sprintf(tx.modeBalanced, len(plan.Piles))
	}
	base := report.Table{
		Lang: rl, Company: "Apex Transport",
		Subtitle: mode + "  ·  " + fmt.Sprintf(tx.counts, plan.TotalReceipts, plan.TotalDropOffs),
		Period:   pilePeriod(rl, plan.StartDate, plan.EndDate),
	}

	book := report.NewBook()
	defer book.Close()

	// ── Checklist: one row per receipt, a band per box ──────────────────────
	checklist := base
	checklist.Title = tx.titleChecklist
	checklist.Landscape = true
	checklist.AutoFilter, checklist.FreezeHeader = true, true
	checklist.Columns = pileColumns(tx.checklistHeaders, []float64{6, 9, 8, 30, 9, 16, 13, 16, 16, 22},
		[]report.ColKind{report.Tick, report.Center, report.Center, report.Text, report.Center,
			report.Center, report.Date, report.Text, report.Center, report.Text})
	for _, pile := range plan.Piles {
		band := report.Band{No: pile.Index, Title: pile.Label, Note: fmt.Sprintf(tx.bandNote, pile.ReceiptCount)}
		// Rows come out in filing order — pile, then letter, then name, then
		// date — so the sheet can be worked top to bottom without hunting.
		for _, letter := range pile.Letters {
			for _, drop := range letter.DropOffs {
				for _, r := range drop.Receipts {
					band.Rows = append(band.Rows, []any{
						nil, pile.Index, letter.Letter, drop.Name,
						fmt.Sprintf("%d/%d", r.Seq, r.OutOf), r.ReceiptNo, pileDate(r.Date),
						r.Terminal, r.CarNoPlate, "",
					})
				}
			}
		}
		checklist.Bands = append(checklist.Bands, band)
	}
	cl, err := book.Table(tx.sheetChecklist, checklist)
	if err != nil {
		return nil, fmt.Errorf("checklist sheet: %w", err)
	}
	hasChecklistRows := plan.TotalReceipts > 0

	// ── Piles: the drop-off names inside each box ───────────────────────────
	piles := base
	piles.Title = tx.titlePiles
	piles.AutoFilter, piles.FreezeHeader = true, true
	piles.Columns = pileColumns(tx.pilesHeaders, []float64{9, 8, 34, 14, 8, 24},
		[]report.ColKind{report.Center, report.Center, report.Text, report.Int, report.Tick, report.Text})
	for _, pile := range plan.Piles {
		band := report.Band{No: pile.Index, Title: pile.Label, Note: fmt.Sprintf(tx.bandNote, pile.ReceiptCount)}
		for _, letter := range pile.Letters {
			for _, drop := range letter.DropOffs {
				band.Rows = append(band.Rows, []any{pile.Index, letter.Letter, drop.Name, drop.ReceiptCount, nil, ""})
			}
		}
		piles.Bands = append(piles.Bands, band)
	}
	if _, err := book.Table(tx.sheetPiles, piles); err != nil {
		return nil, fmt.Errorf("piles sheet: %w", err)
	}

	// ── Boxes: one row per box, progress as live formulas ───────────────────
	boxes := base
	boxes.Title = tx.titleBoxes
	boxes.Columns = pileColumns(tx.boxesHeaders, []float64{10, 30, 13, 15, 10, 12},
		[]report.ColKind{report.Center, report.Center, report.Int, report.Int, report.Int, report.Int})
	for i := 2; i < 6; i++ {
		boxes.Columns[i].Total = true
	}
	boxes.Cards = []report.Card{
		{Label: tx.cardBoxes, Value: report.FormatCount(int64(len(plan.Piles)))},
		{Label: tx.cardLetters, Value: report.FormatCount(int64(plan.TotalLetters))},
		{Label: tx.cardDropOffs, Value: report.FormatCount(int64(plan.TotalDropOffs))},
		{Label: tx.cardReceipts, Value: report.FormatCount(int64(plan.TotalReceipts)), Dark: true},
	}
	var rows [][]any
	for _, pile := range plan.Piles {
		// The tick and remaining columns are live formulas over the checklist,
		// so the operator's progress shows here without another export. The
		// checklist's B column holds the box number and A the tick.
		done := report.Formula{Value: 0, Expr: func(row int) string {
			return fmt.Sprintf(`COUNTIFS('%s'!$B$%d:$B$%d,$A%d,'%s'!$A$%d:$A$%d,"<>")`,
				tx.sheetChecklist, cl.FirstRow, cl.LastRow, row, tx.sheetChecklist, cl.FirstRow, cl.LastRow)
		}}
		var doneCell any = done
		if !hasChecklistRows {
			doneCell = 0
		}
		left := report.Formula{Value: pile.ReceiptCount, Expr: func(row int) string {
			return fmt.Sprintf("D%d-E%d", row, row)
		}}
		rows = append(rows, []any{pile.Index, pile.Label, pile.DropOffCount, pile.ReceiptCount, doneCell, left})
	}
	boxes.Bands = []report.Band{{Rows: rows}}
	if len(rows) > 0 {
		boxes.TotalLabel, boxes.TotalMode = tx.total, report.TotalSum
	}
	boxes.Footnotes = []string{tx.tickNote}
	if plan.SkippedReceipts > 0 {
		boxes.Footnotes = append(boxes.Footnotes, fmt.Sprintf(tx.skippedNote, plan.SkippedReceipts))
	}
	if _, err := book.Table(tx.sheetBoxes, boxes); err != nil {
		return nil, fmt.Errorf("boxes sheet: %w", err)
	}

	// Open on the boxes — the sheet that answers "how many boxes do I need
	// and what goes in them", which is the first question.
	book.SetActive(tx.sheetBoxes)
	if asPDF {
		return book.PDF(tx.titleChecklist)
	}
	return book.XLSX()
}

func pileColumns(headers []string, widths []float64, kinds []report.ColKind) []report.Column {
	cols := make([]report.Column, len(headers))
	for i := range headers {
		cols[i] = report.Column{Header: headers[i], Width: widths[i], Kind: kinds[i]}
	}
	return cols
}

// pileDate turns the stored YYYY-MM-DD text into a date cell; anything else
// prints as it was stored.
func pileDate(s string) any {
	if d, err := time.Parse("2006-01-02", s); err == nil {
		return d
	}
	return s
}

func pilePeriod(l report.Lang, start, end string) string {
	a, errA := time.Parse("2006-01-02", start)
	b, errB := time.Parse("2006-01-02", end)
	if errA != nil || errB != nil {
		return start + " – " + end
	}
	return report.RangeLabel(l, a, b)
}
