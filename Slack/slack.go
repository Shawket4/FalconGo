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
	"strconv"
	"strings"
	"sync"
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

// PendingUpdate represents a pending Slack update for batching
type PendingUpdate struct {
	Company   string
	ChannelID string
	Timer     *time.Timer
}

// CarStatusSnapshot represents a car's status at a point in time for comparison
type CarStatusSnapshot struct {
	CarID       uint
	PlateNumber string
	Status      string
	Location    string
	GeoFence    string
}

// Global variables for batching updates
var (
	pendingUpdates      = make(map[string]*PendingUpdate)
	lastChannelStatuses = make(map[string][]CarStatusSnapshot) // channelID -> car statuses
	statusMutex         sync.RWMutex
	updateMutex         sync.Mutex
	batchDelay          = 5 * time.Second // Wait 5 seconds before sending batched updates
)

// NewSlackClient creates a new Slack client
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
		return nil, fmt.Errorf("error unmarshaling response: %v", response)
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

// SendAndPinWithCleanupTasks sends a task message, pins it, and removes old messages (no car comparison)
func (s *SlackClient) SendAndPinWithCleanupTasks(channel, message string) error {
	fmt.Printf("Cleaning channel and sending new task message to %s...\n", channel)

	// Delete all bot messages
	messages, err := s.GetChannelHistory(channel, 100)
	if err == nil {
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

	// Unpin all existing pinned messages
	pinnedMessages, err := s.GetPinnedMessages(channel)
	if err == nil {
		for _, timestamp := range pinnedMessages {
			if err := s.UnpinMessage(channel, timestamp); err != nil {
				fmt.Printf("Could not unpin message %s: %v\n", timestamp, err)
			}
		}
	}

	// Send new message
	fmt.Printf("Sending task message to %s...\n", channel)
	resp, err := s.SendMessage(channel, message)
	if err != nil {
		return fmt.Errorf("error sending message: %v", err)
	}

	fmt.Printf("Message sent! Timestamp: %s\n", resp.TS)

	// Pin the new message
	time.Sleep(1 * time.Second)
	if err := s.PinMessage(channel, resp.TS); err != nil {
		fmt.Printf("Warning: Message sent but pinning failed: %v\n", err)
		return nil
	}

	fmt.Printf("Message pinned successfully in %s\n", channel)
	return nil
}

// SendAndPinWithCleanup sends a message, pins it, and removes all other bot messages
func (s *SlackClient) SendAndPinWithCleanup(channel, message string, cars []Models.Car) error {
	fmt.Printf("Checking if update needed for channel %s...\n", channel)

	// Create current status snapshots
	var currentStatuses []CarStatusSnapshot
	for _, car := range cars {
		currentStatuses = append(currentStatuses, CarStatusSnapshot{
			CarID:       car.ID,
			PlateNumber: car.CarNoPlate,
			Status:      car.SlackStatus,
			Location:    car.Location,
			GeoFence:    car.GeoFence,
		})
	}

	// Compare with last known statuses for this channel
	statusMutex.Lock()
	lastStatuses, exists := lastChannelStatuses[channel]
	if exists && statusSnapshotsEqual(lastStatuses, currentStatuses) {
		statusMutex.Unlock()
		fmt.Printf("Car statuses unchanged for channel %s, skipping update\n", channel)
		return nil
	}

	// Update stored statuses
	lastChannelStatuses[channel] = currentStatuses
	statusMutex.Unlock()

	fmt.Printf("Car statuses changed for channel %s, proceeding with update...\n", channel)

	// Delete all bot messages
	messages, err := s.GetChannelHistory(channel, 100)
	if err == nil {
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

	// Unpin all existing pinned messages
	pinnedMessages, err := s.GetPinnedMessages(channel)
	if err == nil {
		for _, timestamp := range pinnedMessages {
			if err := s.UnpinMessage(channel, timestamp); err != nil {
				fmt.Printf("Could not unpin message %s: %v\n", timestamp, err)
			}
		}
	}

	// Send new message
	fmt.Printf("Sending message to %s...\n", channel)
	resp, err := s.SendMessage(channel, message)
	if err != nil {
		return fmt.Errorf("error sending message: %v", err)
	}

	fmt.Printf("Message sent! Timestamp: %s\n", resp.TS)

	// Pin the new message
	time.Sleep(1 * time.Second)
	if err := s.PinMessage(channel, resp.TS); err != nil {
		fmt.Printf("Warning: Message sent but pinning failed: %v\n", err)
		return nil
	}

	fmt.Printf("Message pinned successfully in %s\n", channel)
	return nil
}

// statusSnapshotsEqual compares two status snapshots
func statusSnapshotsEqual(a, b []CarStatusSnapshot) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for efficient comparison
	mapA := make(map[uint]CarStatusSnapshot)
	mapB := make(map[uint]CarStatusSnapshot)

	for _, car := range a {
		mapA[car.CarID] = car
	}
	for _, car := range b {
		mapB[car.CarID] = car
	}

	// Compare each car's status
	for carID, statusA := range mapA {
		statusB, exists := mapB[carID]
		if !exists {
			return false
		}
		if statusA.Status != statusB.Status || statusA.Location != statusB.Location || statusA.GeoFence != statusB.GeoFence {
			return false
		}
	}

	return true
}

// ManualUpdateVehicleStatus - API function for manual status updates
func ManualUpdateVehicleStatus(carID uint, newStatus, location, updatedBy string) error {
	var car Models.Car
	if err := Models.DB.Preload("Driver").First(&car, carID).Error; err != nil {
		return fmt.Errorf("car not found: %v", err)
	}

	// Store previous status for comparison
	previousStatus := car.SlackStatus
	car.Location = location
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
		// Queue update for this specific company/channel
		QueueSlackUpdate(car.OperatingCompany)
		log.Printf("Queued Slack update for manual status change of %s", car.CarNoPlate)
	}

	return nil
}

// Company to Slack channel mapping
var CompanyChannelMap = map[string]string{
	"petrol_arrows": "C09GSBV2TSR",
	"taqa":          "C09H1DFP21J",
	"watanya":       "C09GW6QNT46",
}

// QueueSlackUpdate queues a Slack update for a specific company with batching
func QueueSlackUpdate(company string) {
	updateMutex.Lock()
	defer updateMutex.Unlock()

	companyKey := strings.ToLower(company)
	channelID, exists := CompanyChannelMap[companyKey]
	if !exists {
		log.Printf("No channel mapping found for company: %s", company)
		return
	}

	// If there's already a pending update for this company, cancel the old timer
	if existing, exists := pendingUpdates[companyKey]; exists {
		if existing.Timer != nil {
			existing.Timer.Stop()
		}
	}

	// Create new pending update with timer
	pendingUpdates[companyKey] = &PendingUpdate{
		Company:   companyKey,
		ChannelID: channelID,
		Timer: time.AfterFunc(batchDelay, func() {
			processBatchedUpdate(companyKey)
		}),
	}

	log.Printf("Queued Slack update for company %s (channel %s), will send in %v", company, channelID, batchDelay)
}

// processBatchedUpdate processes a batched update for a company
func processBatchedUpdate(companyKey string) {
	updateMutex.Lock()
	update, exists := pendingUpdates[companyKey]
	if !exists {
		updateMutex.Unlock()
		return
	}
	delete(pendingUpdates, companyKey)
	updateMutex.Unlock()

	// Get all recent cars for this company
	var allCars []Models.Car
	if err := Models.DB.Preload("Driver").Where("LOWER(operating_company) = ?", companyKey).Find(&allCars).Error; err != nil {
		log.Printf("Error fetching cars for company %s: %v", companyKey, err)
		return
	}

	// Filter recent cars (24 hour activity)
	var recentCars []Models.Car
	for _, car := range allCars {
		if car.LocationTimeStamp != "" {
			if parsedTime, err := time.Parse("2006-01-02 15:04:05", car.LocationTimeStamp); err == nil {
				if time.Since(parsedTime) <= 24*time.Hour {
					recentCars = append(recentCars, car)
				}
			}
		}
	}

	if len(recentCars) == 0 {
		log.Printf("No recent cars found for company %s, skipping update", companyKey)
		return
	}

	// Send update to Slack
	if err := sendFleetUpdateToChannel(update.ChannelID, recentCars, companyKey); err != nil {
		log.Printf("Error sending batched update for company %s: %v", companyKey, err)
	} else {
		log.Printf("Successfully sent batched update for %s (%d vehicles)", companyKey, len(recentCars))
	}
}

// sendFleetUpdateToChannel sends an update to a specific channel
func sendFleetUpdateToChannel(channelID string, cars []Models.Car, company string) error {
	if err := godotenv.Load(".env"); err != nil {
		return fmt.Errorf("error loading .env file: %v", err)
	}

	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not set")
	}

	client := NewSlackClient(slackToken)
	message := generateSlackMessage(cars, company)

	return client.SendAndPinWithCleanup(channelID, message, cars)
}

// SendFleetUpdatesToSlack sends updates to all channels (for backwards compatibility)
func SendFleetUpdatesToSlack(carsByCompany map[string][]Models.Car) error {
	for company, cars := range carsByCompany {
		if len(cars) == 0 {
			continue
		}

		channelID, exists := CompanyChannelMap[strings.ToLower(company)]
		if !exists {
			log.Printf("No channel mapping found for company: %s", company)
			continue
		}

		if err := sendFleetUpdateToChannel(channelID, cars, company); err != nil {
			log.Printf("Error sending to channel %s: %v", channelID, err)
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
						displayLocation = "Garage"
					} else if geofence.Type == "terminal" {
						displayLocation = fmt.Sprintf("%s Terminal", geofence.Name)
					}
					break
				}
			}
			// If not found in static geofences, it might be a drop-off point
			if displayLocation == car.Location && car.GeoFence != "" {
				displayLocation = fmt.Sprintf("%s Drop-Off Point", car.GeoFence)
			}
		}

		// Get status emoji
		statusEmoji := getStatusEmoji(car.SlackStatus)

		// Generate Google Maps link with coordinate validation
		mapsLink := ""
		if car.Latitude != "" && car.Longitude != "" {
			if lat, latErr := strconv.ParseFloat(car.Latitude, 64); latErr == nil {
				if lng, lngErr := strconv.ParseFloat(car.Longitude, 64); lngErr == nil {
					if isValidCoordinate(lat, lng) {
						mapsLink = fmt.Sprintf("https://maps.google.com/?q=%s,%s", car.Latitude, car.Longitude)
					}
				}
			}
		}

		message.WriteString(fmt.Sprintf("## **%s**\n", car.CarNoPlate))
		message.WriteString(fmt.Sprintf("**Driver:** %s  \n", driverName))
		message.WriteString(fmt.Sprintf("**Area:** %s  \n", car.OperatingArea))
		message.WriteString(fmt.Sprintf("**Status:** %s %s  \n", car.SlackStatus, statusEmoji))
		message.WriteString(fmt.Sprintf("**Location:** %s  \n", displayLocation))

		// Add Google Maps link if coordinates are valid
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
	message.WriteString("🏢 **In Terminal** - At fuel terminal  \n")
	message.WriteString("📦 **In Drop-Off** - At delivery location  \n")
	message.WriteString("🅿️ **In Garage** - At garage/depot  \n")
	message.WriteString("🔧 **Stopped for Maintenance** - Under repair/maintenance  \n")
	message.WriteString("🟡 **On Route to Terminal** - Traveling to fuel terminal  \n")
	message.WriteString("🔴 **On Route to Drop-Off** - Traveling to delivery location  \n")
	message.WriteString("💤 **Driver Resting** - Driver break/rest period\n\n")

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
	{
		Name:      "TAQA Suez Terminal",
		Latitude:  29.964054,
		Longitude: 32.515200,
		Radius:    0.5,
		Type:      "terminal",
	},
	{
		Name:      "TAQA Alex Terminal",
		Latitude:  31.149223,
		Longitude: 29.853037,
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

// isValidCoordinate validates latitude and longitude values
func isValidCoordinate(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

// calculateDistance calculates distance between two coordinates using Haversine formula
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	// Validate coordinates
	if !isValidCoordinate(lat1, lon1) || !isValidCoordinate(lat2, lon2) {
		return math.Inf(1) // Return infinity for invalid coordinates
	}

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
	dropoffName, found := checkDropOffPoints(lat, lng, car.OperatingCompany)
	if found {
		return dropoffName, "dropoff", true
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

// UpdateCarGeofence updates car's geofence based on current location
// Only updates automatically if vehicle enters/exits a geofence
func UpdateCarGeofence(car *Models.Car, lat, lng float64, timestamp string) bool {
	// Parse the timestamp from VehicleStatusStruct
	newTimestamp, err := time.Parse("2006-01-02 15:04:05", timestamp)
	if err != nil {
		log.Printf("Error parsing timestamp %s for car %s: %v", timestamp, car.CarNoPlate, err)
		return false
	}

	// Check if new timestamp is after last updated slack status (allow equal timestamps for same-time different locations)
	if !car.LastUpdatedSlackStatus.IsZero() && newTimestamp.Before(car.LastUpdatedSlackStatus) {
		log.Printf("Skipping update for car %s - timestamp %s is older than last update %s",
			car.CarNoPlate, timestamp, car.LastUpdatedSlackStatus.Format("2006-01-02 15:04:05"))
		return false
	}

	// Check all geofences (including drop-off points)
	geofenceName, geofenceType, inGeofence := checkGeofences(lat, lng, car)

	// Store previous values for comparison
	previousStatus := car.SlackStatus
	previousGeoFence := car.GeoFence

	// Only update status automatically when entering/exiting geofences
	if inGeofence {
		// Vehicle entered a geofence - update status based on geofence type
		car.GeoFence = geofenceName
		switch geofenceType {
		case "garage":
			car.SlackStatus = "In Garage"
		case "terminal":
			car.SlackStatus = "In Terminal"
		case "dropoff":
			car.SlackStatus = "In Drop-Off"
		}

		// Update the last updated timestamp
		car.LastUpdatedSlackStatus = newTimestamp
	} else {
		// Vehicle not in any geofence - only update if it was previously in a geofence
		if car.GeoFence != "" {
			car.GeoFence = ""
			// Don't automatically set status when leaving geofence
			// Status must be set manually using ManualUpdateVehicleStatus
			car.LastUpdatedSlackStatus = newTimestamp
		}
	}

	// Return true only if status or geofence actually changed
	statusChanged := previousStatus != car.SlackStatus || previousGeoFence != car.GeoFence

	if statusChanged {
		log.Printf("Car %s status changed from '%s' to '%s' (geofence: '%s' -> '%s')",
			car.CarNoPlate, previousStatus, car.SlackStatus, previousGeoFence, car.GeoFence)

		// Queue Slack update for this company
		QueueSlackUpdate(car.OperatingCompany)
	}

	return statusChanged
}

// getStatusEmoji returns appropriate emoji for status
func getStatusEmoji(status string) string {
	switch strings.ToLower(status) {
	case "in terminal":
		return "🏢"
	case "in drop-off":
		return "📦"
	case "in garage":
		return "🅿️"
	case "stopped for maintenance":
		return "🔧"
	case "on route to terminal":
		return "🟡"
	case "on route to drop-off":
		return "🔴"
	case "driver resting":
		return "💤"
	default:
		return "❓"
	}
}
