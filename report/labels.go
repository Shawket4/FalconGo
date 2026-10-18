package report

import (
	"fmt"
	"time"
)

// texts holds every label the sheets print, per language.
type texts struct {
	// shared
	prepared   string // "Prepared %s"
	pageOf     string // footer; keeps the &P page-number code
	tripsDated string

	// Summary
	summaryTitle                                  string
	kTrips, kCapacity, kDropOffs, kAmount         string
	nRoutes, nAvgLitres, nTerminals, nAvgAmount   string
	byTerminal                                    string
	hTerminal, hTrips, hCapacity, hRoutes, hShare string
	hAmount, total                                string
	detailBy, unitsNote                           string
	hNo, hDropOff, hDistance, hFeeIdx, hFeeRate   string
	subtotal, grandTotal                          string

	// Trip Details
	detailsTitle                                  string
	stats                                         string // "%s trips  ·  %d routes"
	hDate, hReceipt, hDriver, hPlate, hDistanceKm string
	hCost                                         string
	terminalLabel                                 string // "Terminal: "
	tripCount                                     func(n int) string
	cont                                          string
}

var enTexts = texts{
	prepared:   "Prepared %s",
	pageOf:     "Page &P of %d",
	tripsDated: "Trips dated %s",

	summaryTitle: "Summary by drop-off point",
	kTrips:       "TRIPS",
	kCapacity:    "CAPACITY (L)",
	kDropOffs:    "DROP-OFF POINTS",
	kAmount:      "TOTAL AMOUNT (EGP)",
	nRoutes:      "across %d routes",
	nAvgLitres:   "avg %s L per trip",
	nTerminals:   "from %d terminals",
	nAvgAmount:   "avg %s per trip",
	byTerminal:   "By terminal",
	hTerminal:    "Terminal",
	hTrips:       "Trips",
	hCapacity:    "Capacity (L)",
	hRoutes:      "Routes",
	hShare:       "Share of amount",
	hAmount:      "Amount (EGP)",
	total:        "Total",
	detailBy:     "Detail by drop-off point",
	unitsNote:    "Fee rate per 1,000 L  ·  amounts in EGP  ·  distance in km",
	hNo:          "#",
	hDropOff:     "Drop-off point",
	hDistance:    "Distance",
	hFeeIdx:      "Fee idx",
	hFeeRate:     "Fee rate",
	subtotal:     "Subtotal",
	grandTotal:   "Grand total",

	detailsTitle: "Trip details",
	stats:        "%s trips  ·  %d routes",
	hDate:        "Date",
	hReceipt:     "Receipt",
	hDriver:      "Driver",
	hPlate:       "Plate",
	hDistanceKm:  "Distance (km)",
	hCost:        "Cost (EGP)",
	tripCount: func(n int) string {
		if n == 1 {
			return "1 trip"
		}
		return fmt.Sprintf("%d trips", n)
	},
	terminalLabel: "Terminal: ",
	cont:          "continued",
}

var arTexts = texts{
	prepared:   "تاريخ الإعداد: %s",
	pageOf:     "&P / %d", // LibreOffice garbles Arabic words around the page field
	tripsDated: "فترة الرحلات: %s",

	summaryTitle: "ملخص حسب نقطة التفريغ",
	kTrips:       "عدد الرحلات",
	kCapacity:    "الكمية (لتر)",
	kDropOffs:    "نقاط التفريغ",
	kAmount:      "إجمالي المبلغ (ج.م)",
	nRoutes:      "عدد المسارات: %d",
	nAvgLitres:   "متوسط الرحلة: %s لتر",
	nTerminals:   "عدد المستودعات: %d",
	nAvgAmount:   "متوسط الرحلة: %s ج.م",
	byTerminal:   "حسب المستودع",
	hTerminal:    "المستودع",
	hTrips:       "الرحلات",
	hCapacity:    "الكمية (لتر)",
	hRoutes:      "المسارات",
	hShare:       "النسبة من المبلغ",
	hAmount:      "المبلغ (ج.م)",
	total:        "الإجمالي",
	detailBy:     "التفاصيل حسب نقطة التفريغ",
	unitsNote:    "الفئة لكل 1,000 لتر  ·  المبالغ بالجنيه المصري  ·  المسافة بالكيلومتر",
	hNo:          "م",
	hDropOff:     "نقطة التفريغ",
	hDistance:    "المسافة",
	hFeeIdx:      "الشريحة",
	hFeeRate:     "الفئة",
	subtotal:     "إجمالي فرعي",
	grandTotal:   "الإجمالي العام",

	detailsTitle:  "تفاصيل الرحلات",
	stats:         "عدد الرحلات: %s  ·  عدد المسارات: %d",
	hDate:         "التاريخ",
	hReceipt:      "رقم الإيصال",
	hDriver:       "السائق",
	hPlate:        "اللوحة",
	hDistanceKm:   "المسافة (كم)",
	hCost:         "التكلفة (ج.م)",
	tripCount:     func(n int) string { return fmt.Sprintf("عدد الرحلات: %d", n) },
	terminalLabel: "المستودع: ",
	cont:          "تابع",
}

func (l Lang) texts() *texts {
	if l == Arabic {
		return &arTexts
	}
	return &enTexts
}

var arMonths = [12]string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}

// formatDate renders "18 Oct 2026" or "18 أكتوبر 2026".
func formatDate(l Lang, t time.Time) string {
	if l == Arabic {
		return fmt.Sprintf("%d %s %d", t.Day(), arMonths[t.Month()-1], t.Year())
	}
	return t.Format("2 Jan 2006")
}

// monthYear renders "September 2026" or "سبتمبر 2026".
func monthYear(l Lang, t time.Time) string {
	if l == Arabic {
		return fmt.Sprintf("%s %d", arMonths[t.Month()-1], t.Year())
	}
	return t.Format("January 2006")
}

// dateRange renders a compact range: "1–30 Sep 2026" or "من 1 إلى 30 سبتمبر 2026".
func dateRange(l Lang, a, b time.Time) string {
	if a.After(b) {
		a, b = b, a
	}
	sameYear := a.Year() == b.Year()
	sameMonth := sameYear && a.Month() == b.Month()
	if sameMonth && a.Day() == b.Day() {
		return formatDate(l, b)
	}
	if l == Arabic {
		switch {
		case sameMonth:
			return fmt.Sprintf("من %d إلى %d %s %d", a.Day(), b.Day(), arMonths[b.Month()-1], b.Year())
		case sameYear:
			return fmt.Sprintf("من %d %s إلى %d %s %d", a.Day(), arMonths[a.Month()-1], b.Day(), arMonths[b.Month()-1], b.Year())
		default:
			return "من " + formatDate(l, a) + " إلى " + formatDate(l, b)
		}
	}
	switch {
	case sameMonth:
		return fmt.Sprintf("%d–%s", a.Day(), b.Format("2 Jan 2006"))
	case sameYear:
		return a.Format("2 Jan") + " – " + b.Format("2 Jan 2006")
	default:
		return a.Format("2 Jan 2006") + " – " + b.Format("2 Jan 2006")
	}
}
