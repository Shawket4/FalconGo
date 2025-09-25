package Slack

import (
	"Falcon/Models"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"gorm.io/gorm"
)

// Task channel ID - set this to your desired Slack channel
const TASK_CHANNEL_ID = "C09H2NAGERG" // Your task management channel ID

// ValidateOilChangeTask checks if all oil changes have been updated today
func ValidateOilChangeTask() (bool, string, []string) {
	log.Println("Starting oil change validation...")

	today := time.Now().Format("2006-01-02")

	var activeCars []Models.Car
	if err := Models.DB.Where("id != ?", 15).Find(&activeCars).Error; err != nil {
		log.Printf("Error fetching active cars: %v", err)
		return false, "Error fetching vehicle records from database", nil
	}

	var missingOilChanges []string
	var updatedCars []string

	for _, car := range activeCars {
		var latestOilChange Models.OilChange
		err := Models.DB.Where("car_id = ?", car.ID).
			Order("id DESC").
			First(&latestOilChange).Error

		if err == gorm.ErrRecordNotFound {
			missingOilChanges = append(missingOilChanges, fmt.Sprintf("%s (No records)", car.CarNoPlate))
			continue
		}

		if err != nil {
			log.Printf("Error fetching latest oil change for car %s: %v", car.CarNoPlate, err)
			missingOilChanges = append(missingOilChanges, fmt.Sprintf("%s (Error)", car.CarNoPlate))
			continue
		}

		changeDate, err := time.Parse("2006-01-02", latestOilChange.Date)
		if err != nil {
			log.Printf("Error parsing oil change date for car %s: %v", car.CarNoPlate, err)
			missingOilChanges = append(missingOilChanges, fmt.Sprintf("%s (Date error)", car.CarNoPlate))
			continue
		}

		recordUpdatedToday := latestOilChange.UpdatedAt.Format("2006-01-02") == today
		oilChangeDateToday := changeDate.Format("2006-01-02") == today

		if recordUpdatedToday || oilChangeDateToday {
			driverName := latestOilChange.DriverName
			if driverName == "" {
				driverName = "Unknown"
			}
			updatedCars = append(updatedCars, fmt.Sprintf("%s (Driver: %s)", car.CarNoPlate, driverName))
		} else {
			lastUpdateDate := latestOilChange.UpdatedAt.Format("2006-01-02")
			missingOilChanges = append(missingOilChanges, fmt.Sprintf("%s (Last: %s)", car.CarNoPlate, lastUpdateDate))
		}
	}

	details := []string{
		fmt.Sprintf("Total vehicles: %d", len(activeCars)),
		fmt.Sprintf("Updated today: %d", len(updatedCars)),
		fmt.Sprintf("Missing updates: %d", len(missingOilChanges)),
	}

	if len(missingOilChanges) > 0 {
		errorMsg := fmt.Sprintf("%d vehicles missing oil change updates", len(missingOilChanges))
		details = append(details, fmt.Sprintf("Missing: %s", strings.Join(missingOilChanges, ", ")))
		return false, errorMsg, details
	}

	successMsg := fmt.Sprintf("All %d vehicles have oil change records updated for today", len(activeCars))
	return true, successMsg, details
}

// GenerateSlackTaskMessage creates the daily task message for Slack
func GenerateSlackTaskMessage() string {
	var message strings.Builder

	today := time.Now()
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	// Header
	message.WriteString("# Daily Operations Checklist\n")
	message.WriteString(fmt.Sprintf("*Date: %s*\n\n", today.Format("January 2, 2006")))
	message.WriteString("---\n\n")

	// Task definitions
	taskDefs := []struct {
		num         int
		name        string
		taskType    string
		description string
	}{
		{1, "Update Oil Change Records", "oil_change", "Update oil change records for all vehicles"},
		{2, "Update Vehicle Status & Driver", "vehicle_status", "Update vehicle status and driver assignments"},
		{3, "Register Missing Trips", "register_missing_trips", "Register any missing trips from previous day"},
		{4, "Register New Trips", "register_new_trips", "Register New Trips (Close at end of night shift at 12 P.M)"},
		{5, "Fill Watanya Report", "watanya_report", "Fill Watanya report for previous day"},
		{6, "Coordinate Driver Shifts", "driver_shifts", "Coordinate driver shift changes"},
		{7, "Track Petrol Arrow Vehicles", "petrol_tracking", "Track petrol arrow vehicles (Qena 1 trip / 2 days & Haykstep 1 / day)"},
		{8, "Register Service Events", "service_events", "Register Service Events today and yesterday (Close at end of night shift)"},
	}

	// Process each task
	for i, taskDef := range taskDefs {
		var task Models.DailyTask
		err := Models.DB.Where("task_type = ? AND assigned_date = ?", taskDef.taskType, todayDate).First(&task).Error

		var status, emoji string
		var isCompleted bool = false

		if err == nil && task.IsCompleted {
			status = "COMPLETED"
			emoji = "✅"
			isCompleted = true
		} else if taskDef.taskType == "oil_change" {
			// Special handling for oil change validation
			isValid, _, _ := ValidateOilChangeTask()
			if isValid {
				status = "READY TO COMPLETE"
				emoji = "🟢"
			} else {
				status = "VALIDATION FAILED"
				emoji = "❌"
			}
		} else {
			status = "PENDING"
			emoji = "⏳"
		}

		message.WriteString(fmt.Sprintf("## **%d. %s** %s\n", taskDef.num, taskDef.name, emoji))
		message.WriteString(fmt.Sprintf("**Status:** %s\n", status))
		message.WriteString(fmt.Sprintf("**Description:** %s\n", taskDef.description))

		if isCompleted {
			// Safe handling of completed task data
			if task.CompletedAt != nil {
				message.WriteString(fmt.Sprintf("**Completed by:** %s at %s\n",
					task.CompletedBy, task.CompletedAt.Format("15:04")))
			} else {
				message.WriteString(fmt.Sprintf("**Completed by:** %s\n", task.CompletedBy))
			}

			if task.ValidationData != "" {
				message.WriteString(fmt.Sprintf("**Result:** %s\n", task.ValidationData))
			}
		} else {
			// Show validation status for oil change task
			if taskDef.taskType == "oil_change" {
				isValid, validationMsg, _ := ValidateOilChangeTask()
				if isValid {
					message.WriteString(fmt.Sprintf("**Validation:** %s\n", validationMsg))
				} else {
					message.WriteString(fmt.Sprintf("**Error:** %s\n", validationMsg))
				}
			}
			message.WriteString(fmt.Sprintf("\n*To complete this task, reply with:* `!complete %d [your_name]`\n", taskDef.num))
		}

		if i < len(taskDefs)-1 {
			message.WriteString("\n---\n\n")
		}
	}

	message.WriteString("\n---\n\n")
	message.WriteString("### **Commands:**\n")
	message.WriteString("- `!complete [number] [your_name]` - Complete a task\n")
	message.WriteString("- `!status` - Show current task status\n")
	message.WriteString("- `!help` - Show help information\n\n")
	message.WriteString("*Tasks with validation will be automatically verified before completion*")

	return message.String()
}

// ProcessSlackTaskCommand handles task commands from Slack
func ProcessSlackTaskCommand(command, channelID, userName string) (string, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	switch strings.ToLower(parts[0]) {
	case "!complete":
		if len(parts) < 3 {
			return "Usage: `!complete [task_number] [your_name]`", nil
		}
		return handleCompleteCommand(parts[1], parts[2])

	case "!status":
		return handleStatusCommand()

	case "!help":
		return handleHelpCommand()

	default:
		return "", fmt.Errorf("unknown command")
	}
}

// handleCompleteCommand processes task completion
func handleCompleteCommand(taskNumStr, employeeName string) (string, error) {
	taskNum, err := strconv.Atoi(taskNumStr)
	if err != nil {
		return "Invalid task number. Use 1-8.", nil
	}

	if taskNum < 1 || taskNum > 8 {
		return "Task number must be 1-8", nil
	}

	today := time.Now()
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	// Define task configurations
	taskConfigs := map[int]struct {
		taskType        string
		taskName        string
		needsValidation bool
	}{
		1: {"oil_change", "Update Oil Change Records", true},
		2: {"vehicle_status", "Update Vehicle Status & Driver", false},
		3: {"register_missing_trips", "Register Missing Trips", false},
		4: {"register_new_trips", "Register New Trips", false},
		5: {"watanya_report", "Fill Watanya Report", false},
		6: {"driver_shifts", "Coordinate Driver Shifts", false},
		7: {"petrol_tracking", "Track Petrol Arrow Vehicles", false},
		8: {"service_events", "Register Service Events", false},
	}

	config := taskConfigs[taskNum]

	// Get or create task
	var task Models.DailyTask
	err = Models.DB.Where("task_type = ? AND assigned_date = ?", config.taskType, todayDate).First(&task).Error

	if err == gorm.ErrRecordNotFound {
		// Create the task
		task = Models.DailyTask{
			TaskName:           config.taskName,
			TaskType:           config.taskType,
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: config.needsValidation,
			ValidationData:     "",
			ValidationError:    "",
		}
		if err := Models.DB.Create(&task).Error; err != nil {
			return fmt.Sprintf("Error creating %s task", config.taskName), err
		}
	} else if err != nil {
		return fmt.Sprintf("Error loading %s task", config.taskName), err
	}

	// Check if already completed
	if task.IsCompleted {
		return fmt.Sprintf("Task %d '%s' is already completed by %s", taskNum, config.taskName, task.CompletedBy), nil
	}

	// Special validation for Task 1 (Oil Change)
	if taskNum == 1 {
		isValid, validationMsg, details := ValidateOilChangeTask()
		if !isValid {
			response := fmt.Sprintf("❌ **Cannot complete Task 1 '%s'**\n%s\n\n", config.taskName, validationMsg)
			if len(details) > 0 {
				response += fmt.Sprintf("**Details:**\n%s\n\n", strings.Join(details, "\n"))
			}
			response += "Please update the missing oil change records first, then try again."

			// Save validation error
			task.ValidationError = validationMsg
			Models.DB.Save(&task)

			return response, nil
		}
		// Store validation success data
		task.ValidationData = validationMsg
		task.ValidationError = ""
	}

	// Mark as completed
	now := time.Now()
	task.CompletedAt = &now
	task.IsCompleted = true
	task.CompletedBy = employeeName

	if err := Models.DB.Save(&task).Error; err != nil {
		return fmt.Sprintf("Error saving %s task completion", config.taskName), err
	}

	// Prepare success message
	successMsg := fmt.Sprintf("✅ **Task %d completed successfully!**\n'%s' marked as completed by %s",
		taskNum, config.taskName, employeeName)

	if taskNum == 1 && task.ValidationData != "" {
		successMsg += fmt.Sprintf("\n\n%s", task.ValidationData)
	}

	return successMsg, nil
}

// handleStatusCommand returns current task status
func handleStatusCommand() (string, error) {
	today := time.Now()
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	taskTypes := []struct {
		name     string
		taskType string
	}{
		{"Oil Change Records", "oil_change"},
		{"Vehicle Status", "vehicle_status"},
		{"Register Missing Trips", "register_missing_trips"},
		{"Register New Trips", "register_new_trips"},
		{"Fill Watanya Report", "watanya_report"},
		{"Coordinate Driver Shifts", "driver_shifts"},
		{"Track Petrol Arrow Vehicles", "petrol_tracking"},
		{"Register Service Events", "service_events"},
	}

	response := fmt.Sprintf("**Daily Task Status - %s**\n\n", today.Format("January 2, 2006"))

	for i, taskInfo := range taskTypes {
		var task Models.DailyTask
		err := Models.DB.Where("task_type = ? AND assigned_date = ?", taskInfo.taskType, todayDate).First(&task).Error

		var taskStatus string
		if err == gorm.ErrRecordNotFound {
			if taskInfo.taskType == "oil_change" {
				isValid, validationMsg, _ := ValidateOilChangeTask()
				if isValid {
					taskStatus = "Ready to complete - " + validationMsg
				} else {
					taskStatus = "Validation failed - " + validationMsg
				}
			} else {
				taskStatus = "Pending"
			}
		} else if err != nil {
			taskStatus = "Error loading task"
		} else if task.IsCompleted {
			if task.CompletedAt != nil {
				taskStatus = fmt.Sprintf("Completed by %s at %s", task.CompletedBy, task.CompletedAt.Format("15:04"))
			} else {
				taskStatus = fmt.Sprintf("Completed by %s", task.CompletedBy)
			}
		} else {
			if taskInfo.taskType == "oil_change" {
				isValid, validationMsg, _ := ValidateOilChangeTask()
				if isValid {
					taskStatus = "Ready to complete - " + validationMsg
				} else {
					taskStatus = "Validation failed - " + validationMsg
				}
			} else {
				taskStatus = "Pending"
			}
		}

		response += fmt.Sprintf("**Task %d - %s:** %s\n", i+1, taskInfo.name, taskStatus)
	}

	response += "\nUse `!complete [number] [your_name]` to complete tasks"
	return response, nil
}

// handleHelpCommand returns help information
func handleHelpCommand() (string, error) {
	help := "**Daily Task System Help**\n\n"
	help += "**Commands:**\n"
	help += "`!complete [number] [your_name]` - Complete a task\n"
	help += "`!status` - Show current task status\n"
	help += "`!help` - Show this help message\n\n"
	help += "**Examples:**\n"
	help += "`!complete 1 Ahmed` - Complete oil change task as Ahmed\n"
	help += "`!complete 5 Sara` - Complete Watanya report task as Sara\n\n"
	help += "**Available Tasks (1-8):**\n"
	help += "1. Update Oil Change Records (requires validation)\n"
	help += "2. Update Vehicle Status & Driver\n"
	help += "3. Register Missing Trips\n"
	help += "4. Register New Trips\n"
	help += "5. Fill Watanya Report\n"
	help += "6. Coordinate Driver Shifts\n"
	help += "7. Track Petrol Arrow Vehicles\n"
	help += "8. Register Service Events\n\n"
	help += "**Notes:**\n"
	help += "- Only Task 1 (Oil Changes) requires validation\n"
	help += "- Tasks 4 and 8 close at end of night shift at 12 PM\n"
	help += "- Task list updates automatically each day at 6 AM"

	return help, nil
}

// SendDailyTasksToSlack sends the task list to Slack
func SendDailyTasksToSlack() error {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("Error loading .env file")
	}
	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not set")
	}

	client := NewSlackClient(slackToken)
	message := GenerateSlackTaskMessage()

	return client.SendAndPinWithCleanup(TASK_CHANNEL_ID, message)
}

// CreateDailyTasks creates all daily tasks
func CreateDailyTasks() error {
	today := time.Now()
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	tasks := []Models.DailyTask{
		{
			TaskName:           "Update Oil Change Records",
			TaskType:           "oil_change",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: true,
			ValidationData:     "",
			ValidationError:    "",
		},
		{
			TaskName:           "Update Vehicle Status & Driver",
			TaskType:           "vehicle_status",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
		{
			TaskName:           "Register Missing Trips",
			TaskType:           "register_missing_trips",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
		{
			TaskName:           "Register New Trips",
			TaskType:           "register_new_trips",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
		{
			TaskName:           "Fill Watanya Report",
			TaskType:           "watanya_report",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
		{
			TaskName:           "Coordinate Driver Shifts",
			TaskType:           "driver_shifts",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
		{
			TaskName:           "Track Petrol Arrow Vehicles",
			TaskType:           "petrol_tracking",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
		{
			TaskName:           "Register Service Events",
			TaskType:           "service_events",
			AssignedDate:       todayDate,
			IsCompleted:        false,
			RequiresValidation: false,
		},
	}

	// Use transaction to ensure atomicity
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("error starting transaction: %v", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, task := range tasks {
		var existingTask Models.DailyTask
		err := tx.Where("task_type = ? AND assigned_date = ?", task.TaskType, todayDate).First(&existingTask).Error

		if err == gorm.ErrRecordNotFound {
			if err := tx.Create(&task).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("error creating %s task: %v", task.TaskType, err)
			}
			log.Printf("Created daily %s task for %s", task.TaskType, todayDate.Format("2006-01-02"))
		} else if err != nil {
			tx.Rollback()
			return fmt.Errorf("error checking existing %s task: %v", task.TaskType, err)
		} else {
			log.Printf("Task %s already exists for %s", task.TaskType, todayDate.Format("2006-01-02"))
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("error committing transaction: %v", err)
	}

	return nil
}

// Legacy function maintained for compatibility - now calls CreateDailyTasks
func CreateDailyOilChangeTask() error {
	return CreateDailyTasks()
}

// ScheduleTaskCreationAndSlack runs at startup and then daily at 6 AM
func ScheduleTaskCreationAndSlack() {
	// Calculate time until next 6 AM
	now := time.Now()
	next6AM := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, now.Location())
	if now.After(next6AM) {
		next6AM = next6AM.Add(24 * time.Hour)
	}

	log.Printf("Task scheduler: Next task creation and Slack update at %s", next6AM.Format("2006-01-02 15:04:05"))

	// Sleep until 6 AM
	time.Sleep(time.Until(next6AM))

	// Create tasks and send to Slack at 6 AM
	if err := CreateDailyTasks(); err != nil {
		log.Printf("Error creating daily tasks at 6 AM: %v", err)
	}

	if err := SendDailyTasksToSlack(); err != nil {
		log.Printf("Error sending daily tasks to Slack at 6 AM: %v", err)
	} else {
		log.Printf("Daily tasks sent to Slack at 6 AM")
	}

	// Continue with daily ticker
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		if err := CreateDailyTasks(); err != nil {
			log.Printf("Error creating daily tasks: %v", err)
		}

		if err := SendDailyTasksToSlack(); err != nil {
			log.Printf("Error sending daily tasks to Slack: %v", err)
		} else {
			log.Printf("Daily tasks sent to Slack at 6 AM")
		}
	}
}

// StartSlackTaskListener starts the Slack socket mode listener
func StartSlackTaskListener() error {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("Error loading .env file")
	}
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")

	if botToken == "" || appToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN and SLACK_APP_TOKEN must be set")
	}

	// Initialize Slack API client
	api := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
		slack.OptionDebug(false),
	)

	// Create socket mode client
	socketClient := socketmode.New(api)

	// Handle events
	go func() {
		for envelope := range socketClient.Events {
			switch envelope.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := envelope.Data.(slackevents.EventsAPIEvent)
				if !ok {
					log.Printf("Unexpected event type: %s", envelope.Type)
					continue
				}

				// Acknowledge the event
				socketClient.Ack(*envelope.Request)

				// Handle message events
				if eventsAPIEvent.Type == slackevents.CallbackEvent {
					innerEvent := eventsAPIEvent.InnerEvent
					if ev, ok := innerEvent.Data.(*slackevents.MessageEvent); ok {
						// Skip bot messages and messages from other channels
						if ev.BotID != "" || ev.Channel != TASK_CHANNEL_ID {
							continue
						}

						// Process task commands
						if strings.HasPrefix(ev.Text, "!") {
							response, err := ProcessSlackTaskCommand(ev.Text, ev.Channel, ev.User)
							if err != nil {
								log.Printf("Error processing command: %v", err)
								continue
							}

							if response != "" {
								// Send response
								_, _, err := api.PostMessage(ev.Channel,
									slack.MsgOptionText(response, false),
								)
								if err != nil {
									log.Printf("Error sending response: %v", err)
								}

								// Update pinned message if task was completed
								if strings.Contains(response, "completed successfully") {
									time.Sleep(2 * time.Second)
									updatedMessage := GenerateSlackTaskMessage()

									slackClient := NewSlackClient(botToken)
									if err := slackClient.SendAndPinWithCleanup(TASK_CHANNEL_ID, updatedMessage); err != nil {
										log.Printf("Error updating pinned message: %v", err)
									}
								}
							}
						}
					}
				}
			}
		}
	}()

	log.Println("Starting Slack task listener...")
	return socketClient.Run()
}

// InitializeSlackTaskSystem starts the complete Slack-integrated task system
func InitializeSlackTaskSystem() error {
	// Create today's tasks at startup if they don't exist
	if err := CreateDailyTasks(); err != nil {
		log.Printf("Warning: Could not create today's tasks: %v", err)
	}

	// Send initial task list to Slack
	if err := SendDailyTasksToSlack(); err != nil {
		log.Printf("Warning: Could not send initial task list to Slack: %v", err)
	} else {
		log.Println("Initial task list sent to Slack")
	}

	// Start daily scheduler
	go ScheduleTaskCreationAndSlack()

	// Start Slack event listener
	go func() {
		if err := StartSlackTaskListener(); err != nil {
			log.Printf("Error starting Slack task listener: %v", err)
		}
	}()

	log.Println("Slack task system initialized successfully")
	return nil
}
