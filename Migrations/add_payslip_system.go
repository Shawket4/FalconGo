// Migrations/add_payslip_system.go
package Migrations

import (
	"Falcon/Models"
	"fmt"

	"gorm.io/gorm"
)

func MigratePaySlipSystem(db *gorm.DB) error {
	fmt.Println("Running migration: Add PaySlip System")

	// Auto-migrate all pay slip related tables
	if err := db.AutoMigrate(
		&Models.PaySlip{},
		&Models.PaySlipIncome{},
		&Models.PaySlipExpense{},
		&Models.PaySlipDeduction{},
		&Models.PaySlipAdjustment{},
	); err != nil {
		return fmt.Errorf("failed to migrate pay slip tables: %w", err)
	}

	// Create composite index for entity lookup
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_payslip_entity 
		ON pay_slips (entity_type, entity_id, deleted_at)
	`).Error; err != nil {
		return fmt.Errorf("failed to create entity index: %w", err)
	}

	// Create index for period queries
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_payslip_period 
		ON pay_slips (period_start, period_end, deleted_at)
	`).Error; err != nil {
		return fmt.Errorf("failed to create period index: %w", err)
	}

	// Create index for status queries
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_payslip_status 
		ON pay_slips (status, deleted_at)
	`).Error; err != nil {
		return fmt.Errorf("failed to create status index: %w", err)
	}

	if err := db.AutoMigrate(&Models.PaySlipAudit{}); err != nil {
		return fmt.Errorf("failed to migrate audit table: %w", err)
	}

	fmt.Println("PaySlip system migration completed successfully")
	return nil
}

func RollbackPaySlipSystem(db *gorm.DB) error {
	fmt.Println("Rolling back: PaySlip System")

	if err := db.Migrator().DropTable(
		&Models.PaySlipAdjustment{},
		&Models.PaySlipDeduction{},
		&Models.PaySlipExpense{},
		&Models.PaySlipIncome{},
		&Models.PaySlip{},
	); err != nil {
		return err
	}

	return nil
}
