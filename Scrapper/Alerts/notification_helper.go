package Alerts

import (
	"Falcon/Models"
	"context"
	"fmt"
	"log"
	"strconv"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
	"gorm.io/gorm"
)

// Global Firebase client
var firebaseClient *messaging.Client
var ctx = context.Background()

// Initialize Firebase (call this once at startup)
func InitFirebase() error {
	// Use service account key file - update path to your actual file
	opt := option.WithCredentialsFile("./apex-56555-firebase-adminsdk-fbsvc-ebdbce2bd9.json")

	// Alternative: Use environment variable
	// Set GOOGLE_APPLICATION_CREDENTIALS=/path/to/serviceAccountKey.json
	// and use: opt := option.WithCredentialsFile(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		return fmt.Errorf("error initializing Firebase app: %v", err)
	}

	// Get Firebase Cloud Messaging client
	client, err := app.Messaging(ctx)
	if err != nil {
		return fmt.Errorf("error getting Messaging client: %v", err)
	}

	firebaseClient = client
	log.Println("Firebase initialized successfully")
	return nil
}

// Process new alerts, sending only the highest exceeds_by alert per vehicle (no cooldown)
func ProcessAlertsWithHighestExceed(alerts []Models.SpeedAlert, db *gorm.DB) error {
	// Group alerts by vehicleID
	vehicleAlerts := make(map[string]*Models.SpeedAlert)
	for i := range alerts {
		alert := &alerts[i]
		if existing, ok := vehicleAlerts[alert.VehicleID]; !ok || alert.ExceedsBy > existing.ExceedsBy {
			vehicleAlerts[alert.VehicleID] = alert
		}
	}

	for vehicleID, alert := range vehicleAlerts {
		if err := sendFirebaseNotification(alert); err != nil {
			log.Printf("Error sending notification for vehicle %s: %v", vehicleID, err)
			continue
		}
		alert.AlertedAdmin = true
		if err := db.Save(alert).Error; err != nil {
			log.Printf("Failed to update alert status: %v", err)
		}
		log.Printf("Notification sent for vehicle %s (exceeds by %d km/h)", vehicleID, alert.ExceedsBy)
	}
	return nil
}

// Alternative: Process only new alerts (not yet checked for notification)
func ProcessNewAlertsWithCooldown(cooldownMinutes int, db *gorm.DB) error {
	// Get alerts that haven't been processed for notifications yet
	var alerts []Models.SpeedAlert
	if err := db.Where("alerted_admin IS NULL OR alerted_admin = ?", false).Find(&alerts).Error; err != nil {
		return err
	}

	log.Printf("Processing %d new alerts with %d-minute cooldown", len(alerts), cooldownMinutes)

	return ProcessAlertsWithHighestExceed(alerts, db)
}

// Functional Firebase notification sender
func sendFirebaseNotification(alert *Models.SpeedAlert) error {
	// Check if Firebase client is initialized
	if firebaseClient == nil {
		return fmt.Errorf("firebase client not initialized - call initfirebase() first")
	}

	// Hard-coded FCM token for now (replace with your device's token)
	fcmToken := "cSYz2TO0TK6I-OeFMGkroY:APA91bE0dlEyJvo1CnlFDPhOAhEWAbxrFv1yphjmrsEJ3qwYRXyGjldkyNvRLgnl0w8DFUSFbWA6sOiBarIkBsUuoSIlH-G2gn7qkufg2yrRwDzt2_GtYws"

	// Create the Firebase message
	message := &messaging.Message{
		Token: fcmToken,
		Data: map[string]string{
			"vehicle_id": alert.VehicleID,
			"plate_no":   alert.PlateNo,
			"speed":      strconv.Itoa(alert.Speed),
			"exceeds_by": strconv.Itoa(alert.ExceedsBy),
			"latitude":   alert.Latitude,
			"longitude":  alert.Longitude,
			"timestamp":  alert.Timestamp,
		},
		Notification: &messaging.Notification{
			Title: "🚨 Speed Alert",
			Body: fmt.Sprintf("Vehicle %s (%s) exceeding speed by %d km/h",
				alert.PlateNo, alert.VehicleID, alert.ExceedsBy),
		},
		Android: &messaging.AndroidConfig{
			Notification: &messaging.AndroidNotification{
				Icon:  "speed_alert_icon",
				Color: "#FF0000", // Red color
				Sound: "default",
			},
			Priority: "high", // Move priority to AndroidConfig level
		},
	}

	// Send the message
	response, err := firebaseClient.Send(ctx, message)
	if err != nil {
		return fmt.Errorf("error sending Firebase message: %v", err)
	}

	log.Printf("Successfully sent Firebase notification: %s", response)
	return nil
}
