package CronJobs

import (
	"fmt"
	"log"
	"os"
	"time"

	"Falcon/Scrapper"

	"github.com/robfig/cron/v3"
)

// SpeedChecker represents a scheduled speed check service
type SpeedChecker struct {
	cronScheduler  *cron.Cron
	speedThreshold int
	saveToFile     bool
	runImmediately bool
	jobID          cron.EntryID
}

// NewSpeedChecker creates a new speed checker with the given configuration
func NewSpeedChecker(speedThreshold int, saveToFile, runImmediately bool) *SpeedChecker {
	// Create a new speed checker
	return &SpeedChecker{
		cronScheduler:  cron.New(cron.WithSeconds()),
		speedThreshold: speedThreshold,
		saveToFile:     saveToFile,
		runImmediately: runImmediately,
	}
}

// Start initiates the speed checker cron job
func (s *SpeedChecker) Start() error {
	// Add the scheduled task
	var err error
	s.jobID, err = s.cronScheduler.AddFunc("0 0 1 * * *", func() {
		log.Println("Running scheduled daily speed check")
		s.runSpeedCheck()
	})

	if err != nil {
		return fmt.Errorf("error scheduling cron job: %w", err)
	}

	// Start the scheduler
	s.cronScheduler.Start()
	fmt.Println("Speed check scheduler started - will run daily at 1:00 AM")

	// Run immediately if requested
	if s.runImmediately {
		fmt.Println("Running initial speed check")
		s.runSpeedCheck()
	}

	return nil
}

// Stop terminates the speed checker
func (s *SpeedChecker) Stop() {
	if s.cronScheduler != nil {
		s.cronScheduler.Stop()
		log.Println("Speed check scheduler stopped")
	}
}

// UpdateSchedule changes the schedule of the speed checker
// Format: "0 0 1 * * *" = At 01:00:00 AM every day
func (s *SpeedChecker) UpdateSchedule(schedule string) error {
	// Remove existing job
	s.cronScheduler.Remove(s.jobID)

	// Add with new schedule
	var err error
	s.jobID, err = s.cronScheduler.AddFunc(schedule, func() {
		log.Println("Running scheduled speed check")
		s.runSpeedCheck()
	})

	if err != nil {
		return fmt.Errorf("error updating schedule: %w", err)
	}

	log.Printf("Speed check schedule updated to: %s\n", schedule)
	return nil
}

// UpdateThreshold changes the speed threshold
func (s *SpeedChecker) UpdateThreshold(threshold int) {
	s.speedThreshold = threshold
	log.Printf("Speed threshold updated to %d km/h\n", threshold)
}

// RunManualCheck executes a manual speed check
func (s *SpeedChecker) RunManualCheck() {
	log.Println("Running manual speed check")
	s.runSpeedCheck()
}

// runSpeedCheck executes the speed check and handles errors
func (s *SpeedChecker) runSpeedCheck() {
	// Set up log file for this specific run
	s.setupRunLog()
	fmt.Println("reached 1")
	// Run the check
	report, err := Scrapper.RunDailySpeedAlertCheck(s.speedThreshold, s.saveToFile)

	if err != nil {
		log.Printf("Error in speed check: %v\n", err)
	} else {
		log.Println("Successfully completed speed check")

		// Count alerts in the report
		if report == "" {
			log.Println("No speed violations found")
		} else {
			log.Println("Speed violations found! Check the generated report file.")
		}
	}
}

// setupRunLog creates a log file specifically for this run
func (s *SpeedChecker) setupRunLog() {
	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Printf("Error creating logs directory: %v\n", err)
		return
	}

	// Create log file with timestamp in name
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	logFile, err := os.OpenFile(fmt.Sprintf("logs/speed_check_%s.log", timestamp),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

	if err != nil {
		log.Printf("Error opening run log file: %v\n", err)
		return
	}

	// We don't close the file because the log package will continue using it
	log.SetOutput(logFile)
}
