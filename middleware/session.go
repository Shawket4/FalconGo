package middleware

import (
	"Falcon/Models"
	"Falcon/utils"

	"github.com/gofiber/fiber/v2"
)

// RequireActiveSession ensures driver has an active session
// Must be used after VerifyDriver middleware
func RequireActiveSession() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get driver from context (set by VerifyDriver)
		driver, ok := c.Locals("driver").(Models.Driver)
		if !ok {
			return utils.UnauthorizedResponse(c, "Driver information not found in context")
		}

		// Get active session for this driver
		session, err := utils.GetActiveSession(Models.DB, driver.ID)
		if err != nil {
			return utils.ForbiddenResponse(c, "No active session. Please login to start a session.")
		}

		// Store session in context
		c.Locals("session", *session)
		c.Locals("session_id", session.ID)
		c.Locals("car_id", session.CarID)
		c.Locals("tablet_id", session.TabletID)

		return c.Next()
	}
}

// RequireActiveSessionWithTabletMatch ensures driver has active session and tablet matches
// Must be used after both VerifyDriver and TabletIdentification middleware
func RequireActiveSessionWithTabletMatch() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get driver from context
		driver, ok := c.Locals("driver").(Models.Driver)
		if !ok {
			return utils.UnauthorizedResponse(c, "Driver information not found")
		}

		// Get tablet from context
		tablet, ok := c.Locals("tablet").(Models.Tablet)
		if !ok {
			return utils.BadRequestResponse(c, "Tablet information not found")
		}

		// Get active session
		session, err := utils.GetActiveSessionByDriverAndTablet(Models.DB, driver.ID, tablet.ID)
		if err != nil {
			return utils.ForbiddenResponse(c, "No active session found for this driver and tablet combination")
		}

		// Validate session matches the tablet
		if session.TabletID != tablet.ID {
			return utils.ForbiddenResponse(c, "Session tablet does not match current device")
		}

		// Store session in context
		c.Locals("session", *session)
		c.Locals("session_id", session.ID)
		c.Locals("car_id", session.CarID)

		return c.Next()
	}
}

// OptionalActiveSession loads active session if exists but doesn't fail if not present
// Must be used after VerifyDriver middleware
func OptionalActiveSession() fiber.Handler {
	return func(c *fiber.Ctx) error {
		driver, ok := c.Locals("driver").(Models.Driver)
		if !ok {
			return c.Next() // No driver, skip
		}

		// Try to get active session
		session, err := utils.GetActiveSession(Models.DB, driver.ID)
		if err == nil {
			c.Locals("session", *session)
			c.Locals("session_id", session.ID)
			c.Locals("car_id", session.CarID)
			c.Locals("tablet_id", session.TabletID)
		}

		return c.Next()
	}
}

// LoadSession loads a specific session by ID and validates access
// Useful for session detail endpoints
func LoadSession(c *fiber.Ctx) error {
	sessionID := c.Params("id")

	if sessionID == "" {
		return utils.BadRequestResponse(c, "Session ID is required")
	}

	var session Models.DriverSession
	if err := Models.DB.Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		First(&session, sessionID).Error; err != nil {
		return utils.NotFoundResponse(c, "Session not found")
	}

	// Check if user has permission to view this session
	// Either admin user or the driver who owns the session
	userType, _ := c.Locals("user_type").(string)

	if userType == "driver" {
		driver, _ := c.Locals("driver").(Models.Driver)
		if session.DriverID != driver.ID {
			return utils.ForbiddenResponse(c, "You don't have permission to view this session")
		}
	}
	// Admin users can view any session

	c.Locals("session", session)
	return c.Next()
}
