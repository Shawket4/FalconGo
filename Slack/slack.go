package Slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
