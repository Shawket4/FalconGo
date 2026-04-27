package services

import (
	"Falcon/Models"
	"time"

	"gorm.io/gorm"
)

type UnpaidItemsService struct {
	DB *gorm.DB
}

type UnpaidItemsResponse struct {
	Loans    []Models.Loan    `json:"loans"`
	Expenses []Models.Expense `json:"expenses"`
}

// GetUnpaidItems fetches all unpaid loans and expenses for an entity in a period
func (uis *UnpaidItemsService) GetUnpaidItems(entityType string, entityID uint, periodStart, periodEnd time.Time) (*UnpaidItemsResponse, error) {
	response := &UnpaidItemsResponse{
		Loans:    []Models.Loan{},
		Expenses: []Models.Expense{},
	}

	// Fetch unpaid loans
	loanQuery := uis.DB.Where("is_paid = ?", false).
		Where("date >= ? AND date <= ?", periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"))

	if entityType == "driver" {
		loanQuery = loanQuery.Where("driver_id = ?", entityID)
	} else if entityType == "employee" {
		loanQuery = loanQuery.Where("employee_id = ?", entityID)
	}

	if err := loanQuery.Order("date ASC").Find(&response.Loans).Error; err != nil {
		return nil, err
	}

	// Fetch unpaid expenses (only for drivers)
	if entityType == "driver" {
		expenseQuery := uis.DB.Where("is_paid = ?", false).
			Where("driver_id = ?", entityID).
			Where("date >= ? AND date <= ?", periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"))

		if err := expenseQuery.Order("date ASC").Find(&response.Expenses).Error; err != nil {
			return nil, err
		}
	}

	return response, nil
}

// PopulatePaySlipWithUnpaidItems adds unpaid loans and expenses as line items
func (uis *UnpaidItemsService) PopulatePaySlipWithUnpaidItems(paySlipID uint, entityType string, entityID uint, periodStart, periodEnd time.Time) error {
	// Get unpaid items
	unpaidItems, err := uis.GetUnpaidItems(entityType, entityID, periodStart, periodEnd)
	if err != nil {
		return err
	}

	// Add loans as deduction items
	for i, loan := range unpaidItems.Loans {
		deduction := Models.PaySlipDeduction{
			PaySlipID:      paySlipID,
			Type:           "loan",
			Description:    loan.Description,
			TotalAmount:    loan.Amount,
			DeductedAmount: 0, // User will set this
			OriginalLoanID: &loan.ID,
			Reference:      loan.Method,
			DisplayOrder:   i + 1,
		}

		if err := uis.DB.Create(&deduction).Error; err != nil {
			return err
		}
	}

	// Add expenses as expense items (only for drivers)
	if entityType == "driver" {
		for i, expense := range unpaidItems.Expenses {
			expenseItem := Models.PaySlipExpense{
				PaySlipID:         paySlipID,
				Category:          expense.Category,
				Amount:            expense.Cost,
				Note:              expense.Description,
				OriginalExpenseID: &expense.ID,
				DisplayOrder:      i + 1,
			}

			if err := uis.DB.Create(&expenseItem).Error; err != nil {
				return err
			}
		}
	}

	return nil
}
