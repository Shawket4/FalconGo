package services

import (
	"Falcon/Models"
	"errors"
	"time"

	"gorm.io/gorm"
)

type PaySlipFinalizationService struct {
	DB *gorm.DB
}

// FinalizePaySlip finalizes a pay slip and executes all side effects
func (pfs *PaySlipFinalizationService) FinalizePaySlip(paySlipID uint, approvedBy uint) error {
	// Start transaction
	tx := pfs.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Load pay slip with all items
	var paySlip Models.PaySlip
	if err := tx.
		Preload("DeductionItems").
		Preload("ExpenseItems").
		First(&paySlip, paySlipID).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Validate status
	if paySlip.Status != "draft" {
		tx.Rollback()
		return errors.New("can only finalize draft pay slips")
	}

	// 1. Mark all linked loans as paid
	for _, deduction := range paySlip.DeductionItems {
		if deduction.OriginalLoanID != nil {
			if err := tx.Model(&Models.Loan{}).
				Where("id = ?", *deduction.OriginalLoanID).
				Update("is_paid", true).Error; err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	// 2. Mark all linked expenses as paid
	for _, expense := range paySlip.ExpenseItems {
		if expense.OriginalExpenseID != nil {
			if err := tx.Model(&Models.Expense{}).
				Where("id = ?", *expense.OriginalExpenseID).
				Update("is_paid", true).Error; err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	// 3. Update credit balance
	var creditBalance Models.CreditBalance
	if err := tx.Where("entity_type = ? AND entity_id = ?", paySlip.EntityType, paySlip.EntityID).
		First(&creditBalance).Error; err != nil {
		tx.Rollback()
		return err
	}

	balanceBefore := creditBalance.Balance
	balanceAfter := paySlip.ClosingBalance

	creditBalance.Balance = balanceAfter
	creditBalance.LastUpdated = time.Now()

	if err := tx.Save(&creditBalance).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 4. Create credit transaction for loans not deducted
	loansNotDeducted := paySlip.TotalLoans - paySlip.DeductionAmount
	if loansNotDeducted != 0 {
		transaction := Models.CreditTransaction{
			EntityType:      paySlip.EntityType,
			EntityID:        paySlip.EntityID,
			PaySlipID:       &paySlip.ID,
			TransactionType: "loan_carried_forward",
			Amount:          -loansNotDeducted, // Negative = increases debt
			BalanceBefore:   balanceBefore,
			BalanceAfter:    balanceAfter,
			Description:     "Loans carried forward from pay slip",
			CreatedBy:       approvedBy,
			CreatedAt:       time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// 5. Create credit transaction for salary deduction
	if paySlip.DeductionAmount > 0 {
		transaction := Models.CreditTransaction{
			EntityType:      paySlip.EntityType,
			EntityID:        paySlip.EntityID,
			PaySlipID:       &paySlip.ID,
			TransactionType: "salary_deduction",
			Amount:          paySlip.DeductionAmount, // Positive = reduces debt
			BalanceBefore:   balanceBefore,
			BalanceAfter:    balanceAfter,
			Description:     "Loan deduction from pay slip",
			CreatedBy:       approvedBy,
			CreatedAt:       time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// 6. Update pay slip status
	now := time.Now()
	paySlip.Status = "finalized"
	paySlip.ApprovedBy = &approvedBy
	paySlip.ApprovedAt = &now

	if err := tx.Save(&paySlip).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return err
	}

	return nil
}

// MarkAsPaid marks a finalized pay slip as paid
func (pfs *PaySlipFinalizationService) MarkAsPaid(paySlipID uint, paymentMethod, paymentRef string) error {
	var paySlip Models.PaySlip
	if err := pfs.DB.First(&paySlip, paySlipID).Error; err != nil {
		return err
	}

	if paySlip.Status != "finalized" {
		return errors.New("can only mark finalized pay slips as paid")
	}

	now := time.Now()
	paySlip.Status = "paid"
	paySlip.PaidAt = &now
	paySlip.PaymentMethod = paymentMethod
	paySlip.PaymentRef = paymentRef

	if err := pfs.DB.Save(&paySlip).Error; err != nil {
		return err
	}

	return nil
}
