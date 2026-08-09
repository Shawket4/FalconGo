package FiberConfig

import (
	"Falcon/Apis"
	"Falcon/Controllers"
	"Falcon/ETA"
	"Falcon/Models"
	"Falcon/MultiRouteOptimizer"
	"Falcon/Notifications"
	"Falcon/PetroApp"
	"Falcon/PreviewData"
	"Falcon/Scrapper"
	"Falcon/Slack"
	"Falcon/Whatsapp"
	"Falcon/middleware"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/template/html"
	"gorm.io/gorm"
)

func SetupRoutes(app *fiber.App, db *gorm.DB) {
	// ============================================
	// INITIALIZE CONTROLLERS
	// ============================================
	feeMappingHandler := Controllers.NewFeeMappingHandler(db)
	osrm := Controllers.NewOSRMEnrichmentHandler(db)
	tripHandler := Controllers.NewTripHandler(db)
	vendorController := Controllers.NewVendorController(db)
	transactionController := Controllers.NewTransactionController(db)
	vendorAnalyticsController := Controllers.NewAnalyticsController(db)
	receiptController := &Controllers.ReceiptController{DB: db, UploadDir: "./uploads/receipts"}

	// NEW: Session-based controllers
	tabletController := &Controllers.TabletController{DB: db}
	driverAuthController := &Controllers.DriverAuthController{DB: db}
	sessionController := &Controllers.SessionController{DB: db}

	// ============================================
	// CREATE API GROUP
	// ============================================
	api := app.Group("/api")

	// ============================================
	// DRIVER APP ROUTES (PIN Authentication)
	// ============================================
	driverApp := api.Group("/driver-app")

	// Public driver routes (no auth)
	driverApp.Post("/login",
		middleware.TabletIdentification(),
		middleware.RequireTabletAssigned(),
		driverAuthController.Login,
	)

	// Tablet self-registration
	api.Post("/tablet/register",
		middleware.TabletIdentification(),
		tabletController.Register,
	)

	// Protected driver routes (requires PIN auth + active session)
	driverAppProtected := driverApp.Group("",
		middleware.TabletIdentification(),
		middleware.VerifyDriver(),
	)

	// Driver profile & settings
	driverAppProtected.Get("/me", driverAuthController.GetCurrentDriver)
	driverAppProtected.Put("/change-pin", driverAuthController.ChangePIN)
	driverAppProtected.Post("/logout", driverAuthController.Logout)

	// Driver trip management
	driverAppProtected.Get("/trips", tripHandler.GetDriverTrips)
	driverAppProtected.Get("/sessions", sessionController.GetMySessionHistory)

	// Location tracking (requires active session)
	driverAppProtected.Post("/location/ping",
		middleware.RequireActiveSessionWithTabletMatch(),
		receiptController.RegisterLocationPing,
	)

	// Receipt uploads (requires active session)
	driverAppProtected.Post("/receipts/upload",
		middleware.RequireActiveSession(),
		receiptController.UploadReceiptBatch,
	)

	// ============================================
	// LEGACY DRIVER ROUTES (Backward Compatibility)
	// ============================================
	driverRoutes := api.Group("/driver", middleware.Verify(0))
	driverRoutes.Post("/receipts/upload", receiptController.UploadReceiptBatch)
	driverRoutes.Post("/ping-location", receiptController.RegisterLocationPing)
	driverRoutes.Get("/trips", tripHandler.GetDriverTrips)

	// ============================================
	// RECEIPT MANAGEMENT
	// ============================================
	receipts := api.Group("/receipts", middleware.Verify(1))

	// Receipt steps
	receipts.Post("/step", receiptController.AddReceiptStep)
	receipts.Get("/trip/:tripId", receiptController.GetTripReceiptSteps)
	receipts.Put("/step/:stepId", receiptController.UpdateReceiptStep)
	receipts.Delete("/step/:stepId", receiptController.DeleteReceiptStep)

	// Receipt status and statistics
	receipts.Get("/status", receiptController.GetTripsWithReceiptStatus)
	receipts.Get("/statistics", receiptController.GetReceiptStatistics)

	// Receipt batches
	receipts.Get("/pending", receiptController.GetPendingBatches)
	receipts.Get("/batch/:batch_id", receiptController.GetBatchDetails)
	receipts.Patch("/batch/:batch_id/reject", receiptController.RejectBatch)

	// ============================================
	// TABLET MANAGEMENT (Admin Only)
	// ============================================
	tablets := api.Group("/admin/tablets", middleware.Verify(2))
	tablets.Get("/", tabletController.GetAll)
	tablets.Get("/unassigned", tabletController.GetUnassigned)
	tablets.Get("/:id", tabletController.GetByID)
	tablets.Put("/:id/assign-car", tabletController.AssignCar)
	tablets.Put("/:id/unassign", tabletController.UnassignCar)
	tablets.Delete("/:id", tabletController.Delete)

	// ============================================
	// SESSION MANAGEMENT
	// ============================================
	sessions := api.Group("/sessions")

	// Admin session routes
	sessions.Get("/active", middleware.Verify(1), sessionController.GetActiveSessions)
	sessions.Get("/driver/:driver_id", middleware.Verify(1), sessionController.GetDriverSessions)
	sessions.Get("/car/:car_id", middleware.Verify(1), sessionController.GetCarSessions)
	sessions.Get("/:id", middleware.VerifyAny(), sessionController.GetSession)
	sessions.Get("/:id/location-pings", middleware.Verify(1), sessionController.GetSessionLocationPings)
	sessions.Post("/:id/end", middleware.Verify(2), sessionController.EndSession)

	// ============================================
	// VENDOR MANAGEMENT
	// ============================================
	vendors := api.Group("/vendors", middleware.Verify(3))
	vendors.Get("/", vendorController.GetVendors)
	vendors.Post("/", vendorController.CreateVendor)
	vendors.Get("/:id", vendorController.GetVendor)
	vendors.Put("/:id", vendorController.UpdateVendor)
	vendors.Delete("/:id", vendorController.DeleteVendor)
	vendors.Get("/:id/balance", vendorController.GetVendorBalance)
	vendors.Get("/:vendor_id/transactions", transactionController.GetVendorTransactions)
	vendors.Post("/:vendor_id/transactions", transactionController.CreateTransaction)

	// ============================================
	// TRANSACTION MANAGEMENT
	// ============================================
	transactions := api.Group("/transactions", middleware.Verify(3))
	transactions.Get("/:id", transactionController.GetTransaction)
	transactions.Put("/:id", transactionController.UpdateTransaction)
	transactions.Delete("/:id", transactionController.DeleteTransaction)

	// ============================================
	// ANALYTICS
	// ============================================
	analytics := api.Group("/analytics", middleware.Verify(3))
	analytics.Get("/summary", vendorAnalyticsController.Summary)
	analytics.Get("/monthly", vendorAnalyticsController.MonthlyTransactions)
	analytics.Get("/top-vendors", vendorAnalyticsController.TopVendors)
	analytics.Get("/recent-activity", vendorAnalyticsController.RecentActivity)

	// ============================================
	// FEE MAPPING MANAGEMENT
	// ============================================
	mappings := api.Group("/mappings", middleware.Verify(1))
	mappings.Get("/", feeMappingHandler.GetAllFeeMappings)
	mappings.Post("/enrich-osrm", middleware.Verify(3), osrm.BulkEnrichOSRM)
	mappings.Post("/:id/location", middleware.Verify(3), osrm.UpdateDropOffLocation)

	// Helper routes for dropdowns
	mappings.Get("/companies", feeMappingHandler.GetCompaniesList)
	mappings.Get("/terminals/:company", feeMappingHandler.GetTerminalsByCompany)
	mappings.Get("/dropoffs/:company/:terminal", feeMappingHandler.GetDropOffPointsByTerminal)
	mappings.Get("/fee", feeMappingHandler.GetFeeByMapping)
	api.Get("/all-terminals", feeMappingHandler.GetAllTerminals, middleware.VerifyAny())

	// CRUD operations
	mappings.Get("/:id", feeMappingHandler.GetFeeMapping)
	mappings.Post("/", middleware.Verify(3), feeMappingHandler.CreateFeeMapping)
	mappings.Put("/:id", middleware.Verify(3), feeMappingHandler.UpdateFeeMapping)
	mappings.Delete("/:id", middleware.Verify(3), feeMappingHandler.DeleteFeeMapping)

	// ============================================
	// TRIP MANAGEMENT
	// ============================================
	trips := api.Group("/trips", middleware.Verify(1))
	trips.Get("/", tripHandler.GetAllTrips)
	trips.Get("/statistics", tripHandler.GetTripStatistics)
	trips.Get("/watanya/driver-analytics", tripHandler.GetWatanyaDriverAnalytics)
	trips.Get("/date", tripHandler.GetTripsByDate)
	trips.Get("/company/:company", tripHandler.GetTripsByCompany)
	trips.Post("/watanya/export_report", middleware.Verify(3), tripHandler.GetWatanyaTripsReport)

	// Single trip operations
	trips.Get("/:id", tripHandler.GetTrip)
	trips.Get("/:id/details", tripHandler.GetTripDetails)
	trips.Put("/:id", tripHandler.UpdateTrip)
	trips.Delete("/:id", tripHandler.DeleteTrip)

	// Multi-container trips
	trips.Post("/multi-container", tripHandler.CreateMultiContainerTrip)
	trips.Get("/parent/:parent_id/containers", tripHandler.GetContainersByParent)
	trips.Put("/parent/:parent_id", tripHandler.UpdateParentTrip)
	trips.Delete("/parent/:parent_id", tripHandler.DeleteParentTrip)

	// TRIP SUMMARY ROUTE (Your original endpoint - now session-aware)
	api.Get("/parent-trips/:parent_trip_id/summary", Controllers.GetFullTripSummaryByParentTrip, middleware.Verify(1))

	// Global stats
	api.Get("/stats/widget-data", tripHandler.GetGlobalStats)
	api.Post("/UpdateToken", Models.UpdateToken)

	// ============================================
	// DRIVER MANAGEMENT (Admin)
	// ============================================
	api.Post("/protected/RegisterDriver", Controllers.RegisterDriver, middleware.Verify(1))
	drivers := api.Group("/drivers", middleware.Verify(1))
	drivers.Get("/", Apis.GetDrivers)
	drivers.Post("/", Controllers.RegisterDriver)
	drivers.Get("/:id", Apis.GetDriver)
	drivers.Put("/:id", Controllers.UpdateDriver)
	drivers.Put("/documents/:id", Apis.UpdateDriverDocuments)
	drivers.Delete("/:id", Apis.DeleteDriver)

	drivers.Put("/:id/regenerate-pin", middleware.Verify(3), driverAuthController.RegeneratePIN)
	drivers.Get("/:id/location-pings", Controllers.GetDriverLocationPings)

	// Driver profile data
	api.Use("/GetDriverProfileData", middleware.Verify(1), Apis.GetDriverProfileData)

	// ============================================
	// CAR MANAGEMENT
	// ============================================
	cars := api.Group("/cars", middleware.Verify(1))
	cars.Post("/", Apis.RegisterCar)
	cars.Get("/", Apis.GetCars)
	cars.Get("/:id", Apis.GetCar)
	cars.Put("/:id", Apis.UpdateCarDocuments)
	cars.Delete("/:id", Apis.DeleteCar)
	cars.Get("/:carId/service-invoices", Controllers.GetServiceInvoicesByCarID)

	// Car profile data
	api.Use("/GetCarProfileData", middleware.Verify(1), Apis.GetCarProfileData)

	// Car-Driver pairing
	api.Patch("/protected/SetCarDriverPair", middleware.Verify(1), Apis.SetCarDriverPair)

	// ============================================
	// FINANCIAL MANAGEMENT
	// ============================================
	financial := api.Group("/", middleware.Verify(1))

	// Loans
	financial.Post("/RegisterDriverLoan", middleware.Verify(3), Apis.RegisterDriverLoan)
	financial.Post("/loans/stats", Apis.FetchLoanStats)
	financial.Post("/GetDriverLoans", Apis.GetDriverLoans)
	financial.Post("/GetTripLoans", Apis.GetTripLoans)
	financial.Post("/DeleteLoan", middleware.Verify(3), Apis.DeleteLoan)

	// Expenses
	financial.Post("/RegisterDriverExpense", Apis.RegisterDriverExpense)
	financial.Post("/GetDriverExpenses", Apis.GetDriverExpenses)
	financial.Post("/GetTripExpenses", Apis.GetTripExpenses)
	financial.Post("/DeleteExpense", Apis.DeleteExpense)

	// Salaries
	financial.Post("/RegisterDriverSalary", Apis.RegisterDriverSalary)
	financial.Post("/GetDriverSalaryPreview", Apis.GetDriverSalaryPreview)
	financial.Post("/GetDriverSalaries", Apis.GetDriverSalaries)

	// ============================================
	// FUEL MANAGEMENT
	// ============================================
	fuelHandler := &Apis.FuelHandler{DB: Models.DB}
	fuel := api.Group("/fuel", middleware.Verify(1))
	fuel.Get("/statistics", fuelHandler.GetFuelStatistics)
	fuel.Get("/sync-car-odometers", middleware.Verify(3), Apis.SyncCarLastFuelOdometer)
	fuel.Get("/sync-car-mileages", middleware.Verify(3), Apis.SyncCarMileages)

	protectedApis := api.Group("/protected/", middleware.Verify(1))
	protectedApis.Post("/AddFuelEvent", middleware.Verify(3), Apis.AddFuelEvent)
	protectedApis.Post("/EditFuelEvent", middleware.Verify(3), Apis.EditFuelEvent)
	protectedApis.Post("/DeleteFuelEvent", middleware.Verify(3), Apis.DeleteFuelEvent)
	protectedApis.Get("/GetFuelEvents", Apis.GetFuelEvents)
	protectedApis.Get("/GetFuelEventById/:id", Apis.GetFuelEventById)

	// ============================================
	// SERVICE & MAINTENANCE
	// ============================================
	// Service events
	api.Post("/CreateServiceEvent", middleware.Verify(1), Apis.CreateServiceEvent)
	api.Post("/EditServiceEvent", middleware.Verify(1), Apis.EditServiceEvent)
	api.Post("/DeleteServiceEvent", middleware.Verify(1), Apis.DeleteServiceEvent)
	api.Get("/GetAllServiceEvents", middleware.Verify(1), Apis.GetAllServiceEvents)

	// Oil changes
	api.Post("/CreateOilChange", middleware.Verify(1), Apis.CreateOilChange)
	api.Post("/EditOilChange", middleware.Verify(1), Apis.EditOilChange)
	api.Delete("/DeleteOilChange/:id", middleware.Verify(1), Apis.DeleteOilChange)
	api.Get("/GetAllOilChanges", middleware.Verify(1), Apis.GetAllOilChanges)
	api.Get("/GetOilChange/:oilChangeId", middleware.Verify(1), Apis.GetOilChange)

	// Service invoices
	api.Post("/service-invoices", middleware.Verify(1), Controllers.CreateServiceInvoice)
	api.Get("/service-invoices/search", middleware.Verify(1), Controllers.SearchServiceInvoices)
	api.Get("/service-invoices/:id", middleware.Verify(1), Controllers.GetServiceInvoice)
	api.Get("/service-invoices", middleware.Verify(1), Controllers.GetAllServiceInvoices)
	api.Put("/service-invoices/:id", middleware.Verify(1), Controllers.UpdateServiceInvoice)
	api.Delete("/service-invoices/:id", middleware.Verify(1), Controllers.DeleteServiceInvoice)

	// ============================================
	// TIRE MANAGEMENT
	// ============================================
	tires := api.Group("/", middleware.Verify(1))

	// Trucks
	tires.Get("/trucks", Controllers.GetAllTrucks)
	tires.Get("/trucks/:id", Controllers.GetTruck)
	tires.Post("/trucks", Controllers.CreateTruck)
	tires.Put("/trucks/:id", Controllers.UpdateTruck)
	tires.Delete("/trucks/:id", Controllers.DeleteTruck)
	tires.Get("/trucks/:id/positions", Controllers.GetTruckPositions)

	// Tires
	tires.Get("/tires", Controllers.GetAllTires)
	tires.Get("/tires/:id", Controllers.GetTire)
	tires.Post("/tires", Controllers.CreateTire)
	tires.Put("/tires/:id", Controllers.UpdateTire)
	tires.Delete("/tires/:id", Controllers.DeleteTire)
	tires.Get("/tires/search", Controllers.SearchTires)
	api.Post("/dot/ocr", Controllers.EngravedTextOCR)

	// Positions
	tires.Post("/positions/assign", Controllers.AssignTireToPosition)
	tires.Put("/positions/:id/remove-tire", Controllers.RemoveTireFromPosition)
	app.Post("/UpdateTireList", Apis.UpdateTireList)

	// ============================================
	// TRANSPORTER MANAGEMENT
	// ============================================
	api.Post("/RegisterTransporter", middleware.Verify(1), Apis.RegisterTransporter)
	api.Post("/GetTransporters", middleware.Verify(1), Apis.GetTransporters)
	api.Post("/EditTransporter", middleware.Verify(1), Apis.EditTransporter)
	api.Use("/GetTransporterProfileData", middleware.Verify(1), Apis.GetTransporterProfileData)

	// ============================================
	// APPROVAL MANAGEMENT
	// ============================================
	api.Use("/GetPendingRequests", middleware.Verify(1), Apis.GetPendingRequests)
	api.Post("/ApproveRequest", middleware.Verify(1), Apis.ApproveRequest)
	api.Post("/RejectRequest", middleware.Verify(1), Apis.RejectRequest)
	api.Post("/UpdateTempPermission", middleware.Verify(1), Apis.UpdateTempPermission)
	api.Use("/GetNonDriverUsers", middleware.Verify(1), Apis.GetNonDriverUsers)

	// ============================================
	// VEHICLE TRACKING & MONITORING
	// ============================================
	api.Use("/GetVehicleStatus", middleware.Verify(1), Apis.GetVehicleStatus)
	api.Use("/GetVehicleMapPoints", middleware.Verify(1), Apis.GetVehicleMapPoints)
	api.Use("/GetVehicleMilage", middleware.Verify(1), Scrapper.GetVehicleMileageHistory)
	api.Get("/GetVehicleSpeedAlerts", middleware.Verify(1), Controllers.GetVehicleSpeedAlerts)
	protectedApis.Get("/GetVehicleRouteByTrip", Scrapper.GetVehicleRouteByTrip)
	protectedApis.Get("/GetVehicleRouteByDate", Scrapper.GetVehicleRouteByDate)
	protectedApis.Get("/StoreVehicleRouteData", Scrapper.StoreVehicleRouteData)

	// ============================================
	// LOCATION MANAGEMENT
	// ============================================
	api.Get("/GetLocations", middleware.Verify(1), Apis.GetLocations)
	protectedApis.Post("/CreateLocation/", Apis.CreateLocation)
	protectedApis.Post("/CreateTerminal/", Apis.CreateTerminal)

	// ============================================
	// PETRO APP & UTILITIES
	// ============================================
	app.Post("/get-closest-stations", middleware.VerifyAny(), PetroApp.GetClosestStations)
	api.Post("/multi-route-optimizer", MultiRouteOptimizer.OptimalRouteHandler)
	app.Post("/convert/upload", middleware.Verify(1), ETA.ConvertToExcel)
	// ============================================
	// NOTIFICATIONS
	// ============================================
	api.Use("/GetNotifications", middleware.Verify(1), Notifications.ReturnNotifications)

	// ============================================
	// WHATSAPP INTEGRATION
	// ============================================
	protectedApis.Get("/CheckWPLogin", middleware.Verify(4), Whatsapp.CheckWPLogin)
	protectedApis.Get("/GetWhatsAppQRCode", middleware.Verify(4), Whatsapp.GetQRCode)
	protectedApis.Post("/WhatsAppReconnect", middleware.Verify(4), Whatsapp.Reconnect)

	// ============================================
	// SLACK INTEGRATION
	// ============================================
	Slack.RegisterSlackRoutes(app, middleware.Verify(1))
	RegisterViolationRoutes(app)

	// ============================================
	// LOGS & MONITORING (Super Admin Only)
	// ============================================
	api.Get("/logs", middleware.Verify(4), Controllers.GetLogs)
	api.Get("/logs/stats", middleware.Verify(4), Controllers.GetLogStats)
	api.Get("/logs/path/:path", middleware.Verify(4), Controllers.GetLogsByPath)

	// ============================================
	// PREVIEW & DEBUG ROUTES
	// ============================================
	app.Use("/ShowAllDeliveries", PreviewData.ShowAllDailyDeliveries)

	// ============================================
	// STATIC FILE SERVING
	// ============================================
	staticConfig := fiber.Static{
		Browse:        false,
		Index:         "",
		CacheDuration: 24 * time.Hour,
		MaxAge:        86400,
		Compress:      true,
	}

	// Receipts
	static := app.Group("/images", middleware.Verify(1))
	static.Use(compress.New(compress.Config{
		Level: compress.LevelBestCompression,
	}))
	static.Static("/receipts", "./uploads/receipts", staticConfig)

	// Car license images
	static.Static("/car-licenses", "./uploads/car-licenses", staticConfig)
	static.Static("/car-licenses-back", "./uploads/car-licenses-back", staticConfig)

	// Calibration license images
	static.Static("/calibration-licenses", "./uploads/calibration-licenses", staticConfig)
	static.Static("/calibration-licenses-back", "./uploads/calibration-licenses-back", staticConfig)

	// Tank license images
	static.Static("/tank-licenses", "./uploads/tank-licenses", staticConfig)
	static.Static("/tank-licenses-back", "./uploads/tank-licenses-back", staticConfig)

	static.Static("/driver-id-licenses", "./uploads/driver-id-licenses", staticConfig)
	static.Static("/driver-id-licenses-back", "./uploads/driver-id-licenses-back", staticConfig)
	static.Static("/driver-licenses", "./uploads/driver-licenses", staticConfig)
	static.Static("/driver-licenses-back", "./uploads/driver-licenses-back", staticConfig)
	static.Static("/safety-licenses", "./uploads/safety-licenses", staticConfig)
	static.Static("/drug-tests", "./uploads/drug-tests", staticConfig)

	// Static files
	static.Static("/static", "static/", staticConfig)
}

func RegisterViolationRoutes(app *fiber.App) {
	violations := app.Group("/api/violations", middleware.Verify(1))
	violations.Get("/", Controllers.GetViolations)
	violations.Get("/stats", Controllers.GetViolationStats)
	violations.Get("/:id", Controllers.GetViolation)
	violations.Put("/:id", middleware.Verify(3), Controllers.UpdateViolation)
	violations.Delete("/:id", middleware.Verify(3), Controllers.DeleteViolation)
}

func FiberConfig() {
	fmt.Println("Server Up...")
	engine := html.New("./Templates", ".html")

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	// Request logging
	app.Use(middleware.RequestLogger())

	// Compression
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))

	// CORS
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "https://apextransport.ddns.net, http://localhost:3000, http://localhost:5173, http://192.168.1.7:5174, tauri://localhost, https://tauri.localhost",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Device-ID, X-Device-Name, X-Device-Type",
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public routes
	app.Post("/RegisterInstapay", Controllers.RegisterInstapayNew)
	app.Post("/RegisterFinancialNote", Controllers.RegisterFinancialNote)
	app.Post("/api/Login", Controllers.Login)
	app.Get("/api/validate-token", Controllers.ValidateToken)

	// User management
	app.Post("/api/RegisterUser", middleware.Verify(4), Controllers.RegisterUser)
	app.Patch("/api/UpdateUser", middleware.Verify(4), Controllers.UpdateUser)
	app.Get("/api/FetchUsers", middleware.Verify(4), Controllers.FetchUsers)
	app.Delete("/api/DeleteUser", middleware.Verify(4), Controllers.DeleteUser)
	app.Use("/api/User", middleware.Verify(1), Controllers.User)
	app.Use("/api/Logout", middleware.Verify(1), Controllers.Logout)

	SetupRoutes(app, Models.DB)
	SetupEmployeeRoutes(app, Models.DB)
	// Payroll is DEPRECATED and no longer routed. The module was broken and has
	// been withdrawn rather than left half-working.
	//
	// Deliberately a route-level withdrawal, not a data deletion: pay_slips and
	// its related tables are untouched and still readable directly, so nothing
	// historical is destroyed and the decision stays reversible. Re-enable by
	// restoring this call.
	// SetupPaySlipRoutes(app, Models.DB)
	SetupDeprecatedPaySlipRoutes(app)
	app.Listen("127.0.0.1:3001")
}

func SetupTripSummaryRoutes(app *fiber.App) {
	// Trip summary routes - require authentication
	api := app.Group("/api", middleware.Verify(1))

	// Get trip summary by parent trip ID (most common use case)
	// GET /api/parent-trips/:parent_trip_id/summary
	api.Get("/parent-trips/:parent_trip_id/summary", Controllers.GetFullTripSummaryByParentTrip)

	// Get trip summary by its direct ID (alternative endpoint)
	// GET /api/trip-summaries/:summary_id
	api.Get("/trip-summaries/:summary_id", Controllers.GetTripSummaryByID)

	// List all trip summaries with optional filters
	// GET /api/trip-summaries?company=Watanya&session_id=123&page=1&limit=20
	api.Get("/trip-summaries", Controllers.ListTripSummaries)

	// Optional: Trigger manual summary generation (if needed)
	// POST /api/parent-trips/:parent_trip_id/generate-summary
	api.Post("/parent-trips/:parent_trip_id/generate-summary", Controllers.ManuallyGenerateTripSummary)
}

func SetupEmployeeRoutes(app *fiber.App, db *gorm.DB) {
	employeeController := &Controllers.EmployeeController{DB: db}
	creditController := &Controllers.CreditBalanceController{DB: db}

	api := app.Group("/api", middleware.Verify(3))

	// Employee routes
	api.Post("/employees", employeeController.CreateEmployee)
	api.Get("/employees", employeeController.GetAllEmployees)
	api.Get("/employees/:id", employeeController.GetEmployee)
	api.Put("/employees/:id", employeeController.UpdateEmployee)
	api.Delete("/employees/:id", employeeController.DeleteEmployee)

	// Credit balance routes
	api.Get("/credit-balance/:entity_type/:entity_id", creditController.GetCreditBalance)
	api.Get("/credit-transactions/:entity_type/:entity_id", creditController.GetCreditTransactions)
	api.Post("/credit-balance/adjust", creditController.ManualAdjustment)
}

// SetupDeprecatedPaySlipRoutes answers every payroll path with 410 Gone.
//
// A 410 rather than a 404: the endpoints existed and were deliberately removed,
// and any client still calling them should surface a clear message instead of
// looking like a routing bug.
func SetupDeprecatedPaySlipRoutes(app *fiber.App) {
	gone := func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusGone).JSON(fiber.Map{
			"error": "The payroll module has been deprecated and is no longer available.",
			"code":  "payroll_deprecated",
		})
	}
	api := app.Group("/api", middleware.Verify(3))
	api.All("/payslips", gone)
	api.All("/payslips/*", gone)
}

func SetupPaySlipRoutes(app *fiber.App, db *gorm.DB) {
	paySlipController := &Controllers.PaySlipController{DB: db}
	itemsController := &Controllers.PaySlipItemsController{DB: db}
	statsController := &Controllers.PaySlipStatsController{DB: db}

	api := app.Group("/api", middleware.Verify(3))

	// ========== BATCH ENDPOINT ==========
	api.Post("/payslips/batch-create", paySlipController.BatchCreatePaySlip)

	// ========== STATS APIs (Phase 4) ==========
	api.Get("/payslips/driver-stats", statsController.GetDriverStats)
	api.Get("/payslips/employee-stats", statsController.GetEmployeeStats)

	api.Get("/payslips/unpaid-items", paySlipController.GetUnpaidItems)
	api.Post("/payslips/:id/auto-populate", paySlipController.AutoPopulateUnpaidItems)
	api.Post("/payslips/:id/recalculate", paySlipController.RecalculateTotals)
	api.Post("/payslips/:id/finalize", paySlipController.FinalizePaySlip)
	api.Post("/payslips/:id/mark-paid", paySlipController.MarkAsPaid)

	// ========== PAY SLIP CRUD ==========
	api.Post("/payslips/", paySlipController.CreatePaySlip)
	api.Get("/payslips/", paySlipController.GetAllPaySlips)
	api.Get("/payslips/:id", paySlipController.GetPaySlip)
	api.Put("/payslips/:id", paySlipController.UpdatePaySlip)
	api.Delete("/payslips/:id", paySlipController.DeletePaySlip)

	// ========== BUSINESS LOGIC ==========

	// ========== PHASE 4 FEATURES ==========
	api.Post("/payslips/:id/generate-pdf", paySlipController.GeneratePDF)
	api.Get("/payslips/:id/download-pdf", paySlipController.DownloadPDF)
	api.Get("/payslips/:id/audit-trail", paySlipController.GetAuditTrail)
	api.Post("/payslips/:id/duplicate", paySlipController.DuplicatePaySlip)

	// ========== LINE ITEMS (for editing) ==========
	// Income
	api.Post("/payslips/:pay_slip_id/income", itemsController.AddIncomeItem)
	api.Put("/payslips/income/:id", itemsController.UpdateIncomeItem)
	api.Delete("/payslips/income/:id", itemsController.DeleteIncomeItem)

	// Expense
	api.Post("/payslips/:pay_slip_id/expenses", itemsController.AddExpenseItem)
	api.Put("/payslips/expenses/:id", itemsController.UpdateExpenseItem)
	api.Delete("/payslips/expenses/:id", itemsController.DeleteExpenseItem)

	// Deduction
	api.Post("/payslips/:pay_slip_id/deductions", itemsController.AddDeductionItem)
	api.Put("/payslips/deductions/:id", itemsController.UpdateDeductionItem)
	api.Delete("/payslips/deductions/:id", itemsController.DeleteDeductionItem)

	// Adjustment
	api.Post("/payslips/:pay_slip_id/adjustments", itemsController.AddAdjustmentItem)
	api.Put("/payslips/adjustments/:id", itemsController.UpdateAdjustmentItem)
	api.Delete("/payslips/adjustments/:id", itemsController.DeleteAdjustmentItem)
}
