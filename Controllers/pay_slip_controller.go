// Controllers/PaySlipController.go
package Controllers

import (
	"Falcon/Models"
	"Falcon/services"
	"Falcon/utils"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type PaySlipController struct {
	DB *gorm.DB
}

// CreatePaySlip creates a new draft pay slip
func (psc *PaySlipController) CreatePaySlip(c *fiber.Ctx) error {
	var request struct {
		EntityType  string    `json:"entity_type" validate:"required,oneof=driver employee"`
		EntityID    uint      `json:"entity_id" validate:"required"`
		PeriodStart time.Time `json:"period_start" validate:"required"`
		PeriodEnd   time.Time `json:"period_end" validate:"required"`
		Notes       string    `json:"notes"`
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	userID := utils.GetUserIDFromContext(c)

	// Validate period
	if request.PeriodEnd.Before(request.PeriodStart) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Period end must be after period start",
		})
	}

	// Validate entity exists
	if request.EntityType == "driver" {
		var driver Models.Driver
		if err := psc.DB.First(&driver, request.EntityID).Error; err != nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Driver not found",
			})
		}
	} else {
		var employee Models.Employee
		if err := psc.DB.First(&employee, request.EntityID).Error; err != nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Employee not found",
			})
		}
	}

	// Get opening balance
	var creditBalance Models.CreditBalance
	if err := psc.DB.Where("entity_type = ? AND entity_id = ?", request.EntityType, request.EntityID).
		First(&creditBalance).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch credit balance",
		})
	}

	// Create pay slip
	paySlip := Models.PaySlip{
		EntityType:     request.EntityType,
		EntityID:       request.EntityID,
		PeriodStart:    request.PeriodStart,
		PeriodEnd:      request.PeriodEnd,
		OpeningBalance: creditBalance.Balance,
		ClosingBalance: creditBalance.Balance, // Will be recalculated
		Status:         "draft",
		Notes:          request.Notes,
		CreatedBy:      userID,
	}

	if err := psc.DB.Create(&paySlip).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create pay slip",
		})
	}

	// Load relationships
	psc.loadPaySlipRelationships(&paySlip)

	return c.Status(http.StatusCreated).JSON(paySlip)
}

// GetPaySlip returns a single pay slip with all line items
// Controllers/PaySlipController.go - UPDATE GetPaySlip
// When fetching pay slip, manually load original expenses
func (psc *PaySlipController) GetPaySlip(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid pay slip ID",
		})
	}

	var paySlip Models.PaySlip
	if err := psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&paySlip, id).Error; err != nil {
		// ... error handling
	}

	// Manually load original expenses
	for i := range paySlip.ExpenseItems {
		if paySlip.ExpenseItems[i].OriginalExpenseID != nil {
			var expense Models.Expense
			if psc.DB.First(&expense, *paySlip.ExpenseItems[i].OriginalExpenseID).Error == nil {
				paySlip.ExpenseItems[i].OriginalExpense = &expense
			}
		}
	}

	// Manually load original loans
	for i := range paySlip.DeductionItems {
		if paySlip.DeductionItems[i].OriginalLoanID != nil {
			var loan Models.Loan
			if psc.DB.First(&loan, *paySlip.DeductionItems[i].OriginalLoanID).Error == nil {
				paySlip.DeductionItems[i].OriginalLoan = &loan
			}
		}
	}

	// Manually load creator/approver
	if paySlip.CreatedBy > 0 {
		var creator Models.User
		if psc.DB.First(&creator, paySlip.CreatedBy).Error == nil {
			paySlip.Creator = &creator
		}
	}

	if paySlip.ApprovedBy != nil && *paySlip.ApprovedBy > 0 {
		var approver Models.User
		if psc.DB.First(&approver, *paySlip.ApprovedBy).Error == nil {
			paySlip.Approver = &approver
		}
	}

	psc.loadPaySlipRelationships(&paySlip)

	return c.JSON(paySlip)
}

// GetAllPaySlips returns all pay slips with filters
func (psc *PaySlipController) GetAllPaySlips(c *fiber.Ctx) error {
	var paySlips []Models.PaySlip

	query := psc.DB.Model(&Models.PaySlip{})

	// Filter by entity type
	if entityType := c.Query("entity_type"); entityType != "" {
		query = query.Where("entity_type = ?", entityType)
	}

	// Filter by entity ID
	if entityID := c.Query("entity_id"); entityID != "" {
		query = query.Where("entity_id = ?", entityID)
	}

	// Filter by status
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// Filter by period
	if periodStart := c.Query("period_start"); periodStart != "" {
		query = query.Where("period_start >= ?", periodStart)
	}
	if periodEnd := c.Query("period_end"); periodEnd != "" {
		query = query.Where("period_end <= ?", periodEnd)
	}

	// Order by created date (newest first)
	query = query.Order("created_at DESC")

	// Pagination
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	query = query.Limit(limit).Offset(offset)

	if err := query.Find(&paySlips).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch pay slips",
		})
	}

	// Load entity details for each
	for i := range paySlips {
		psc.loadPaySlipRelationships(&paySlips[i])
	}

	return c.JSON(paySlips)
}

// UpdatePaySlip updates a draft pay slip
func (psc *PaySlipController) UpdatePaySlip(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid pay slip ID",
		})
	}

	var paySlip Models.PaySlip
	if err := psc.DB.First(&paySlip, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Pay slip not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch pay slip",
		})
	}

	// Check if editable
	if !paySlip.IsEditable() {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot edit finalized or paid pay slip",
		})
	}

	var updateData struct {
		PeriodStart time.Time `json:"period_start"`
		PeriodEnd   time.Time `json:"period_end"`
		Notes       string    `json:"notes"`
	}

	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Update fields
	if !updateData.PeriodStart.IsZero() {
		paySlip.PeriodStart = updateData.PeriodStart
	}
	if !updateData.PeriodEnd.IsZero() {
		paySlip.PeriodEnd = updateData.PeriodEnd
	}
	paySlip.Notes = updateData.Notes

	if err := psc.DB.Save(&paySlip).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update pay slip",
		})
	}

	psc.loadPaySlipRelationships(&paySlip)

	return c.JSON(paySlip)
}

// DeletePaySlip soft deletes a draft pay slip
func (psc *PaySlipController) DeletePaySlip(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid pay slip ID",
		})
	}

	var paySlip Models.PaySlip
	if err := psc.DB.First(&paySlip, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Pay slip not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch pay slip",
		})
	}

	// Can only delete drafts
	if paySlip.Status != "draft" {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Can only delete draft pay slips",
		})
	}

	// Soft delete (cascade will handle line items)
	if err := psc.DB.Delete(&paySlip).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete pay slip",
		})
	}

	return c.JSON(fiber.Map{
		"message": "Pay slip deleted successfully",
	})
}

func (pc *PaySlipController) GetUnpaidItems(c *fiber.Ctx) error {
	entityType := c.Query("entity_type")
	entityIDStr := c.Query("entity_id")
	periodStart := c.Query("period_start")
	periodEnd := c.Query("period_end")
	// Validate required parameters
	if entityType == "" || entityIDStr == "" || periodStart == "" || periodEnd == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Missing required parameters: entity_type, entity_id, period_start, period_end",
		})
	}

	// Validate entity type
	if entityType != "driver" && entityType != "employee" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid entity type. Must be 'driver' or 'employee'",
		})
	}
	// Parse entity ID
	entityID, err := strconv.ParseUint(entityIDStr, 10, 32)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid entity ID",
		})
	}

	// Parse dates
	startTime, err := time.Parse(time.RFC3339, periodStart)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid period_start format. Use RFC3339 format",
		})
	}

	endTime, err := time.Parse(time.RFC3339, periodEnd)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid period_end format. Use RFC3339 format",
		})
	}

	// Get unpaid loans
	var loans []Models.Loan
	if err := pc.DB.Where(
		"driver_id = ? AND is_paid = ? AND date BETWEEN ? AND ?",
		uint(entityID),
		false,
		startTime.Format("2006-01-02"),
		endTime.Format("2006-01-02"),
	).Order("date ASC").Find(&loans).Error; err != nil {
		log.Println("Error fetching unpaid loans:", err)
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch unpaid loans",
		})
	}

	// Get unpaid expenses (only for drivers)
	var expenses []Models.Expense
	if entityType == "driver" {
		fmt.Println(entityID)
		// fmt.Println(len(expenses))
		if err := pc.DB.Model(&Models.Expense{}).Where(
			"driver_id = ? AND is_paid = ? AND date BETWEEN ? AND ?",
			uint(entityID),
			false,
			startTime.Format("2006-01-02"),
			endTime.Format("2006-01-02"),
		).Order("date ASC").Find(&expenses).Error; err != nil {
			log.Println("Error fetching unpaid expenses:", err)
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to fetch unpaid expenses",
			})
		}
		fmt.Println(len(expenses))
	}

	return c.JSON(fiber.Map{
		"loans":    loans,
		"expenses": expenses,
	})
}

// AutoPopulateUnpaidItems fetches and adds unpaid loans/expenses to a pay slip
func (psc *PaySlipController) AutoPopulateUnpaidItems(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	var paySlip Models.PaySlip
	if err := psc.DB.First(&paySlip, paySlipID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Pay slip not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch pay slip",
		})
	}

	if !paySlip.IsEditable() {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": "Cannot modify finalized pay slip",
		})
	}

	service := &services.UnpaidItemsService{DB: psc.DB}
	if err := service.PopulatePaySlipWithUnpaidItems(
		paySlip.ID,
		paySlip.EntityType,
		paySlip.EntityID,
		paySlip.PeriodStart,
		paySlip.PeriodEnd,
	); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to populate unpaid items",
		})
	}

	// Recalculate totals
	calcService := &services.PaySlipCalculationService{DB: psc.DB}
	updatedPaySlip, err := calcService.CalculateTotals(paySlip.ID)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to calculate totals",
		})
	}

	// Reload with all items
	psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(updatedPaySlip, paySlip.ID)

	psc.loadPaySlipRelationships(updatedPaySlip)

	return c.JSON(fiber.Map{
		"message":  "Unpaid items populated successfully",
		"pay_slip": updatedPaySlip,
	})
}

// RecalculateTotals recalculates all totals for a pay slip
func (psc *PaySlipController) RecalculateTotals(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	service := &services.PaySlipCalculationService{DB: psc.DB}
	paySlip, err := service.CalculateTotals(uint(paySlipID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Pay slip not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to calculate totals",
		})
	}

	// Reload with all items
	psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(paySlip, paySlipID)

	psc.loadPaySlipRelationships(paySlip)

	return c.JSON(paySlip)
}

// FinalizePaySlip finalizes a draft pay slip
func (psc *PaySlipController) FinalizePaySlip(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	userID := utils.GetUserIDFromContext(c)

	// Recalculate totals before finalizing
	calcService := &services.PaySlipCalculationService{DB: psc.DB}
	if _, err := calcService.CalculateTotals(uint(paySlipID)); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to calculate totals",
		})
	}

	// Finalize
	finalizationService := &services.PaySlipFinalizationService{DB: psc.DB}
	if err := finalizationService.FinalizePaySlip(uint(paySlipID), userID); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Reload finalized pay slip
	var paySlip Models.PaySlip
	psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&paySlip, paySlipID)

	psc.loadPaySlipRelationships(&paySlip)

	return c.JSON(fiber.Map{
		"message":  "Pay slip finalized successfully",
		"pay_slip": paySlip,
	})
}

// MarkAsPaid marks a finalized pay slip as paid
func (psc *PaySlipController) MarkAsPaid(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	var request struct {
		PaymentMethod string `json:"payment_method" validate:"required"`
		PaymentRef    string `json:"payment_ref"`
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	service := &services.PaySlipFinalizationService{DB: psc.DB}
	if err := service.MarkAsPaid(uint(paySlipID), request.PaymentMethod, request.PaymentRef); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Reload pay slip
	var paySlip Models.PaySlip
	psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&paySlip, paySlipID)

	psc.loadPaySlipRelationships(&paySlip)

	return c.JSON(fiber.Map{
		"message":  "Pay slip marked as paid",
		"pay_slip": paySlip,
	})
}

// Helper to load entity relationships (already exists from Phase 2)
func (psc *PaySlipController) loadPaySlipRelationships(paySlip *Models.PaySlip) {
	if paySlip.EntityType == "driver" {
		var driver Models.Driver
		psc.DB.First(&driver, paySlip.EntityID)
		paySlip.Driver = &driver
	} else if paySlip.EntityType == "employee" {
		var employee Models.Employee
		psc.DB.First(&employee, paySlip.EntityID)
		paySlip.Employee = &employee
	}
}

func (psc *PaySlipController) BatchCreatePaySlip(c *fiber.Ctx) error {
	var request struct {
		EntityType         string    `json:"entity_type" validate:"required,oneof=driver employee"`
		EntityID           uint      `json:"entity_id" validate:"required"`
		PeriodStart        time.Time `json:"period_start" validate:"required"`
		PeriodEnd          time.Time `json:"period_end" validate:"required"`
		AutoPopulateUnpaid bool      `json:"auto_populate_unpaid"`
		IncomeItems        []struct {
			Type         string  `json:"type"`
			Description  string  `json:"description"`
			Quantity     float64 `json:"quantity"`
			Rate         float64 `json:"rate"`
			IsAPIFetched bool    `json:"is_api_fetched"`
		} `json:"income_items"`
		Adjustments []struct {
			Type   string  `json:"type"`
			Amount float64 `json:"amount"`
			Note   string  `json:"note"`
		} `json:"adjustments"`
		Notes string `json:"notes"`
		// Settlement options
		SettlementMode string  `json:"settlement_mode"` // "full" or "custom"
		CustomPayment  float64 `json:"custom_payment"`  // Only used if mode is "custom"
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	userID := utils.GetUserIDFromContext(c)

	// Validate period
	if request.PeriodEnd.Before(request.PeriodStart) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Period end must be after period start",
		})
	}

	// Start transaction
	tx := psc.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. Validate entity exists
	if request.EntityType == "driver" {
		var driver Models.Driver
		if err := tx.First(&driver, request.EntityID).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Driver not found",
			})
		}
	} else {
		var employee Models.Employee
		if err := tx.First(&employee, request.EntityID).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Employee not found",
			})
		}
	}

	// 2. Get opening balance
	var creditBalance Models.CreditBalance
	if err := tx.Where("entity_type = ? AND entity_id = ?", request.EntityType, request.EntityID).
		First(&creditBalance).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch credit balance",
		})
	}

	// 3. Create pay slip
	paySlip := Models.PaySlip{
		EntityType:     request.EntityType,
		EntityID:       request.EntityID,
		PeriodStart:    request.PeriodStart,
		PeriodEnd:      request.PeriodEnd,
		OpeningBalance: creditBalance.Balance,
		Status:         "draft",
		Notes:          request.Notes,
		CreatedBy:      userID,
	}

	if err := tx.Create(&paySlip).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create pay slip",
		})
	}

	// 4. Auto-populate unpaid loans and expenses if requested
	if request.AutoPopulateUnpaid {
		// Fetch unpaid loans
		var loans []Models.Loan
		loanQuery := tx.Where("is_paid = ?", false).
			Where("date >= ? AND date <= ?",
				request.PeriodStart.Format("2006-01-02"),
				request.PeriodEnd.Format("2006-01-02"))

		if request.EntityType == "driver" {
			loanQuery = loanQuery.Where("driver_id = ?", request.EntityID)
		} else {
			loanQuery = loanQuery.Where("employee_id = ?", request.EntityID)
		}

		if err := loanQuery.Find(&loans).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to fetch loans",
			})
		}

		// Create deduction items for loans (all with full amount, no partial deductions)
		for i, loan := range loans {
			deduction := Models.PaySlipDeduction{
				PaySlipID:      paySlip.ID,
				Type:           "loan",
				Description:    loan.Description,
				TotalAmount:    loan.Amount,
				DeductedAmount: loan.Amount, // Full amount
				OriginalLoanID: &loan.ID,
				Reference:      loan.Method,
				DisplayOrder:   i + 1,
			}

			if err := tx.Create(&deduction).Error; err != nil {
				tx.Rollback()
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to create deduction item",
				})
			}
		}

		// Fetch and create expense items (only for drivers)
		if request.EntityType == "driver" {
			var expenses []Models.Expense
			if err := tx.Where("is_paid = ?", false).
				Where("driver_id = ?", request.EntityID).
				Where("date >= ? AND date <= ?",
					request.PeriodStart.Format("2006-01-02"),
					request.PeriodEnd.Format("2006-01-02")).
				Find(&expenses).Error; err != nil {
				tx.Rollback()
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to fetch expenses",
				})
			}

			for i, expense := range expenses {
				expenseItem := Models.PaySlipExpense{
					PaySlipID:         paySlip.ID,
					Category:          expense.Category,
					Amount:            expense.Cost,
					Note:              expense.Description,
					OriginalExpenseID: &expense.ID,
					DisplayOrder:      i + 1,
				}

				if err := tx.Create(&expenseItem).Error; err != nil {
					tx.Rollback()
					return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
						"error": "Failed to create expense item",
					})
				}
			}
		}
	}

	// 5. Add income items
	for i, incomeData := range request.IncomeItems {
		income := Models.PaySlipIncome{
			PaySlipID:    paySlip.ID,
			Type:         incomeData.Type,
			Description:  incomeData.Description,
			Quantity:     incomeData.Quantity,
			Rate:         incomeData.Rate,
			IsAPIFetched: incomeData.IsAPIFetched,
			DisplayOrder: i + 1,
		}

		if err := tx.Create(&income).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to create income item",
			})
		}
	}

	// 6. Add adjustments
	for i, adjData := range request.Adjustments {
		adjustment := Models.PaySlipAdjustment{
			PaySlipID:    paySlip.ID,
			Type:         adjData.Type,
			Amount:       adjData.Amount,
			Note:         adjData.Note,
			DisplayOrder: i + 1,
		}

		if err := tx.Create(&adjustment).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to create adjustment",
			})
		}
	}

	// 7. Calculate totals with CORRECT FORMULA
	var grossIncome, totalExpenses, totalLoans, totalAdjustments float64

	// Income
	tx.Model(&Models.PaySlipIncome{}).
		Where("pay_slip_id = ?", paySlip.ID).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&grossIncome)

	// Expenses (to be reimbursed)
	tx.Model(&Models.PaySlipExpense{}).
		Where("pay_slip_id = ?", paySlip.ID).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&totalExpenses)

	// Loans (total taken)
	tx.Model(&Models.PaySlipDeduction{}).
		Where("pay_slip_id = ?", paySlip.ID).
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&totalLoans)

	// Adjustments
	tx.Model(&Models.PaySlipAdjustment{}).
		Where("pay_slip_id = ?", paySlip.ID).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&totalAdjustments)

	// CORRECT CALCULATION
	// Period Net = Income - (Loans - Expenses) + Adjustments
	periodNet := grossIncome - (totalLoans - totalExpenses) + totalAdjustments
	totalToSettle := paySlip.OpeningBalance + periodNet

	// Settlement logic - THIS IS THE KEY PART
	var netPayable, closingBalance float64

	if request.SettlementMode == "custom" {
		// Custom payment - use the exact amount specified
		netPayable = request.CustomPayment
		closingBalance = totalToSettle - request.CustomPayment
	} else {
		// Full settlement
		if totalToSettle > 0 {
			netPayable = totalToSettle
			closingBalance = 0
		} else {
			netPayable = 0
			closingBalance = totalToSettle // Debt remains
		}
	}

	// Update pay slip with calculated values
	paySlip.GrossIncome = grossIncome
	paySlip.TotalExpenses = totalExpenses
	paySlip.TotalLoans = totalLoans
	paySlip.DeductionAmount = totalLoans // Since we're taking all loans
	paySlip.TotalAdjustments = totalAdjustments
	paySlip.NetPayable = netPayable // This is the actual cash payment
	paySlip.ClosingBalance = closingBalance

	if err := tx.Save(&paySlip).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update pay slip totals",
		})
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to commit transaction",
		})
	}

	// 8. Load complete pay slip with all relationships
	psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&paySlip, paySlip.ID)

	psc.loadPaySlipRelationships(&paySlip)

	// Return with detailed calculation breakdown
	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"message":  "Pay slip created successfully",
		"pay_slip": paySlip,
		"calculation": fiber.Map{
			"gross_income":      grossIncome,
			"total_loans":       totalLoans,
			"total_expenses":    totalExpenses,
			"total_adjustments": totalAdjustments,
			"period_net":        periodNet,
			"opening_balance":   paySlip.OpeningBalance,
			"total_to_settle":   totalToSettle,
			"settlement_mode":   request.SettlementMode,
			"cash_payment":      netPayable,
			"closing_balance":   closingBalance,
		},
	})
}

func (psc *PaySlipController) GeneratePDF(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	userID := utils.GetUserIDFromContext(c)

	var paySlip Models.PaySlip
	if err := psc.DB.First(&paySlip, paySlipID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Pay slip not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch pay slip",
		})
	}

	// Generate PDF
	pdfService := &services.PaySlipPDFService{DB: psc.DB}
	pdfPath, err := pdfService.GeneratePDF(uint(paySlipID))
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate PDF",
		})
	}

	// Update pay slip with PDF path
	paySlip.PDFPath = pdfPath
	psc.DB.Save(&paySlip)

	// Log audit
	auditService := &services.PaySlipAuditService{DB: psc.DB}
	auditService.LogAction(
		paySlip.ID,
		"pdf_generated",
		userID,
		fiber.Map{"pdf_path": pdfPath},
		c.IP(),
		c.Get("User-Agent"),
	)

	return c.JSON(fiber.Map{
		"message":  "PDF generated successfully",
		"pdf_path": pdfPath,
	})
}

// DownloadPDF serves the PDF file for download
func (psc *PaySlipController) DownloadPDF(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	var paySlip Models.PaySlip
	if err := psc.DB.First(&paySlip, paySlipID).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Pay slip not found",
		})
	}

	if paySlip.PDFPath == "" {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "PDF not generated yet",
		})
	}

	return c.SendFile(paySlip.PDFPath)
}

// GetAuditTrail returns audit history for a pay slip
func (psc *PaySlipController) GetAuditTrail(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))

	auditService := &services.PaySlipAuditService{DB: psc.DB}
	audits, err := auditService.GetAuditTrail(uint(paySlipID))
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch audit trail",
		})
	}

	return c.JSON(audits)
}

// DuplicatePaySlip creates a copy of an existing pay slip
func (psc *PaySlipController) DuplicatePaySlip(c *fiber.Ctx) error {
	paySlipID, _ := strconv.Atoi(c.Params("id"))
	userID := utils.GetUserIDFromContext(c)
	// Load original pay slip
	var original Models.PaySlip
	if err := psc.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&original, paySlipID).Error; err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "Pay slip not found",
		})
	}

	// Start transaction
	tx := psc.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get current balance
	var creditBalance Models.CreditBalance
	tx.Where("entity_type = ? AND entity_id = ?", original.EntityType, original.EntityID).
		First(&creditBalance)

	// Create new pay slip
	duplicate := Models.PaySlip{
		EntityType:     original.EntityType,
		EntityID:       original.EntityID,
		PeriodStart:    original.PeriodEnd.AddDate(0, 0, 1), // Next day after original
		PeriodEnd:      original.PeriodEnd.AddDate(0, 1, 0), // One month later
		OpeningBalance: creditBalance.Balance,
		Status:         "draft",
		Notes:          original.Notes + " (Duplicated)",
		CreatedBy:      userID,
	}

	if err := tx.Create(&duplicate).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to duplicate pay slip",
		})
	}

	// Copy income items
	for _, item := range original.IncomeItems {
		newItem := Models.PaySlipIncome{
			PaySlipID:    duplicate.ID,
			Type:         item.Type,
			Description:  item.Description,
			Quantity:     0, // Reset quantity
			Rate:         item.Rate,
			IsAPIFetched: false,
			DisplayOrder: item.DisplayOrder,
		}
		// Checked, like the pay slip itself above. Unchecked, a failed copy
		// left the loop running, the transaction committing and the caller
		// reading "duplicated successfully" over a pay slip missing a line.
		if err := tx.Create(&newItem).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to duplicate pay slip income items",
			})
		}
	}

	// Copy adjustments (if recurring)
	for _, item := range original.AdjustmentItems {
		newItem := Models.PaySlipAdjustment{
			PaySlipID:    duplicate.ID,
			Type:         item.Type,
			Amount:       item.Amount,
			Note:         item.Note + " (from previous period)",
			DisplayOrder: item.DisplayOrder,
		}
		if err := tx.Create(&newItem).Error; err != nil {
			tx.Rollback()
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to duplicate pay slip adjustments",
			})
		}
	}

	// Commit
	if err := tx.Commit().Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to commit duplication",
		})
	}

	// Load complete duplicate
	psc.DB.
		Preload("IncomeItems").
		Preload("AdjustmentItems").
		First(&duplicate, duplicate.ID)

	psc.loadPaySlipRelationships(&duplicate)

	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"message":  "Pay slip duplicated successfully",
		"pay_slip": duplicate,
	})
}
