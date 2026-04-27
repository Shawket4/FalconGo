// Migrations/add_employee_and_credit_system.go
package Migrations

import (
	"Falcon/Models"
	"fmt"
	"time"

	"gorm.io/gorm"
)

func MigrateEmployeeAndCreditSystem(db *gorm.DB) error {
	fmt.Println("Running migration: Add Employee and Credit Balance System")

	// Auto-migrate new tables
	if err := db.AutoMigrate(
		&Models.Employee{},
		&Models.CreditBalance{},
		&Models.CreditTransaction{},
	); err != nil {
		return fmt.Errorf("failed to migrate employee and credit tables: %w", err)
	}

	// Add EmployeeID column to loans table
	if !db.Migrator().HasColumn(&Models.Loan{}, "employee_id") {
		if err := db.Migrator().AddColumn(&Models.Loan{}, "EmployeeID"); err != nil {
			return fmt.Errorf("failed to add employee_id to loans: %w", err)
		}
	}

	// Add Description column to loans table if not exists
	if !db.Migrator().HasColumn(&Models.Loan{}, "description") {
		if err := db.Migrator().AddColumn(&Models.Loan{}, "Description"); err != nil {
			return fmt.Errorf("failed to add description to loans: %w", err)
		}
	}

	// Create unique index on entity_type and entity_id for credit_balances
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_entity_unique 
		ON credit_balances (entity_type, entity_id) 
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		return fmt.Errorf("failed to create unique index: %w", err)
	}

	// Initialize credit balances for all existing drivers with 0 balance
	if err := initializeDriverBalances(db); err != nil {
		return fmt.Errorf("failed to initialize driver balances: %w", err)
	}

	fmt.Println("Migration completed successfully")
	return nil
}

func initializeDriverBalances(db *gorm.DB) error {
	var drivers []Models.Driver
	if err := db.Find(&drivers).Error; err != nil {
		return err
	}

	now := time.Now()

	for _, driver := range drivers {
		// Check if balance already exists
		var existingBalance Models.CreditBalance
		result := db.Where("entity_type = ? AND entity_id = ?", "driver", driver.ID).First(&existingBalance)

		if result.Error == gorm.ErrRecordNotFound {
			// Create new balance with 0
			balance := Models.CreditBalance{
				EntityType:  "driver",
				EntityID:    driver.ID,
				Balance:     0,
				LastUpdated: now,
			}

			if err := db.Create(&balance).Error; err != nil {
				return fmt.Errorf("failed to create balance for driver %d: %w", driver.ID, err)
			}

			// Create initial transaction record
			transaction := Models.CreditTransaction{
				EntityType:      "driver",
				EntityID:        driver.ID,
				TransactionType: "initial_balance",
				Amount:          0,
				BalanceBefore:   0,
				BalanceAfter:    0,
				Description:     "Initial balance set to 0",
				CreatedBy:       0, // System
				CreatedAt:       now,
			}

			if err := db.Create(&transaction).Error; err != nil {
				return fmt.Errorf("failed to create initial transaction for driver %d: %w", driver.ID, err)
			}
		}
	}

	return nil
}

// Rollback function if needed
func RollbackEmployeeAndCreditSystem(db *gorm.DB) error {
	fmt.Println("Rolling back: Employee and Credit Balance System")

	if err := db.Migrator().DropTable(
		&Models.CreditTransaction{},
		&Models.CreditBalance{},
		&Models.Employee{},
	); err != nil {
		return err
	}

	if db.Migrator().HasColumn(&Models.Loan{}, "employee_id") {
		if err := db.Migrator().DropColumn(&Models.Loan{}, "EmployeeID"); err != nil {
			return err
		}
	}

	if db.Migrator().HasColumn(&Models.Loan{}, "description") {
		if err := db.Migrator().DropColumn(&Models.Loan{}, "Description"); err != nil {
			return err
		}
	}

	return nil
}
