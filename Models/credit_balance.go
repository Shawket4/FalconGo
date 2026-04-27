package Models

import (
	"time"

	"gorm.io/gorm"
)

type CreditBalance struct {
	gorm.Model
	EntityType  string    `json:"entity_type" gorm:"type:varchar(20);index:idx_entity"` // "driver" or "employee"
	EntityID    uint      `json:"entity_id" gorm:"index:idx_entity"`
	Balance     float64   `json:"balance" gorm:"default:0"` // Negative = owes company, Positive = company owes them
	LastUpdated time.Time `json:"last_updated" gorm:"autoUpdateTime"`

	// Virtual relationships (not stored, populated on fetch)
	Driver   *Driver   `json:"driver,omitempty" gorm:"-"`
	Employee *Employee `json:"employee,omitempty" gorm:"-"`
}

func (CreditBalance) TableName() string {
	return "credit_balances"
}

// Helper to get entity name
func (cb *CreditBalance) GetEntityName() string {
	if cb.Driver != nil {
		return cb.Driver.Name
	}
	if cb.Employee != nil {
		return cb.Employee.Name
	}
	return ""
}

type CreditTransaction struct {
	gorm.Model
	EntityType      string    `json:"entity_type" gorm:"type:varchar(20);index"`
	EntityID        uint      `json:"entity_id" gorm:"index:idx_entity"`
	PaySlipID       *uint     `json:"pay_slip_id" gorm:"index"`
	TransactionType string    `json:"transaction_type" gorm:"type:varchar(50)"`
	Amount          float64   `json:"amount"`
	BalanceBefore   float64   `json:"balance_before"`
	BalanceAfter    float64   `json:"balance_after"`
	Description     string    `json:"description" gorm:"type:text"`
	Reference       string    `json:"reference"`
	CreatedBy       uint      `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`

	// Relationships
	User *User `json:"user,omitempty" gorm:"-"` // ✅ ADD THIS
}

func (CreditTransaction) TableName() string {
	return "credit_transactions"
}
