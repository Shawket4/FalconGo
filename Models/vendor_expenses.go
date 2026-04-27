package Models

import (
	"time"

	"gorm.io/gorm"
)

type Vendor struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Name      string         `json:"name" gorm:"not null;uniqueIndex"` // Added uniqueIndex constraint
	Contact   string         `json:"contact"`
	Notes     string         `json:"notes"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	// Relationships
	Transactions []VendorTransaction `json:"-" gorm:"foreignKey:VendorID"`
}

// Transaction represents a financial transaction with a vendor
type VendorTransaction struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	VendorID    uint           `json:"vendor_id" gorm:"not null;index"`
	Date        time.Time      `json:"date" gorm:"not null;index"` // Added index for date-based queries
	Description string         `json:"description" gorm:"not null"`
	Amount      float64        `json:"amount" gorm:"not null"` // Positive for credit (vendor provided), negative for debit (payment to vendor)
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`

	// Relationships
	Vendor Vendor `json:"-" gorm:"foreignKey:VendorID"`
}

// TransactionSummary represents analytics data for transactions
type TransactionSummary struct {
	TotalCredits float64 `json:"total_credits"`
	TotalDebits  float64 `json:"total_debits"`
	NetBalance   float64 `json:"net_balance"`
	VendorCount  int64   `json:"vendor_count"`
}
