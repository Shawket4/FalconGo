package Alerts

import (
	"Falcon/Models"
	"encoding/json"
	"fmt"
	"strings"
)

// Create detailed WhatsApp message for speed alert
func createSpeedAlertMessage(speedAlert *Models.SpeedAlert) string {
	var messageBuilder strings.Builder

	// Header with severity indicator
	severityIcon := getSeverityIcon(speedAlert.ExceedsBy)
	messageBuilder.WriteString(fmt.Sprintf("%s *SPEED VIOLATION ALERT*\n\n", severityIcon))

	// Vehicle information
	messageBuilder.WriteString(fmt.Sprintf("🚗 *Vehicle:* %s\n", speedAlert.PlateNo))
	messageBuilder.WriteString(fmt.Sprintf("🆔 *Vehicle ID:* %s\n", speedAlert.VehicleID))

	// Time information
	messageBuilder.WriteString(fmt.Sprintf("📅 *Date:* %s\n", speedAlert.ParsedTime.Format("2006-01-02")))
	messageBuilder.WriteString(fmt.Sprintf("🕐 *Time:* %s\n\n", speedAlert.ParsedTime.Format("15:04:05")))

	// Speed violation details
	messageBuilder.WriteString("⚡ *Speed Violation Details:*\n")
	messageBuilder.WriteString(fmt.Sprintf("- Current Speed: %d km/h\n", speedAlert.Speed))
	messageBuilder.WriteString(fmt.Sprintf("- Exceeds Limit By: %d km/h\n", speedAlert.ExceedsBy))
	messageBuilder.WriteString(fmt.Sprintf("- Severity: %s\n\n", getSeverityLevel(speedAlert.ExceedsBy)))

	// Location information
	messageBuilder.WriteString("📍 *Location:*\n")
	messageBuilder.WriteString(fmt.Sprintf("- Latitude: %s\n", speedAlert.Latitude))
	messageBuilder.WriteString(fmt.Sprintf("- Longitude: %s\n", speedAlert.Longitude))

	// Google Maps link
	if speedAlert.Latitude != "" && speedAlert.Longitude != "" {
		mapsLink := fmt.Sprintf("https://maps.google.com/maps?q=%s,%s", speedAlert.Latitude, speedAlert.Longitude)
		messageBuilder.WriteString(fmt.Sprintf("- Maps: %s\n\n", mapsLink))
	} else {
		messageBuilder.WriteString("\n")
	}

	// Action required section
	messageBuilder.WriteString(getActionRequired(speedAlert.ExceedsBy))

	rawMessage := messageBuilder.String()

	// Properly escape for JSON
	jsonBytes, _ := json.Marshal(rawMessage)
	escapedMessage := string(jsonBytes[1 : len(jsonBytes)-1])

	return escapedMessage
}

// Create compact WhatsApp message for minor speed alerts
func createCompactSpeedAlertMessage(speedAlert *Models.SpeedAlert) string {
	severityIcon := getSeverityIcon(speedAlert.ExceedsBy)
	timeFormatted := speedAlert.ParsedTime.Format("15:04")

	message := fmt.Sprintf("%s *SPEED ALERT* | Vehicle: %s | Speed: %d km/h (+%d) | Time: %s | Severity: %s",
		severityIcon,
		speedAlert.PlateNo,
		speedAlert.Speed,
		speedAlert.ExceedsBy,
		timeFormatted,
		getSeverityLevel(speedAlert.ExceedsBy))

	// Add location if available
	if speedAlert.Latitude != "" && speedAlert.Longitude != "" {
		mapsLink := fmt.Sprintf("https://maps.google.com/maps?q=%s,%s", speedAlert.Latitude, speedAlert.Longitude)
		message += fmt.Sprintf(" | Location: %s", mapsLink)
	}

	// Properly escape for JSON
	jsonBytes, _ := json.Marshal(message)
	escapedMessage := string(jsonBytes[1 : len(jsonBytes)-1])

	return escapedMessage
}

// Get severity icon based on speed excess
func getSeverityIcon(exceedsBy int) string {
	switch {
	case exceedsBy >= 50:
		return "🚨" // Critical - Red alert
	case exceedsBy >= 30:
		return "⚠️" // High - Warning
	case exceedsBy >= 15:
		return "🔸" // Medium - Caution
	default:
		return "⚡" // Low - Minor violation
	}
}

// Get severity level text
func getSeverityLevel(exceedsBy int) string {
	switch {
	case exceedsBy >= 50:
		return "CRITICAL"
	case exceedsBy >= 30:
		return "HIGH"
	case exceedsBy >= 15:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// Get action required message based on severity
func getActionRequired(exceedsBy int) string {
	switch {
	case exceedsBy >= 50:
		return "🚨 *IMMEDIATE ACTION REQUIRED*\n- Contact driver immediately\n- Consider vehicle immobilization\n- Review driver training"
	case exceedsBy >= 30:
		return "⚠️ *HIGH PRIORITY*\n- Contact driver for explanation\n- Issue formal warning\n- Monitor closely"
	case exceedsBy >= 15:
		return "🔸 *MODERATE CONCERN*\n- Send warning message to driver\n- Log incident for review"
	default:
		return "⚡ *MINOR VIOLATION*\n- Driver notification sent\n- Monitor for patterns"
	}
}
