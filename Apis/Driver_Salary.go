package Apis

import (
	"Falcon/Models"
	"log"
	"math"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
)

func RegisterDriverLoan(c *fiber.Ctx) error {
	var input struct {
		DriverID uint        `json:"driver_id"`
		Loan     Models.Loan `json:"loan"`
	}
	if err := c.BodyParser(&input); err != nil {
		log.Println(err)
		return err
	}

	input.Loan.DriverID = input.DriverID

	if err := Models.DB.Create(&input.Loan).Error; err != nil {
		log.Println(err.Error())
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Loan Registered Successfully",
	})
}

type LoanStatRequest struct {
	FromDate string `json:"from_date"`
	ToDate   string `json:"to_date"`
}

// Define a response structure for the statistics
type LoanStatResponse struct {
	// Basic statistics
	TotalLoans    int     `json:"total_loans"`
	TotalAmount   float64 `json:"total_amount"`
	AverageAmount float64 `json:"average_amount"`
	MedianAmount  float64 `json:"median_amount"`
	MinAmount     float64 `json:"min_amount"`
	MaxAmount     float64 `json:"max_amount"`

	// Payment method statistics
	MethodStats   map[string]int     `json:"method_stats"`
	MethodAmounts map[string]float64 `json:"method_amounts"`

	// Time-based analysis
	DailyStats   []DailyStat            `json:"daily_stats"`
	WeekdayStats map[string]WeekdayStat `json:"weekday_stats"`

	// Frequency analysis
	AmountRanges map[string]int `json:"amount_ranges"`

	// Growth metrics
	GrowthRate float64 `json:"growth_rate"` // Percentage growth compared to previous period

	// Period metadata
	PeriodDays  int     `json:"period_days"`
	PeriodWeeks float64 `json:"period_weeks"`
}

type DailyStat struct {
	Date        string  `json:"date"`
	LoanCount   int     `json:"loan_count"`
	TotalAmount float64 `json:"total_amount"`
	AvgAmount   float64 `json:"avg_amount"`
}

// WeekdayStat represents statistics for a specific weekday
type WeekdayStat struct {
	LoanCount   int     `json:"loan_count"`
	TotalAmount float64 `json:"total_amount"`
	AvgAmount   float64 `json:"avg_amount"`
}

func FetchLoanStats(c *fiber.Ctx) error {
	// Parse request body
	var req LoanStatRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse request body",
		})
	}

	// Validate dates
	if req.FromDate == "" || req.ToDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Both from_date and to_date are required",
		})
	}

	// Query loans between the dates
	var loans []Models.Loan
	if err := Models.DB.Where("date BETWEEN ? AND ?", req.FromDate, req.ToDate).Find(&loans).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch loans",
		})
	}

	// Calculate statistics
	stats := calculateEnhancedLoanStats(loans, req.FromDate, req.ToDate)

	// Also fetch loans from previous period of equal length for growth comparison
	fromDate, _ := time.Parse("2006-01-02", req.FromDate)
	toDate, _ := time.Parse("2006-01-02", req.ToDate)
	periodDuration := toDate.Sub(fromDate)

	prevFromDate := fromDate.Add(-periodDuration)
	prevToDate := fromDate.Add(-time.Hour * 24) // Day before current period

	var prevLoans []Models.Loan
	Models.DB.Where("date BETWEEN ? AND ?",
		prevFromDate.Format("2006-01-02"),
		prevToDate.Format("2006-01-02")).Find(&prevLoans)

	// Calculate growth rate
	if len(prevLoans) > 0 {
		var prevTotalAmount float64
		for _, loan := range prevLoans {
			prevTotalAmount += loan.Amount
		}

		if prevTotalAmount > 0 {
			growthRate := ((stats.TotalAmount - prevTotalAmount) / prevTotalAmount) * 100
			stats.GrowthRate = math.Round(growthRate*100) / 100
		}
	}

	// Return the enhanced statistics
	return c.JSON(fiber.Map{
		"message": "Loan statistics retrieved successfully",
		"stats":   stats,
	})
}

// calculateEnhancedLoanStats processes loan data and returns comprehensive statistics
func calculateEnhancedLoanStats(loans []Models.Loan, fromDateStr, toDateStr string) LoanStatResponse {
	stats := LoanStatResponse{
		MethodStats:   make(map[string]int),
		MethodAmounts: make(map[string]float64),
		WeekdayStats:  make(map[string]WeekdayStat),
		AmountRanges:  make(map[string]int),
		DailyStats:    []DailyStat{},
	}

	// Early return if no loans found
	if len(loans) == 0 {
		fromDate, _ := time.Parse("2006-01-02", fromDateStr)
		toDate, _ := time.Parse("2006-01-02", toDateStr)
		stats.PeriodDays = int(toDate.Sub(fromDate).Hours()/24) + 1
		stats.PeriodWeeks = float64(stats.PeriodDays) / 7
		return stats
	}

	// Create daily statistics map
	dailyMap := make(map[string]DailyStat)

	// Create amount slice for median calculation
	amounts := make([]float64, len(loans))

	// Extract date range information
	fromDate, _ := time.Parse("2006-01-02", fromDateStr)
	toDate, _ := time.Parse("2006-01-02", toDateStr)
	stats.PeriodDays = int(toDate.Sub(fromDate).Hours()/24) + 1
	stats.PeriodWeeks = float64(stats.PeriodDays) / 7

	// Initialize values
	stats.MinAmount = loans[0].Amount
	stats.MaxAmount = loans[0].Amount

	// Process each loan
	for i, loan := range loans {
		// Basic stats
		stats.TotalLoans++
		stats.TotalAmount += loan.Amount
		amounts[i] = loan.Amount

		// Min/Max tracking
		if loan.Amount < stats.MinAmount {
			stats.MinAmount = loan.Amount
		}
		if loan.Amount > stats.MaxAmount {
			stats.MaxAmount = loan.Amount
		}

		// Method statistics
		stats.MethodStats[loan.Method]++
		stats.MethodAmounts[loan.Method] += loan.Amount

		// Daily statistics
		date := loan.Date
		dailyStat, exists := dailyMap[date]
		if !exists {
			dailyStat = DailyStat{
				Date: date,
			}
		}
		dailyStat.LoanCount++
		dailyStat.TotalAmount += loan.Amount
		dailyMap[date] = dailyStat

		// Weekday statistics
		loanDate, err := time.Parse("2006-01-02", loan.Date)
		if err == nil {
			weekday := loanDate.Weekday().String()
			weekdayStat, exists := stats.WeekdayStats[weekday]
			if !exists {
				weekdayStat = WeekdayStat{}
			}
			weekdayStat.LoanCount++
			weekdayStat.TotalAmount += loan.Amount
			stats.WeekdayStats[weekday] = weekdayStat
		}

		// Amount range categorization
		amountRange := categorizeAmount(loan.Amount)
		stats.AmountRanges[amountRange]++
	}

	// Calculate average amount
	stats.AverageAmount = stats.TotalAmount / float64(stats.TotalLoans)

	// Calculate median amount
	sort.Float64s(amounts)
	if len(amounts)%2 == 0 {
		// Even number of loans
		stats.MedianAmount = (amounts[len(amounts)/2-1] + amounts[len(amounts)/2]) / 2
	} else {
		// Odd number of loans
		stats.MedianAmount = amounts[len(amounts)/2]
	}

	// Finalize daily statistics
	for date, stat := range dailyMap {
		if stat.LoanCount > 0 {
			stat.AvgAmount = stat.TotalAmount / float64(stat.LoanCount)
		}
		stat.Date = date
		stats.DailyStats = append(stats.DailyStats, stat)
	}

	// Sort daily stats by date
	sort.Slice(stats.DailyStats, func(i, j int) bool {
		return stats.DailyStats[i].Date < stats.DailyStats[j].Date
	})

	// Finalize weekday statistics
	for day, stat := range stats.WeekdayStats {
		if stat.LoanCount > 0 {
			stat.AvgAmount = stat.TotalAmount / float64(stat.LoanCount)
			stats.WeekdayStats[day] = stat
		}
	}

	return stats
}

// categorizeAmount groups loan amounts into meaningful ranges
func categorizeAmount(amount float64) string {
	switch {
	case amount < 250:
		return "Under $250"
	case amount < 500:
		return "$250-$499"
	case amount < 1000:
		return "$500-$999"
	case amount < 2000:
		return "$1000-$1999"
	case amount < 5000:
		return "$2000-$4999"
	default:
		return "$5000+"
	}
}

// NormalizeLoanData helper function to combine similar method names (e.g., Arabic/English variants)
func NormalizeLoanData(stats *LoanStatResponse) {
	// Map to store normalized method names
	normalizedMethods := make(map[string]string)
	methodStats := make(map[string]int)
	methodAmounts := make(map[string]float64)

	// Define mapping for known variants
	// For example: mapping Arabic "شادى" to English "Shady"
	knownVariants := map[string]string{
		"شادى": "Shady",
	}

	// First pass: determine canonical names
	for method := range stats.MethodStats {
		if canonical, exists := knownVariants[method]; exists {
			normalizedMethods[method] = canonical
		} else {
			normalizedMethods[method] = method
		}
	}

	// Second pass: combine statistics
	for method, count := range stats.MethodStats {
		canonical := normalizedMethods[method]
		methodStats[canonical] += count
		methodAmounts[canonical] += stats.MethodAmounts[method]
	}

	// Update the stats with normalized values
	stats.MethodStats = methodStats
	stats.MethodAmounts = methodAmounts
}

func RegisterDriverExpense(c *fiber.Ctx) error {
	var input struct {
		Expense Models.Expense `json:"expense"`
	}
	if err := c.BodyParser(&input); err != nil {
		log.Println(err)
		return err
	}

	if err := Models.DB.Create(&input.Expense).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	return c.JSON(fiber.Map{
		"message": "Expense Registered Successfully",
	})
}

// RegisterDriverSalary handles creating a new salary record
func RegisterDriverSalary(c *fiber.Ctx) error {
	var input struct {
		DriverID   uint    `json:"driver_id"`
		DriverCost float64 `json:"driver_cost"`
		StartDate  string  `json:"start_date"`
		CloseDate  string  `json:"close_date"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err)
		return err
	}

	// Validate dates
	if input.StartDate == "" || input.CloseDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Start date and close date are required",
		})
	}

	// Fetch all unpaid expenses for this driver within the date range
	var expenses []Models.Expense
	if err := Models.DB.Model(&Models.Expense{}).Where(
		"driver_id = ? AND is_paid = ? AND date >= ? AND date <= ?",
		input.DriverID, false, input.StartDate, input.CloseDate,
	).Find(&expenses).Error; err != nil {
		log.Println("Error fetching expenses:", err.Error())
		return err
	}

	// Calculate total expenses
	totalExpenses := 0.0
	for _, expense := range expenses {
		totalExpenses += expense.Cost
	}

	// Fetch all loans for this driver within the date range
	var loans []Models.Loan
	if err := Models.DB.Model(&Models.Loan{}).Where(
		"driver_id = ? AND is_paid = ? AND date >= ? AND date <= ?",
		input.DriverID, false, input.StartDate, input.CloseDate,
	).Find(&loans).Error; err != nil {
		log.Println("Error fetching loans:", err.Error())
		return err
	}

	// Calculate total loans
	totalLoans := 0.0
	for _, loan := range loans {
		totalLoans += loan.Amount
	}

	// Create the salary object
	salary := Models.Salary{
		DriverID:      input.DriverID,
		DriverCost:    input.DriverCost,
		TotalExpenses: totalExpenses,
		TotalLoans:    totalLoans,
		StartDate:     input.StartDate,
		CloseDate:     input.CloseDate,
	}

	// Begin transaction
	tx := Models.DB.Begin()

	// Save salary record
	if err := tx.Create(&salary).Error; err != nil {
		tx.Rollback()
		log.Println("Error creating salary:", err.Error())
		return err
	}

	// Mark all fetched expenses as paid
	for i := range expenses {
		expenses[i].IsPaid = true
		if err := tx.Save(&expenses[i]).Error; err != nil {
			tx.Rollback()
			log.Println("Error updating expense:", err.Error())
			return err
		}
	}

	for i := range loans {
		loans[i].IsPaid = true
		if err := tx.Save(&loans[i]).Error; err != nil {
			tx.Rollback()
			log.Println("Error updating loan:", err.Error())
			return err
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		log.Println("Error committing transaction:", err.Error())
		return err
	}

	return c.JSON(fiber.Map{
		"message":             "Salary Registered Successfully",
		"salary":              salary,
		"paid_expenses_count": len(expenses),
		"loans_count":         len(loans),
	})
}

// GetDriverSalaryPreview returns expenses and loans for a given date range
func GetDriverSalaryPreview(c *fiber.Ctx) error {
	var input struct {
		DriverID  uint   `json:"driver_id"`
		StartDate string `json:"start_date"`
		CloseDate string `json:"close_date"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err)
		return err
	}

	// Validate input
	if input.DriverID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Driver ID is required",
		})
	}

	if input.StartDate == "" || input.CloseDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Start date and end date are required",
		})
	}

	// Fetch unpaid expenses for this driver within the date range
	var expenses []Models.Expense
	if err := Models.DB.Model(&Models.Expense{}).Where(
		"driver_id = ? AND is_paid <> ? AND date >= ? AND date <= ?",
		input.DriverID, true, input.StartDate, input.CloseDate,
	).Find(&expenses).Error; err != nil {
		log.Println("Error fetching expenses:", err.Error())
		return err
	}

	// Calculate total expenses
	totalExpenses := 0.0
	for _, expense := range expenses {
		totalExpenses += expense.Cost // Using lowercase field name to match the struct
	}

	// Fetch loans for this driver within the date range
	var loans []Models.Loan
	if err := Models.DB.Model(&Models.Loan{}).Where(
		"driver_id = ? AND is_paid <> ? AND date >= ? AND date <= ?",
		input.DriverID, true, input.StartDate, input.CloseDate,
	).Find(&loans).Error; err != nil {
		log.Println("Error fetching loans:", err.Error())
		return err
	}

	// Calculate total loans
	totalLoans := 0.0
	for _, loan := range loans {
		totalLoans += loan.Amount
	}

	// Fetch driver information for display purposes
	var driver Models.Driver
	driverName := "Driver"
	if err := Models.DB.First(&driver, input.DriverID).Error; err == nil {
		driverName = driver.Name
	}

	return c.JSON(fiber.Map{
		"expenses":       expenses,
		"loans":          loans,
		"total_expenses": totalExpenses,
		"total_loans":    totalLoans,
		"expenses_count": len(expenses),
		"loans_count":    len(loans),
		"driver_name":    driverName,
		"date_range": map[string]string{
			"start": input.StartDate,
			"end":   input.CloseDate,
		},
	})
}

// GetDriverSalaries fetches salary records with optional filtering
func GetDriverSalaries(c *fiber.Ctx) error {
	var input struct {
		DriverID  uint   `json:"driver_id"`  // Optional: filter by driver
		StartDate string `json:"start_date"` // Optional: filter by end date range
		CloseDate string `json:"close_date"` // Optional: filter by start date range
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err)
		return err
	}

	// Start building the query
	query := Models.DB.Model(&Models.Salary{})

	// Apply filters if provided
	if input.DriverID != 0 {
		query = query.Where("driver_id = ?", input.DriverID)
	}

	if input.CloseDate != "" {
		query = query.Where("start_date >= ?", input.CloseDate)
	}

	if input.StartDate != "" {
		query = query.Where("close_date <= ?", input.StartDate)
	}

	// Get the results
	var salaries []Models.Salary
	if err := query.Order("created_at DESC").Find(&salaries).Error; err != nil {
		log.Println("Error fetching salaries:", err.Error())
		return err
	}

	// If specifically looking for one driver's salaries, also get the driver name
	driverName := ""
	if input.DriverID != 0 {
		var driver Models.Driver
		if err := Models.DB.First(&driver, input.DriverID).Error; err == nil {
			driverName = driver.Name
		}
	}

	// Calculate some summary statistics
	totalSalaries := len(salaries)
	totalCost := 0.0
	totalExpenses := 0.0
	totalLoans := 0.0

	for _, salary := range salaries {
		totalCost += salary.DriverCost
		totalExpenses += salary.TotalExpenses
		totalLoans += salary.TotalLoans
	}

	return c.JSON(fiber.Map{
		"salaries":    salaries,
		"total_count": totalSalaries,
		"driver_name": driverName,
		"summary": fiber.Map{
			"total_cost":     totalCost,
			"total_expenses": totalExpenses,
			"total_loans":    totalLoans,
			"net_paid":       totalCost - totalExpenses + totalLoans,
		},
	})
}

// func CalculateDriverSalary(c *fiber.Ctx) error {
// 	var input struct {
// 		ID       uint   `json:"id"`
// 		DateFrom string `json:"date_from"`
// 		DateTo   string `json:"date_to"`
// 	}

// 	if err := c.BodyParser(&input); err != nil {
// 		log.Println(err)
// 		return err
// 	}

// 	DateFrom, err := AbstractFunctions.ParseDate(input.DateFrom)

// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	DateTo, err := AbstractFunctions.ParseDate(input.DateTo)
// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	var Driver Models.Driver

// 	if err := Models.DB.Model(&Models.Driver{}).Where("id = ?", input.ID).Find(&Driver).Error; err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	var DriverTrips []Models.TripStruct

// 	if err := Models.DB.Model(&Models.TripStruct{}).Where("date BETWEEN ? AND ?", DateFrom, DateTo).Where("driver_name = ?", Driver.Name).Preload("Route").Preload("Expenses").Preload("Loans").Find(&DriverTrips).Error; err != nil {
// 		log.Println(err)
// 		return err
// 	}
// 	var DriverExpenses []Models.Expense
// 	for _, trip := range DriverTrips {
// 		var TripExpenses []Models.Expense
// 		if err := Models.DB.Model(&Models.Expense{}).Where("trip_struct_id = ?", trip.ID).Find(&TripExpenses).Error; err != nil {
// 			log.Println(err.Error())
// 			return err
// 		}
// 		DriverExpenses = append(DriverExpenses, TripExpenses...)
// 	}
// 	var DriverLoans []Models.Loan

// 	for _, trip := range DriverTrips {
// 		var TripLoans []Models.Loan
// 		if err := Models.DB.Model(&Models.Loan{}).Where("trip_struct_id = ?", trip.ID).Find(&TripLoans).Error; err != nil {
// 			log.Println(err.Error())
// 			return err
// 		}
// 		DriverLoans = append(DriverLoans, TripLoans...)
// 	}

// 	var (
// 		TotalDriverFees     float64
// 		TotalDriverExpenses float64
// 		TotalDriverLoans    float64
// 	)

// 	for _, trip := range DriverTrips {
// 		TotalDriverFees += trip.Route.DriverFees
// 	}

// 	for _, expense := range DriverExpenses {
// 		TotalDriverExpenses += expense.Cost
// 	}

// 	for _, loan := range DriverLoans {
// 		TotalDriverLoans += loan.Amount
// 	}

// 	file := excelize.NewFile()
// 	file.NewSheet("Salary")
// 	file.DeleteSheet("Sheet1")

// 	headers := map[string]string{
// 		"A1": "Receipt No", "B1": "Date", "C1": "Driver Name", "D1": "Car No Plate", "E1": "Distance", "F1": "Driver Fees", "G1": "Trip Expenses", "H1": "Total Expenses", "I1": "Description", "J1": "Trip Loans", "K1": "Total Loans", "L1": "Notes", "M1": "Trip Salary", "N1": "Start Time", "O1": "End Time",
// 		"A2": "رقم الفاتورة", "B2": "التاريخ", "C2": "اسم السائق", "D2": "رقم السيارة", "E2": "المسافة", "F2": "نولون السائق", "G2": "مصروفات النقلة", "H2": "اجمالي المصروفات", "I2": "تفصيل المصروفات", " J2": "العهد", "K2": "اجمالي العهد", "L2": "ملاحظات", "M2": "اجمالي حساب النقلة", "N2": "معاد البداية", "O2": "معاد النهاية",
// 	}

// 	for k, v := range headers {
// 		file.SetCellValue("Salary", k, v)
// 	}

// 	for index, trip := range DriverTrips {
// 		file.SetCellValue("Salary", fmt.Sprintf("A%v", index+3), trip.ReceiptNo)
// 		file.SetCellValue("Salary", fmt.Sprintf("B%v", index+3), trip.Date)
// 		file.SetCellValue("Salary", fmt.Sprintf("C%v", index+3), trip.DriverName)
// 		file.SetCellValue("Salary", fmt.Sprintf("D%v", index+3), trip.CarNoPlate)
// 		file.SetCellValue("Salary", fmt.Sprintf("E%v", index+3), trip.Mileage)
// 		file.SetCellValue("Salary", fmt.Sprintf("F%v", index+3), trip.DriverFees)
// 		var totalSalary float64
// 		var totalExpenses float64
// 		var tripExpenses string
// 		var expensesDescriptions string
// 		for i, expense := range trip.Expenses {
// 			totalExpenses += expense.Cost
// 			if i == 0 {
// 				tripExpenses = fmt.Sprintf("%v", expense.Cost)
// 				expensesDescriptions = expense.Description
// 				continue
// 			}
// 			tripExpenses = fmt.Sprintf("%s+%v", tripExpenses, expense.Cost)
// 			expensesDescriptions = fmt.Sprintf("%s+%s", expensesDescriptions, expense.Description)
// 		}
// 		file.SetCellValue("Salary", fmt.Sprintf("G%v", index+3), tripExpenses)
// 		file.SetCellValue("Salary", fmt.Sprintf("H%v", index+3), totalExpenses)
// 		file.SetCellValue("Salary", fmt.Sprintf("I%v", index+3), expensesDescriptions)

// 		var totalLoans float64
// 		var tripLoans string
// 		var loansMethods string
// 		for i, loan := range trip.Loans {
// 			totalLoans += loan.Amount
// 			if i == 0 {
// 				tripLoans = fmt.Sprintf("%v", loan.Amount)
// 				loansMethods = loan.Method
// 				continue
// 			}
// 			tripLoans = fmt.Sprintf("%s+%v", tripLoans, loan.Amount)
// 			loansMethods = fmt.Sprintf("%s+%s", loansMethods, loan.Method)
// 		}
// 		file.SetCellValue("Salary", fmt.Sprintf("J%v", index+3), tripLoans)
// 		file.SetCellValue("Salary", fmt.Sprintf("K%v", index+3), totalLoans)
// 		file.SetCellValue("Salary", fmt.Sprintf("L%v", index+3), loansMethods)
// 		totalSalary = totalExpenses + trip.DriverFees - totalLoans
// 		file.SetCellValue("Salary", fmt.Sprintf("M%v", index+3), totalSalary)
// 		file.SetCellValue("Salary", fmt.Sprintf("N%v", index+3), trip.StartTime)
// 		file.SetCellValue("Salary", fmt.Sprintf("O%v", index+3), trip.EndTime)
// 	}

// 	var filename string = fmt.Sprintf("./Salaries/Salary For %s From %s To %s.xlsx", Driver.Name, input.DateFrom, input.DateTo)
// 	err = file.SaveAs(filename)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	return c.SendFile(filename, true)
// }

func GetDriverExpenses(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}

	var DriverExpenses []Models.Expense

	if err := Models.DB.Model(&Models.Expense{}).Where("driver_id = ?", input.ID).Find(&DriverExpenses).Error; err != nil {
		return err
	}

	return c.JSON(
		DriverExpenses,
	)
}

func GetDriverLoans(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}

	var DriverLoans []Models.Loan

	if err := Models.DB.Model(&Models.Loan{}).Where("driver_id = ?", input.ID).Find(&DriverLoans).Error; err != nil {
		return err
	}

	return c.JSON(
		DriverLoans,
	)
}

func GetTripExpenses(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}

	var TripExpenses []Models.Expense
	if err := Models.DB.Model(&Models.Expense{}).Where("trip_struct_id = ?", input.ID).Find(&TripExpenses).Error; err != nil {
		log.Println(err.Error())
		return err
	}

	return c.JSON(TripExpenses)
}

func GetTripLoans(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}

	var TripLoans []Models.Loan
	if err := Models.DB.Model(&Models.Loan{}).Where("trip_struct_id = ?", input.ID).Find(&TripLoans).Error; err != nil {
		log.Println(err.Error())
		return err
	}

	return c.JSON(TripLoans)
}

func DeleteExpense(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}
	if err := Models.DB.Model(&Models.Expense{}).Delete("id = ?", input.ID).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	return c.JSON(fiber.Map{
		"message": "Expense Deleted Successfully",
	})
}

func DeleteLoan(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}
	if err := Models.DB.Model(&Models.Loan{}).Delete("id = ?", input.ID).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	return c.JSON(fiber.Map{
		"message": "Loan Deleted Successfully",
	})
}
