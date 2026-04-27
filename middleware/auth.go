package middleware

import (
	"Falcon/Models"
	"Falcon/utils"
	"log"

	"github.com/gofiber/fiber/v2"
)

// SimpleLogger provides basic request logging
func Logger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		log.Println(c.Method(), c.Path())
		return c.Next()
	}
}

// Verify validates admin user JWT and checks permission level
// This is for admin users only (web dashboard)
func Verify(requiredPermission int) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get JWT from cookies
		cookie := c.Cookies("jwt")
		if cookie == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Not Logged In.",
			})
		}

		// Parse token with custom claims
		claims, err := utils.ParseToken(cookie)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Invalid or expired token",
			})
		}
		// Ensure this is an admin user token, not a driver token
		if claims.UserType != "admin_user" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"message": "This endpoint requires admin user access",
			})
		}

		// Get user from database
		var user Models.User
		result := Models.DB.Where("id = ?", claims.UserID).First(&user)
		if result.Error != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "User not found",
			})
		}

		// Store user in context for later use in handlers
		c.Locals("user", &user)

		// If no specific permission is required, just check if user has any permission
		if requiredPermission == 0 {
			return c.Next()
		}

		// Check if user has the required permission level
		if user.Permission >= requiredPermission {
			return c.Next()
		}

		// User doesn't have sufficient permissions
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"message": "Insufficient permissions to access this resource",
		})
	}
}

// VerifyAny validates JWT for either admin user or driver
// Useful for endpoints that both types can access
func VerifyAny() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Try to get JWT from cookie first (web users)
		token := c.Cookies("jwt")

		// If no cookie, try Authorization header (mobile drivers)
		if token == "" {
			authHeader := c.Get("Authorization")
			if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
				token = authHeader[7:]
			}
		}

		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Not authenticated",
			})
		}

		// Parse token
		claims, err := utils.ParseToken(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Invalid or expired token",
			})
		}

		// Store claims in context
		c.Locals("claims", claims)
		c.Locals("user_type", claims.UserType)

		// Load appropriate user based on type
		if claims.UserType == "admin_user" {
			var user Models.User
			if err := Models.DB.First(&user, claims.UserID).Error; err != nil {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"message": "User not found",
				})
			}
			c.Locals("user", user)
		} else if claims.UserType == "driver" {
			var driver Models.Driver
			if err := Models.DB.First(&driver, claims.DriverID).Error; err != nil {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"message": "Driver not found",
				})
			}
			c.Locals("driver", driver)
		}

		return c.Next()
	}
}
