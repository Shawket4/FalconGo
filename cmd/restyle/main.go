// Command restyle rebuilds an existing watanya_report workbook with the new
// Summary and Trip Details sheets. It is a harness for previewing the layout;
// in the API, fill report.Report from your own data instead of parsing the
// old sheets.
//
//	go run ./cmd/restyle -in watanya_report_16.xlsx -out styled_en.xlsx
//	go run ./cmd/restyle -in watanya_report_16.xlsx -out styled_ar.xlsx -lang ar
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"Falcon/report"
)

func main() {
	in := flag.String("in", "", "existing report .xlsx")
	out := flag.String("out", "", "output .xlsx")
	lang := flag.String("lang", "en", "en or ar")
	company := flag.String("company", "Apex Transport", "company name")
	title := flag.String("title", "", "report title (default per language)")
	flag.Parse()
	if *in == "" || *out == "" {
		log.Fatal("usage: restyle -in report.xlsx -out styled.xlsx [-lang ar]")
	}

	src, err := excelize.OpenFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	defer src.Close()
	groups, err := readOldSummary(src, "Summary")
	if err != nil {
		log.Fatal(err)
	}
	sections, err := readOldDetails(src, "Trip Details")
	if err != nil {
		log.Fatal(err)
	}

	rep := report.Report{
		Lang:     report.English,
		Company:  *company,
		Title:    "Watanya Trips Report",
		Groups:   groups,
		Sections: sections,
	}
	summaryName, detailsName := "Summary", "Trip Details"
	if *lang == "ar" {
		rep.Lang = report.Arabic
		rep.Title = "تقرير رحلات وطنية"
		summaryName, detailsName = "الملخص", "تفاصيل الرحلات"
	}
	if *title != "" {
		rep.Title = *title
	}

	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", summaryName); err != nil {
		log.Fatal(err)
	}
	p1, err := report.WriteSummarySheet(f, summaryName, rep)
	if err != nil {
		log.Fatal(err)
	}
	p2, err := report.WriteDetailsSheet(f, detailsName, rep)
	if err != nil {
		log.Fatal(err)
	}
	f.SetActiveSheet(0)
	if err := f.SaveAs(*out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: Summary %d pages, Trip Details %d pages\n", *out, p1, p2)
}

// readOldSummary parses the previous Summary layout: header in row 5, line
// rows, a "Subtotal: <name>" row per drop-off point, then "GRAND TOTAL".
func readOldSummary(f *excelize.File, sheet string) ([]report.SummaryGroup, error) {
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, err
	}
	var groups []report.SummaryGroup
	var cur []report.SummaryLine
	var name string
	for i := 5; i < len(rows); i++ {
		c := make([]string, 9)
		copy(c, rows[i])
		label := strings.TrimSpace(c[1])
		switch {
		case strings.HasPrefix(label, "Subtotal:"):
			groups = append(groups, report.SummaryGroup{DropOff: name, Lines: cur})
			cur, name = nil, ""
		case strings.EqualFold(label, "GRAND TOTAL"):
			if len(cur) > 0 {
				return nil, errors.New("lines after the last subtotal")
			}
			return groups, nil
		case strings.TrimSpace(c[0]) != "":
			name = label
			cur = append(cur, report.SummaryLine{
				No: atoi(c[0]), Terminal: strings.TrimSpace(c[2]), Trips: atoi(c[3]),
				Capacity: atof(c[4]), Distance: atof(c[5]), FeeIndex: atoi(c[6]),
				FeeRate: atof(c[7]), Amount: atof(c[8]),
			})
		}
	}
	return nil, errors.New("no GRAND TOTAL row found")
}

// readOldDetails parses the previous Trip Details layout: a "terminal ←
// drop-off" title per route, a header row, trip rows and a TOTAL row.
func readOldDetails(f *excelize.File, sheet string) ([]report.TripSection, error) {
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, err
	}
	isDate := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	var secs []report.TripSection
	for _, row := range rows {
		c := make([]string, 9)
		copy(c, row)
		a := strings.TrimSpace(c[0])
		switch {
		case strings.Contains(a, "←"):
			parts := strings.SplitN(a, "←", 2)
			secs = append(secs, report.TripSection{
				No: len(secs) + 1, Terminal: strings.TrimSpace(parts[0]), DropOff: strings.TrimSpace(parts[1]),
			})
		case len(secs) > 0 && isDate.MatchString(strings.TrimSpace(c[1])):
			d, err := time.Parse("2006-01-02", strings.TrimSpace(c[1]))
			if err != nil {
				return nil, err
			}
			s := &secs[len(secs)-1]
			s.Trips = append(s.Trips, report.Trip{
				Date: d, Receipt: strings.TrimSpace(c[2]), Driver: strings.TrimSpace(c[3]),
				Plate: strings.TrimSpace(c[4]), Capacity: atof(c[5]), Distance: atof(c[6]),
				FeeRate: atof(c[7]), Cost: atof(c[8]),
			})
		}
	}
	if len(secs) == 0 {
		return nil, errors.New("no trip sections found")
	}
	return secs, nil
}

func atoi(s string) int {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int(v)
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
