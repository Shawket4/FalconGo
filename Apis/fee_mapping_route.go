package Apis

import (
	"net/http"
	"strconv"

	"Falcon/Models"
	"Falcon/services"

	"github.com/gofiber/fiber/v2"
)

// FeeMappingRoute returns the road route for one fee mapping.
//
// The response is deliberately the SAME SHAPE as the trip-details endpoint's
// location block — terminal_location, drop_off_point_location, route_data —
// so the map dialog on the trips screen can be reused verbatim rather than
// grown a second variant that drifts.
//
// Pins come from the canonical terminals and drop_off_points tables, and the
// route from services.Route, so the line drawn here is the same line the trip
// audit measures against.
func FeeMappingRoute(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"message": "Invalid ID"})
	}

	var mapping Models.FeeMapping
	if err := Models.DB.First(&mapping, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"message": "Fee mapping not found"})
	}

	var terminal Models.Terminal
	if err := Models.DB.Where("name = ?", mapping.Terminal).First(&terminal).Error; err != nil {
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"data":    mapping,
			"message": "terminal not found",
		})
	}

	var point Models.DropOffPoint
	_ = Models.DB.Where("name = ?", mapping.DropOffPoint).First(&point).Error

	body := fiber.Map{
		"data":    mapping,
		"message": "Fee mapping route retrieved",
	}
	if terminal.Latitude != 0 || terminal.Longitude != 0 {
		body["terminal_location"] = fiber.Map{"lat": terminal.Latitude, "lng": terminal.Longitude}
	}
	if point.Latitude != nil && point.Longitude != nil {
		body["drop_off_point_location"] = fiber.Map{"lat": *point.Latitude, "lng": *point.Longitude}
	}

	// Only route when both ends are pinned. The dialog renders a single marker
	// and suppresses the line otherwise, which is the honest picture — a line
	// drawn to an unpinned end would be a line to (0, 0).
	if (terminal.Latitude != 0 || terminal.Longitude != 0) && point.Latitude != nil && point.Longitude != nil {
		km, min, geometry, err := services.RouteWithGeometry(
			terminal.Latitude, terminal.Longitude, *point.Latitude, *point.Longitude, true)
		if err == nil {
			body["route_data"] = fiber.Map{
				"distance": km,
				"duration": min * 60, // seconds, as the trip endpoint reports
				"geometry": geometry,
			}
		}
	}

	return c.Status(http.StatusOK).JSON(body)
}
