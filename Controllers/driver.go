package Controllers

import (
	"Falcon/Models"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetDriverLocationPings(c *fiber.Ctx) error {
	id := c.Params("id")
	from := c.Query("from")
	to := c.Query("to")

	// Load Cairo timezone
	cairoLocation, err := time.LoadLocation("Africa/Cairo")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to load Cairo timezone",
		})
	}

	var driverLocationPings []Models.LocationPing
	query := Models.DB.Where("driver_id = ?", id)

	if from != "" {
		// Try multiple formats in order
		var fromTime time.Time
		var parseErr error

		// Try RFC3339 with timezone (e.g., 2025-10-09T08:00:00Z)
		fromTime, parseErr = time.Parse(time.RFC3339, from)
		if parseErr != nil {
			// Try ISO 8601 without timezone (e.g., 2025-10-09T08:00:00)
			// Assume Cairo timezone if no timezone provided
			fromTime, parseErr = time.ParseInLocation("2006-01-02T15:04:05", from, cairoLocation)
			if parseErr != nil {
				// Try date only (e.g., 2025-10-09)
				// Assume Cairo timezone and start of day
				fromTime, parseErr = time.ParseInLocation("2006-01-02", from, cairoLocation)
				if parseErr != nil {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error": "Invalid 'from' date format. Use YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS",
					})
				}
			}
		}
		query = query.Where("time_stamp >= ?", fromTime)
	}

	if to != "" {
		var toTime time.Time
		var parseErr error

		// Try RFC3339 with timezone
		toTime, parseErr = time.Parse(time.RFC3339, to)
		if parseErr != nil {
			// Try ISO 8601 without timezone
			// Assume Cairo timezone if no timezone provided
			toTime, parseErr = time.ParseInLocation("2006-01-02T15:04:05", to, cairoLocation)
			if parseErr != nil {
				// Try date only
				// Assume Cairo timezone and start of day
				toTime, parseErr = time.ParseInLocation("2006-01-02", to, cairoLocation)
				if parseErr != nil {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error": "Invalid 'to' date format. Use YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS",
					})
				}
			}
		}

		// If only date provided (10 chars), add 23:59:59 to get end of day
		if len(to) == 10 {
			toTime = toTime.Add(24*time.Hour - time.Second)
		}

		query = query.Where("time_stamp <= ?", toTime)
	}

	if err := query.Order("time_stamp DESC").
		Find(&driverLocationPings).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"data":  driverLocationPings,
		"count": len(driverLocationPings),
	})
}
