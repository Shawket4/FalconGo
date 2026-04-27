// Controllers/PaySlipItemsController.go
package Controllers

import (
	"Falcon/Models"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type PaySlipItemsController struct {
	DB *gorm.DB
}

// ============= INCOME ITEMS =============

func (pic *PaySlipItemsController) AddIncomeItem(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("pay_slip_id"))

	// Check if pay slip is editable
	if !pic.isPaySlipEditable(uint(paySlipID)) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var item Models.PaySlipIncome
	if err := c.BodyParser(&item); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	item.PaySlipID = uint(paySlipID)

	if err := pic.DB.Create(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create income item",
		})
	}

	return c.Status(http.StatusCreated).JSON(item)
}

func (pic *PaySlipItemsController) UpdateIncomeItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipIncome
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Income item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var updateData Models.PaySlipIncome
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Update fields
	item.Type = updateData.Type
	item.Description = updateData.Description
	item.Quantity = updateData.Quantity
	item.Rate = updateData.Rate
	item.IsAPIFetched = updateData.IsAPIFetched
	item.DisplayOrder = updateData.DisplayOrder

	if err := pic.DB.Save(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update income item",
		})
	}

	return c.JSON(item)
}

func (pic *PaySlipItemsController) DeleteIncomeItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipIncome
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Income item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	if err := pic.DB.Delete(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete income item",
		})
	}

	return c.JSON(fiber.Map{"message": "Income item deleted"})
}

// ============= EXPENSE ITEMS =============

func (pic *PaySlipItemsController) AddExpenseItem(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("pay_slip_id"))

	if !pic.isPaySlipEditable(uint(paySlipID)) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var item Models.PaySlipExpense
	if err := c.BodyParser(&item); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	item.PaySlipID = uint(paySlipID)

	if err := pic.DB.Create(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create expense item",
		})
	}

	return c.Status(http.StatusCreated).JSON(item)
}

func (pic *PaySlipItemsController) UpdateExpenseItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipExpense
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Expense item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var updateData Models.PaySlipExpense
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	item.Category = updateData.Category
	item.Amount = updateData.Amount
	item.ReceiptRef = updateData.ReceiptRef
	item.Note = updateData.Note
	item.DisplayOrder = updateData.DisplayOrder

	if err := pic.DB.Save(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update expense item",
		})
	}

	return c.JSON(item)
}

func (pic *PaySlipItemsController) DeleteExpenseItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipExpense
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Expense item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	if err := pic.DB.Delete(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete expense item",
		})
	}

	return c.JSON(fiber.Map{"message": "Expense item deleted"})
}

// ============= DEDUCTION ITEMS =============

func (pic *PaySlipItemsController) AddDeductionItem(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("pay_slip_id"))

	if !pic.isPaySlipEditable(uint(paySlipID)) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var item Models.PaySlipDeduction
	if err := c.BodyParser(&item); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	item.PaySlipID = uint(paySlipID)

	if err := pic.DB.Create(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create deduction item",
		})
	}

	return c.Status(http.StatusCreated).JSON(item)
}

func (pic *PaySlipItemsController) UpdateDeductionItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipDeduction
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Deduction item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var updateData Models.PaySlipDeduction
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	item.Type = updateData.Type
	item.Description = updateData.Description
	item.TotalAmount = updateData.TotalAmount
	item.DeductedAmount = updateData.DeductedAmount
	item.Reference = updateData.Reference
	item.DisplayOrder = updateData.DisplayOrder

	if err := pic.DB.Save(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update deduction item",
		})
	}

	return c.JSON(item)
}

func (pic *PaySlipItemsController) DeleteDeductionItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipDeduction
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Deduction item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	if err := pic.DB.Delete(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete deduction item",
		})
	}

	return c.JSON(fiber.Map{"message": "Deduction item deleted"})
}

// ============= ADJUSTMENT ITEMS =============

func (pic *PaySlipItemsController) AddAdjustmentItem(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("pay_slip_id"))

	if !pic.isPaySlipEditable(uint(paySlipID)) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var item Models.PaySlipAdjustment
	if err := c.BodyParser(&item); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Validate note is required
	if item.Note == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Note is required for adjustments",
		})
	}

	item.PaySlipID = uint(paySlipID)

	if err := pic.DB.Create(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create adjustment item",
		})
	}

	return c.Status(http.StatusCreated).JSON(item)
}

func (pic *PaySlipItemsController) UpdateAdjustmentItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipAdjustment
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Adjustment item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	var updateData Models.PaySlipAdjustment
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	item.Type = updateData.Type
	item.Amount = updateData.Amount
	item.Note = updateData.Note
	item.DisplayOrder = updateData.DisplayOrder

	if err := pic.DB.Save(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update adjustment item",
		})
	}

	return c.JSON(item)
}

func (pic *PaySlipItemsController) DeleteAdjustmentItem(c *fiber.Ctx) error {
	id, _ := strconv.Atoi(c.Params("id"))

	var item Models.PaySlipAdjustment
	if err := pic.DB.First(&item, id).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Adjustment item not found",
		})
	}

	if !pic.isPaySlipEditable(item.PaySlipID) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized pay slip",
		})
	}

	if err := pic.DB.Delete(&item).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete adjustment item",
		})
	}

	return c.JSON(fiber.Map{"message": "Adjustment item deleted"})
}

// ============= HELPER FUNCTIONS =============

func (pic *PaySlipItemsController) isPaySlipEditable(paySlipID uint) bool {
	var paySlip Models.PaySlip
	if err := pic.DB.First(&paySlip, paySlipID).Error; err != nil {
		return false
	}
	return paySlip.IsEditable()
}
