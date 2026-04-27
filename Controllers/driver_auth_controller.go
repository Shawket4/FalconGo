package Controllers

import (
	"Falcon/Models"
	"Falcon/utils"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type DriverAuthController struct {
	DB *gorm.DB
}

// Login - Driver login with PIN only
// POST /api/driver/login
// Headers: X-Device-ID (required)
// Body: {"pin": "1234"}
func (ctrl *DriverAuthController) Login(c *fiber.Ctx) error {
	fmt.Println("Reached Login")
	var input struct {
		PIN string `json:"pin"`
	}

	if err := c.BodyParser(&input); err != nil {
		return utils.BadRequestResponse(c, "Invalid request body")
	}

	if input.PIN == "" {
		return utils.BadRequestResponse(c, "PIN is required")
	}

	// Validate PIN length (must be 4 digits)
	if len(input.PIN) != 4 {
		return utils.BadRequestResponse(c, "PIN must be 4 digits")
	}

	// Get tablet from context (set by TabletIdentification middleware)
	tablet, ok := c.Locals("tablet").(Models.Tablet)
	if !ok {
		return utils.InternalServerErrorResponse(c, "Tablet information not found")
	}

	// Check if tablet is assigned to a car
	if tablet.CurrentCarID == nil {
		return utils.ForbiddenResponse(c, "This tablet is not assigned to any vehicle. Please contact your administrator.")
	}

	// Find driver by PIN (iterate through all drivers and compare hashed PINs)
	var drivers []Models.Driver
	if err := Models.DB.Find(&drivers).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to authenticate")
	}

	var authenticatedDriver *Models.Driver

	type result struct {
		driver *Models.Driver
		found  bool
	}

	resultChan := make(chan result, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := range drivers {
		go func(d Models.Driver) {
			select {
			case <-ctx.Done():
				return
			default:
				// Add nil check here
				if d.PIN != nil && utils.ValidatePIN(input.PIN, *d.PIN) {
					select {
					case resultChan <- result{driver: &d, found: true}:
						cancel()
					case <-ctx.Done():
					}
				}
			}
		}(drivers[i])
	}

	// Wait for result with timeout
	select {
	case res := <-resultChan:
		if res.found {
			authenticatedDriver = res.driver
		}
	case <-time.After(5 * time.Second):
		return utils.UnauthorizedResponse(c, "Authentication timeout")
	}

	if authenticatedDriver == nil {
		return utils.UnauthorizedResponse(c, "Invalid PIN")
	}

	// Check if driver already has an active session
	if utils.HasActiveSession(Models.DB, authenticatedDriver.ID) {
		// End the old session first
		if err := utils.EndDriverActiveSession(Models.DB, authenticatedDriver.ID); err != nil {
			log.Printf("Warning: Failed to end old session for driver %d: %v", authenticatedDriver.ID, err)
		}
	}

	// Create new session
	session, err := utils.CreateSession(Models.DB, authenticatedDriver.ID, *tablet.CurrentCarID, tablet.ID)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create session: "+err.Error())
	}

	// Generate JWT token
	token, err := utils.GenerateDriverToken(authenticatedDriver.ID)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to generate authentication token")
	}

	return utils.SuccessResponse(c, fiber.Map{
		"token":          token,
		"driver":         authenticatedDriver,
		"active_session": session,
		"car_id":         *tablet.CurrentCarID,
	})
}

// Logout - End driver session
// POST /api/driver/logout
func (ctrl *DriverAuthController) Logout(c *fiber.Ctx) error {
	// Get driver from context (set by VerifyDriver middleware)
	driver, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return utils.UnauthorizedResponse(c, "Driver not found")
	}

	// End active session
	if err := utils.EndDriverActiveSession(Models.DB, driver.ID); err != nil {
		// If no active session, that's fine
		if err.Error() == "no active session found for driver" {
			return utils.SuccessMessageResponse(c, "Already logged out")
		}
		return utils.InternalServerErrorResponse(c, "Failed to end session")
	}

	return utils.SuccessMessageResponse(c, "Logged out successfully")
}

// GetCurrentDriver - Get current driver info
// GET /api/driver/me
func (ctrl *DriverAuthController) GetCurrentDriver(c *fiber.Ctx) error {
	driver, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return utils.UnauthorizedResponse(c, "Driver not found")
	}

	// Get active session if exists
	session, _ := utils.GetActiveSession(Models.DB, driver.ID)

	return utils.SuccessResponse(c, fiber.Map{
		"driver":         driver,
		"active_session": session,
	})
}

// ChangePIN - Driver changes their own PIN
// PUT /api/driver/change-pin
// Body: {"old_pin": "1234", "new_pin": "5678"}
func (ctrl *DriverAuthController) ChangePIN(c *fiber.Ctx) error {
	driver, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return utils.UnauthorizedResponse(c, "Driver not found")
	}

	var input struct {
		OldPIN string `json:"old_pin"`
		NewPIN string `json:"new_pin"`
	}

	if err := c.BodyParser(&input); err != nil {
		return utils.BadRequestResponse(c, "Invalid request body")
	}

	// Validate input
	if input.OldPIN == "" || input.NewPIN == "" {
		return utils.BadRequestResponse(c, "Both old_pin and new_pin are required")
	}

	if len(input.NewPIN) != 4 {
		return utils.BadRequestResponse(c, "New PIN must be 4 digits")
	}

	// Check if driver has a PIN set
	if driver.PIN == nil || *driver.PIN == "" {
		return utils.BadRequestResponse(c, "No PIN is currently set for this driver")
	}

	// Validate old PIN
	if !utils.ValidatePIN(input.OldPIN, *driver.PIN) {
		return utils.UnauthorizedResponse(c, "Old PIN is incorrect")
	}

	// Check if new PIN is same as old
	if input.OldPIN == input.NewPIN {
		return utils.BadRequestResponse(c, "New PIN must be different from old PIN")
	}

	// Hash new PIN
	newHashedPIN := utils.HashPIN(input.NewPIN)

	// Update driver PIN
	if err := Models.DB.Model(&driver).Update("pin", newHashedPIN).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to update PIN")
	}

	return utils.SuccessMessageResponse(c, "PIN changed successfully")
}

// RegeneratePIN - Admin regenerates PIN for a driver
// PUT /api/admin/drivers/:id/regenerate-pin
func (ctrl *DriverAuthController) RegeneratePIN(c *fiber.Ctx) error {
	driverID := c.Params("id")

	var driver Models.Driver
	if err := Models.DB.First(&driver, driverID).Error; err != nil {
		return utils.NotFoundResponse(c, "Driver not found")
	}

	// Generate new PIN
	plainPIN, hashedPIN, err := utils.RegeneratePIN(Models.DB)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to generate new PIN")
	}

	// Update driver
	if err := Models.DB.Model(&driver).Update("pin", hashedPIN).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to save new PIN")
	}

	// Log the PIN regeneration
	log.Printf("PIN regenerated for driver %d (%s): %s", driver.ID, driver.Name, plainPIN)

	return utils.SuccessResponse(c, fiber.Map{
		"message": "PIN regenerated successfully",
		"pin":     plainPIN,
		"driver":  driver,
	})
}
