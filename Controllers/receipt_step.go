package Controllers

import (
	"Falcon/Models"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type ReceiptController struct {
	DB *gorm.DB
}

// Request DTOs
type AddReceiptStepRequest struct {
	TripID     uint   `json:"trip_id" validate:"required"`
	Location   string `json:"location" validate:"required,oneof=Garage Office"`
	ReceivedBy string `json:"received_by" validate:"required"`
	Stamped    bool   `json:"stamped"`
	Notes      string `json:"notes"`
}

type UpdateReceiptStepRequest struct {
	Location   string `json:"location" validate:"oneof=Garage Office"`
	ReceivedBy string `json:"received_by"`
	Stamped    *bool  `json:"stamped"`
	Notes      string `json:"notes"`
}

// Response DTOs
type TripWithStepsResponse struct {
	Trip         Models.TripStruct    `json:"trip"`
	ReceiptSteps []Models.ReceiptStep `json:"receipt_steps"`
}

// AddReceiptStep adds a new receipt step to a trip
// POST /api/receipts/step
// AddReceiptStep adds a new receipt step to a trip
// POST /api/receipts/step
func (rc *ReceiptController) AddReceiptStep(c *fiber.Ctx) error {
	var req AddReceiptStepRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	// Validate location
	if req.Location != "Garage" && req.Location != "Office" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Location must be either 'Garage' or 'Office'",
		})
	}

	// Validate received_by is not empty
	if strings.TrimSpace(req.ReceivedBy) == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Received by field is required",
		})
	}

	// Check if trip exists
	var trip Models.TripStruct
	if err := rc.DB.First(&trip, req.TripID).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "Trip not found",
			"error":   err.Error(),
		})
	}

	// Get current step count
	var stepCount int64
	rc.DB.Model(&Models.ReceiptStep{}).Where("trip_id = ?", req.TripID).Count(&stepCount)

	// Check if maximum steps reached
	if stepCount >= 2 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Maximum 2 receipt steps allowed per trip",
		})
	}

	// Check if this location already exists for this trip
	var existingStep Models.ReceiptStep
	if err := rc.DB.Where("trip_id = ? AND location = ?", req.TripID, req.Location).First(&existingStep).Error; err == nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Receipt already received at " + req.Location,
		})
	}

	// Create new receipt step
	receiptStep := Models.ReceiptStep{
		TripID:     req.TripID,
		Location:   req.Location,
		ReceivedBy: strings.TrimSpace(req.ReceivedBy),
		ReceivedAt: time.Now(),
		StepOrder:  int(stepCount) + 1,
		Stamped:    req.Stamped,
		Notes:      strings.TrimSpace(req.Notes),
	}

	if err := rc.DB.Create(&receiptStep).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to create receipt step",
			"error":   err.Error(),
		})
	}

	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"message":      "Receipt step added successfully",
		"receipt_step": receiptStep,
	})
}

// GetTripReceiptSteps gets all receipt steps for a trip
// GET /api/receipts/trip/:tripId
func (rc *ReceiptController) GetTripReceiptSteps(c *fiber.Ctx) error {
	tripID := c.Params("tripId")

	var trip Models.TripStruct
	if err := rc.DB.First(&trip, tripID).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "Trip not found",
			"error":   err.Error(),
		})
	}

	var receiptSteps []Models.ReceiptStep
	if err := rc.DB.Where("trip_id = ?", tripID).Order("step_order ASC").Find(&receiptSteps).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch receipt steps",
			"error":   err.Error(),
		})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Receipt steps retrieved successfully",
		"trip":    trip,
		"steps":   receiptSteps,
	})
}
func (rc *ReceiptController) GetTripsWithReceiptStatus(c *fiber.Ctx) error {
	status := c.Query("status", "all") // all, pending, in_garage, in_office
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	company := c.Query("company")

	query := rc.DB.Model(&Models.TripStruct{}).
		Preload("ReceiptSteps", func(db *gorm.DB) *gorm.DB {
			return db.Order("received_at DESC") // Order by most recent first
		})

	// Apply filters
	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	}

	if company != "" {
		query = query.Where("company = ?", company)
	}

	var trips []Models.TripStruct
	if err := query.Find(&trips).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   err.Error(),
		})
	}

	// Filter by receipt status if specified
	if status != "all" {
		var filteredTrips []Models.TripStruct
		for _, trip := range trips {
			stepCount := len(trip.ReceiptSteps)

			switch status {
			case "pending":
				if stepCount == 0 {
					filteredTrips = append(filteredTrips, trip)
				}
			case "in_garage":
				// Check if the LATEST step is Garage
				if stepCount > 0 && trip.ReceiptSteps[0].Location == "Garage" {
					filteredTrips = append(filteredTrips, trip)
				}
			case "in_office":
				// Check if the LATEST step is Office
				if stepCount > 0 && trip.ReceiptSteps[0].Location == "Office" {
					filteredTrips = append(filteredTrips, trip)
				}
			}
		}
		trips = filteredTrips
	}

	// Build response with status
	type TripWithStatus struct {
		Trip         Models.TripStruct    `json:"trip"`
		ReceiptSteps []Models.ReceiptStep `json:"receipt_steps"`
		Status       string               `json:"status"`
	}

	var response []TripWithStatus
	for _, trip := range trips {
		stepCount := len(trip.ReceiptSteps)
		var statusStr string

		if stepCount == 0 {
			statusStr = "pending"
		} else {
			// Status is based on the LATEST step (first in the ordered list)
			latestLocation := trip.ReceiptSteps[0].Location
			if latestLocation == "Garage" {
				statusStr = "in_garage"
			} else if latestLocation == "Office" {
				statusStr = "in_office"
			}
		}

		response = append(response, TripWithStatus{
			Trip:         trip,
			ReceiptSteps: trip.ReceiptSteps,
			Status:       statusStr,
		})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Trips retrieved successfully",
		"data":    response,
		"total":   len(response),
	})
}

// UpdateReceiptStep updates an existing receipt step
// PUT /api/receipts/step/:stepId
func (rc *ReceiptController) UpdateReceiptStep(c *fiber.Ctx) error {
	stepID := c.Params("stepId")

	var req UpdateReceiptStepRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	var receiptStep Models.ReceiptStep
	if err := rc.DB.First(&receiptStep, stepID).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "Receipt step not found",
			"error":   err.Error(),
		})
	}

	// Update fields if provided
	if req.Location != "" {
		if req.Location != "Garage" && req.Location != "Office" {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"message": "Location must be either 'Garage' or 'Office'",
			})
		}
		receiptStep.Location = req.Location
	}

	if req.ReceivedBy != "" {
		receiptStep.ReceivedBy = req.ReceivedBy
	}

	if req.Stamped != nil {
		receiptStep.Stamped = *req.Stamped
	}

	if req.Notes != "" {
		receiptStep.Notes = req.Notes
	}

	if err := rc.DB.Save(&receiptStep).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update receipt step",
			"error":   err.Error(),
		})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":      "Receipt step updated successfully",
		"receipt_step": receiptStep,
	})
}

// DeleteReceiptStep deletes a receipt step
// DELETE /api/receipts/step/:stepId
func (rc *ReceiptController) DeleteReceiptStep(c *fiber.Ctx) error {
	stepID := c.Params("stepId")

	var receiptStep Models.ReceiptStep
	if err := rc.DB.First(&receiptStep, stepID).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"message": "Receipt step not found",
			"error":   err.Error(),
		})
	}

	// Check if this is the last step, we need to reorder
	var laterSteps []Models.ReceiptStep
	rc.DB.Where("trip_id = ? AND step_order > ?", receiptStep.TripID, receiptStep.StepOrder).
		Order("step_order ASC").
		Find(&laterSteps)

	// Delete the step
	if err := rc.DB.Delete(&receiptStep).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to delete receipt step",
			"error":   err.Error(),
		})
	}

	// Reorder remaining steps
	for i, step := range laterSteps {
		step.StepOrder = receiptStep.StepOrder + i
		rc.DB.Save(&step)
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Receipt step deleted successfully",
	})
}

// GetReceiptStatistics gets statistics about receipt processing
// GET /api/receipts/statistics
func (rc *ReceiptController) GetReceiptStatistics(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	company := c.Query("company")

	query := rc.DB.Model(&Models.TripStruct{}).
		Preload("ReceiptSteps")

	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	}

	if company != "" {
		query = query.Where("company = ?", company)
	}

	var trips []Models.TripStruct
	if err := query.Find(&trips).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch trips",
			"error":   err.Error(),
		})
	}

	// Calculate statistics
	var pending, inGarage, completed int
	for _, trip := range trips {
		stepCount := len(trip.ReceiptSteps)
		switch stepCount {
		case 0:
			pending++
		case 1:
			inGarage++
		default:
			completed++
		}
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Statistics retrieved successfully",
		"statistics": fiber.Map{
			"total":     len(trips),
			"pending":   pending,
			"in_garage": inGarage,
			"completed": completed,
			"percentage": fiber.Map{
				"pending":   float64(pending) / float64(len(trips)) * 100,
				"in_garage": float64(inGarage) / float64(len(trips)) * 100,
				"completed": float64(completed) / float64(len(trips)) * 100,
			},
		},
	})
}
