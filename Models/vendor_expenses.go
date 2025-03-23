package Models

import (
	"gorm.io/gorm"
)

type Vendor struct {
	gorm.Model
	Name     string          `json:"name" gorm:"not null;uniqueIndex"`
	Expenses []VendorExpense `json:"expenses,omitempty" gorm:"foreignKey:VendorID"`
}

// Expense represents an expense associated with a vendor
type VendorExpense struct {
	gorm.Model
	VendorID    uint    `json:"vendor_id" gorm:"not null;index"`
	Description string  `json:"description" gorm:"not null"`
	Price       float64 `json:"price" gorm:"not null"`
}
