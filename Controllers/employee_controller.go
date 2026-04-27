package Controllers

import (
	"Falcon/Models"
	"Falcon/utils"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type EmployeeController struct {
	DB *gorm.DB
}

// CreateEmployee creates a new employee and initializes their credit balance
func (ec *EmployeeController) CreateEmployee(c *fiber.Ctx) error {
	var employee Models.Employee

	if err := c.BodyParser(&employee); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	userID := utils.GetUserIDFromContext(c)

	// Validate employee type
	if employee.Type != "garage" && employee.Type != "office" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Employee type must be either 'garage' or 'office'",
		})
	}

	// Start transaction
	tx := ec.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Create employee
	if err := tx.Create(&employee).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create employee",
		})
	}

	// Create initial credit balance
	creditBalance := Models.CreditBalance{
		EntityType:  "employee",
		EntityID:    employee.ID,
		Balance:     0,
		LastUpdated: time.Now(),
	}

	if err := tx.Create(&creditBalance).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create credit balance",
		})
	}

	// Create initial transaction
	transaction := Models.CreditTransaction{
		EntityType:      "employee",
		EntityID:        employee.ID,
		TransactionType: "initial_balance",
		Amount:          0,
		BalanceBefore:   0,
		BalanceAfter:    0,
		Description:     "Initial balance set to 0",
		CreatedBy:       userID,
		CreatedAt:       time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create initial transaction",
		})
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to commit transaction",
		})
	}

	// Load the created employee with balance
	ec.DB.Preload("CreditBalance").First(&employee, employee.ID)

	return c.Status(http.StatusCreated).JSON(employee)
}

// GetAllEmployees returns all employees with their credit balances
func (ec *EmployeeController) GetAllEmployees(c *fiber.Ctx) error {
	var employees []Models.Employee

	query := ec.DB

	// Filter by type if provided
	employeeType := c.Query("type")
	if employeeType != "" {
		if employeeType != "garage" && employeeType != "office" {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid employee type",
			})
		}
		query = query.Where("type = ?", employeeType)
	}

	// Filter by active status
	isActive := c.Query("is_active")
	if isActive != "" {
		query = query.Where("is_active = ?", isActive == "true")
	}

	if err := query.Find(&employees).Error; err != nil {
		log.Println(err)
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch employees",
		})
	}

	// Manually load credit balances
	for i := range employees {
		var balance Models.CreditBalance
		err := ec.DB.Where("entity_type = ? AND entity_id = ?", "employee", employees[i].ID).First(&balance).Error
		if err == nil {
			employees[i].CreditBalance = &balance
		}
	}

	return c.JSON(employees)
}

// GetEmployee returns a single employee with balance and recent transactions
func (ec *EmployeeController) GetEmployee(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid employee ID",
		})
	}

	var employee Models.Employee
	if err := ec.DB.Preload("CreditBalance").Preload("Loans").First(&employee, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Employee not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch employee",
		})
	}

	// Get recent transactions
	var transactions []Models.CreditTransaction
	ec.DB.Where("entity_type = ? AND entity_id = ?", "employee", id).
		Order("created_at DESC").
		Limit(10).
		Find(&transactions)

	return c.JSON(fiber.Map{
		"employee":            employee,
		"recent_transactions": transactions,
	})
}

// UpdateEmployee updates employee information
func (ec *EmployeeController) UpdateEmployee(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid employee ID",
		})
	}

	var employee Models.Employee
	if err := ec.DB.First(&employee, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "Employee not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch employee",
		})
	}

	var updateData Models.Employee
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Validate type if being updated
	if updateData.Type != "" && updateData.Type != "garage" && updateData.Type != "office" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Employee type must be either 'garage' or 'office'",
		})
	}

	// Update fields
	if updateData.Name != "" {
		employee.Name = updateData.Name
	}
	if updateData.MobileNumber != "" {
		employee.MobileNumber = updateData.MobileNumber
	}
	if updateData.Type != "" {
		employee.Type = updateData.Type
	}
	employee.IsActive = updateData.IsActive

	if err := ec.DB.Save(&employee).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update employee",
		})
	}

	ec.DB.Preload("CreditBalance").First(&employee, id)

	return c.JSON(employee)
}

// DeleteEmployee soft deletes an employee
func (ec *EmployeeController) DeleteEmployee(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid employee ID",
		})
	}

	if err := ec.DB.Delete(&Models.Employee{}, id).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete employee",
		})
	}

	return c.JSON(fiber.Map{
		"message": "Employee deleted successfully",
	})
}
