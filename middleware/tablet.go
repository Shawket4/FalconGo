package middleware

import (
	"Falcon/Models"
	"Falcon/utils"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// TabletIdentification extracts tablet HWID from header and loads tablet info
// This middleware should be used on all driver app endpoints
func TabletIdentification() fiber.Handler {
	return func(c *fiber.Ctx) error {
		fmt.Println("Reached 1")
		// Get device ID from header
		hwid := c.Get("X-Device-ID")

		if hwid == "" {
			return utils.BadRequestResponse(c, "Device ID is required (X-Device-ID header)")
		}

		// Find or create tablet record
		deviceName := c.Get("X-Device-Name", "Unknown Device")
		deviceType := c.Get("X-Device-Type", "unknown")

		tablet, err := utils.RegisterTablet(Models.DB, hwid, deviceName, deviceType)
		if err != nil {
			return utils.InternalServerErrorResponse(c, "Failed to register tablet")
		}

		// Store tablet info in context
		c.Locals("tablet_id", tablet.ID)
		c.Locals("tablet", *tablet)
		c.Locals("hwid", hwid)

		return c.Next()
	}
}

// RequireTabletAssigned ensures the tablet is assigned to a car
// Use this middleware after TabletIdentification
func RequireTabletAssigned() fiber.Handler {
	return func(c *fiber.Ctx) error {
		fmt.Println("Reached 2")
		tablet, ok := c.Locals("tablet").(Models.Tablet)
		if !ok {
			return utils.InternalServerErrorResponse(c, "Tablet information not found in context")
		}

		if tablet.CurrentCarID == nil {
			return utils.ForbiddenResponse(c, "This tablet is not assigned to any vehicle. Please contact your administrator.")
		}

		// Load car info
		var car Models.Car
		if err := Models.DB.First(&car, *tablet.CurrentCarID).Error; err != nil {
			return utils.InternalServerErrorResponse(c, "Failed to load vehicle information")
		}

		// Store car info in context
		c.Locals("car_id", car.ID)
		c.Locals("car", car)

		return c.Next()
	}
}

// OptionalTabletIdentification attempts to identify tablet but doesn't fail if not present
// Useful for endpoints that work with or without tablet info
func OptionalTabletIdentification() fiber.Handler {
	return func(c *fiber.Ctx) error {
		hwid := c.Get("X-Device-ID")

		if hwid != "" {
			deviceName := c.Get("X-Device-Name", "Unknown Device")
			deviceType := c.Get("X-Device-Type", "unknown")

			tablet, err := utils.GetTabletByHWID(Models.DB, hwid)
			if err == nil {
				c.Locals("tablet_id", tablet.ID)
				c.Locals("tablet", *tablet)
				c.Locals("hwid", hwid)
			} else {
				// Try to register
				tablet, err = utils.RegisterTablet(Models.DB, hwid, deviceName, deviceType)
				if err == nil {
					c.Locals("tablet_id", tablet.ID)
					c.Locals("tablet", *tablet)
					c.Locals("hwid", hwid)
				}
			}
		}

		return c.Next()
	}
}
