// Services/PaySlipCalculationService.go
package services

import (
	"Falcon/Models"

	"gorm.io/gorm"
)

type PaySlipCalculationService struct {
	DB *gorm.DB
}

// CalculateTotals recalculates all totals for a pay slip with correct formula
func (ps *PaySlipCalculationService) CalculateTotals(paySlipID uint) (*Models.PaySlip, error) {
	var paySlip Models.PaySlip

	// Load pay slip with all line items
	if err := ps.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&paySlip, paySlipID).Error; err != nil {
		return nil, err
	}

	// Calculate gross income (trips, bonuses, etc.)
	var grossIncome float64
	for _, item := range paySlip.IncomeItems {
		grossIncome += item.Amount
	}
	paySlip.GrossIncome = grossIncome

	// Calculate total expenses (what driver paid from pocket - to be reimbursed)
	var totalExpenses float64
	for _, item := range paySlip.ExpenseItems {
		totalExpenses += item.Amount
	}
	paySlip.TotalExpenses = totalExpenses

	// Calculate total loans/custodies taken
	var totalLoans float64
	var deductionAmount float64
	for _, item := range paySlip.DeductionItems {
		totalLoans += item.TotalAmount
		deductionAmount += item.DeductedAmount
	}
	paySlip.TotalLoans = totalLoans
	paySlip.DeductionAmount = deductionAmount

	// Calculate total adjustments (bonuses/penalties)
	var totalAdjustments float64
	for _, item := range paySlip.AdjustmentItems {
		totalAdjustments += item.Amount
	}
	paySlip.TotalAdjustments = totalAdjustments

	// CORRECTED FORMULA
	// Period Net = Income - (Loans + Custodies - Expenses) + Adjustments
	periodNet := grossIncome - (totalLoans - totalExpenses) + totalAdjustments

	// Total to settle = Opening Balance + Period Net
	totalToSettle := paySlip.OpeningBalance + periodNet

	// For now, NetPayable will be the total to settle (can be overridden by settlement)
	paySlip.NetPayable = totalToSettle

	// Closing balance depends on how much we actually pay
	// If we pay everything (full settlement), closing = 0
	// If we pay custom amount, closing = totalToSettle - customPayment
	// For now, assume full settlement if totalToSettle > 0
	if totalToSettle > 0 {
		paySlip.ClosingBalance = 0 // Assuming full settlement
	} else {
		paySlip.ClosingBalance = totalToSettle // Debt remains
		paySlip.NetPayable = 0                 // Can't pay negative
	}

	// Save updated totals
	if err := ps.DB.Model(&paySlip).Updates(map[string]interface{}{
		"gross_income":      paySlip.GrossIncome,
		"total_expenses":    paySlip.TotalExpenses,
		"total_loans":       paySlip.TotalLoans,
		"deduction_amount":  paySlip.DeductionAmount,
		"total_adjustments": paySlip.TotalAdjustments,
		"net_payable":       paySlip.NetPayable,
		"closing_balance":   paySlip.ClosingBalance,
	}).Error; err != nil {
		return nil, err
	}

	return &paySlip, nil
}
