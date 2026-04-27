package Controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// LogEntry represents a single log entry
type LogEntry struct {
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

// LogGroup represents a group of logs by path
type LogGroup struct {
	Path        string     `json:"path"`
	Method      string     `json:"method"`
	Count       int        `json:"count"`
	AvgLatency  float64    `json:"avg_latency_ms"`
	MinLatency  float64    `json:"min_latency_ms"`
	MaxLatency  float64    `json:"max_latency_ms"`
	SuccessRate float64    `json:"success_rate"`
	Logs        []LogEntry `json:"logs"`
}

// LogsResponse represents the response structure for logs API
type LogsResponse struct {
	Groups      []LogGroup `json:"groups"`
	TotalLogs   int        `json:"total_logs"`
	TotalGroups int        `json:"total_groups"`
	Page        int        `json:"page"`
	PageSize    int        `json:"page_size"`
	TotalPages  int        `json:"total_pages"`
	DateFrom    time.Time  `json:"date_from"`
	DateTo      time.Time  `json:"date_to"`
}

// GetLogs retrieves logs with pagination, date filtering, and grouping
func GetLogs(c *fiber.Ctx) error {
	// Parse query parameters
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "50"))
	dateFromStr := c.Query("date_from", "")
	dateToStr := c.Query("date_to", "")
	pathFilter := c.Query("path", "")
	methodFilter := c.Query("method", "")
	statusFilter := c.Query("status", "")

	// Set default date range to today if not provided
	var dateFrom, dateTo time.Time
	if dateFromStr == "" && dateToStr == "" {
		// Default to today
		now := time.Now()
		dateFrom = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		dateTo = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
	} else {
		// Parse provided dates
		if dateFromStr != "" {
			parsed, err := time.Parse("2006-01-02", dateFromStr)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{
					"error": "Invalid date_from format. Use YYYY-MM-DD",
				})
			}
			dateFrom = parsed
		} else {
			dateFrom = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}

		if dateToStr != "" {
			parsed, err := time.Parse("2006-01-02", dateToStr)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{
					"error": "Invalid date_to format. Use YYYY-MM-DD",
				})
			}
			// Set to end of day
			dateTo = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 999999999, parsed.Location())
		} else {
			dateTo = time.Now()
		}
	}

	// Validate pagination parameters
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	// Read logs from file
	logs, err := readLogsFromFile("logs/requests.log", dateFrom, dateTo)
	if err != nil {
		log.Printf("Error reading logs: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"error": "Failed to read logs",
		})
	}

	// Filter logs by path and method if specified
	filteredLogs := filterLogs(logs, pathFilter, methodFilter, statusFilter)

	// Group logs by path
	groups := groupLogsByPath(filteredLogs)

	// Calculate pagination
	totalGroups := len(groups)
	totalPages := (totalGroups + pageSize - 1) / pageSize
	startIndex := (page - 1) * pageSize
	endIndex := startIndex + pageSize

	if startIndex >= totalGroups {
		startIndex = totalGroups
	}
	if endIndex > totalGroups {
		endIndex = totalGroups
	}

	// Get paginated groups
	var paginatedGroups []LogGroup
	if startIndex < totalGroups {
		paginatedGroups = groups[startIndex:endIndex]
	}

	// Calculate total logs across all groups
	totalLogs := 0
	for _, group := range groups {
		totalLogs += len(group.Logs)
	}

	response := LogsResponse{
		Groups:      paginatedGroups,
		TotalLogs:   totalLogs,
		TotalGroups: totalGroups,
		Page:        page,
		PageSize:    pageSize,
		TotalPages:  totalPages,
		DateFrom:    dateFrom,
		DateTo:      dateTo,
	}

	return c.JSON(response)
}

// readLogsFromFile reads logs from the specified file and filters by date range
func readLogsFromFile(filePath string, dateFrom, dateTo time.Time) ([]LogEntry, error) {
	file, err := os.OpenFile(filePath, os.O_RDONLY, 0666)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var logs []LogEntry
	lines := strings.Split(string(content), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var logEntry LogEntry
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			// Skip invalid JSON lines
			continue
		}

		// Filter by date range
		if logEntry.Timestamp.After(dateFrom) && logEntry.Timestamp.Before(dateTo) {
			logs = append(logs, logEntry)
		}
	}

	return logs, nil
}

// filterLogs filters logs by path, method, and status
func filterLogs(logs []LogEntry, pathFilter, methodFilter, statusFilter string) []LogEntry {
	var filtered []LogEntry

	for _, log := range logs {
		// Filter by path
		if pathFilter != "" && !strings.Contains(strings.ToLower(log.Path), strings.ToLower(pathFilter)) {
			continue
		}

		// Filter by method
		if methodFilter != "" && strings.ToUpper(log.Method) != strings.ToUpper(methodFilter) {
			continue
		}

		// Filter by status
		if statusFilter != "" {
			status, err := strconv.Atoi(statusFilter)
			if err == nil && log.Status != status {
				continue
			}
		}

		filtered = append(filtered, log)
	}

	return filtered
}

// groupLogsByPath groups logs by path and calculates statistics
func groupLogsByPath(logs []LogEntry) []LogGroup {
	groupMap := make(map[string]*LogGroup)

	for _, log := range logs {
		key := fmt.Sprintf("%s %s", log.Method, log.Path)

		if group, exists := groupMap[key]; exists {
			group.Count++
			group.Logs = append(group.Logs, log)

			// Update latency statistics
			latencyMs := float64(log.Latency.Microseconds()) / 1000.0
			group.AvgLatency = (group.AvgLatency*float64(group.Count-1) + latencyMs) / float64(group.Count)

			if latencyMs < group.MinLatency || group.MinLatency == 0 {
				group.MinLatency = latencyMs
			}
			if latencyMs > group.MaxLatency {
				group.MaxLatency = latencyMs
			}

			// Update success rate
			if log.Status >= 200 && log.Status < 300 {
				group.SuccessRate = (group.SuccessRate*float64(group.Count-1) + 1.0) / float64(group.Count)
			} else {
				group.SuccessRate = (group.SuccessRate * float64(group.Count-1)) / float64(group.Count)
			}
		} else {
			latencyMs := float64(log.Latency.Microseconds()) / 1000.0
			successRate := 0.0
			if log.Status >= 200 && log.Status < 300 {
				successRate = 1.0
			}

			groupMap[key] = &LogGroup{
				Path:        log.Path,
				Method:      log.Method,
				Count:       1,
				AvgLatency:  latencyMs,
				MinLatency:  latencyMs,
				MaxLatency:  latencyMs,
				SuccessRate: successRate,
				Logs:        []LogEntry{log},
			}
		}
	}

	// Convert map to slice and sort by count (descending)
	var groups []LogGroup
	for _, group := range groupMap {
		groups = append(groups, *group)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Count > groups[j].Count
	})

	return groups
}

// GetLogsByPath retrieves logs for a specific path
func GetLogsByPath(c *fiber.Ctx) error {
	path := c.Params("path")
	if path == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "Path parameter is required",
		})
	}

	// Parse query parameters
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "50"))
	dateFromStr := c.Query("date_from", "")
	dateToStr := c.Query("date_to", "")

	// Set default date range to today if not provided
	var dateFrom, dateTo time.Time
	if dateFromStr == "" && dateToStr == "" {
		now := time.Now()
		dateFrom = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		dateTo = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
	} else {
		if dateFromStr != "" {
			parsed, err := time.Parse("2006-01-02", dateFromStr)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{
					"error": "Invalid date_from format. Use YYYY-MM-DD",
				})
			}
			dateFrom = parsed
		} else {
			dateFrom = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}

		if dateToStr != "" {
			parsed, err := time.Parse("2006-01-02", dateToStr)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{
					"error": "Invalid date_to format. Use YYYY-MM-DD",
				})
			}
			dateTo = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 999999999, parsed.Location())
		} else {
			dateTo = time.Now()
		}
	}

	// Read and filter logs
	logs, err := readLogsFromFile("logs/requests.log", dateFrom, dateTo)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "Failed to read logs",
		})
	}

	// Filter logs for the specific path
	var pathLogs []LogEntry
	for _, log := range logs {
		if strings.Contains(log.Path, path) {
			pathLogs = append(pathLogs, log)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(pathLogs, func(i, j int) bool {
		return pathLogs[i].Timestamp.After(pathLogs[j].Timestamp)
	})

	// Calculate pagination
	totalLogs := len(pathLogs)
	totalPages := (totalLogs + pageSize - 1) / pageSize
	startIndex := (page - 1) * pageSize
	endIndex := startIndex + pageSize

	if startIndex >= totalLogs {
		startIndex = totalLogs
	}
	if endIndex > totalLogs {
		endIndex = totalLogs
	}

	// Get paginated logs
	var paginatedLogs []LogEntry
	if startIndex < totalLogs {
		paginatedLogs = pathLogs[startIndex:endIndex]
	}

	response := fiber.Map{
		"logs":        paginatedLogs,
		"total_logs":  totalLogs,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
		"path":        path,
		"date_from":   dateFrom,
		"date_to":     dateTo,
	}

	return c.JSON(response)
}

// GetLogStats returns statistics about logs
func GetLogStats(c *fiber.Ctx) error {
	dateFromStr := c.Query("date_from", "")
	dateToStr := c.Query("date_to", "")

	// Set default date range to today if not provided
	var dateFrom, dateTo time.Time
	if dateFromStr == "" && dateToStr == "" {
		now := time.Now()
		dateFrom = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		dateTo = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
	} else {
		if dateFromStr != "" {
			parsed, err := time.Parse("2006-01-02", dateFromStr)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{
					"error": "Invalid date_from format. Use YYYY-MM-DD",
				})
			}
			dateFrom = parsed
		} else {
			dateFrom = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}

		if dateToStr != "" {
			parsed, err := time.Parse("2006-01-02", dateToStr)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{
					"error": "Invalid date_to format. Use YYYY-MM-DD",
				})
			}
			dateTo = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 999999999, parsed.Location())
		} else {
			dateTo = time.Now()
		}
	}

	// Read logs
	logs, err := readLogsFromFile("logs/requests.log", dateFrom, dateTo)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "Failed to read logs",
		})
	}

	// Calculate statistics
	var totalRequests, successfulRequests, errorRequests int
	var totalLatency time.Duration
	var minLatency, maxLatency time.Duration
	methodStats := make(map[string]int)
	statusStats := make(map[int]int)
	pathStats := make(map[string]int)

	for i, log := range logs {
		totalRequests++

		if log.Status >= 200 && log.Status < 300 {
			successfulRequests++
		} else if log.Status >= 400 {
			errorRequests++
		}

		totalLatency += log.Latency

		if i == 0 || log.Latency < minLatency {
			minLatency = log.Latency
		}
		if log.Latency > maxLatency {
			maxLatency = log.Latency
		}

		methodStats[log.Method]++
		statusStats[log.Status]++
		pathStats[log.Path]++
	}

	avgLatency := time.Duration(0)
	if totalRequests > 0 {
		avgLatency = totalLatency / time.Duration(totalRequests)
	}

	successRate := 0.0
	if totalRequests > 0 {
		successRate = float64(successfulRequests) / float64(totalRequests) * 100
	}

	// Get top paths
	var topPaths []fiber.Map
	for path, count := range pathStats {
		topPaths = append(topPaths, fiber.Map{
			"path":  path,
			"count": count,
		})
	}
	sort.Slice(topPaths, func(i, j int) bool {
		return topPaths[i]["count"].(int) > topPaths[j]["count"].(int)
	})
	if len(topPaths) > 10 {
		topPaths = topPaths[:10]
	}

	response := fiber.Map{
		"total_requests":      totalRequests,
		"successful_requests": successfulRequests,
		"error_requests":      errorRequests,
		"success_rate":        successRate,
		"avg_latency_ms":      float64(avgLatency.Microseconds()) / 1000.0,
		"min_latency_ms":      float64(minLatency.Microseconds()) / 1000.0,
		"max_latency_ms":      float64(maxLatency.Microseconds()) / 1000.0,
		"method_stats":        methodStats,
		"status_stats":        statusStats,
		"top_paths":           topPaths,
		"date_from":           dateFrom,
		"date_to":             dateTo,
	}

	return c.JSON(response)
}
