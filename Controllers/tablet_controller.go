package Controllers

import (
	"Falcon/Models"
	"Falcon/utils"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type TabletController struct {
	DB *gorm.DB
}

// Register - Self-registration endpoint for tablets (called by app on first launch)
// POST /api/tablet/register
func (ctrl *TabletController) Register(c *fiber.Ctx) error {
	// Tablet already registered by middleware
	tablet, ok := c.Locals("tablet").(Models.Tablet)
	if !ok {
		return utils.InternalServerErrorResponse(c, "Tablet information not found")
	}

	return utils.SuccessResponse(c, fiber.Map{
		"message": "Tablet registered successfully",
		"tablet":  tablet,
	})
}

// GetAll - Get all tablets (admin only)
// GET /api/admin/tablets
func (ctrl *TabletController) GetAll(c *fiber.Ctx) error {
	var tablets []Models.Tablet

	if err := Models.DB.Preload("CurrentCar").Find(&tablets).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch tablets")
	}

	return utils.SuccessResponse(c, tablets)
}

// GetUnassigned - Get all unassigned tablets (admin only)
// GET /api/admin/tablets/unassigned
func (ctrl *TabletController) GetUnassigned(c *fiber.Ctx) error {
	tablets, err := utils.GetUnassignedTablets(Models.DB)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch unassigned tablets")
	}

	return utils.SuccessResponse(c, tablets)
}

// GetByID - Get tablet by ID (admin only)
// GET /api/admin/tablets/:id
func (ctrl *TabletController) GetByID(c *fiber.Ctx) error {
	id := c.Params("id")

	var tablet Models.Tablet
	if err := Models.DB.Preload("CurrentCar").First(&tablet, id).Error; err != nil {
		return utils.NotFoundResponse(c, "Tablet not found")
	}

	return utils.SuccessResponse(c, tablet)
}

// AssignCar - Assign tablet to a car (admin only)
// PUT /api/admin/tablets/:id/assign-car
// Body: {"car_id": 123}
func (ctrl *TabletController) AssignCar(c *fiber.Ctx) error {
	tabletID := c.Params("id")

	var input struct {
		CarID uint `json:"car_id"`
	}

	if err := c.BodyParser(&input); err != nil {
		return utils.BadRequestResponse(c, "Invalid request body")
	}

	if input.CarID == 0 {
		return utils.BadRequestResponse(c, "car_id is required")
	}

	// Convert string ID to uint
	var tabletIDUint uint
	if _, err := fmt.Sscanf(tabletID, "%d", &tabletIDUint); err != nil {
		return utils.BadRequestResponse(c, "Invalid tablet ID")
	}

	// Assign tablet to car
	if err := utils.AssignTabletToCar(Models.DB, tabletIDUint, input.CarID); err != nil {
		return utils.BadRequestResponse(c, err.Error())
	}

	// Fetch updated tablet
	var tablet Models.Tablet
	Models.DB.Preload("CurrentCar").First(&tablet, tabletIDUint)

	return utils.SuccessResponse(c, fiber.Map{
		"message": "Tablet assigned to car successfully",
		"tablet":  tablet,
	})
}

// UnassignCar - Remove tablet's car assignment (admin only)
// PUT /api/admin/tablets/:id/unassign
func (ctrl *TabletController) UnassignCar(c *fiber.Ctx) error {
	tabletID := c.Params("id")

	var tabletIDUint uint
	if _, err := fmt.Sscanf(tabletID, "%d", &tabletIDUint); err != nil {
		return utils.BadRequestResponse(c, "Invalid tablet ID")
	}

	// Unassign tablet
	if err := utils.UnassignTabletFromCar(Models.DB, tabletIDUint); err != nil {
		return utils.BadRequestResponse(c, err.Error())
	}

	// Fetch updated tablet
	var tablet Models.Tablet
	Models.DB.First(&tablet, tabletIDUint)

	return utils.SuccessResponse(c, fiber.Map{
		"message": "Tablet unassigned from car successfully",
		"tablet":  tablet,
	})
}

// Delete - Delete a tablet (admin only)
// DELETE /api/admin/tablets/:id
func (ctrl *TabletController) Delete(c *fiber.Ctx) error {
	tabletID := c.Params("id")

	// Check for active sessions
	var activeSessionCount int64
	Models.DB.Model(&Models.DriverSession{}).
		Where("tablet_id = ? AND end_time IS NULL", tabletID).
		Count(&activeSessionCount)

	if activeSessionCount > 0 {
		return utils.ConflictResponse(c, "Cannot delete tablet with active sessions")
	}

	// Delete tablet
	if err := Models.DB.Delete(&Models.Tablet{}, tabletID).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to delete tablet")
	}

	return utils.SuccessMessageResponse(c, "Tablet deleted successfully")
}
