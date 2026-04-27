// Models/PaySlip.go
package Models

import (
	"time"

	"gorm.io/gorm"
)

type PaySlip struct {
	gorm.Model

	// Entity Reference (polymorphic - either Driver or Employee)
	EntityType string `json:"entity_type" gorm:"type:varchar(20);index"` // "driver" or "employee"
	EntityID   uint   `json:"entity_id" gorm:"index"`

	// Period
	PeriodStart time.Time `json:"period_start" gorm:"not null;index"`
	PeriodEnd   time.Time `json:"period_end" gorm:"not null;index"`

	// Calculated Totals (auto-computed from line items)
	GrossIncome      float64 `json:"gross_income" gorm:"default:0"`      // Sum of all income items
	TotalExpenses    float64 `json:"total_expenses" gorm:"default:0"`    // Sum of expense reimbursements
	TotalLoans       float64 `json:"total_loans" gorm:"default:0"`       // Sum of all loans in period
	DeductionAmount  float64 `json:"deduction_amount" gorm:"default:0"`  // Amount actually deducted from loans
	TotalAdjustments float64 `json:"total_adjustments" gorm:"default:0"` // Net adjustments (positive - negative)
	NetPayable       float64 `json:"net_payable" gorm:"default:0"`       // Final cash amount to pay

	// Credit Balance Tracking
	OpeningBalance float64 `json:"opening_balance" gorm:"default:0"` // Balance at period start
	ClosingBalance float64 `json:"closing_balance" gorm:"default:0"` // Balance at period end

	// Status & Workflow
	Status        string     `json:"status" gorm:"default:'draft';index"` // draft, finalized, paid
	CreatedBy     uint       `json:"created_by"`
	ApprovedBy    *uint      `json:"approved_by"`
	ApprovedAt    *time.Time `json:"approved_at"`
	PaidAt        *time.Time `json:"paid_at"`
	PaymentMethod string     `json:"payment_method"` // cash, bank_transfer, check, etc.
	PaymentRef    string     `json:"payment_ref"`    // check number, transfer ID, etc.

	// PDF Generation
	PDFPath string `json:"pdf_path"`

	// Notes
	Notes string `json:"notes" gorm:"type:text"`

	// Relationships (virtual - populated on fetch)
	Driver   *Driver   `json:"driver,omitempty" gorm:"-"`
	Employee *Employee `json:"employee,omitempty" gorm:"-"`
	Creator  *User     `json:"creator,omitempty" gorm:"-"`
	Approver *User     `json:"approver,omitempty" gorm:"-"`

	// Line Items (cascade delete)
	IncomeItems     []PaySlipIncome     `json:"income_items,omitempty" gorm:"foreignKey:PaySlipID;constraint:OnDelete:CASCADE"`
	ExpenseItems    []PaySlipExpense    `json:"expense_items,omitempty" gorm:"foreignKey:PaySlipID;constraint:OnDelete:CASCADE"`
	DeductionItems  []PaySlipDeduction  `json:"deduction_items,omitempty" gorm:"foreignKey:PaySlipID;constraint:OnDelete:CASCADE"`
	AdjustmentItems []PaySlipAdjustment `json:"adjustment_items,omitempty" gorm:"foreignKey:PaySlipID;constraint:OnDelete:CASCADE"`
}

func (PaySlip) TableName() string {
	return "pay_slips"
}

// Helper to get entity name
func (ps *PaySlip) GetEntityName() string {
	if ps.Driver != nil {
		return ps.Driver.Name
	}
	if ps.Employee != nil {
		return ps.Employee.Name
	}
	return ""
}

// Helper to check if editable
func (ps *PaySlip) IsEditable() bool {
	return ps.Status == "draft"
}

type PaySlipIncome struct {
	gorm.Model
	PaySlipID    uint    `json:"pay_slip_id" gorm:"index;not null"`
	Type         string  `json:"type" gorm:"type:varchar(50)"` // "trip", "day", "maintenance", "fixed"
	Description  string  `json:"description" gorm:"type:varchar(255)"`
	Quantity     float64 `json:"quantity" gorm:"default:0"`           // Number of trips/days/units
	Rate         float64 `json:"rate" gorm:"default:0"`               // Amount per unit
	Amount       float64 `json:"amount" gorm:"default:0"`             // quantity * rate (auto-calculated)
	IsAPIFetched bool    `json:"is_api_fetched" gorm:"default:false"` // Was quantity from API?
	DisplayOrder int     `json:"display_order" gorm:"default:0"`      // For UI sorting

	// Relationship
	PaySlip *PaySlip `json:"pay_slip,omitempty" gorm:"foreignKey:PaySlipID"`
}

func (PaySlipIncome) TableName() string {
	return "pay_slip_incomes"
}

// Auto-calculate amount before save
func (psi *PaySlipIncome) BeforeSave(tx *gorm.DB) error {
	psi.Amount = psi.Quantity * psi.Rate
	return nil
}

type PaySlipExpense struct {
	gorm.Model
	PaySlipID         uint    `json:"pay_slip_id" gorm:"index;not null"`
	Category          string  `json:"category" gorm:"type:varchar(50)"`
	Amount            float64 `json:"amount" gorm:"default:0"`
	ReceiptRef        string  `json:"receipt_ref" gorm:"type:varchar(100)"`
	Note              string  `json:"note" gorm:"type:text"`
	OriginalExpenseID *uint   `json:"original_expense_id" gorm:"index"`
	DisplayOrder      int     `json:"display_order" gorm:"default:0"`

	// Relationships
	PaySlip         *PaySlip `json:"pay_slip,omitempty" gorm:"foreignKey:PaySlipID"`
	OriginalExpense *Expense `json:"original_expense,omitempty" gorm:"-"` // ✅ FIX: Virtual field
}

func (PaySlipExpense) TableName() string {
	return "pay_slip_expenses"
}

type PaySlipDeduction struct {
	gorm.Model
	PaySlipID      uint    `json:"pay_slip_id" gorm:"index;not null"`
	Type           string  `json:"type" gorm:"type:varchar(50)"`
	Description    string  `json:"description" gorm:"type:varchar(255)"`
	TotalAmount    float64 `json:"total_amount" gorm:"default:0"`
	DeductedAmount float64 `json:"deducted_amount" gorm:"default:0"`
	CarriedForward float64 `json:"carried_forward" gorm:"default:0"`
	OriginalLoanID *uint   `json:"original_loan_id" gorm:"index"`
	Reference      string  `json:"reference" gorm:"type:varchar(100)"`
	DisplayOrder   int     `json:"display_order" gorm:"default:0"`

	// Relationships
	PaySlip      *PaySlip `json:"pay_slip,omitempty" gorm:"foreignKey:PaySlipID"`
	OriginalLoan *Loan    `json:"original_loan,omitempty" gorm:"-"` // ✅ FIX: Virtual field
}

func (PaySlipDeduction) TableName() string {
	return "pay_slip_deductions"
}

// Auto-calculate carried forward before save
func (psd *PaySlipDeduction) BeforeSave(tx *gorm.DB) error {
	psd.CarriedForward = psd.TotalAmount - psd.DeductedAmount
	return nil
}

type PaySlipAdjustment struct {
	gorm.Model
	PaySlipID    uint    `json:"pay_slip_id" gorm:"index;not null"`
	Type         string  `json:"type" gorm:"type:varchar(50)"`   // "bonus", "penalty", "correction", "fuel_incentive", "violation", "other"
	Amount       float64 `json:"amount" gorm:"default:0"`        // Can be positive or negative
	Note         string  `json:"note" gorm:"type:text;not null"` // Required explanation
	DisplayOrder int     `json:"display_order" gorm:"default:0"`

	// Relationship
	PaySlip *PaySlip `json:"pay_slip,omitempty" gorm:"foreignKey:PaySlipID"`
}

func (PaySlipAdjustment) TableName() string {
	return "pay_slip_adjustments"
}

type PaySlipAudit struct {
	gorm.Model
	PaySlipID     uint   `json:"pay_slip_id" gorm:"index;not null"`
	Action        string `json:"action" gorm:"type:varchar(50)"`
	ChangedBy     uint   `json:"changed_by"`
	ChangeDetails string `json:"change_details" gorm:"type:text"`
	IPAddress     string `json:"ip_address"`
	UserAgent     string `json:"user_agent"`

	// Relationships
	PaySlip *PaySlip `json:"pay_slip,omitempty" gorm:"foreignKey:PaySlipID"`
	User    *User    `json:"user,omitempty" gorm:"-"`
}

func (PaySlipAudit) TableName() string {
	return "pay_slip_audits"
}
