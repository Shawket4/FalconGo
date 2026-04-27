package middleware

import (
	"Falcon/Models"
	"Falcon/utils"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// VerifyDriver validates driver JWT token
// This is for driver app endpoints only
func VerifyDriver() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get JWT from Authorization header (driver app uses header, not cookies)
		authHeader := c.Get("Authorization")

		if authHeader == "" {
			return utils.UnauthorizedResponse(c, "Authorization header is required")
		}

		// Check Bearer format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return utils.UnauthorizedResponse(c, "Invalid authorization format. Use: Bearer <token>")
		}

		token := parts[1]

		// Parse and validate token
		claims, err := utils.ParseToken(token)
		if err != nil {
			return utils.UnauthorizedResponse(c, "Invalid or expired token")
		}

		// Ensure this is a driver token
		if claims.UserType != "driver" {
			return utils.ForbiddenResponse(c, "This endpoint requires driver access")
		}

		// Get driver from database
		var driver Models.Driver
		if err := Models.DB.First(&driver, claims.DriverID).Error; err != nil {
			return utils.UnauthorizedResponse(c, "Driver not found")
		}

		// Store driver and claims in context
		c.Locals("driver", driver)
		c.Locals("driver_id", driver.ID)
		c.Locals("claims", claims)

		return c.Next()
	}
}

// OptionalDriverAuth attempts to authenticate driver but doesn't fail if not present
// Useful for endpoints that work with or without authentication
func OptionalDriverAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")

		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token := parts[1]

				claims, err := utils.ParseToken(token)
				if err == nil && claims.UserType == "driver" {
					var driver Models.Driver
					if err := Models.DB.First(&driver, claims.DriverID).Error; err == nil {
						c.Locals("driver", driver)
						c.Locals("driver_id", driver.ID)
						c.Locals("claims", claims)
					}
				}
			}
		}

		return c.Next()
	}
}
