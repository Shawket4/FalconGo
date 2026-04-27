package utils

import "github.com/gofiber/fiber/v2"

// Standard response structures for consistency

// SuccessResponse sends a success response with data
func SuccessResponse(c *fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    data,
	})
}

// SuccessMessageResponse sends a success response with just a message
func SuccessMessageResponse(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": message,
	})
}

// CreatedResponse sends a 201 Created response
func CreatedResponse(c *fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    data,
	})
}

// ErrorResponse sends an error response
func ErrorResponse(c *fiber.Ctx, statusCode int, message string) error {
	return c.Status(statusCode).JSON(fiber.Map{
		"success": false,
		"error":   message,
	})
}

// BadRequestResponse sends a 400 Bad Request response
func BadRequestResponse(c *fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusBadRequest, message)
}

// UnauthorizedResponse sends a 401 Unauthorized response
func UnauthorizedResponse(c *fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusUnauthorized, message)
}

// ForbiddenResponse sends a 403 Forbidden response
func ForbiddenResponse(c *fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusForbidden, message)
}

// NotFoundResponse sends a 404 Not Found response
func NotFoundResponse(c *fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusNotFound, message)
}

// ConflictResponse sends a 409 Conflict response
func ConflictResponse(c *fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusConflict, message)
}

// InternalServerErrorResponse sends a 500 Internal Server Error response
func InternalServerErrorResponse(c *fiber.Ctx, message string) error {
	return ErrorResponse(c, fiber.StatusInternalServerError, message)
}

// ValidationErrorResponse sends a validation error response with details
func ValidationErrorResponse(c *fiber.Ctx, errors map[string]string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"success": false,
		"error":   "Validation failed",
		"details": errors,
	})
}

// PaginatedResponse sends a paginated response
func PaginatedResponse(c *fiber.Ctx, data interface{}, total int64, page int, pageSize int) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    data,
		"pagination": fiber.Map{
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"pages":     (total + int64(pageSize) - 1) / int64(pageSize),
		},
	})
}
