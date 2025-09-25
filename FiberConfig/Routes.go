package FiberConfig

import (
	"Falcon/Apis"
	"Falcon/Controllers"
	"Falcon/ETA"
	"Falcon/ManipulateData"
	"Falcon/Models"
	"Falcon/MultiRouteOptimizer"
	"Falcon/Notifications"
	"Falcon/PetroApp"
	"Falcon/PreviewData"
	"Falcon/Scrapper"
	"Falcon/Slack"
	"Falcon/middleware"
	"fmt"
	"time"

	// "log"
	"Falcon/Whatsapp"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/template/html"
	"gorm.io/gorm"
	// "github.com/gofiber/websocket/v2"
)

func SetupRoutes(app *fiber.App, db *gorm.DB) {
	// Initialize handlers
	feeMappingHandler := Controllers.NewFeeMappingHandler(db)
	tripHandler := Controllers.NewTripHandler(db)
	vendorController := Controllers.NewVendorController(db)
	transactionController := Controllers.NewTransactionController(db)
	vendorAnalyticsController := Controllers.NewAnalyticsController(db)

	// API group
	api := app.Group("/api")

	// Vendor routes
	vendors := api.Group("/vendors", middleware.Verify(3))
	vendors.Get("/", vendorController.GetVendors)
	vendors.Post("/", vendorController.CreateVendor)
	vendors.Get("/:id", vendorController.GetVendor)
	vendors.Put("/:id", vendorController.UpdateVendor)
	vendors.Delete("/:id", vendorController.DeleteVendor)
	vendors.Get("/:id/balance", vendorController.GetVendorBalance)

	// Transaction routes under vendors
	vendors.Get("/:vendor_id/transactions", transactionController.GetVendorTransactions)
	vendors.Post("/:vendor_id/transactions", transactionController.CreateTransaction)

	// Direct transaction routes
	transactions := api.Group("/transactions", middleware.Verify(3))
	transactions.Get("/:id", transactionController.GetTransaction)
	transactions.Put("/:id", transactionController.UpdateTransaction)
	transactions.Delete("/:id", transactionController.DeleteTransaction)

	// Analytics routes
	analytics := api.Group("/analytics", middleware.Verify(3))
	analytics.Get("/summary", vendorAnalyticsController.Summary)
	analytics.Get("/monthly", vendorAnalyticsController.MonthlyTransactions)
	analytics.Get("/top-vendors", vendorAnalyticsController.TopVendors)
	analytics.Get("/recent-activity", vendorAnalyticsController.RecentActivity)

	// Fee Mapping routes
	mappings := api.Group("/mappings", middleware.Verify(1))
	mappings.Get("/", feeMappingHandler.GetAllFeeMappings)

	// Helper routes for dropdowns - place these BEFORE the ID route to avoid conflicts
	mappings.Get("/companies", feeMappingHandler.GetCompaniesList)
	mappings.Get("/terminals/:company", feeMappingHandler.GetTerminalsByCompany)
	mappings.Get("/dropoffs/:company/:terminal", feeMappingHandler.GetDropOffPointsByTerminal)
	mappings.Get("/fee", feeMappingHandler.GetFeeByMapping)

	// ID-based routes
	mappings.Get("/:id", feeMappingHandler.GetFeeMapping)
	mappings.Post("/", middleware.Verify(3), feeMappingHandler.CreateFeeMapping)
	mappings.Put("/:id", middleware.Verify(3), feeMappingHandler.UpdateFeeMapping)
	mappings.Delete("/:id", middleware.Verify(3), feeMappingHandler.DeleteFeeMapping)

	// Trip routes
	trips := api.Group("/trips", middleware.Verify(1))
	trips.Get("/", tripHandler.GetAllTrips)
	app.Get("/api/stats/widget-data", tripHandler.GetGlobalStats)
	app.Post("/api/UpdateToken", Models.UpdateToken)
	trips.Get("/statistics", tripHandler.GetTripStatistics)
	trips.Get("/watanya/driver-analytics", tripHandler.GetWatanyaDriverAnalytics)
	trips.Get("/date", tripHandler.GetTripsByDate)
	trips.Get("/:id", tripHandler.GetTrip)
	trips.Post("/", tripHandler.CreateTrip)
	trips.Put("/:id", tripHandler.UpdateTrip)
	trips.Get("/:id/details", tripHandler.GetTripDetails)
	trips.Delete("/:id", tripHandler.DeleteTrip)

	// Additional trip routes for filtering and stats
	trips.Get("/company/:company", tripHandler.GetTripsByCompany)
	trips.Post("/watanya/export_report", middleware.Verify(3), tripHandler.GetWatanyaTripsReport)
	app.Post("/api/multi-route-optimizer", MultiRouteOptimizer.OptimalRouteHandler)
}

func FiberConfig() {
	fmt.Println("Server Up...")
	engine := html.New("./Templates", ".html")
	// Html Template engine
	app := fiber.New(fiber.Config{
		Views: engine,
	})
	app.Use(middleware.RequestLogger())
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestCompression, // 2
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*", // Allow all origins
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With",
		AllowCredentials: true, // Important for cookies
		MaxAge:           300,  // Max age for preflight requests caching (5 minutes)
	}))

	SetupRoutes(app, Models.DB)
	app.Post("/RegisterInstapay", Controllers.RegisterInstapayNew)
	app.Post("/RegisterFinancialNote", Controllers.RegisterFinancialNote)
	app.Static("/static", "static/")
	app.Post("/api/RegisterUser", middleware.Verify(4), Controllers.RegisterUser)
	app.Patch("/api/UpdateUser", middleware.Verify(4), Controllers.UpdateUser)
	app.Get("/api/FetchUsers", middleware.Verify(4), Controllers.FetchUsers)
	app.Delete("/api/DeleteUser", middleware.Verify(4), Controllers.DeleteUser)
	app.Post("/api/RegisterCar", Apis.RegisterCar)
	app.Post("/api/RegisterTransporter", Apis.RegisterTransporter)
	app.Post("/api/Login", Controllers.Login)
	app.Get("/api/validate-token", Controllers.ValidateToken)
	app.Use("/api/User", Controllers.User)
	app.Use("/api/Logout", Controllers.Logout)
	app.Get("/api/GetVehicleSpeedViolations", middleware.Verify(1), Controllers.GetVehicleSpeedViolations)

	// Logs API routes
	app.Get("/api/logs", middleware.Verify(4), Controllers.GetLogs)
	app.Get("/api/logs/stats", middleware.Verify(4), Controllers.GetLogStats)
	app.Get("/api/logs/path/:path", middleware.Verify(4), Controllers.GetLogsByPath)

	// app.Get("/ShowAllServiceEvents", adaptor.HTTPHandlerFunc(PreviewData.ShowAllServiceEvents))
	app.Post("/api/removedata", ManipulateData.DeleteData)
	app.Post("/api/editdata", ManipulateData.EditData)
	//app.Post("/api/GetDriverTrip", Controllers.GetDriverTrip)
	// app.Post("/api/NextStep", Controllers.NextStep)
	// app.Post("/api/PreviousStep", Controllers.PreviousStep)
	// app.Post("/api/CompleteTrip", Controllers.CompleteTrip)
	app.Get("/api/GetCars", Apis.GetCars)
	app.Post("/api/GetCar", Apis.GetCar)
	app.Get("/api/GetDrivers", Apis.GetDrivers)
	app.Post("/api/GetDriver", Apis.GetDriver)
	app.Post("/api/GetTransporters", Apis.GetTransporters)
	app.Use("/api/GetCarProfileData", Apis.GetCarProfileData)
	app.Use("/api/GetDriverProfileData", Apis.GetDriverProfileData)
	app.Use("/api/GetTransporterProfileData", Apis.GetTransporterProfileData)
	app.Post("/api/DeleteDriver", Apis.DeleteDriver)
	app.Post("/api/DeleteCar", Apis.DeleteCar)
	app.Post("/api/UpdateCar", Apis.UpdateCar)
	app.Post("/api/EditTransporter", Apis.EditTransporter)
	app.Use("/api/GetPendingRequests", Apis.GetPendingRequests)
	app.Post("/api/ApproveRequest", Apis.ApproveRequest)
	app.Post("/api/RejectRequest", Apis.RejectRequest)
	app.Post("/api/UpdateTempPermission", Apis.UpdateTempPermission)
	app.Use("/api/GetNonDriverUsers", Apis.GetNonDriverUsers)
	// app.Use("/AddServiceEvent", adaptor.HTTPHandlerFunc(AddEvent.AddServiceEventTmpl))
	app.Post("/api/CreateServiceEvent", Apis.CreateServiceEvent)
	app.Post("/api/EditServiceEvent", Apis.EditServiceEvent)
	app.Post("/api/DeleteServiceEvent", Apis.DeleteServiceEvent)
	app.Get("/api/GetAllServiceEvents", Apis.GetAllServiceEvents)
	app.Post("/api/CreateOilChange", Apis.CreateOilChange)
	app.Post("/api/EditOilChange", Apis.EditOilChange)
	app.Post("/api/DeleteOilChange", middleware.Verify(1), Apis.DeleteOilChange)
	app.Get("/api/GetAllOilChanges", middleware.Verify(1), Apis.GetAllOilChanges)
	app.Get("/api/GetOilChange/:oilChangeId", middleware.Verify(1), Apis.GetOilChange)
	app.Use("/api/GetVehicleStatus", Apis.GetVehicleStatus)
	app.Use("/api/GetVehicleMapPoints", Apis.GetVehicleMapPoints)

	app.Use("/api/GetVehicleMilage", Scrapper.GetVehicleMileageHistory)
	app.Get("/api/GetLocations", Apis.GetLocations)
	app.Post("/api/RegisterDriverLoan", middleware.Verify(3), Apis.RegisterDriverLoan)
	app.Post("/api/loans/stats", Apis.FetchLoanStats)
	app.Post("/api/RegisterDriverExpense", Apis.RegisterDriverExpense)
	app.Post("/api/RegisterDriverSalary", Apis.RegisterDriverSalary)
	app.Post("/api/GetDriverSalaryPreview", Apis.GetDriverSalaryPreview)
	app.Post("/api/GetDriverSalaries", Apis.GetDriverSalaries)
	app.Post("/api/DeleteExpense", Apis.DeleteExpense)
	app.Post("/api/DeleteLoan", middleware.Verify(3), Apis.DeleteLoan)
	app.Post("/api/GetDriverExpenses", Apis.GetDriverExpenses)
	app.Post("/api/GetTripExpenses", Apis.GetTripExpenses)
	app.Post("/api/GetDriverLoans", Apis.GetDriverLoans)

	app.Post("/api/GetTripLoans", Apis.GetTripLoans)
	// app.Post("/api/CalculateDriverSalary", Apis.CalculateDriverSalary)
	app.Use("/api/GetNotifications", Notifications.ReturnNotifications)
	protectedApis := app.Group("/api/protected/", middleware.Verify(1))
	// protectedApis := app.Group("/api/protected/")
	protectedApis.Post("/CreateLocation/", Apis.CreateLocation)
	// protectedApis.Post("/GetCarExpenses", Apis.GetCarExpenses)
	protectedApis.Post("/CreateTerminal/", Apis.CreateTerminal)
	protectedApis.Patch("/SetCarDriverPair", Apis.SetCarDriverPair)
	protectedApis.Post("/GetPhotoAlbum", Apis.GetPhotoAlbum)
	protectedApis.Post("/RegisterDriver", Controllers.RegisterDriver)
	protectedApis.Post("/UpdateDriver", Controllers.UpdateDriver)
	app.Post("/convert/upload", ETA.ConvertToExcel)
	//protectedApis.Use(middleware.Verify)
	handler := &Apis.FuelHandler{DB: Models.DB}

	// Group fuel routes under /api/fuel
	fuel := app.Group("/api/fuel", middleware.Verify(1))
	fuel.Get("/statistics", handler.GetFuelStatistics)
	fuel.Get("/sync-car-odometers", middleware.Verify(3), Apis.SyncCarLastFuelOdometer)
	protectedApis.Post("/AddFuelEvent", middleware.Verify(3), Apis.AddFuelEvent)
	protectedApis.Post("/EditFuelEvent", middleware.Verify(3), Apis.EditFuelEvent)
	protectedApis.Post("/DeleteFuelEvent", middleware.Verify(3), Apis.DeleteFuelEvent)
	protectedApis.Get("/GetFuelEvents", Apis.GetFuelEvents, middleware.Verify(1))
	protectedApis.Get("/GetFuelEventById/:id", Apis.GetFuelEventById, middleware.Verify(1))
	protectedApis.Get("/CheckWPLogin", Whatsapp.CheckWPLogin, middleware.Verify(4))
	protectedApis.Get("/GetWhatsAppQRCode", Whatsapp.GetQRCode, middleware.Verify(4))
	protectedApis.Get("/GetVehicleRouteByTrip", Scrapper.GetVehicleRouteByTrip, middleware.Verify(1))
	protectedApis.Get("/GetVehicleRouteByDate", Scrapper.GetVehicleRouteByDate, middleware.Verify(1))
	protectedApis.Get("/StoreVehicleRouteData", Scrapper.StoreVehicleRouteData, middleware.Verify(1))
	// app.Post("/api/GenerateReceipt", Apis.GenerateCSVReceipt)
	// app.Use("/api/AddCar", AddEvent.AddCarHandler)
	// app.Use("/api/AddServiceEvent", AddEvent.AddCarHandler)
	app.Use("/ShowAllDeliveries", PreviewData.ShowAllDailyDeliveries)
	// Serve Static Images
	app.Static("/CarLicenses", "./CarLicenses", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/CarLicensesBack", "./CarLicensesBack", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/CalibrationLicenses", "./CalibrationLicenses", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/CalibrationLicensesBack", "./CalibrationLicensesBack", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/DriverLicenses", "./DriverLicenses", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/SafetyLicenses", "./SafetyLicenses", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/DrugTests", "./DrugTests", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/CriminalRecords", "./CriminalRecords", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/IDLicenses", "./IDLicenses", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/IDLicensesBack", "./IDLicensesBack", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/TankLicenses", "./TankLicenses", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/TankLicensesBack", "./TankLicensesBack", fiber.Static{Compress: true, CacheDuration: time.Second * 10})
	app.Static("/ServiceProofs", "./ServiceProofs", fiber.Static{Compress: true, CacheDuration: time.Second * 10})

	app.Post("/UpdateTireList", Apis.UpdateTireList)

	api := app.Group("/api", middleware.Verify(1))

	// Truck routes
	api.Get("/trucks", Controllers.GetAllTrucks)
	api.Get("/trucks/:id", Controllers.GetTruck)
	api.Post("/trucks", Controllers.CreateTruck)
	api.Put("/trucks/:id", Controllers.UpdateTruck)
	api.Delete("/trucks/:id", Controllers.DeleteTruck)

	// Tire routes
	api.Get("/tires", Controllers.GetAllTires)
	api.Get("/tires/:id", Controllers.GetTire)
	api.Post("/tires", Controllers.CreateTire)
	api.Put("/tires/:id", Controllers.UpdateTire)
	api.Delete("/tires/:id", Controllers.DeleteTire)
	api.Get("/tires/search", Controllers.SearchTires)
	app.Post("/api/dot/ocr", Controllers.EngravedTextOCR)

	// Position routes
	api.Get("/trucks/:id/positions", Controllers.GetTruckPositions)
	api.Post("/positions/assign", Controllers.AssignTireToPosition)
	api.Put("/positions/:id/remove-tire", Controllers.RemoveTireFromPosition)
	app.Post("/get-closest-stations", PetroApp.GetClosestStations, middleware.Verify(1))

	app.Post("/api/service-invoices", Controllers.CreateServiceInvoice, middleware.Verify(1))
	app.Get("/api/service-invoices/:id", Controllers.GetServiceInvoice, middleware.Verify(1))
	app.Get("/api/service-invoices", Controllers.GetAllServiceInvoices, middleware.Verify(1))
	app.Get("/api/cars/:carId/service-invoices", Controllers.GetServiceInvoicesByCarID, middleware.Verify(1))
	app.Put("/api/service-invoices/:id", Controllers.UpdateServiceInvoice, middleware.Verify(1))
	app.Delete("/api/service-invoices/:id", Controllers.DeleteServiceInvoice, middleware.Verify(1))
	Slack.RegisterSlackRoutes(app, middleware.Verify(1))
	// WebSocket
	// app.Use("/ws", func(c *fiber.Ctx) error {
	// 	// IsWebSocketUpgrade returns true if the client
	// 	// requested upgrade to the WebSocket protocol.
	// 	if websocket.IsWebSocketUpgrade(c) {
	// 		c.Locals("allowed", true)
	// 		return c.Next()
	// 	}
	// 	return fiber.ErrUpgradeRequired
	// })

	// app.Get("/api/ws/", websocket.New(func(c *websocket.Conn) {
	// 	// c.Locals is added to the *websocket.Conn
	// 	log.Println(c.Locals("allowed")) // true

	// 	// websocket.Conn bindings https://pkg.go.dev/github.com/fasthttp/websocket?tab=doc#pkg-index
	// 	var (
	// 		mt  int
	// 		msg []byte
	// 		err error
	// 	)

	// 	for {
	// 		if mt, msg, err = c.ReadMessage(); err != nil {
	// 			log.Println("read:", err)
	// 			break
	// 		}
	// 		log.Printf("recv: %s", msg)

	// 		if err = c.WriteMessage(mt, msg); err != nil {
	// 			log.Println("write:", err)
	// 			break
	// 		}
	// 	}

	// }))
	// app.ListenTLS(":3001", "/etc/letsencrypt/live/apextransport.ddns.net/fullchain.pem", "/etc/letsencrypt/live/apextransport.ddns.net/privkey.pem")
	app.Listen(":3001")
}
