package services

import (
	"Falcon/Models"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jung-kurt/gofpdf"
	"gorm.io/gorm"
)

type PaySlipPDFService struct {
	DB *gorm.DB
}

// GeneratePDF creates a PDF for a pay slip
func (ps *PaySlipPDFService) GeneratePDF(paySlipID uint) (string, error) {
	// Load pay slip with all items
	var paySlip Models.PaySlip
	if err := ps.DB.
		Preload("IncomeItems").
		Preload("ExpenseItems").
		Preload("DeductionItems").
		Preload("AdjustmentItems").
		First(&paySlip, paySlipID).Error; err != nil {
		return "", err
	}

	// Load entity details
	var entityName string
	var entityMobile string

	if paySlip.EntityType == "driver" {
		var driver Models.Driver
		ps.DB.First(&driver, paySlip.EntityID)
		entityName = driver.Name
		entityMobile = driver.MobileNumber
	} else {
		var employee Models.Employee
		ps.DB.First(&employee, paySlip.EntityID)
		entityName = employee.Name
		entityMobile = employee.MobileNumber
	}

	// Create PDF
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	// Add Arabic font support (if needed)
	// pdf.AddUTF8Font("Arial", "", "arial.ttf")

	// Header
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(0, 10, "Apex Transport - Pay Slip")
	pdf.Ln(12)

	// Pay slip info
	pdf.SetFont("Arial", "", 12)
	pdf.Cell(50, 7, "Pay Slip ID:")
	pdf.Cell(0, 7, fmt.Sprintf("#%d", paySlip.ID))
	pdf.Ln(7)

	pdf.Cell(50, 7, fmt.Sprintf("%s Name:", capitalize(paySlip.EntityType)))
	pdf.Cell(0, 7, entityName)
	pdf.Ln(7)

	pdf.Cell(50, 7, "Mobile:")
	pdf.Cell(0, 7, entityMobile)
	pdf.Ln(7)

	pdf.Cell(50, 7, "Period:")
	pdf.Cell(0, 7, fmt.Sprintf("%s to %s",
		paySlip.PeriodStart.Format("2006-01-02"),
		paySlip.PeriodEnd.Format("2006-01-02")))
	pdf.Ln(7)

	pdf.Cell(50, 7, "Status:")
	pdf.Cell(0, 7, capitalize(paySlip.Status))
	pdf.Ln(12)

	// Income section
	if len(paySlip.IncomeItems) > 0 {
		pdf.SetFont("Arial", "B", 14)
		pdf.Cell(0, 10, "Income")
		pdf.Ln(10)

		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(80, 7, "Description")
		pdf.Cell(30, 7, "Quantity")
		pdf.Cell(30, 7, "Rate")
		pdf.Cell(40, 7, "Amount")
		pdf.Ln(7)

		pdf.SetFont("Arial", "", 10)
		for _, item := range paySlip.IncomeItems {
			pdf.Cell(80, 6, item.Description)
			pdf.Cell(30, 6, fmt.Sprintf("%.2f", item.Quantity))
			pdf.Cell(30, 6, fmt.Sprintf("%.2f", item.Rate))
			pdf.Cell(40, 6, fmt.Sprintf("%.2f EGP", item.Amount))
			pdf.Ln(6)
		}
		pdf.Ln(5)
	}

	// Expenses section
	if len(paySlip.ExpenseItems) > 0 {
		pdf.SetFont("Arial", "B", 14)
		pdf.Cell(0, 10, "Expenses (Reimbursements)")
		pdf.Ln(10)

		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(100, 7, "Category")
		pdf.Cell(40, 7, "Amount")
		pdf.Cell(50, 7, "Note")
		pdf.Ln(7)

		pdf.SetFont("Arial", "", 10)
		for _, item := range paySlip.ExpenseItems {
			pdf.Cell(100, 6, item.Category)
			pdf.Cell(40, 6, fmt.Sprintf("%.2f EGP", item.Amount))
			pdf.Cell(50, 6, truncate(item.Note, 25))
			pdf.Ln(6)
		}
		pdf.Ln(5)
	}

	// Deductions section
	if len(paySlip.DeductionItems) > 0 {
		pdf.SetFont("Arial", "B", 14)
		pdf.Cell(0, 10, "Deductions (Loans)")
		pdf.Ln(10)

		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(60, 7, "Description")
		pdf.Cell(35, 7, "Total")
		pdf.Cell(35, 7, "Deducted")
		pdf.Cell(35, 7, "Remaining")
		pdf.Ln(7)

		pdf.SetFont("Arial", "", 10)
		for _, item := range paySlip.DeductionItems {
			pdf.Cell(60, 6, item.Description)
			pdf.Cell(35, 6, fmt.Sprintf("%.2f", item.TotalAmount))
			pdf.Cell(35, 6, fmt.Sprintf("%.2f", item.DeductedAmount))
			pdf.Cell(35, 6, fmt.Sprintf("%.2f", item.CarriedForward))
			pdf.Ln(6)
		}
		pdf.Ln(5)
	}

	// Adjustments section
	if len(paySlip.AdjustmentItems) > 0 {
		pdf.SetFont("Arial", "B", 14)
		pdf.Cell(0, 10, "Adjustments")
		pdf.Ln(10)

		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(100, 7, "Type")
		pdf.Cell(40, 7, "Amount")
		pdf.Cell(50, 7, "Note")
		pdf.Ln(7)

		pdf.SetFont("Arial", "", 10)
		for _, item := range paySlip.AdjustmentItems {
			pdf.Cell(100, 6, item.Type)
			pdf.Cell(40, 6, fmt.Sprintf("%.2f EGP", item.Amount))
			pdf.Cell(50, 6, truncate(item.Note, 25))
			pdf.Ln(6)
		}
		pdf.Ln(5)
	}

	// Summary section
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, "Summary")
	pdf.Ln(10)

	pdf.SetFont("Arial", "", 11)
	pdf.Cell(100, 7, "Gross Income:")
	pdf.Cell(0, 7, fmt.Sprintf("%.2f EGP", paySlip.GrossIncome))
	pdf.Ln(7)

	pdf.Cell(100, 7, "Expenses Reimbursed:")
	pdf.Cell(0, 7, fmt.Sprintf("+ %.2f EGP", paySlip.TotalExpenses))
	pdf.Ln(7)

	pdf.Cell(100, 7, "Adjustments:")
	pdf.Cell(0, 7, fmt.Sprintf("%.2f EGP", paySlip.TotalAdjustments))
	pdf.Ln(7)

	pdf.Cell(100, 7, "Loans Deducted:")
	pdf.Cell(0, 7, fmt.Sprintf("- %.2f EGP", paySlip.DeductionAmount))
	pdf.Ln(7)

	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(100, 10, "Net Payable:")
	pdf.Cell(0, 10, fmt.Sprintf("%.2f EGP", paySlip.NetPayable))
	pdf.Ln(12)

	// Balance section
	pdf.SetFont("Arial", "", 11)
	pdf.Cell(100, 7, "Opening Balance:")
	pdf.Cell(0, 7, fmt.Sprintf("%.2f EGP", paySlip.OpeningBalance))
	pdf.Ln(7)

	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(100, 7, "Closing Balance:")
	pdf.Cell(0, 7, fmt.Sprintf("%.2f EGP", paySlip.ClosingBalance))
	pdf.Ln(12)

	// Footer
	pdf.SetFont("Arial", "I", 9)
	pdf.Cell(0, 5, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")))

	// Create directory if not exists
	pdfDir := "./storage/payslips"
	if err := os.MkdirAll(pdfDir, 0755); err != nil {
		return "", err
	}

	// Generate filename
	filename := fmt.Sprintf("payslip_%d_%s.pdf", paySlip.ID, time.Now().Format("20060102_150405"))
	filepath := filepath.Join(pdfDir, filename)

	// Save PDF
	if err := pdf.OutputFileAndClose(filepath); err != nil {
		return "", err
	}

	return filepath, nil
}

// Helper functions
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-32) + s[1:]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
