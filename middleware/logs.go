package middleware

import (
	"Falcon/Models"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

// LogConfig holds configuration for the logging middleware
type LogConfig struct {
	// Enable console logging
	Console bool
	// Enable file logging
	File bool
	// Log file path
	LogFilePath string
	// Log format: "json" or "text"
	Format string
	// Include request body in logs
	IncludeBody bool
	// Include response body in logs
	IncludeResponse bool
	// Include user ID in logs
	IncludeUserID bool
	// Skip logging for specific paths
	SkipPaths []string
	// Custom logger function
	CustomLogger func(c *fiber.Ctx, data LogData)
}

// LogData contains all the information that will be logged
type LogData struct {
	Timestamp     time.Time     `json:"timestamp"`
	Method        string        `json:"method"`
	Path          string        `json:"path"`
	URL           string        `json:"url"`
	Status        int           `json:"status"`
	Latency       time.Duration `json:"latency"`
	IP            string        `json:"ip"`
	UserAgent     string        `json:"user_agent"`
	RequestID     string        `json:"request_id"`
	RequestBody   interface{}   `json:"request_body,omitempty"`
	ResponseBody  interface{}   `json:"response_body,omitempty"`
	Error         string        `json:"error,omitempty"`
	UserID        interface{}   `json:"user_id"`
	Username      string        `json:"username"`
	ContentLength int64         `json:"content_length"`
}

// DefaultLogConfig returns a default configuration for the logging middleware
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Console:         true,
		File:            true,
		LogFilePath:     "logs/requests.log",
		Format:          "json",
		IncludeBody:     false,
		IncludeResponse: false,
		IncludeUserID:   true,
		SkipPaths:       []string{"/health", "/metrics"},
	}
}

// LoggingMiddleware creates a new logging middleware with the given configuration
func LoggingMiddleware(config ...LogConfig) fiber.Handler {
	cfg := DefaultLogConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	// Ensure logs directory exists
	if cfg.File {
		if err := os.MkdirAll("logs", 0755); err != nil {
			log.Printf("Error creating logs directory: %v\n", err)
		}
	}

	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Check if we should skip this path
		for _, skipPath := range cfg.SkipPaths {
			if c.Path() == skipPath {
				return c.Next()
			}
		}

		// Capture request body if needed
		var requestBody interface{}
		if cfg.IncludeBody && c.Method() != "GET" {
			body := c.Body()
			if len(body) > 0 {
				// Try to parse as JSON, fallback to string
				var jsonData interface{}
				if err := json.Unmarshal(body, &jsonData); err == nil {
					requestBody = jsonData
				} else {
					requestBody = string(body)
				}
			}
		}

		// Create a custom response writer to capture response body
		var responseBody interface{}
		if cfg.IncludeResponse {
			// Store original response
			originalBody := c.Response().Body()
			defer func() {
				c.Response().SetBody(originalBody)
			}()
		}

		// Process request
		err := c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Get user ID from context if available
		var userID interface{}
		var username string
		if cfg.IncludeUserID {
			if user := c.Locals("user"); user != nil {
				// Try to get user ID from Models.User struct
				if userStruct, ok := user.(Models.User); ok {
					userID = userStruct.Id
					username = userStruct.Name
				} else if userMap, ok := user.(map[string]interface{}); ok {
					userID = userMap["id"]
					username = fmt.Sprintf("%v", userMap["name"])
				}
			}
		}

		// Create log data
		logData := LogData{
			Timestamp:     start,
			Method:        c.Method(),
			Path:          c.Path(),
			URL:           c.OriginalURL(),
			Status:        c.Response().StatusCode(),
			Latency:       latency,
			IP:            c.IP(),
			UserAgent:     c.Get("User-Agent"),
			RequestID:     c.Get("X-Request-ID"),
			RequestBody:   requestBody,
			ResponseBody:  responseBody,
			UserID:        userID,
			Username:      username,
			ContentLength: int64(len(c.Response().Body())),
		}

		// Add error if present
		if err != nil {
			logData.Error = err.Error()
		}

		// Log the data
		logRequest(cfg, logData)

		return err
	}
}

// logRequest handles the actual logging based on configuration
func logRequest(cfg LogConfig, data LogData) {
	// Use custom logger if provided
	if cfg.CustomLogger != nil {
		// We need a context for the custom logger, but we don't have it here
		// This is a limitation of the current design
		return
	}

	var logMessage string

	// Format the log message
	switch cfg.Format {
	case "json":
		jsonData, _ := json.Marshal(data)
		logMessage = string(jsonData)
	case "text":
		logMessage = formatTextLog(data)
	default:
		logMessage = formatTextLog(data)
	}

	// Console logging
	if cfg.Console {
		log.Println(logMessage)
	}

	// File logging
	if cfg.File {
		logToFile(cfg.LogFilePath, logMessage)
	}
}

// formatTextLog formats the log data as human-readable text
func formatTextLog(data LogData) string {
	statusColor := getStatusColor(data.Status)
	latencyColor := getLatencyColor(data.Latency)

	// Format user ID for display
	userIDStr := ""
	if data.UserID != nil {
		userIDStr = fmt.Sprintf(" user:%v", data.UserID)
	}

	return fmt.Sprintf(
		"[%s] %s %s %s %d %s %s %s%s",
		data.Timestamp.Format("2006-01-02 15:04:05"),
		data.Method,
		data.Path,
		statusColor,
		data.Status,
		latencyColor,
		data.Latency,
		data.IP,
		userIDStr,
	)
}

// getStatusColor returns a color indicator for HTTP status codes
func getStatusColor(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "✅" // Green checkmark for success
	case status >= 300 && status < 400:
		return "🔄" // Blue arrow for redirect
	case status >= 400 && status < 500:
		return "⚠️" // Yellow warning for client error
	case status >= 500:
		return "❌" // Red X for server error
	default:
		return "❓" // Question mark for unknown
	}
}

// getLatencyColor returns a color indicator for response latency
func getLatencyColor(latency time.Duration) string {
	switch {
	case latency < 100*time.Millisecond:
		return "🟢" // Green for fast
	case latency < 500*time.Millisecond:
		return "🟡" // Yellow for medium
	case latency < 1*time.Second:
		return "🟠" // Orange for slow
	default:
		return "🔴" // Red for very slow
	}
}

// logToFile writes the log message to a file
func logToFile(filePath, message string) {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Error opening log file: %v\n", err)
		return
	}
	defer file.Close()

	// Add newline if not present
	if len(message) > 0 && message[len(message)-1] != '\n' {
		message += "\n"
	}

	_, err = file.WriteString(message)
	if err != nil {
		log.Printf("Error writing to log file: %v\n", err)
	}
}

// SimpleLogger provides a simple logging middleware for backward compatibility
func SimpleLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		latency := time.Since(start)

		// Get user ID from context if available (always include for simple logger)
		var userIDStr string
		if user := c.Locals("user"); user != nil {
			if userStruct, ok := user.(Models.User); ok {
				userIDStr = fmt.Sprintf(" user:%v(%s)", userStruct.Id, userStruct.Name)
			} else if userMap, ok := user.(map[string]interface{}); ok {
				userIDStr = fmt.Sprintf(" user:%v(%s)", userMap["id"], userMap["name"])
			}
		}

		log.Printf(
			"[%s] %s %s %d %s %s%s",
			time.Now().Format("2006-01-02 15:04:05"),
			c.Method(),
			c.Path(),
			c.Response().StatusCode(),
			latency,
			c.IP(),
			userIDStr,
		)

		return err
	}
}

// RequestLogger creates a middleware that logs detailed request information
func RequestLogger() fiber.Handler {
	return LoggingMiddleware(LogConfig{
		Console:         true,
		File:            true,
		LogFilePath:     "logs/requests.log",
		Format:          "json",
		IncludeBody:     false,
		IncludeResponse: false,
		IncludeUserID:   true,
		SkipPaths:       []string{"/health", "/metrics", "/static"},
	})
}

// ErrorLogger creates a middleware that only logs errors
func ErrorLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		// Only log if there's an error or status code >= 400
		if err != nil || c.Response().StatusCode() >= 400 {
			latency := time.Since(start)

			// Get user ID from context if available (always include for error logger)
			var userID interface{}
			var username string
			if user := c.Locals("user"); user != nil {
				if userStruct, ok := user.(Models.User); ok {
					userID = userStruct.Id
					username = userStruct.Name
				} else if userMap, ok := user.(map[string]interface{}); ok {
					userID = userMap["id"]
					username = fmt.Sprintf("%v", userMap["name"])
				}
			}

			logData := LogData{
				Timestamp: start,
				Method:    c.Method(),
				Path:      c.Path(),
				URL:       c.OriginalURL(),
				Status:    c.Response().StatusCode(),
				Latency:   latency,
				IP:        c.IP(),
				UserAgent: c.Get("User-Agent"),
				UserID:    userID,
				Username:  username,
			}

			if err != nil {
				logData.Error = err.Error()
			}

			// Log to error file
			jsonData, _ := json.Marshal(logData)
			logToFile("logs/errors.log", string(jsonData))
		}

		return err
	}
}
