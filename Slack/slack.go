package Slack

import (
	"Falcon/Models"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// SlackClient holds the Slack bot token and base URL
type SlackClient struct {
	Token   string
	BaseURL string
}

// SlackMessage represents a message payload
type SlackMessage struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
	Parse   string `json:"parse,omitempty"`
}

// SlackResponse represents the API response
type SlackResponse struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel,omitempty"`
	TS      string `json:"ts,omitempty"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// PinMessageRequest represents the pin message payload
type PinMessageRequest struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
}

// ChannelMessage represents a message from channel history
type ChannelMessage struct {
	TS      string `json:"ts"`
	Text    string `json:"text"`
	BotID   string `json:"bot_id,omitempty"`
	User    string `json:"user,omitempty"`
	Subtype string `json:"subtype,omitempty"`
}

// FleetVehicle represents a single truck for fleet tracking
type FleetVehicle struct {
	PlateNumber string    `json:"plate_number"`
	Driver      string    `json:"driver"`
	Area        string    `json:"area"`
	Status      string    `json:"status"`
	Location    string    `json:"location"`
	LastUpdate  time.Time `json:"last_update"`
}

// NewSlackClient creates a new Slack client
// Required Bot Token Scopes:
// - chat:write (send messages)
// - pins:write (pin/unpin messages)
// - pins:read (list pinned messages)
// - channels:history (read channel messages)
// - chat:write.public (send to channels without being invited)
// - channels:manage (create/manage public channels)
func NewSlackClient(token string) *SlackClient {
	return &SlackClient{
		Token:   token,
		BaseURL: "https://slack.com/api",
	}
}

// SendMessage sends a message to a Slack channel
func (s *SlackClient) SendMessage(channel, message string) (*SlackResponse, error) {
	payload := SlackMessage{
		Channel: channel,
		Text:    message,
		Parse:   "full",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON: %v", err)
	}

	url := fmt.Sprintf("%s/chat.postMessage", s.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %v", err)
	}

	var slackResp SlackResponse
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return nil, fmt.Errorf("error unmarshaling response: %v", err)
	}

	if !slackResp.OK {
		return &slackResp, fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	return &slackResp, nil
}

// PinMessage pins a message to a channel
func (s *SlackClient) PinMessage(channel, timestamp string) error {
	payload := PinMessageRequest{
		Channel:   channel,
		Timestamp: timestamp,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %v", err)
	}

	url := fmt.Sprintf("%s/pins.add", s.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %v", err)
	}

	var slackResp SlackResponse
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return fmt.Errorf("error unmarshaling response: %v", err)
	}

	if !slackResp.OK {
		switch slackResp.Error {
		case "no_permission":
			return fmt.Errorf("bot lacks 'pins:write' permission")
		case "channel_not_found":
			return fmt.Errorf("channel '%s' not found or bot not in channel", channel)
		case "message_not_found":
			return fmt.Errorf("message with timestamp '%s' not found", timestamp)
		case "already_pinned":
			return nil // Already pinned, not an error
		default:
			return fmt.Errorf("slack API error: %s", slackResp.Error)
		}
	}

	return nil
}

// UnpinMessage unpins a message from a channel
func (s *SlackClient) UnpinMessage(channel, timestamp string) error {
	payload := PinMessageRequest{
		Channel:   channel,
		Timestamp: timestamp,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %v", err)
	}

	url := fmt.Sprintf("%s/pins.remove", s.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %v", err)
	}

	var slackResp SlackResponse
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return fmt.Errorf("error unmarshaling response: %v", err)
	}

	if !slackResp.OK && slackResp.Error != "no_pin" {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	return nil
}

// GetPinnedMessages gets all pinned messages from a channel
func (s *SlackClient) GetPinnedMessages(channel string) ([]string, error) {
	url := fmt.Sprintf("%s/pins.list?channel=%s", s.BaseURL, channel)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %v", err)
	}

	var response struct {
		OK    bool `json:"ok"`
		Items []struct {
			Message struct {
				TS   string `json:"ts"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"items"`
		Error string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error unmarshaling response: %v", err)
	}

	if !response.OK {
		return nil, fmt.Errorf("slack API error: %s", response.Error)
	}

	var timestamps []string
	for _, item := range response.Items {
		timestamps = append(timestamps, item.Message.TS)
	}

	return timestamps, nil
}

// DeleteMessage deletes a message
func (s *SlackClient) DeleteMessage(channel, timestamp string) error {
	payload := map[string]string{
		"channel": channel,
		"ts":      timestamp,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %v", err)
	}

	url := fmt.Sprintf("%s/chat.delete", s.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %v", err)
	}

	var slackResp SlackResponse
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return fmt.Errorf("error unmarshaling response: %v", err)
	}

	if !slackResp.OK {
		switch slackResp.Error {
		case "message_not_found":
			return nil // Message already deleted
		case "cant_delete_message":
			return fmt.Errorf("cannot delete message (might be too old)")
		default:
			return fmt.Errorf("slack API error: %s", slackResp.Error)
		}
	}

	return nil
}

// GetChannelHistory gets recent messages from a channel
func (s *SlackClient) GetChannelHistory(channel string, limit int) ([]ChannelMessage, error) {
	url := fmt.Sprintf("%s/conversations.history?channel=%s&limit=%d", s.BaseURL, channel, limit)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %v", err)
	}

	var response struct {
		OK       bool `json:"ok"`
		Messages []struct {
			TS      string `json:"ts"`
			Text    string `json:"text"`
			BotID   string `json:"bot_id,omitempty"`
			User    string `json:"user,omitempty"`
			Subtype string `json:"subtype,omitempty"`
		} `json:"messages"`
		Error string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error unmarshaling response: %v", err)
	}

	if !response.OK {
		return nil, fmt.Errorf("slack API error: %s", response.Error)
	}

	var messages []ChannelMessage
	for _, msg := range response.Messages {
		messages = append(messages, ChannelMessage{
			TS:      msg.TS,
			Text:    msg.Text,
			BotID:   msg.BotID,
			User:    msg.User,
			Subtype: msg.Subtype,
		})
	}

	return messages, nil
}

// SendAndPinWithCleanup sends a message, pins it, and removes all other bot messages
// This is the main function to call from other packages
func (s *SlackClient) SendAndPinWithCleanup(channel, message string) error {
	fmt.Printf("Cleaning channel and sending new message to %s...\n", channel)

	// Step 1: Get channel history and check if message content is different
	messages, err := s.GetChannelHistory(channel, 100)
	if err != nil {
		fmt.Printf("Warning: Could not get channel history: %v\n", err)
	} else {
		// Check if the most recent bot message has the same content
		for _, msg := range messages {
			if msg.BotID != "" {
				// Compare message content (excluding timestamps)
				if messagesAreEqual(msg.Text, message) {
					fmt.Printf("Message content unchanged, skipping update for %s\n", channel)
					return nil // Skip sending if content is the same
				}
				break // Only check the most recent bot message
			}
		}

		// Delete all bot messages if we're sending a new one
		botMessageCount := 0
		for _, msg := range messages {
			if msg.BotID != "" {
				if err := s.DeleteMessage(channel, msg.TS); err != nil {
					fmt.Printf("Could not delete message %s: %v\n", msg.TS, err)
				} else {
					botMessageCount++
				}
				time.Sleep(200 * time.Millisecond) // Rate limiting
			}
		}
		if botMessageCount > 0 {
			fmt.Printf("Deleted %d old bot messages\n", botMessageCount)
		}
	}

	// Step 2: Unpin all existing pinned messages
	pinnedMessages, err := s.GetPinnedMessages(channel)
	if err != nil {
		fmt.Printf("Warning: Could not get pinned messages: %v\n", err)
	} else {
		for _, timestamp := range pinnedMessages {
			if err := s.UnpinMessage(channel, timestamp); err != nil {
				fmt.Printf("Could not unpin message %s: %v\n", timestamp, err)
			}
		}
	}

	// Step 3: Send new message
	fmt.Printf("Sending message to %s...\n", channel)
	resp, err := s.SendMessage(channel, message)
	if err != nil {
		return fmt.Errorf("error sending message: %v", err)
	}

	fmt.Printf("Message sent! Timestamp: %s\n", resp.TS)

	// Step 4: Pin the new message
	time.Sleep(1 * time.Second)
	if err := s.PinMessage(channel, resp.TS); err != nil {
		fmt.Printf("Warning: Message sent but pinning failed: %v\n", err)
		return nil
	}

	fmt.Printf("Message pinned successfully in %s\n", channel)
	return nil
}

// messagesAreEqual compares two messages, ignoring timestamp differences
func messagesAreEqual(oldMessage, newMessage string) bool {
	// Remove timestamp lines from both messages for comparison
	oldFiltered := removeTimestampLines(oldMessage)
	newFiltered := removeTimestampLines(newMessage)

	return strings.TrimSpace(oldFiltered) == strings.TrimSpace(newFiltered)
}

// removeTimestampLines removes lines containing timestamps and "Last Updated" for comparison
func removeTimestampLines(message string) string {
	lines := strings.Split(message, "\n")
	var filteredLines []string

	for _, line := range lines {
		// Skip lines containing timestamps or "Last Updated"
		if !strings.Contains(line, "*Last Updated:") &&
			!strings.Contains(line, "**Last Update:**") {
			filteredLines = append(filteredLines, line)
		}
	}

	return strings.Join(filteredLines, "\n")
}

// ManualUpdateVehicleStatus - API function for manual status updates
func ManualUpdateVehicleStatus(carID uint, newStatus, loaction, updatedBy string) error {
	var car Models.Car
	if err := Models.DB.Preload("Driver").First(&car, carID).Error; err != nil {
		return fmt.Errorf("car not found: %v", err)
	}

	// Store previous status for comparison
	previousStatus := car.SlackStatus
	car.Location = loaction
	// Update status manually
	car.SlackStatus = newStatus
	car.LastUpdatedSlackStatus = time.Now()

	// Save to database
	if err := Models.DB.Save(&car).Error; err != nil {
		return fmt.Errorf("error updating car: %v", err)
	}

	log.Printf("Manual status update: %s changed from '%s' to '%s' by %s",
		car.CarNoPlate, previousStatus, newStatus, updatedBy)

	// Only trigger Slack update if status actually changed
	if previousStatus != newStatus {
		// Get all cars for this company and send update
		var allCars []Models.Car
		if err := Models.DB.Preload("Driver").Where("operating_company = ?", car.OperatingCompany).Find(&allCars).Error; err != nil {
			return fmt.Errorf("error fetching company cars: %v", err)
		}

		// Filter recent cars
		var recentCars []Models.Car
		for _, c := range allCars {
			if c.LocationTimeStamp != "" {
				if parsedTime, err := time.Parse("2006-01-02 15:04:05", c.LocationTimeStamp); err == nil {
					if time.Since(parsedTime) <= 24*time.Hour {
						recentCars = append(recentCars, c)
					}
				}
			}
		}

		// Send Slack update for this company
		carsByCompany := map[string][]Models.Car{
			strings.ToLower(car.OperatingCompany): recentCars,
		}

		if err := SendFleetUpdatesToSlack(carsByCompany); err != nil {
			log.Printf("Error sending manual update to Slack: %v", err)
			return err
		}

		log.Printf("Slack update sent for manual status change of %s", car.CarNoPlate)
	}

	return nil
}

// Company to Slack channel mapping
var CompanyChannelMap = map[string]string{
	"petrol_arrows": "C09GSBV2TSR",
	"taqa":          "C09H1DFP21J",
	"watanya":       "C09GW6QNT46",
}

func SendFleetUpdatesToSlack(carsByCompany map[string][]Models.Car) error {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("Error loading .env file")
	}
	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not set")
	}

	client := NewSlackClient(slackToken)

	for company, cars := range carsByCompany {
		if len(cars) == 0 {
			continue
		}

		// Get channel ID from mapping
		channelID, exists := CompanyChannelMap[company]
		if !exists {
			log.Printf("No channel mapping found for company: %s", company)
			continue
		}

		// Generate message
		message := generateSlackMessage(cars, company)

		log.Printf("Sending %s fleet status to channel %s (%d vehicles)", company, channelID, len(cars))

		if err := client.SendAndPinWithCleanup(channelID, message); err != nil {
			log.Printf("Error sending to channel %s: %v", channelID, err)
		} else {
			log.Printf("Successfully sent %s fleet status", company)
		}
	}

	return nil
}

func generateSlackMessage(cars []Models.Car, company string) string {
	var message strings.Builder

	// Header
	message.WriteString(fmt.Sprintf("# %s FLEET STATUS\n", strings.ToUpper(company)))
	message.WriteString(fmt.Sprintf("*Last Updated: %s*\n\n", time.Now().Format("January 2, 2006 - 15:04:05 MST")))
	message.WriteString("---\n\n")

	// Vehicle details
	for _, car := range cars {
		// Get driver name
		driverName := "Unknown Driver"
		if car.Driver.Name != "" {
			driverName = car.Driver.Name
		}

		// Determine location to display
		displayLocation := car.Location
		if car.GeoFence != "" {
			// Check if it's a terminal, garage, or drop-off point
			for _, geofence := range AllGeoFences {
				if geofence.Name == car.GeoFence {
					if geofence.Type == "garage" {
						displayLocation = "Garage - Parked"
					} else if geofence.Type == "terminal" {
						displayLocation = fmt.Sprintf("%s", geofence.Name)
					}
					break
				}
			}
			// If not found in static geofences, it might be a drop-off point
			if displayLocation == car.Location {
				displayLocation = fmt.Sprintf("%s - Drop Off Point", car.GeoFence)
			}
		}

		// Get status emoji
		statusEmoji := getStatusEmoji(car.SlackStatus)

		// Generate Google Maps link
		mapsLink := ""
		if car.Latitude != "" && car.Longitude != "" {
			mapsLink = fmt.Sprintf("https://maps.google.com/?q=%s,%s", car.Latitude, car.Longitude)
		}

		message.WriteString(fmt.Sprintf("## **%s**\n", car.CarNoPlate))
		message.WriteString(fmt.Sprintf("**Driver:** %s  \n", driverName))
		message.WriteString(fmt.Sprintf("**Area:** %s  \n", car.OperatingArea))
		message.WriteString(fmt.Sprintf("**Status:** %s %s  \n", car.SlackStatus, statusEmoji))
		message.WriteString(fmt.Sprintf("**Location:** %s  \n", displayLocation))

		// Add Google Maps link if coordinates are available
		if mapsLink != "" {
			message.WriteString(fmt.Sprintf("**Maps:** [View Location](%s)  \n", mapsLink))
		}

		// Parse and format timestamp
		if car.LocationTimeStamp != "" {
			if parsedTime, err := time.Parse("2006-01-02 15:04:05", car.LocationTimeStamp); err == nil {
				message.WriteString(fmt.Sprintf("**Last Update:** %s\n\n", parsedTime.Format("15:04")))
			} else {
				message.WriteString(fmt.Sprintf("**Last Update:** %s\n\n", car.LocationTimeStamp))
			}
		} else {
			message.WriteString("**Last Update:** Unknown\n\n")
		}

		message.WriteString("---\n\n")
	}

	// Status legend
	message.WriteString("### **Status Legend:**\n")
	message.WriteString("🟢 **Available & On** - Ready for dispatch (engine on)  \n")
	message.WriteString("🟢 **Available & Off** - Ready for dispatch (engine off)  \n")
	message.WriteString("🟡 **At Terminal** - At fuel terminal  \n")
	message.WriteString("🔵 **Loading** - Loading fuel at terminal  \n")
	message.WriteString("🔴 **En Route** - Traveling to destination  \n")
	message.WriteString("🟠 **At Delivery** - Unloading at drop-off point  \n")
	message.WriteString("🅿️ **Parked** - At garage/depot  \n")
	message.WriteString("⚫ **Off Duty** - Driver break/end of shift\n\n")

	// Footer
	message.WriteString("---\n")
	message.WriteString("*Auto-updated by Apex Transport System*")

	return message.String()
}

var AllGeoFences = []GeoFence{
	// Garage
	{
		Name:      "garage",
		Latitude:  30.128955,
		Longitude: 31.298539,
		Radius:    0.4, // 400 meters radius
		Type:      "garage",
	},
	// Terminals
	{
		Name:      "Badr Terminal",
		Latitude:  30.1020583,
		Longitude: 31.81396,
		Radius:    0.5, // 500 meters radius
		Type:      "terminal",
	},
	{
		Name:      "CPC Mostorod Terminal",
		Latitude:  30.144197,
		Longitude: 31.296322,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Fayoum Terminal",
		Latitude:  29.3391616,
		Longitude: 30.9257033,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Misr Petroleum Bor Saed Terminal",
		Latitude:  31.235575,
		Longitude: 32.301198,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Mobil Bor Saed Terminal",
		Latitude:  31.23365,
		Longitude: 32.298082,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Haykstep Terminal",
		Latitude:  30.12486,
		Longitude: 31.3580633,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Somed Terminal",
		Latitude:  29.594416,
		Longitude: 32.329073,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "Agroud Terminal",
		Latitude:  30.071958,
		Longitude: 32.381296,
		Radius:    0.5,
		Type:      "terminal",
	},
}

// GeoFence represents a geographical boundary
type GeoFence struct {
	Name      string
	Latitude  float64
	Longitude float64
	Radius    float64 // in kilometers
	Type      string  // "garage", "terminal", or "dropoff"
}

// calculateDistance calculates distance between two coordinates using Haversine formula
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth's radius in kilometers

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	distance := R * c

	return distance
}

// checkGeofences checks which geofence the vehicle is in (if any)
func checkGeofences(lat, lng float64, car *Models.Car) (string, string, bool) {
	// First check static geofences (garage and terminals)
	for _, geofence := range AllGeoFences {
		distance := calculateDistance(lat, lng, geofence.Latitude, geofence.Longitude)
		if distance <= geofence.Radius {
			return geofence.Name, geofence.Type, true
		}
	}

	// Then check drop-off point geofences from database
	// Only check if vehicle is stopped (speed <= 5)
	if car.Speed <= 5 {
		dropoffName, found := checkDropOffPoints(lat, lng, car.OperatingCompany)
		if found {
			return dropoffName, "dropoff", true
		}
	}

	return "", "", false
}

func checkDropOffPoints(lat, lng float64, company string) (string, bool) {
	var feeMappings []Models.FeeMapping

	// Get all fee mappings for this company
	if err := Models.DB.Where("company = ?", company).Find(&feeMappings).Error; err != nil {
		log.Printf("Error fetching fee mappings: %v", err)
		return "", false
	}

	// Check each drop-off point (500m radius)
	for _, mapping := range feeMappings {
		distance := calculateDistance(lat, lng, mapping.Latitude, mapping.Longitude)
		if distance <= 0.5 { // 500 meters radius
			return mapping.DropOffPoint, true
		}
	}

	return "", false
}

// updateCarGeofence updates car's geofence based on current location
// Only updates if timestamp is newer AND vehicle has a geofence
func UpdateCarGeofence(car *Models.Car, lat, lng float64, timestamp string) bool {
	// Parse the timestamp from VehicleStatusStruct
	newTimestamp, err := time.Parse("2006-01-02 15:04:05", timestamp)
	if err != nil {
		log.Printf("Error parsing timestamp %s for car %s: %v", timestamp, car.CarNoPlate, err)
		return false
	}

	// Check if new timestamp is after last updated slack status
	if !car.LastUpdatedSlackStatus.IsZero() && !newTimestamp.After(car.LastUpdatedSlackStatus) {
		log.Printf("Skipping update for car %s - timestamp %s is not newer than last update %s",
			car.CarNoPlate, timestamp, car.LastUpdatedSlackStatus.Format("2006-01-02 15:04:05"))
		return false
	}

	// Check all geofences (including drop-off points)
	geofenceName, geofenceType, inGeofence := checkGeofences(lat, lng, car)

	// Only update if vehicle is in a geofence
	if !inGeofence {
		log.Printf("Skipping update for car %s - not in any geofence", car.CarNoPlate)
		return false
	}

	// Store previous status for comparison
	previousStatus := car.SlackStatus
	previousGeoFence := car.GeoFence

	// Update geofence and status
	car.GeoFence = geofenceName
	switch geofenceType {
	case "garage":
		car.SlackStatus = "Parked"
	case "terminal":
		if car.EngineStatus == "Ignition On" && car.Speed > 5 {
			car.SlackStatus = "Loading"
		} else {
			car.SlackStatus = "At Terminal"
		}
	case "dropoff":
		car.SlackStatus = "At Delivery"
	}

	// Update the last updated timestamp
	car.LastUpdatedSlackStatus = newTimestamp

	// Return true only if status or geofence actually changed
	return previousStatus != car.SlackStatus || previousGeoFence != car.GeoFence
}

// getStatusEmoji returns appropriate emoji for status
func getStatusEmoji(status string) string {
	switch strings.ToLower(status) {
	case "available & on":
		return "🟢"
	case "available & off":
		return "🟢"
	case "at terminal":
		return "🟡"
	case "loading":
		return "🔵"
	case "en route":
		return "🔴"
	case "at delivery":
		return "🟠"
	case "parked":
		return "🅿️"
	case "off duty":
		return "⚫"
	default:
		return "❓"
	}
}
