package Controllers

import (
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/utils"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type SessionController struct {
	DB *gorm.DB
}

// GetActiveSessions - Get all currently active sessions (admin only)
// GET /api/admin/sessions/active
func (ctrl *SessionController) GetActiveSessions(c *fiber.Ctx) error {
	// Fetch sessions with only necessary preloads
	var sessions []Models.DriverSession
	if err := ctrl.DB.
		Preload("Driver", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, name")
		}).
		Preload("Car", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, car_no_plate")
		}).
		Preload("Tablet", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, hwid")
		}).
		Select("id, driver_id, car_id, tablet_id, start_time").
		Where("end_time IS NULL").
		Order("start_time DESC").
		Find(&sessions).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch active sessions")
	}

	// Build minimal response structure
	type MinimalDriver struct {
		ID   uint   `json:"ID" msgpack:"ID"`
		Name string `json:"name" msgpack:"name"`
	}

	type MinimalCar struct {
		ID         uint   `json:"ID" msgpack:"ID"`
		CarNoPlate string `json:"car_no_plate" msgpack:"car_no_plate"`
	}

	type MinimalTablet struct {
		ID   uint   `json:"ID" msgpack:"ID"`
		Hwid string `json:"hwid" msgpack:"hwid"`
	}

	type MinimalSession struct {
		ID        uint           `json:"ID" msgpack:"ID"`
		Driver    *MinimalDriver `json:"driver" msgpack:"driver"`
		Car       *MinimalCar    `json:"car" msgpack:"car"`
		Tablet    *MinimalTablet `json:"tablet" msgpack:"tablet"`
		StartTime time.Time      `json:"start_time" msgpack:"start_time"`
	}

	// Transform to minimal structure
	minimalSessions := make([]MinimalSession, len(sessions))
	for i, session := range sessions {
		minimalSessions[i] = MinimalSession{
			ID:        session.ID,
			StartTime: session.StartTime,
		}

		// Add driver if exists
		if session.Driver.ID != 0 {
			minimalSessions[i].Driver = &MinimalDriver{
				ID:   session.Driver.ID,
				Name: session.Driver.Name,
			}
		}

		// Add car if exists
		if session.Car.ID != 0 {
			minimalSessions[i].Car = &MinimalCar{
				ID:         session.Car.ID,
				CarNoPlate: session.Car.CarNoPlate,
			}
		}

		// Add tablet if exists
		if session.Tablet.ID != 0 {
			minimalSessions[i].Tablet = &MinimalTablet{
				ID:   session.Tablet.ID,
				Hwid: session.Tablet.HWID,
			}
		}
	}

	return utils.SuccessResponse(c, minimalSessions)
}

// GetDriverSessions - Get session history for a specific driver
// GET /api/admin/sessions/driver/:driver_id
// Query params: start_date, end_date (optional)
func (ctrl *SessionController) GetDriverSessions(c *fiber.Ctx) error {
	driverID := c.Params("driver_id")

	// Parse optional date filters
	var startDate, endDate *time.Time

	if start := c.Query("start_date"); start != "" {
		parsed, err := time.Parse("2006-01-02", start)
		if err == nil {
			startDate = &parsed
		}
	}

	if end := c.Query("end_date"); end != "" {
		parsed, err := time.Parse("2006-01-02", end)
		if err == nil {
			// Set to end of day
			endOfDay := parsed.Add(24*time.Hour - time.Second)
			endDate = &endOfDay
		}
	}

	// Convert string ID to uint
	var driverIDUint uint
	if _, err := fmt.Sscanf(driverID, "%d", &driverIDUint); err != nil {
		return utils.BadRequestResponse(c, "Invalid driver ID")
	}

	sessions, err := utils.GetDriverSessions(ctrl.DB, driverIDUint, startDate, endDate)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch driver sessions")
	}

	return utils.SuccessResponse(c, sessions)
}

// GetMySessionHistory - Driver gets their own session history
// GET /api/driver/sessions
// Query params: start_date, end_date (optional)
func (ctrl *SessionController) GetMySessionHistory(c *fiber.Ctx) error {
	driver, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return utils.UnauthorizedResponse(c, "Driver not found")
	}

	// Parse optional date filters
	var startDate, endDate *time.Time

	if start := c.Query("start_date"); start != "" {
		parsed, err := time.Parse("2006-01-02", start)
		if err == nil {
			startDate = &parsed
		}
	}

	if end := c.Query("end_date"); end != "" {
		parsed, err := time.Parse("2006-01-02", end)
		if err == nil {
			endOfDay := parsed.Add(24*time.Hour - time.Second)
			endDate = &endOfDay
		}
	}

	sessions, err := utils.GetDriverSessions(ctrl.DB, driver.ID, startDate, endDate)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch sessions")
	}

	return utils.SuccessResponse(c, sessions)
}

// GetSession - Get specific session details
// GET /api/sessions/:id
func (ctrl *SessionController) GetSession(c *fiber.Ctx) error {
	sessionID := c.Params("id")

	var session Models.DriverSession
	if err := ctrl.DB.Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		First(&session, sessionID).Error; err != nil {
		return utils.NotFoundResponse(c, "Session not found")
	}

	// Check permissions
	userType, _ := c.Locals("user_type").(string)
	if userType == "driver" {
		driver, _ := c.Locals("driver").(Models.Driver)
		if session.DriverID != driver.ID {
			return utils.ForbiddenResponse(c, "You don't have permission to view this session")
		}
	}

	// Calculate duration
	durationMinutes := session.CalculateDuration()

	return utils.SuccessResponse(c, fiber.Map{
		"session":          session,
		"duration_minutes": durationMinutes,
	})
}

// GetSessionLocationPings - Get location pings for a specific session
// GET /api/sessions/:id/location-pings?stats=true
func (ctrl *SessionController) GetSessionLocationPings(c *fiber.Ctx) error {
	totalStart := time.Now()
	sessionID := c.Params("id")

	log.Printf("📍 [DEBUG] ========== GetSessionLocationPings START ==========")
	log.Printf("📍 [DEBUG] Session ID: %s", sessionID)

	// TIMING 1: Session lookup (just for permission check)
	t1 := time.Now()
	var session Models.DriverSession
	if err := ctrl.DB.Select("id, driver_id").First(&session, sessionID).Error; err != nil {
		log.Printf("❌ [DEBUG] Session not found: %s", sessionID)
		return utils.NotFoundResponse(c, "Session not found")
	}
	log.Printf("⏱️  [TIMING 1] Session lookup: %v", time.Since(t1))

	// Check permissions
	userType, _ := c.Locals("user_type").(string)
	if userType == "driver" {
		driver, _ := c.Locals("driver").(Models.Driver)
		if session.DriverID != driver.ID {
			return utils.ForbiddenResponse(c, "You don't have permission to view this session")
		}
	}

	// Parse query parameters
	includeStats := c.Query("stats", "true") == "true"

	// Parse downsample parameter
	downsampleCount := 0
	if downsampleStr := c.Query("downsample"); downsampleStr != "" {
		if parsed, err := strconv.Atoi(downsampleStr); err == nil && parsed > 0 {
			downsampleCount = parsed
			log.Printf("📉 [DEBUG] Downsampling requested: %d pings", downsampleCount)
		}
	}

	// TIMING 2: Stats query
	t2 := time.Now()
	var summary *Models.SessionLocationSummary
	var totalPings int64 = 0

	if includeStats {
		viewsExist := Models.MaterializedViewsExist()

		if viewsExist {
			var sessionIDUint uint
			if _, err := fmt.Sscanf(sessionID, "%d", &sessionIDUint); err == nil {
				tempSummary, err := Models.GetSessionLocationSummary(sessionIDUint)

				if err == nil && tempSummary != nil {
					summary = tempSummary
					totalPings = int64(tempSummary.TotalPings)
				}
			}
		}

		if summary == nil {
			ctrl.DB.Model(&Models.LocationPing{}).
				Where("session_id = ?", sessionID).
				Count(&totalPings)
		}
	}
	statsTime := time.Since(t2)
	log.Printf("⏱️  [TIMING 2] Stats query: %v (Total pings: %d)", statsTime, totalPings)

	// TIMING 3: Fetch location pings (optimized with minimal fields)
	t3 := time.Now()
	log.Printf("📊 [DEBUG] Fetching ALL %d location pings...", totalPings)

	// Define a lightweight struct with only essential fields
	type LocationPingLite struct {
		ID        uint      `json:"id" msgpack:"id"`
		Lat       float64   `json:"lat" msgpack:"lat"`
		Lng       float64   `json:"lng" msgpack:"lng"`
		TimeStamp time.Time `json:"time_stamp" msgpack:"time_stamp"`
		Speed     *float64  `json:"speed,omitempty" msgpack:"speed,omitempty"`
	}

	var locationPings []LocationPingLite

	// Direct optimized query with minimal fields
	if err := ctrl.DB.
		Model(&Models.LocationPing{}).
		Select("id, lat, lng, time_stamp, speed").
		Where("session_id = ?", sessionID).
		Order("time_stamp ASC").
		Scan(&locationPings).Error; err != nil {
		log.Printf("❌ [DEBUG] Query failed: %v", err)
		return utils.InternalServerErrorResponse(c, "Failed to fetch location pings")
	}

	fetchTime := time.Since(t3)
	log.Printf("⏱️  [TIMING 3] Fetch pings: %v (%d records)", fetchTime, len(locationPings))

	// TIMING 3.5: Downsample if requested
	originalCount := len(locationPings)
	if downsampleCount > 0 && len(locationPings) > downsampleCount {
		tDownsample := time.Now()
		locationPings = downsamplePings(locationPings, downsampleCount)
		downsampleTime := time.Since(tDownsample)
		log.Printf("📉 [TIMING 3.5] Downsampled: %d → %d pings in %v (%.1f%% reduction)",
			originalCount, len(locationPings), downsampleTime,
			(1.0-float64(len(locationPings))/float64(originalCount))*100)
	}

	// TIMING 4: Build response
	t4 := time.Now()

	// 🚀 ONLY RETURN PINGS - NO SESSION OBJECT!
	response := fiber.Map{
		"pings": locationPings,
		"count": len(locationPings),
	}

	// Add downsampling info if applied
	if downsampleCount > 0 {
		response["original_count"] = originalCount
		response["downsampled"] = originalCount > downsampleCount
	}

	if includeStats && summary != nil {
		response["stats"] = fiber.Map{
			"total_pings":     summary.TotalPings,
			"first_ping_time": summary.FirstPingTime,
			"last_ping_time":  summary.LastPingTime,
			"avg_speed":       summary.AvgSpeed,
			"max_speed":       summary.MaxSpeed,
			"bounding_box": fiber.Map{
				"min_lat": summary.MinLat,
				"max_lat": summary.MaxLat,
				"min_lng": summary.MinLng,
				"max_lng": summary.MaxLng,
			},
		}
		response["source"] = "materialized_view"
	} else if includeStats {
		response["total_pings"] = totalPings
		response["source"] = "direct_query"
	}

	buildTime := time.Since(t4)
	log.Printf("⏱️  [TIMING 4] Build response: %v", buildTime)

	// TIMING 5: Send response with smart format detection
	t5 := time.Now()

	// Use smart response (auto-detects JSON vs MessagePack)
	result := utils.FastSuccessResponse(c, response)
	sendTime := time.Since(t5)

	totalTime := time.Since(totalStart)
	log.Printf("⏱️  [TIMING 5] Send response: %v", sendTime)
	log.Printf("⏱️  [TOTAL TIME] %v", totalTime)
	log.Printf("📍 [DEBUG] ========== GetSessionLocationPings END ==========\n")

	return result
}

// Helper function: downsamplePings reduces number of pings while maintaining route shape
func downsamplePings[T any](pings []T, targetCount int) []T {
	if len(pings) <= targetCount {
		return pings
	}

	step := float64(len(pings)) / float64(targetCount)
	result := make([]T, 0, targetCount)

	for i := 0; i < targetCount; i++ {
		idx := int(float64(i) * step)
		if idx < len(pings) {
			result = append(result, pings[idx])
		}
	}

	return result
}

// GetCarSessions - Get all sessions for a specific car (admin only)
// GET /api/admin/sessions/car/:car_id
// Query params: start_date, end_date (optional)
func (ctrl *SessionController) GetCarSessions(c *fiber.Ctx) error {
	carID := c.Params("car_id")

	// Parse optional date filters
	var startDate, endDate *time.Time

	if start := c.Query("start_date"); start != "" {
		parsed, err := time.Parse("2006-01-02", start)
		if err == nil {
			startDate = &parsed
		}
	}

	if end := c.Query("end_date"); end != "" {
		parsed, err := time.Parse("2006-01-02", end)
		if err == nil {
			endOfDay := parsed.Add(24*time.Hour - time.Second)
			endDate = &endOfDay
		}
	}

	// Convert string ID to uint
	var carIDUint uint
	if _, err := fmt.Sscanf(carID, "%d", &carIDUint); err != nil {
		return utils.BadRequestResponse(c, "Invalid car ID")
	}

	sessions, err := utils.GetCarSessions(ctrl.DB, carIDUint, startDate, endDate)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch car sessions")
	}

	return utils.SuccessResponse(c, sessions)
}

// EndSession - Admin manually ends a session
// POST /api/admin/sessions/:id/end
func (ctrl *SessionController) EndSession(c *fiber.Ctx) error {
	sessionID := c.Params("id")

	var sessionIDUint uint
	if _, err := fmt.Sscanf(sessionID, "%d", &sessionIDUint); err != nil {
		return utils.BadRequestResponse(c, "Invalid session ID")
	}

	if err := utils.EndSession(ctrl.DB, sessionIDUint); err != nil {
		return utils.BadRequestResponse(c, err.Error())
	}

	// Trigger materialized view refresh after ending session
	if Models.MaterializedViewsExist() {
		go func() {
			defer Observability.Guard("materialized_view_refresh")
			if err := Models.RefreshMaterializedViews(); err != nil {
				Observability.ReportJobFailure("materialized_view_refresh", "after_session_end", err)
			}
		}()
	}

	return utils.SuccessMessageResponse(c, "Session ended successfully")
}

// GetActiveSessionsWithLocation returns all active sessions with their last location
// GET /api/admin/sessions/active-with-location
func (ctrl *SessionController) GetActiveSessionsWithLocation(c *fiber.Ctx) error {
	// Try to use materialized view first (FAST!)
	if Models.MaterializedViewsExist() {
		sessions, err := Models.GetActiveSessionsWithLastLocation()
		if err == nil {
			return utils.SuccessResponse(c, fiber.Map{
				"sessions": sessions,
				"count":    len(sessions),
				"source":   "materialized_view",
			})
		}
	}

	// Fallback to regular query if materialized view not available
	var sessions []Models.DriverSession
	if err := ctrl.DB.
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		Where("end_time IS NULL").
		Find(&sessions).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to fetch active sessions")
	}

	// Get last location for each session
	type SessionWithLocation struct {
		Models.DriverSession
		LastLocation *Models.LocationPing `json:"last_location"`
	}

	var result []SessionWithLocation
	for _, session := range sessions {
		var lastPing Models.LocationPing
		err := ctrl.DB.
			Where("session_id = ?", session.ID).
			Order("time_stamp DESC").
			First(&lastPing).Error

		sessionWithLoc := SessionWithLocation{
			DriverSession: session,
		}

		if err == nil {
			sessionWithLoc.LastLocation = &lastPing
		}

		result = append(result, sessionWithLoc)
	}

	return utils.SuccessResponse(c, fiber.Map{
		"sessions": result,
		"count":    len(result),
		"source":   "direct_query",
	})
}

// GetSessionSummary returns summary statistics for a session
// GET /api/sessions/:id/summary
func (ctrl *SessionController) GetSessionSummary(c *fiber.Ctx) error {
	sessionID := c.Params("id")

	// Verify session exists
	var session Models.DriverSession
	if err := ctrl.DB.
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		First(&session, sessionID).Error; err != nil {
		return utils.NotFoundResponse(c, "Session not found")
	}

	// Check permissions
	userType, _ := c.Locals("user_type").(string)
	if userType == "driver" {
		driver, _ := c.Locals("driver").(Models.Driver)
		if session.DriverID != driver.ID {
			return utils.ForbiddenResponse(c, "You don't have permission to view this session")
		}
	}

	// Try to get summary from materialized view (FAST!)
	if Models.MaterializedViewsExist() {
		var sessionIDUint uint
		if _, err := fmt.Sscanf(sessionID, "%d", &sessionIDUint); err == nil {
			summary, err := Models.GetSessionLocationSummary(sessionIDUint)
			if err == nil && summary != nil {
				return utils.SuccessResponse(c, fiber.Map{
					"session": session,
					"summary": summary,
					"source":  "materialized_view",
				})
			}
		}
	}

	// Fallback to direct calculation
	var totalPings int64
	ctrl.DB.Model(&Models.LocationPing{}).
		Where("session_id = ?", sessionID).
		Count(&totalPings)

	type DirectSummary struct {
		TotalPings    int64    `json:"total_pings"`
		FirstPingTime *string  `json:"first_ping_time"`
		LastPingTime  *string  `json:"last_ping_time"`
		AvgSpeed      *float64 `json:"avg_speed"`
		MaxSpeed      *float64 `json:"max_speed"`
	}

	var summary DirectSummary
	summary.TotalPings = totalPings

	if totalPings > 0 {
		type SpeedStats struct {
			AvgSpeed *float64
			MaxSpeed *float64
		}
		var speedStats SpeedStats
		ctrl.DB.Model(&Models.LocationPing{}).
			Select("AVG(speed) as avg_speed, MAX(speed) as max_speed").
			Where("session_id = ? AND speed IS NOT NULL", sessionID).
			Scan(&speedStats)

		summary.AvgSpeed = speedStats.AvgSpeed
		summary.MaxSpeed = speedStats.MaxSpeed

		// Get first and last ping times
		var firstPing Models.LocationPing
		if err := ctrl.DB.
			Where("session_id = ?", sessionID).
			Order("time_stamp ASC").
			First(&firstPing).Error; err == nil {
			timeStr := firstPing.TimeStamp.Format("2006-01-02T15:04:05Z")
			summary.FirstPingTime = &timeStr
		}

		var lastPing Models.LocationPing
		if err := ctrl.DB.
			Where("session_id = ?", sessionID).
			Order("time_stamp DESC").
			First(&lastPing).Error; err == nil {
			timeStr := lastPing.TimeStamp.Format("2006-01-02T15:04:05Z")
			summary.LastPingTime = &timeStr
		}
	}

	return utils.SuccessResponse(c, fiber.Map{
		"session": session,
		"summary": summary,
		"source":  "direct_query",
	})
}
