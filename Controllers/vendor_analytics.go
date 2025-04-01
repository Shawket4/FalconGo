package Controllers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"Falcon/Models"
)

// AnalyticsController handles analytics-related API endpoints
type AnalyticsController struct {
	DB *gorm.DB
}

// NewAnalyticsController creates a new AnalyticsController
func NewAnalyticsController(db *gorm.DB) *AnalyticsController {
	return &AnalyticsController{DB: db}
}

// Summary returns overall financial summary
func (c *AnalyticsController) Summary(ctx *fiber.Ctx) error {
	var summary Models.TransactionSummary

	// Count total vendors (not deleted)
	c.DB.Model(&Models.Vendor{}).Count(&summary.VendorCount)

	// Calculate total credits (purchases from vendors)
	c.DB.Model(&Models.VendorTransaction{}).Where("amount > 0").Select("COALESCE(SUM(amount), 0)").Scan(&summary.TotalCredits)

	// Calculate total debits (payments to vendors)
	c.DB.Model(&Models.VendorTransaction{}).Where("amount < 0").Select("COALESCE(SUM(amount), 0)").Scan(&summary.TotalDebits)

	// Calculate net balance
	summary.NetBalance = summary.TotalCredits + summary.TotalDebits

	return ctx.JSON(summary)
}

// MonthlyTransactions returns transactions summed by month
func (c *AnalyticsController) MonthlyTransactions(ctx *fiber.Ctx) error {
	type MonthlyData struct {
		Month   string  `json:"month"`
		Credits float64 `json:"credits"`
		Debits  float64 `json:"debits"`
		Net     float64 `json:"net"`
	}

	// Get start date (12 months ago from today)
	endDate := time.Now()
	startDate := endDate.AddDate(-1, 0, 0)

	// Let's take a different approach - query all transactions in the date range
	// and process them in Go rather than trying to do complex date formatting in SQL
	var transactions []Models.VendorTransaction

	result := c.DB.Where("date BETWEEN ? AND ? AND deleted_at IS NULL",
		startDate, endDate).
		Find(&transactions)

	if result.Error != nil {
		return ctx.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "Failed to retrieve transactions"})
	}

	// Group transactions by month
	monthlySummary := make(map[string]*MonthlyData)

	// First, create entries for all 12 months (even if no data)
	for i := 0; i < 12; i++ {
		date := endDate.AddDate(0, -i, 0)
		monthKey := date.Format("2006-01")
		monthLabel := date.Format("Jan 2006")

		monthlySummary[monthKey] = &MonthlyData{
			Month:   monthLabel,
			Credits: 0,
			Debits:  0,
			Net:     0,
		}
	}

	// Process each transaction
	for _, txn := range transactions {
		// Get the year-month as a string key
		monthKey := txn.Date.Format("2006-01")

		if data, exists := monthlySummary[monthKey]; exists {
			// Add to credits or debits based on amount
			if txn.Amount > 0 {
				data.Credits += txn.Amount
			} else {
				data.Debits += txn.Amount // This will be negative
			}

			// Update the net balance
			data.Net = data.Credits + data.Debits
		}
	}

	// Convert map to slice for JSON response
	var response []MonthlyData
	for i := 0; i < 12; i++ {
		date := endDate.AddDate(0, -i, 0)
		monthKey := date.Format("2006-01")
		if data, exists := monthlySummary[monthKey]; exists {
			response = append(response, *data)
		}
	}

	// Reverse to get chronological order
	for i, j := 0, len(response)-1; i < j; i, j = i+1, j-1 {
		response[i], response[j] = response[j], response[i]
	}

	return ctx.JSON(response)
}

// TopVendors returns the top vendors by transaction volume
func (c *AnalyticsController) TopVendors(ctx *fiber.Ctx) error {
	type VendorSummary struct {
		ID       uint    `json:"id"`
		Name     string  `json:"name"`
		Credits  float64 `json:"credits"`
		Debits   float64 `json:"debits"`
		Net      float64 `json:"net"`
		TxnCount int     `json:"transaction_count"`
	}

	var results []VendorSummary

	c.DB.Raw(`
		SELECT 
			v.id,
			v.name,
			SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END) as credits,
			SUM(CASE WHEN t.amount < 0 THEN t.amount ELSE 0 END) as debits,
			SUM(t.amount) as net,
			COUNT(t.id) as txn_count
		FROM vendors v
		JOIN vendor_transactions t ON v.id = t.vendor_id
		WHERE v.deleted_at IS NULL
		AND t.deleted_at IS NULL
		GROUP BY v.id, v.name
		ORDER BY ABS(net) DESC
		LIMIT 5
	`).Scan(&results)

	return ctx.JSON(results)
}

// RecentActivity returns the most recent transactions
func (c *AnalyticsController) RecentActivity(ctx *fiber.Ctx) error {
	type RecentTransaction struct {
		ID          uint      `json:"id"`
		Date        time.Time `json:"date"`
		VendorName  string    `json:"vendor_name"`
		Description string    `json:"description"`
		Amount      float64   `json:"amount"`
	}

	var results []RecentTransaction

	c.DB.Raw(`
		SELECT 
			t.id,
			t.date,
			v.name as vendor_name,
			t.description,
			t.amount
		FROM vendor_transactions t
		JOIN vendors v ON t.vendor_id = v.id
		WHERE t.deleted_at IS NULL
		AND v.deleted_at IS NULL
		ORDER BY t.date DESC, t.id DESC
		LIMIT 10
	`).Scan(&results)

	return ctx.JSON(results)
}
