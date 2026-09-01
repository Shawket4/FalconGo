package Models

import (
	"errors"

	"gorm.io/gorm"
)

type Loan struct {
	gorm.Model
	DriverID   *uint   `json:"driver_id" gorm:"index"`
	EmployeeID *uint   `json:"employee_id" gorm:"index"`
	Amount     float64 `json:"amount" validate:"required,gt=0"`
	Method     string  `json:"method"`
	Date       string  `json:"date"`
	IsPaid     bool    `json:"is_paid" gorm:"not null;default:false"`

	// Kind distinguishes money that is arithmetically identical but means
	// different things:
	//
	//   advance - paid out ahead of earnings, recovered later. Every row that
	//             existed before this column was added is one of these.
	//   loan    - a genuine debt, repaid over time rather than against one period.
	//   salary  - part of the salary itself, recorded here for visibility only.
	//
	// Defaulted to 'advance' so historical rows keep their existing meaning
	// without a backfill pass being required for correctness.
	Kind        string `json:"kind" gorm:"type:varchar(16);not null;default:'advance';index"`
	Description string `json:"description" gorm:"type:text"` // Optional note about the loan

	// Relationships
	Driver   *Driver   `json:"driver,omitempty" gorm:"foreignKey:DriverID"`
	Employee *Employee `json:"employee,omitempty" gorm:"foreignKey:EmployeeID"`
}

func (Loan) TableName() string {
	return "loans"
}

// Validation: Ensure either DriverID or EmployeeID is set, not both
func (l *Loan) Validate() error {
	if l.DriverID == nil && l.EmployeeID == nil {
		return errors.New("loan must be associated with either a driver or an employee")
	}
	if l.DriverID != nil && l.EmployeeID != nil {
		return errors.New("loan cannot be associated with both driver and employee")
	}
	return nil
}

// BeforeSave defaults an unset kind to "advance".
//
// The database CHECK only accepts advance/loan/salary, and Go's zero value for
// a string is "" -- so any caller that omits the field (every existing client
// does) would otherwise fail the constraint. Defaulting here covers every write
// path rather than each handler remembering to.
func (l *Loan) BeforeSave(tx *gorm.DB) error {
	switch l.Kind {
	case "advance", "loan", "salary":
	default:
		l.Kind = "advance"
	}
	return nil
}
