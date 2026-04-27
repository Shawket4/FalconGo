// Controllers/PaySlipStatsController.go
package Controllers

import (
	"Falcon/Models"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type PaySlipStatsController struct {
	DB *gorm.DB
}

type DriverStatsResponse struct {
	TripCount      int     `json:"trip_count"`      // Number of parent trips
	WorkingDays    int     `json:"working_days"`    // Unique days with trips
	TotalRevenue   float64 `json:"total_revenue"`   // Sum of all trip revenues
	TotalMileage   float64 `json:"total_mileage"`   // Sum of all trip mileages
	UnpaidLoans    float64 `json:"unpaid_loans"`    // Total unpaid loans in period
	UnpaidExpenses float64 `json:"unpaid_expenses"` // Total unpaid expenses in period
	CurrentBalance float64 `json:"current_balance"` // Current credit balance
}

type EmployeeStatsResponse struct {
	WorkingDays    int     `json:"working_days"`    // Days in period (for garage employees)
	UnpaidLoans    float64 `json:"unpaid_loans"`    // Total unpaid loans in period
	CurrentBalance float64 `json:"current_balance"` // Current credit balance
}

// GetDriverStats returns statistics for a driver in a period
func (psc *PaySlipStatsController) GetDriverStats(c *fiber.Ctx) error {
	driverID, err := strconv.Atoi(c.Query("driver_id"))
	if err != nil || driverID == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "driver_id is required",
		})
	}

	periodStart, err := time.Parse(time.RFC3339, c.Query("period_start"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "period_start is required (RFC3339 format)",
		})
	}

	periodEnd, err := time.Parse(time.RFC3339, c.Query("period_end"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "period_end is required (RFC3339 format)",
		})
	}

	stats := DriverStatsResponse{}

	// 1. Count parent trips in period
	var tripCount int64
	psc.DB.Model(&Models.ParentTrip{}).
		Where("driver_id = ?", driverID).
		Where("date >= ? AND date <= ?",
			periodStart.Format("2006-01-02"),
			periodEnd.Format("2006-01-02")).
		Count(&tripCount)
	stats.TripCount = int(tripCount)

	// 2. Count unique working days and sum revenue/mileage
	var parentTrips []Models.ParentTrip
	psc.DB.Preload("ContainerTrips").
		Where("driver_id = ?", driverID).
		Where("date >= ? AND date <= ?",
			periodStart.Format("2006-01-02"),
			periodEnd.Format("2006-01-02")).
		Find(&parentTrips)

	uniqueDays := make(map[string]bool)
	var totalRevenue, totalMileage float64

	for _, pt := range parentTrips {
		// Count unique days
		uniqueDays[pt.Date] = true

		// Sum revenue from container trips
		for _, ct := range pt.ContainerTrips {
			totalRevenue += ct.Revenue
			totalMileage += ct.Mileage
		}
	}

	stats.WorkingDays = len(uniqueDays)
	stats.TotalRevenue = totalRevenue
	stats.TotalMileage = totalMileage

	// 3. Sum unpaid loans in period
	var unpaidLoans float64
	psc.DB.Model(&Models.Loan{}).
		Where("driver_id = ?", driverID).
		Where("is_paid = ?", false).
		Where("date >= ? AND date <= ?",
			periodStart.Format("2006-01-02"),
			periodEnd.Format("2006-01-02")).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&unpaidLoans)
	stats.UnpaidLoans = unpaidLoans

	// 4. Sum unpaid expenses in period
	var unpaidExpenses float64
	psc.DB.Model(&Models.Expense{}).
		Where("driver_id = ?", driverID).
		Where("is_paid = ?", false).
		Where("date >= ? AND date <= ?",
			periodStart.Format("2006-01-02"),
			periodEnd.Format("2006-01-02")).
		Select("COALESCE(SUM(cost), 0)").
		Scan(&unpaidExpenses)
	stats.UnpaidExpenses = unpaidExpenses

	// 5. Get current balance
	var creditBalance Models.CreditBalance
	if err := psc.DB.Where("entity_type = ? AND entity_id = ?", "driver", driverID).
		First(&creditBalance).Error; err == nil {
		stats.CurrentBalance = creditBalance.Balance
	}

	return c.JSON(stats)
}

// GetEmployeeStats returns statistics for an employee in a period
func (psc *PaySlipStatsController) GetEmployeeStats(c *fiber.Ctx) error {
	employeeID, err := strconv.Atoi(c.Query("employee_id"))
	if err != nil || employeeID == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "employee_id is required",
		})
	}

	periodStart, err := time.Parse(time.RFC3339, c.Query("period_start"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "period_start is required (RFC3339 format)",
		})
	}

	periodEnd, err := time.Parse(time.RFC3339, c.Query("period_end"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "period_end is required (RFC3339 format)",
		})
	}

	stats := EmployeeStatsResponse{}

	// 1. Calculate working days (calendar days in period)
	// For garage employees who are paid per day, this is useful
	daysDiff := periodEnd.Sub(periodStart).Hours() / 24
	stats.WorkingDays = int(daysDiff) + 1 // Include both start and end day

	// 2. Sum unpaid loans in period
	var unpaidLoans float64
	psc.DB.Model(&Models.Loan{}).
		Where("employee_id = ?", employeeID).
		Where("is_paid = ?", false).
		Where("date >= ? AND date <= ?",
			periodStart.Format("2006-01-02"),
			periodEnd.Format("2006-01-02")).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&unpaidLoans)
	stats.UnpaidLoans = unpaidLoans

	// 3. Get current balance
	var creditBalance Models.CreditBalance
	if err := psc.DB.Where("entity_type = ? AND entity_id = ?", "employee", employeeID).
		First(&creditBalance).Error; err == nil {
		stats.CurrentBalance = creditBalance.Balance
	}

	return c.JSON(stats)
}
