package Apis

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"Falcon/Models"
	"Falcon/services"

	"github.com/gofiber/fiber/v2"
)

// The expiry window is the caller's, not ours. The frontend owns that rule
// (DOCUMENT_EXPIRY_WARNING_DAYS in entities/car/expiry.ts) and apex-rust takes
// it as a query parameter for the same reason: a second definition here would
// be one more thing to keep in step with the screen the sheet is exported from.
const (
	defaultCarDocHorizonDays = 30
	maxCarDocHorizonDays     = 3650
)

// ExportCarsExcel streams an .xlsx of the fleet and its dated documents.
func ExportCarsExcel(c *fiber.Ctx) error {
	var labels services.CarExportLabels
	if err := c.BodyParser(&labels); err != nil {
		labels = services.CarExportLabels{}
	}

	horizon := defaultCarDocHorizonDays
	if raw := c.Query("doc_horizon_days"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			// Clamped: past ten years it is not a filter but the whole table,
			// and a negative one would mark every valid paper as expiring.
			if v < 0 {
				v = 0
			}
			if v > maxCarDocHorizonDays {
				v = maxCarDocHorizonDays
			}
			horizon = v
		}
	}

	var cars []Models.Car
	if err := Models.DB.Model(&Models.Car{}).
		Preload("Driver").
		Order("car_no_plate").
		Find(&cars).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch cars",
			"error":   err.Error(),
		})
	}

	if len(cars) == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "No vehicles to export",
		})
	}

	meta := fmt.Sprintf("%s · %d vehicles · %d-day expiry window",
		time.Now().Format("2 Jan 2006 15:04"), len(cars), horizon)

	data, err := services.BuildCarsExcel(cars, labels, horizon, meta)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to build the spreadsheet",
			"error":   err.Error(),
		})
	}

	filename := fmt.Sprintf("cars-%s.xlsx", time.Now().Format("2006-01-02"))
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.PathEscape(filename)))
	// Without this the browser's fetch cannot read the header to name the file.
	c.Set("Access-Control-Expose-Headers", "Content-Disposition")
	c.Set("Cache-Control", "no-store")

	return c.Status(http.StatusOK).Send(data)
}
