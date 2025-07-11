package main

import (
	"Falcon/FiberConfig"
	"Falcon/Models"
	"Falcon/Scrapper"
	"Falcon/Scrapper/Alerts"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// CheckExpirationDates Each Minute
	// go func() {
	// 	for {
	// setupLogging()
	// Scrapper.GetVehicleData()
	// speedChecker := CronJobs.NewSpeedChecker(10, true, true)

	// if err := speedChecker.Start(); err != nil {
	// 	fmt.Printf("Failed to start speed checker: %v", err)
	// } else {
	// 	fmt.Println("Started")
	// }

	// 		// AbstractFunctions.DetectServiceMilage()
	// 		time.Sleep(time.Minute * 10)
	// 	}
	// }()
	// go func() {
	// 	for {
	// 		Notifications.GetExpiringDocuments()
	// 		time.Sleep(time.Hour)
	// 	}
	// }()

	go func() {
		if err := Alerts.InitFirebase(); err != nil {
			log.Fatal("Failed to initialize Firebase:", err)
		}
		for {
			Scrapper.GetVehicleData()
			// time.Sleep(time.Second * 10)
			// Scrapper.CalculateDistanceWorker()
			time.Sleep(time.Minute * 15)
		}
	}()
	// go func() {
	// 	time.Sleep(time.Second * 30)
	// 	for {
	// 		if err := Scrapper.CheckLandMarks(); err != nil {
	// 			log.Println(err)
	// 		}
	// 		time.Sleep(time.Hour * 12)
	// 	}
	// }()

	// Setup routes
	Models.Connect()
	// Scrapper.SetupLandMarks()
	FiberConfig.FiberConfig()
}

func setupLogging() {
	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Printf("Error creating logs directory: %v\n", err)
		return
	}

	// Set up main application log file
	logFile, err := os.OpenFile("logs/application.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

	if err != nil {
		log.Printf("Error opening log file: %v\n", err)
		return
	}

	// Redirect log output to the file
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime)
}
