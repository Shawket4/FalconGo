package utils

import (
	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v2"
	"github.com/vmihailenco/msgpack/v5"
)

// FastJSONResponse sends a response with sonic (ultra-fast JSON serializer)
// Up to 10x faster than standard encoding/json
func FastJSONResponse(c *fiber.Ctx, statusCode int, data interface{}) error {
	// Use sonic for blazing fast JSON serialization
	jsonBytes, err := sonic.Marshal(data)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to serialize response",
		})
	}

	c.Set("Content-Type", "application/json")
	return c.Status(statusCode).Send(jsonBytes)
}

// FastMsgPackResponse sends a response with MessagePack (binary format)
// 40-50% smaller and 2-3x faster than JSON
func FastMsgPackResponse(c *fiber.Ctx, statusCode int, data interface{}) error {
	msgBytes, err := msgpack.Marshal(data)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to serialize response",
		})
	}

	c.Set("Content-Type", "application/msgpack")
	return c.Status(statusCode).Send(msgBytes)
}

// SmartResponse auto-detects if client supports MessagePack and uses it, otherwise JSON
func SmartResponse(c *fiber.Ctx, statusCode int, data interface{}) error {
	// Check Accept header
	accept := c.Get("Accept")
	if accept == "application/msgpack" || accept == "application/x-msgpack" {
		return FastMsgPackResponse(c, statusCode, data)
	}

	// Default to JSON
	return FastJSONResponse(c, statusCode, data)
}

// FastSuccessResponse - Fast success response wrapper
func FastSuccessResponse(c *fiber.Ctx, data interface{}) error {
	response := fiber.Map{
		"success": true,
		"data":    data,
	}
	return SmartResponse(c, fiber.StatusOK, response)
}

// FastErrorResponse - Fast error response wrapper
func FastErrorResponse(c *fiber.Ctx, statusCode int, message string) error {
	response := fiber.Map{
		"success": false,
		"error":   message,
	}
	return FastJSONResponse(c, statusCode, response)
}
