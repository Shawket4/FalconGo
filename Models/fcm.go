package Models

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type FCMToken struct {
	gorm.Model
	Value string `json:"value"`
}

type UpdateTokenRequest struct {
	Value string `json:"value" validate:"required"`
}

func UpdateToken(c *fiber.Ctx) error {
	// Parse request body
	var req UpdateTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Validate token value
	if req.Value == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Token value is required",
		})
	}

	var token FCMToken

	// Find token with ID 1 or create it
	err := DB.Where("id = ?", 1).FirstOrCreate(&token, FCMToken{
		Model: gorm.Model{ID: 1},
		Value: req.Value,
	}).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create/update token",
		})
	}

	// If token exists, update the value
	if token.Value != req.Value {
		token.Value = req.Value
		if err := DB.Save(&token).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to update token",
			})
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Token updated successfully",
		"token":   token,
	})
}
