package AbstractFunctions

import (
	"time"
)

func GetFormattedDate(date string) (string, error) {
	const (
		layoutISO = "2006-01-02"
		layoutUS  = "01/02/2006"
	)
	start_date, err := time.Parse(layoutISO, date)
	if err != nil {
		return "", err
	}
	return start_date.Format(layoutUS), nil
}

func GetFormattedDateExcel(date string) (string, error) {
	parsedTime, err := time.Parse("01-02-06", date)
	if err != nil {
		return "", err
	}

	// Format the parsedTime to the desired layout "2006-01-02"
	formattedDate := parsedTime.Format("2006-01-02")

	return formattedDate, nil
}

func ParseDate(dateStr string) (time.Time, error) {
	// Change the layout to match the "dd-mm-yyyy" format
	layout := "02-01-2006"
	return time.Parse(layout, dateStr)
}
