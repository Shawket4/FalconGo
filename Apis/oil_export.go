package Apis

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/services"

	"github.com/gofiber/fiber/v2"
)

// ExportOilChangesExcel streams an .xlsx of every oil-change record.
//
// Replaces two separate browser-side ExcelJS builds — one on the fleet screen
// that exported only the latest change per vehicle, one on the history screen
// that exported a single truck. Neither could produce the whole fleet's
// history; you had to open every vehicle in turn. This returns both shapes in
// one workbook: a master sheet of the latest change per vehicle, then a sheet
// per vehicle carrying its full history.
//
// Unfiltered on purpose. The fleet screen's filters are a way of finding a
// truck on a page, not a statement about which records matter, and a
// spreadsheet that silently omitted history would be the kind of wrong that
// looks complete.
func ExportOilChangesExcel(c *fiber.Ctx) error {
	// Labels come from the client so the sheet reads in the user's language
	// and there is no second set of English strings here to drift.
	var labels services.OilExportLabels
	if err := c.BodyParser(&labels); err != nil {
		labels = services.OilExportLabels{}
	}

	var records []Models.OilChange
	if err := Models.DB.Model(&Models.OilChange{}).
		Order("date DESC, id DESC").
		Find(&records).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch oil changes",
			"error":   err.Error(),
		})
	}

	if len(records) == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "No oil-change records to export",
		})
	}

	plates := map[string]bool{}
	for _, r := range records {
		plates[r.CarNoPlate] = true
	}

	var data []byte
	var err error
	// The two halves of an export cost very different amounts; split them so a
	// slow one says which half was slow.
	err = Observability.Span(c, "export.build.oil_changes", func() error {
		var e error
		data, e = services.BuildOilChangesExcel(
			records, labels, services.OilExportMeta(len(records), len(plates)))
		return e
	})
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to build the spreadsheet",
			"error":   err.Error(),
		})
	}

	filename := fmt.Sprintf("oil-changes-%s.xlsx", time.Now().Format("2006-01-02"))

	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.PathEscape(filename)))
	// Without this the browser's fetch cannot read the header to name the file.
	c.Set("Access-Control-Expose-Headers", "Content-Disposition")
	c.Set("Cache-Control", "no-store")

	return c.Status(http.StatusOK).Send(data)
}
