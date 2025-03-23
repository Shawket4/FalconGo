package Controllers

import (
	"Falcon/Models"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// VendorHandler contains the database connection
type VendorHandler struct {
	DB *gorm.DB
}

// NewVendorHandler creates a new vendor handler with the given database connection
func NewVendorHandler(db *gorm.DB) *VendorHandler {
	return &VendorHandler{DB: db}
}

// CreateVendor adds a new vendor to the database
func (h *VendorHandler) CreateVendor(c *fiber.Ctx) error {
	vendor := new(Models.Vendor)

	// Parse request body
	if err := c.BodyParser(vendor); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// Validate vendor data
	if vendor.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vendor name is required",
		})
	}

	// Create vendor in database
	result := h.DB.Create(&vendor)
	if result.Error != nil {
		// Check for duplicate vendor name
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "A vendor with this name already exists",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create vendor",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(vendor)
}

// GetVendors returns all vendors
func (h *VendorHandler) GetVendors(c *fiber.Ctx) error {
	var vendors []Models.Vendor

	result := h.DB.Find(&vendors)
	if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendors",
		})
	}

	return c.JSON(vendors)
}

// GetVendor returns a specific vendor by ID
func (h *VendorHandler) GetVendor(c *fiber.Ctx) error {
	id := c.Params("id")
	var vendor Models.Vendor

	result := h.DB.Preload("Expenses").First(&vendor, id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	return c.JSON(vendor)
}

// UpdateVendor updates a vendor's information
func (h *VendorHandler) UpdateVendor(c *fiber.Ctx) error {
	id := c.Params("id")
	var vendor Models.Vendor

	// Check if vendor exists
	if result := h.DB.First(&vendor, id); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Parse request body
	updates := new(Models.Vendor)
	if err := c.BodyParser(updates); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// Validate updates
	if updates.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vendor name is required",
		})
	}

	// Update vendor
	vendor.Name = updates.Name
	if result := h.DB.Save(&vendor); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "A vendor with this name already exists",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update vendor",
		})
	}

	return c.JSON(vendor)
}

// DeleteVendor deletes a vendor by ID
func (h *VendorHandler) DeleteVendor(c *fiber.Ctx) error {
	id := c.Params("id")
	var vendor Models.Vendor

	// Check if vendor exists
	if result := h.DB.First(&vendor, id); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Delete vendor
	if result := h.DB.Delete(&vendor); result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete vendor",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Vendor deleted successfully",
	})
}

// ExpenseHandler contains the database connection
type ExpenseHandler struct {
	DB *gorm.DB
}

// NewExpenseHandler creates a new expense handler with the given database connection
func NewExpenseHandler(db *gorm.DB) *ExpenseHandler {
	return &ExpenseHandler{DB: db}
}

// CreateExpense adds a new expense to a vendor
func (h *ExpenseHandler) CreateExpense(c *fiber.Ctx) error {
	vendorID := c.Params("vendorId")
	var vendor Models.Vendor

	// Check if vendor exists
	if result := h.DB.First(&vendor, vendorID); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Parse request body
	expense := new(Models.VendorExpense)
	if err := c.BodyParser(expense); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// Validate expense data
	if expense.Description == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Expense description is required",
		})
	}

	if expense.Price <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Expense price must be greater than zero",
		})
	}

	// Set vendor ID and create expense
	vendorIDUint, _ := strconv.ParseUint(vendorID, 10, 32)
	expense.VendorID = uint(vendorIDUint)

	result := h.DB.Create(&expense)
	if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create expense",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(expense)
}

// GetExpenses returns all expenses for a specific vendor
func (h *ExpenseHandler) GetExpenses(c *fiber.Ctx) error {
	vendorID := c.Params("vendorId")
	var expenses []Models.VendorExpense

	// Check if vendor exists
	var vendor Models.Vendor
	if result := h.DB.First(&vendor, vendorID); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Get all expenses for the vendor
	result := h.DB.Where("vendor_id = ?", vendorID).Find(&expenses)
	if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch expenses",
		})
	}

	return c.JSON(expenses)
}

// GetExpense returns a specific expense by ID
func (h *ExpenseHandler) GetExpense(c *fiber.Ctx) error {
	vendorID := c.Params("vendorId")
	expenseID := c.Params("expenseId")
	var expense Models.VendorExpense

	// Check if vendor exists
	var vendor Models.Vendor
	if result := h.DB.First(&vendor, vendorID); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Get the specific expense
	result := h.DB.Where("vendor_id = ? AND id = ?", vendorID, expenseID).First(&expense)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Expense not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch expense",
		})
	}

	return c.JSON(expense)
}

// UpdateExpense updates an expense's information
func (h *ExpenseHandler) UpdateExpense(c *fiber.Ctx) error {
	vendorID := c.Params("vendorId")
	expenseID := c.Params("expenseId")
	var expense Models.VendorExpense

	// Check if vendor exists
	var vendor Models.Vendor
	if result := h.DB.First(&vendor, vendorID); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Check if expense exists
	if result := h.DB.Where("vendor_id = ? AND id = ?", vendorID, expenseID).First(&expense); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Expense not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch expense",
		})
	}

	// Parse request body
	updates := new(Models.VendorExpense)
	if err := c.BodyParser(updates); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// Validate updates
	if updates.Description == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Expense description is required",
		})
	}

	if updates.Price <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Expense price must be greater than zero",
		})
	}

	// Update expense
	expense.Description = updates.Description
	expense.Price = updates.Price

	if result := h.DB.Save(&expense); result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update expense",
		})
	}

	return c.JSON(expense)
}

// DeleteExpense deletes an expense by ID
func (h *ExpenseHandler) DeleteExpense(c *fiber.Ctx) error {
	vendorID := c.Params("vendorId")
	expenseID := c.Params("expenseId")
	var expense Models.VendorExpense

	// Check if vendor exists
	var vendor Models.Vendor
	if result := h.DB.First(&vendor, vendorID); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Vendor not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch vendor",
		})
	}

	// Check if expense exists
	if result := h.DB.Where("vendor_id = ? AND id = ?", vendorID, expenseID).First(&expense); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Expense not found",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch expense",
		})
	}

	// Delete expense
	if result := h.DB.Delete(&expense); result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete expense",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Expense deleted successfully",
	})
}
