package Slack

import (
	"Falcon/Models"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// UpdateVehicleStatusRequest represents the request payload for updating vehicle status
type UpdateVehicleStatusRequest struct {
	CarID    uint   `json:"car_id" validate:"required"`
	Status   string `json:"status" validate:"required"`
	Location string `json:"location"` // Optional
}

// UpdateStatusRequestBody represents the request body for URL parameter route
type UpdateStatusRequestBody struct {
	Status   string `json:"status" validate:"required"`
	Location string `json:"location"` // Optional
}

// UpdateVehicleStatusResponse represents the response for vehicle status update
type UpdateVehicleStatusResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	CarID   uint   `json:"car_id,omitempty"`
}

// BulkUpdateRequest represents individual update in bulk request
type BulkUpdateRequest struct {
	CarID    uint   `json:"car_id" validate:"required"`
	Status   string `json:"status" validate:"required"`
	Location string `json:"location"` // Optional
}

// BulkUpdateVehicleStatusRequest represents bulk status update request
type BulkUpdateVehicleStatusRequest struct {
	Updates []BulkUpdateRequest `json:"updates" validate:"required"`
}

// BulkUpdateVehicleStatusResponse represents bulk update response
type BulkUpdateVehicleStatusResponse struct {
	Success      bool                          `json:"success"`
	Message      string                        `json:"message"`
	SuccessCount int                           `json:"success_count"`
	FailedCount  int                           `json:"failed_count"`
	Results      []UpdateVehicleStatusResponse `json:"results"`
}

// GetValidStatusesResponse represents available status options
type GetValidStatusesResponse struct {
	Statuses []StatusOption `json:"statuses"`
}

type StatusOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Emoji       string `json:"emoji"`
	Category    string `json:"category"`
}

// ValidStatuses defines all allowed status values
var ValidStatuses = []StatusOption{
	{
		Value:       "In Terminal",
		Label:       "In Terminal",
		Description: "Vehicle is at a fuel terminal",
		Emoji:       "🏢",
		Category:    "Automatic", // Set by geofence
	},
	{
		Value:       "In Drop-Off",
		Label:       "In Drop-Off",
		Description: "Vehicle is at a delivery location",
		Emoji:       "📦",
		Category:    "Automatic", // Set by geofence
	},
	{
		Value:       "In Garage",
		Label:       "In Garage",
		Description: "Vehicle is at garage/depot",
		Emoji:       "🅿️",
		Category:    "Automatic", // Set by geofence
	},
	{
		Value:       "Stopped for Maintenance",
		Label:       "Stopped for Maintenance",
		Description: "Vehicle is under repair or maintenance",
		Emoji:       "🔧",
		Category:    "Manual",
	},
	{
		Value:       "On Route to Terminal",
		Label:       "On Route to Terminal",
		Description: "Vehicle is traveling to a fuel terminal",
		Emoji:       "🟡",
		Category:    "Manual",
	},
	{
		Value:       "On Route to Drop-Off",
		Label:       "On Route to Drop-Off",
		Description: "Vehicle is traveling to delivery location",
		Emoji:       "🔴",
		Category:    "Manual",
	},
	{
		Value:       "Driver Resting",
		Label:       "Driver Resting",
		Description: "Driver is on break or rest period",
		Emoji:       "💤",
		Category:    "Manual",
	},
}

// isValidStatus checks if the provided status is valid
func isValidStatus(status string) bool {
	for _, validStatus := range ValidStatuses {
		if validStatus.Value == status {
			return true
		}
	}
	return false
}

// getUserFromContext extracts user from fiber context
func getUserFromContext(c *fiber.Ctx) (*Models.User, error) {
	user, ok := c.Locals("user").(Models.User)
	if !ok {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "User authentication required")
	}
	return &user, nil
}

// UpdateVehicleStatus handles manual vehicle status updates
func UpdateVehicleStatus(c *fiber.Ctx) error {
	// Get authenticated user
	user, err := getUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "User authentication required",
		})
	}

	var req UpdateVehicleStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Invalid request body: " + err.Error(),
		})
	}

	// Validate required fields
	if req.CarID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "car_id is required",
		})
	}

	if strings.TrimSpace(req.Status) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "status is required",
		})
	}

	// Validate status value
	if !isValidStatus(req.Status) {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Invalid status. Use GET /api/slack/statuses to see valid options",
		})
	}

	// Use authenticated user's name as updated_by
	updatedBy := user.Name

	// Call the manual update function
	err = ManualUpdateVehicleStatus(req.CarID, req.Status, req.Location, updatedBy)
	if err != nil {
		log.Printf("Error updating vehicle status: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Failed to update vehicle status: " + err.Error(),
		})
	}

	log.Printf("API: Vehicle %d status updated to '%s' by %s", req.CarID, req.Status, updatedBy)

	return c.JSON(UpdateVehicleStatusResponse{
		Success: true,
		Message: "Vehicle status updated successfully",
		CarID:   req.CarID,
	})
}

// BulkUpdateVehicleStatus handles multiple vehicle status updates
func BulkUpdateVehicleStatus(c *fiber.Ctx) error {
	// Get authenticated user
	user, err := getUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(BulkUpdateVehicleStatusResponse{
			Success: false,
			Message: "User authentication required",
		})
	}

	var req BulkUpdateVehicleStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(BulkUpdateVehicleStatusResponse{
			Success: false,
			Message: "Invalid request body: " + err.Error(),
		})
	}

	if len(req.Updates) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(BulkUpdateVehicleStatusResponse{
			Success: false,
			Message: "No updates provided",
		})
	}

	if len(req.Updates) > 50 {
		return c.Status(fiber.StatusBadRequest).JSON(BulkUpdateVehicleStatusResponse{
			Success: false,
			Message: "Maximum 50 updates allowed per request",
		})
	}

	var results []UpdateVehicleStatusResponse
	successCount := 0
	failedCount := 0
	updatedBy := user.Name // Use authenticated user's name

	for i, update := range req.Updates {
		// Validate each update
		if update.CarID == 0 {
			results = append(results, UpdateVehicleStatusResponse{
				Success: false,
				Message: "car_id is required",
				CarID:   update.CarID,
			})
			failedCount++
			continue
		}

		if strings.TrimSpace(update.Status) == "" {
			results = append(results, UpdateVehicleStatusResponse{
				Success: false,
				Message: "status is required",
				CarID:   update.CarID,
			})
			failedCount++
			continue
		}

		if !isValidStatus(update.Status) {
			results = append(results, UpdateVehicleStatusResponse{
				Success: false,
				Message: "Invalid status",
				CarID:   update.CarID,
			})
			failedCount++
			continue
		}

		// Attempt update
		err := ManualUpdateVehicleStatus(update.CarID, update.Status, update.Location, updatedBy)
		if err != nil {
			log.Printf("Bulk update [%d]: Error updating vehicle %d: %v", i, update.CarID, err)
			results = append(results, UpdateVehicleStatusResponse{
				Success: false,
				Message: "Failed to update: " + err.Error(),
				CarID:   update.CarID,
			})
			failedCount++
		} else {
			log.Printf("Bulk update [%d]: Vehicle %d status updated to '%s' by %s", i, update.CarID, update.Status, updatedBy)
			results = append(results, UpdateVehicleStatusResponse{
				Success: true,
				Message: "Updated successfully",
				CarID:   update.CarID,
			})
			successCount++
		}
	}

	overallSuccess := failedCount == 0
	message := ""
	if overallSuccess {
		message = "All updates completed successfully"
	} else if successCount > 0 {
		message = "Partial success: some updates failed"
	} else {
		message = "All updates failed"
	}

	statusCode := fiber.StatusOK
	if failedCount > 0 && successCount == 0 {
		statusCode = fiber.StatusBadRequest
	} else if failedCount > 0 {
		statusCode = fiber.StatusMultiStatus
	}

	return c.Status(statusCode).JSON(BulkUpdateVehicleStatusResponse{
		Success:      overallSuccess,
		Message:      message,
		SuccessCount: successCount,
		FailedCount:  failedCount,
		Results:      results,
	})
}

// GetValidStatuses returns all valid status options
func GetValidStatuses(c *fiber.Ctx) error {
	return c.JSON(GetValidStatusesResponse{
		Statuses: ValidStatuses,
	})
}

// UpdateVehicleStatusByPlate handles status updates using plate number instead of ID
func UpdateVehicleStatusByPlate(c *fiber.Ctx) error {
	// Get authenticated user
	user, err := getUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "User authentication required",
		})
	}

	plateNumber := c.Params("plate")
	if plateNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "plate number is required",
		})
	}

	var req UpdateStatusRequestBody
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Invalid request body: " + err.Error(),
		})
	}

	// Validate required fields
	if strings.TrimSpace(req.Status) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "status is required",
		})
	}

	// Validate status
	if !isValidStatus(req.Status) {
		return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Invalid status. Use GET /api/slack/statuses to see valid options",
		})
	}

	// Find car by plate number
	var car Models.Car
	if err := Models.DB.Where("car_no_plate = ?", plateNumber).First(&car).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Vehicle with plate number '" + plateNumber + "' not found",
		})
	}

	// Use authenticated user's name as updated_by
	updatedBy := user.Name

	// Update status
	err = ManualUpdateVehicleStatus(car.ID, req.Status, req.Location, updatedBy)
	if err != nil {
		log.Printf("Error updating vehicle status by plate: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(UpdateVehicleStatusResponse{
			Success: false,
			Message: "Failed to update vehicle status: " + err.Error(),
		})
	}

	log.Printf("API: Vehicle %s (ID: %d) status updated to '%s' by %s", plateNumber, car.ID, req.Status, updatedBy)

	return c.JSON(UpdateVehicleStatusResponse{
		Success: true,
		Message: "Vehicle status updated successfully",
		CarID:   car.ID,
	})
}

// RegisterSlackRoutes registers all Slack-related API routes with middleware
func RegisterSlackRoutes(app *fiber.App, middleware ...fiber.Handler) {
	// Create API group with middleware
	api := app.Group("/api/slack", middleware...)

	// Vehicle status update routes
	api.Post("/vehicles/:id/status", func(c *fiber.Ctx) error {
		// Get authenticated user
		user, err := getUserFromContext(c)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(UpdateVehicleStatusResponse{
				Success: false,
				Message: "User authentication required",
			})
		}

		// Parse car ID from URL parameter
		carIDStr := c.Params("id")
		carID, err := strconv.ParseUint(carIDStr, 10, 32)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
				Success: false,
				Message: "Invalid car ID",
			})
		}

		// Parse request body
		var bodyReq UpdateStatusRequestBody
		if err := c.BodyParser(&bodyReq); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
				Success: false,
				Message: "Invalid request body: " + err.Error(),
			})
		}

		// Validate required fields
		if strings.TrimSpace(bodyReq.Status) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
				Success: false,
				Message: "status is required",
			})
		}

		// Validate status value
		if !isValidStatus(bodyReq.Status) {
			return c.Status(fiber.StatusBadRequest).JSON(UpdateVehicleStatusResponse{
				Success: false,
				Message: "Invalid status. Use GET /api/slack/statuses to see valid options",
			})
		}

		// Use authenticated user's name as updated_by
		updatedBy := user.Name

		// Call the manual update function
		err = ManualUpdateVehicleStatus(uint(carID), bodyReq.Status, bodyReq.Location, updatedBy)
		if err != nil {
			log.Printf("Error updating vehicle status: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(UpdateVehicleStatusResponse{
				Success: false,
				Message: "Failed to update vehicle status: " + err.Error(),
			})
		}

		log.Printf("API: Vehicle %d status updated to '%s' by %s", carID, bodyReq.Status, updatedBy)

		return c.JSON(UpdateVehicleStatusResponse{
			Success: true,
			Message: "Vehicle status updated successfully",
			CarID:   uint(carID),
		})
	})

	// Alternative routes
	api.Post("/vehicles/status", UpdateVehicleStatus)                     // Body contains car_id
	api.Post("/vehicles/status/bulk", BulkUpdateVehicleStatus)            // Bulk updates
	api.Post("/vehicles/plate/:plate/status", UpdateVehicleStatusByPlate) // Update by plate number
	api.Get("/statuses", GetValidStatuses)                                // Get valid status options

	log.Println("Slack API routes registered:")
	log.Println("  POST /api/slack/vehicles/:id/status")
	log.Println("  POST /api/slack/vehicles/status")
	log.Println("  POST /api/slack/vehicles/status/bulk")
	log.Println("  POST /api/slack/vehicles/plate/:plate/status")
	log.Println("  GET  /api/slack/statuses")
}
