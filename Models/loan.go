package Models

import (
	"errors"

	"gorm.io/gorm"
)

type Loan struct {
	gorm.Model
	DriverID    *uint   `json:"driver_id" gorm:"index"`
	EmployeeID  *uint   `json:"employee_id" gorm:"index"`
	Amount      float64 `json:"amount" validate:"required,gt=0"`
	Method      string  `json:"method"`
	Date        string  `json:"date"`
	IsPaid      bool    `json:"is_paid" gorm:"not null;default:false"`
	Description string  `json:"description" gorm:"type:text"` // Optional note about the loan

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
