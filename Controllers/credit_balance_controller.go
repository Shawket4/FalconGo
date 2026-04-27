// Controllers/CreditBalanceController.go
package Controllers

import (
	"Falcon/Models"
	"Falcon/utils"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type CreditBalanceController struct {
	DB *gorm.DB
}

// GetCreditBalance returns the current credit balance for a driver or employee
func (cbc *CreditBalanceController) GetCreditBalance(c *fiber.Ctx) error {
	entityType := c.Params("entity_type")
	entityID, err := strconv.Atoi(c.Params("entity_id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid entity ID",
		})
	}

	if entityType != "driver" && entityType != "employee" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Entity type must be 'driver' or 'employee'",
		})
	}

	var balance Models.CreditBalance
	if err := cbc.DB.Where("entity_type = ? AND entity_id = ?", entityType, entityID).First(&balance).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Credit balance not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch credit balance",
		})
	}

	// Load entity details
	if entityType == "driver" {
		var driver Models.Driver
		cbc.DB.First(&driver, entityID)
		balance.Driver = &driver
	} else {
		var employee Models.Employee
		cbc.DB.First(&employee, entityID)
		balance.Employee = &employee
	}

	return c.JSON(balance)
}

// GetCreditTransactions returns transaction history for a driver or employee
// Controllers/CreditBalanceController.go - UPDATE GetCreditTransactions
func (cbc *CreditBalanceController) GetCreditTransactions(c *fiber.Ctx) error {
	entityType := c.Params("entity_type")
	entityID, err := strconv.Atoi(c.Params("entity_id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid entity ID",
		})
	}

	if entityType != "driver" && entityType != "employee" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Entity type must be 'driver' or 'employee'",
		})
	}

	var transactions []Models.CreditTransaction
	query := cbc.DB.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("created_at DESC")

	// Pagination
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	query = query.Limit(limit).Offset(offset)

	if err := query.Find(&transactions).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch transactions",
		})
	}

	// Manually load users
	for i := range transactions {
		if transactions[i].CreatedBy > 0 {
			var user Models.User
			if cbc.DB.First(&user, transactions[i].CreatedBy).Error == nil {
				transactions[i].User = &user
			}
		}
	}

	return c.JSON(transactions)
}

// ManualAdjustment allows manual correction of credit balance
func (cbc *CreditBalanceController) ManualAdjustment(c *fiber.Ctx) error {
	var request struct {
		EntityType  string  `json:"entity_type" validate:"required,oneof=driver employee"`
		EntityID    uint    `json:"entity_id" validate:"required"`
		Amount      float64 `json:"amount" validate:"required"` // Positive = reduce debt/add credit, Negative = add debt
		Description string  `json:"description" validate:"required"`
		CreatedBy   uint    `json:"created_by"` // TODO: Get from auth context
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}
	userID := utils.GetUserIDFromContext(c)
	// Start transaction
	tx := cbc.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get current balance
	var balance Models.CreditBalance
	if err := tx.Where("entity_type = ? AND entity_id = ?", request.EntityType, request.EntityID).First(&balance).Error; err != nil {
		tx.Rollback()
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Credit balance not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch credit balance",
		})
	}

	balanceBefore := balance.Balance
	balanceAfter := balanceBefore + request.Amount

	// Update balance
	balance.Balance = balanceAfter
	balance.LastUpdated = time.Now()

	if err := tx.Save(&balance).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update balance",
		})
	}

	// Create transaction record
	transaction := Models.CreditTransaction{
		EntityType:      request.EntityType,
		EntityID:        request.EntityID,
		TransactionType: "manual_correction",
		Amount:          request.Amount,
		BalanceBefore:   balanceBefore,
		BalanceAfter:    balanceAfter,
		Description:     request.Description,
		CreatedBy:       userID,
		CreatedAt:       time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create transaction record",
		})
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to commit transaction",
		})
	}

	return c.JSON(fiber.Map{
		"message":        "Balance adjusted successfully",
		"balance_before": balanceBefore,
		"balance_after":  balanceAfter,
		"adjustment":     request.Amount,
		"transaction":    transaction,
	})
}
