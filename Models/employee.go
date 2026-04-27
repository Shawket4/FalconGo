package Models

import "gorm.io/gorm"

type Employee struct {
	gorm.Model
	Name         string `json:"name" validate:"required,min=3,max=100"`
	MobileNumber string `json:"mobile_number"`
	Type         string `json:"type" validate:"required,oneof=garage office"` // "garage" or "office"
	IsActive     bool   `json:"is_active" gorm:"default:true"`

	// Relationships
	Loans         []Loan         `json:"loans,omitempty" gorm:"foreignKey:EmployeeID"`
	CreditBalance *CreditBalance `json:"credit_balance,omitempty" gorm:"-"`
}

func (Employee) TableName() string {
	return "employees"
}
