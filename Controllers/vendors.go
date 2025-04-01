package Controllers

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"Falcon/Models"
)

// VendorController handles vendor-related API endpoints
type VendorController struct {
	DB *gorm.DB
}

// NewVendorController creates a new VendorController
func NewVendorController(db *gorm.DB) *VendorController {
	return &VendorController{DB: db}
}

// GetVendors retrieves all vendors
func (c *VendorController) GetVendors(ctx *fiber.Ctx) error {
	var vendors []Models.Vendor
	result := c.DB.Find(&vendors)
	if result.Error != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve vendors"})
	}

	return ctx.JSON(vendors)
}

// GetVendor retrieves a single vendor by ID
func (c *VendorController) GetVendor(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid vendor ID"})
	}

	var vendor Models.Vendor
	result := c.DB.First(&vendor, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Vendor not found"})
	}

	return ctx.JSON(vendor)
}

// CreateVendor creates a new vendor
func (c *VendorController) CreateVendor(ctx *fiber.Ctx) error {
	var input Models.Vendor

	if err := ctx.BodyParser(&input); err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Create vendor
	vendor := Models.Vendor{
		Name:    input.Name,
		Contact: input.Contact,
		Notes:   input.Notes,
	}

	result := c.DB.Create(&vendor)
	if result.Error != nil {
		// Check if it's a unique constraint error
		if strings.Contains(result.Error.Error(), "unique constraint") ||
			strings.Contains(result.Error.Error(), "Duplicate entry") {
			return ctx.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "A vendor with this name already exists",
			})
		}

		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create vendor",
		})
	}

	return ctx.Status(fiber.StatusCreated).JSON(vendor)
}

// UpdateVendor updates an existing vendor
func (c *VendorController) UpdateVendor(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid vendor ID"})
	}

	var vendor Models.Vendor
	result := c.DB.First(&vendor, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Vendor not found"})
	}

	var input Models.Vendor
	if err := ctx.BodyParser(&input); err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Update fields
	c.DB.Model(&vendor).Updates(Models.Vendor{
		Name:    input.Name,
		Contact: input.Contact,
		Notes:   input.Notes,
	})

	return ctx.JSON(vendor)
}

// DeleteVendor soft deletes a vendor
func (c *VendorController) DeleteVendor(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid vendor ID"})
	}

	var vendor Models.Vendor
	result := c.DB.First(&vendor, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Vendor not found"})
	}

	c.DB.Delete(&vendor)

	return ctx.JSON(fiber.Map{"message": "Vendor deleted successfully"})
}

// GetVendorBalance calculates the current balance for a vendor
func (c *VendorController) GetVendorBalance(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid vendor ID"})
	}

	var vendor Models.Vendor
	result := c.DB.First(&vendor, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Vendor not found"})
	}

	// Calculate balance from transactions
	var balance float64
	c.DB.Model(&Models.Transaction{}).Where("vendor_id = ?", id).Select("COALESCE(SUM(amount), 0)").Scan(&balance)

	return ctx.JSON(fiber.Map{
		"vendor_id": id,
		"name":      vendor.Name,
		"balance":   balance,
	})
}
