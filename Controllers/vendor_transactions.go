package Controllers

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"Falcon/Models"
)

// TransactionController handles transaction-related API endpoints
type TransactionController struct {
	DB *gorm.DB
}

// NewTransactionController creates a new TransactionController
func NewTransactionController(db *gorm.DB) *TransactionController {
	return &TransactionController{DB: db}
}

// GetVendorTransactions retrieves all transactions for a specific vendor
func (c *TransactionController) GetVendorTransactions(ctx *fiber.Ctx) error {
	vendorID, err := strconv.Atoi(ctx.Params("vendor_id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid vendor ID"})
	}

	// Verify vendor exists
	var vendor Models.Vendor
	if result := c.DB.First(&vendor, vendorID); result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Vendor not found"})
	}

	var transactions []Models.VendorTransaction
	result := c.DB.Where("vendor_id = ?", vendorID).Order("date DESC").Find(&transactions)
	if result.Error != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve transactions"})
	}

	return ctx.JSON(transactions)
}

// GetTransaction retrieves a single transaction by ID
func (c *TransactionController) GetTransaction(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid transaction ID"})
	}

	var transaction Models.VendorTransaction
	result := c.DB.First(&transaction, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Transaction not found"})
	}

	return ctx.JSON(transaction)
}

// CreateTransaction creates a new transaction for a vendor
type CreateTransactionInput struct {
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Date        string  `json:"date"`
	Type        string  `json:"type"`
}

func (c *TransactionController) CreateTransaction(ctx *fiber.Ctx) error {
	vendorID, err := strconv.Atoi(ctx.Params("vendor_id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid vendor ID"})
	}

	// Verify vendor exists
	var vendor Models.Vendor
	if result := c.DB.First(&vendor, vendorID); result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Vendor not found"})
	}

	var input CreateTransactionInput
	if err := ctx.BodyParser(&input); err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Validate required fields
	if input.Description == "" || input.Date == "" || input.Type == "" {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Description, date, and type are required fields"})
	}

	// Parse date
	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid date format. Use YYYY-MM-DD"})
	}

	// Handle transaction type (credit/debit)
	// For credits (purchases from vendor), amount is positive
	// For debits (payments to vendor), amount is negative
	if input.Type == "debit" && input.Amount > 0 {
		input.Amount = -input.Amount
	}

	transaction := Models.VendorTransaction{
		VendorID:    uint(vendorID),
		Date:        date,
		Description: input.Description,
		Amount:      input.Amount,
	}

	result := c.DB.Create(&transaction)
	if result.Error != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create transaction"})
	}

	return ctx.Status(fiber.StatusCreated).JSON(transaction)
}

// UpdateTransaction updates an existing transaction
func (c *TransactionController) UpdateTransaction(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid transaction ID"})
	}

	var transaction Models.VendorTransaction
	result := c.DB.First(&transaction, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Transaction not found"})
	}

	var input CreateTransactionInput
	if err := ctx.BodyParser(&input); err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Parse date if provided
	var date time.Time
	if input.Date != "" {
		var err error
		date, err = time.Parse("2006-01-02", input.Date)
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid date format. Use YYYY-MM-DD"})
		}
	}

	// Handle transaction type (credit/debit)
	// For credits (purchases from vendor), amount is positive
	// For debits (payments to vendor), amount is negative
	amount := input.Amount
	if input.Type == "debit" && input.Amount > 0 {
		amount = -input.Amount
	}

	// Update transaction fields that are provided
	updates := make(map[string]interface{})

	if input.Description != "" {
		updates["description"] = input.Description
	}

	if input.Date != "" {
		updates["date"] = date
	}

	if input.Amount != 0 {
		updates["amount"] = amount
	}

	// Apply updates if any
	if len(updates) > 0 {
		c.DB.Model(&transaction).Updates(updates)

		// Refresh transaction data
		c.DB.First(&transaction, id)
	}

	return ctx.JSON(transaction)
}

// DeleteTransaction soft deletes a transaction
func (c *TransactionController) DeleteTransaction(ctx *fiber.Ctx) error {
	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid transaction ID"})
	}

	var transaction Models.VendorTransaction
	result := c.DB.First(&transaction, id)
	if result.Error != nil {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Transaction not found"})
	}

	c.DB.Delete(&transaction)

	return ctx.JSON(fiber.Map{"message": "Transaction deleted successfully"})
}
