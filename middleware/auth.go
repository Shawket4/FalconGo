package middleware

import (
	"Falcon/Models"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
)

const SecretKey = "secret"

func Logger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		log.Println(c.Method(), c.Path())
		return c.Next()
	}
}

func Verify(requiredPermission int) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get JWT from cookies
		cookie := c.Cookies("jwt")
		if cookie == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Not Logged In.",
			})
		}

		// Parse and validate the token
		token, err := jwt.ParseWithClaims(cookie, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		})

		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Invalid or expired token",
			})
		}

		// Extract claims
		claims, ok := token.Claims.(*jwt.RegisteredClaims)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Invalid token claims",
			})
		}

		// Get user from database
		var user Models.User
		result := Models.DB.Where("id = ?", claims.Issuer).First(&user)
		if result.Error != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "User not found",
			})
		}

		// Store user in context for later use in handlers
		c.Locals("user", user)

		// If no specific permission is required, just check if user has any permission
		if requiredPermission == 0 {
			if user.Permission != 0 {
				return c.Next()
			} else {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"message": "You do not have permission to access this page",
				})
			}
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
