package services

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"Falcon/Models"

	"gorm.io/gorm"
)

type TripSummaryService struct {
	DB          *gorm.DB
	OSRMService *OSRMService
}

func NewTripSummaryService(db *gorm.DB) *TripSummaryService {
	return &TripSummaryService{
		DB:          db,
		OSRMService: NewOSRMService(),
	}
}

// ============================================================================
// Public API Methods
// ============================================================================

func (s *TripSummaryService) GetSummaryWithPings(summaryID uint) (*Models.TripSummary, error) {
	var summary Models.TripSummary

	if err := s.DB.
		Preload("ActualLegs", func(db *gorm.DB) *gorm.DB {
			return db.Where("leg_type = ?", "actual").Order("sequence_number ASC")
		}).
		Preload("OptimalLegs", func(db *gorm.DB) *gorm.DB {
			return db.Where("leg_type = ?", "optimal").Order("sequence_number ASC")
		}).
		Preload("ParentTrip").
		First(&summary, summaryID).Error; err != nil {
		return nil, fmt.Errorf("summary not found: %w", err)
	}

	if err := s.populatePings(&summary); err != nil {
		return nil, err
	}

	return &summary, nil
}

func (s *TripSummaryService) GetSummaryByParentTripIDWithPings(parentTripID uint) (*Models.TripSummary, error) {
	var summary Models.TripSummary

	if err := s.DB.
		Preload("ActualLegs", func(db *gorm.DB) *gorm.DB {
			return db.Where("leg_type = ?", "actual").Order("sequence_number ASC")
		}).
		Preload("OptimalLegs", func(db *gorm.DB) *gorm.DB {
			return db.Where("leg_type = ?", "optimal").Order("sequence_number ASC")
		}).
		Preload("ParentTrip").
		Where("parent_trip_id = ?", parentTripID).
		First(&summary).Error; err != nil {
		return nil, fmt.Errorf("summary not found for parent trip: %w", err)
	}

	if err := s.populatePings(&summary); err != nil {
		return nil, err
	}

	return &summary, nil
}

// populatePings fetches and attaches location pings to summary and legs
func (s *TripSummaryService) populatePings(summary *Models.TripSummary) error {
	var parentTrip Models.ParentTrip
	if err := s.DB.First(&parentTrip, summary.ParentTripID).Error; err != nil {
		return fmt.Errorf("parent trip not found: %w", err)
	}

	if parentTrip.SessionID == nil || *parentTrip.SessionID == 0 {
		return fmt.Errorf("parent trip has no session_id")
	}

	// Fetch all pings from the session within the trip timeframe
	startTime := summary.StartTime
	endTime := summary.EndTime
	if summary.ReturnedToBase && summary.ReturnTime != nil {
		endTime = *summary.ReturnTime
	}

	var allPings []Models.LocationPing
	if err := s.DB.Where("session_id = ? AND time_stamp >= ? AND time_stamp <= ?",
		parentTrip.SessionID, startTime, endTime).
		Order("time_stamp ASC").
		Find(&allPings).Error; err != nil {
		return fmt.Errorf("failed to fetch location pings: %w", err)
	}

	summary.LocationPings = allPings

	// Attach pings to each actual leg
	for i := range summary.ActualLegs {
		summary.ActualLegs[i].LocationPings = filterPingsBetween(
			allPings,
			summary.ActualLegs[i].DepartureTime,
			summary.ActualLegs[i].ArrivalTime,
		)
	}

	return nil
}

// ============================================================================
// Main Entry Point - REVISED
// ============================================================================

func (s *TripSummaryService) GenerateSummary(parentTripID uint) (*Models.TripSummary, error) {
	// 1. Load parent trip with session
	var parentTrip Models.ParentTrip
	if err := s.DB.Preload("ContainerTrips").First(&parentTrip, parentTripID).Error; err != nil {
		return nil, fmt.Errorf("parent trip not found: %w", err)
	}

	if parentTrip.SessionID == nil || *parentTrip.SessionID == 0 {
		return nil, fmt.Errorf("parent trip has no session_id - cannot generate summary")
	}

	log.Printf("🔍 Generating Summary for ParentTrip %d (Session %d)", parentTripID, parentTrip.SessionID)

	// 2. Get session info
	var session Models.DriverSession
	if err := s.DB.First(&session, parentTrip.SessionID).Error; err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	// 3. Get start terminal location
	var startTerminal Models.Terminal
	if err := s.DB.Where("name = ?", parentTrip.Terminal).First(&startTerminal).Error; err != nil {
		return nil, fmt.Errorf("start terminal not found: %s", parentTrip.Terminal)
	}
	startTerminalLocation := GeoPoint{Lat: startTerminal.Latitude, Lng: startTerminal.Longitude}

	// 4. Fetch ALL pings from this session (session-based, not driver-based)
	var sessionPings []Models.LocationPing
	query := s.DB.Where("session_id = ?", parentTrip.SessionID).Order("time_stamp ASC")

	// EDGE CASE: If session is ongoing, limit to reasonable timeframe
	if session.EndTime == nil {
		// Session still active - fetch up to now
		query = query.Where("time_stamp <= ?", time.Now())
	}

	if err := query.Find(&sessionPings).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch session pings: %w", err)
	}

	if len(sessionPings) == 0 {
		return nil, fmt.Errorf("no pings found for session %d", parentTrip.SessionID)
	}

	log.Printf("   - Session pings: %d (from %s to %s)",
		len(sessionPings),
		sessionPings[0].TimeStamp.Format("15:04:05"),
		sessionPings[len(sessionPings)-1].TimeStamp.Format("15:04:05"),
	)

	// 5. CRITICAL: Filter pings to this parent trip's timeframe
	// Multiple parent trips can exist in same session - we need THIS trip's deliveries only
	tripWindowStart := parentTrip.CreatedAt.Add(-2 * time.Hour) // Buffer before trip creation
	tripWindowEnd := time.Now()

	// If session ended, use that as upper bound
	if session.EndTime != nil {
		tripWindowEnd = *session.EndTime
	}

	// EDGE CASE: Check if there's a NEXT parent trip in same session
	var nextTrip Models.ParentTrip
	err := s.DB.Where("session_id = ? AND created_at > ? AND id != ?",
		parentTrip.SessionID, parentTrip.CreatedAt, parentTrip.ID).
		Order("created_at ASC").
		First(&nextTrip).Error

	if err == nil {
		// There's another trip after this one - use its creation as upper bound
		tripWindowEnd = nextTrip.CreatedAt
		log.Printf("   - Multiple trips in session detected, limiting to next trip start")
	}

	tripPings := filterPingsBetween(sessionPings, tripWindowStart, tripWindowEnd)
	log.Printf("   - Trip window pings: %d", len(tripPings))

	if len(tripPings) < 5 {
		return nil, fmt.Errorf("insufficient pings in trip window (need at least 5, got %d)", len(tripPings))
	}

	// 6. Detect delivery visits
	visits := s.detectDeliveryVisits(parentTrip, tripPings)
	if len(visits) == 0 {
		return nil, fmt.Errorf("no delivery visits detected")
	}

	// Sort by arrival time
	sort.Slice(visits, func(i, j int) bool {
		return visits[i].ArrivalTime.Before(visits[j].ArrivalTime)
	})

	firstDeliveryTime := visits[0].ArrivalTime
	lastDeliveryDepartureTime := visits[len(visits)-1].DepartureTime

	log.Printf("   - Visits detected: %d", len(visits))
	log.Printf("   - First delivery: %s at %s",
		visits[0].LocationName,
		firstDeliveryTime.Format("15:04:05"),
	)
	log.Printf("   - Last delivery departure: %s", lastDeliveryDepartureTime.Format("15:04:05"))

	// 7. Find ACTUAL trip start (last terminal departure before first delivery)
	tripStartTime := s.findTripStart(tripPings, startTerminalLocation, firstDeliveryTime, parentTrip.CreatedAt)
	log.Printf("   - Trip start: %s", tripStartTime.Format("15:04:05"))

	// EDGE CASE: Ensure trip start is not after first delivery
	if tripStartTime.After(firstDeliveryTime) {
		log.Printf("   ⚠️  Trip start after first delivery - adjusting to 30 min before")
		tripStartTime = firstDeliveryTime.Add(-30 * time.Minute)
	}

	// 8. Trip end = when driver left last delivery
	tripEndTime := lastDeliveryDepartureTime

	// 9. Filter pings for actual trip (start to end, NOT including return yet)
	actualTripPings := filterPingsBetween(tripPings, tripStartTime, tripEndTime)
	log.Printf("   - Actual trip pings: %d", len(actualTripPings))

	// 10. Check for return to ANY terminal AFTER trip end
	returnTerminalName, returnTime := s.detectTerminalReturn(tripPings, lastDeliveryDepartureTime, tripWindowEnd)
	returnedToBase := (returnTime != nil)

	if returnedToBase {
		log.Printf("   - Returned to terminal: %s at %s",
			returnTerminalName,
			returnTime.Format("15:04:05"),
		)
	} else {
		log.Printf("   - No terminal return detected")
	}

	// 11. Build actual delivery legs
	actualDeliveryLegs := s.buildDeliveryLegs(startTerminalLocation, visits, actualTripPings, tripStartTime)

	// 12. Add return leg if applicable
	var actualLegs []Models.TripLeg
	var returnTerminalLocation GeoPoint
	var finalReturnTime *time.Time

	if returnedToBase {
		var returnTerminal Models.Terminal
		if err := s.DB.Where("name = ?", returnTerminalName).First(&returnTerminal).Error; err != nil {
			log.Printf("   ⚠️  Return terminal not found: %s", returnTerminalName)
			returnedToBase = false
		} else {
			returnTerminalLocation = GeoPoint{Lat: returnTerminal.Latitude, Lng: returnTerminal.Longitude}
			returnPings := filterPingsBetween(tripPings, lastDeliveryDepartureTime, *returnTime)

			returnLeg := s.buildReturnLeg(
				visits[len(visits)-1].Location,
				visits[len(visits)-1].LocationName,
				returnTerminalLocation,
				returnTerminalName,
				lastDeliveryDepartureTime,
				*returnTime,
				returnPings,
				len(actualDeliveryLegs)+1,
			)
			actualLegs = append(actualDeliveryLegs, returnLeg)
			finalReturnTime = returnTime
		}
	}

	if !returnedToBase {
		actualLegs = actualDeliveryLegs
	}

	// 13. Calculate actual distance (delivery legs only)
	totalActualDistance := 0.0
	for _, leg := range actualDeliveryLegs {
		totalActualDistance += leg.ActualDistance
	}

	// 14. Build optimal route with OSRM
	deliveryLocations := []GeoPoint{}
	for _, visit := range visits {
		deliveryLocations = append(deliveryLocations, visit.Location)
	}

	// Use return terminal if driver returned, otherwise use start terminal
	terminalForOptimal := startTerminalLocation
	if returnedToBase {
		terminalForOptimal = returnTerminalLocation
		log.Printf("   - Using return terminal for optimal route: %s", returnTerminalName)
	}

	osrmResult, err := s.OSRMService.CalculateOptimalRoundTrip(terminalForOptimal, deliveryLocations)
	if err != nil {
		return nil, fmt.Errorf("OSRM calculation failed: %w", err)
	}

	// Extract geometry
	var osrmGeometry string
	if len(osrmResult.Trips) > 0 {
		osrmGeometry = osrmResult.Trips[0].Geometry
	}

	// 15. Build optimal legs
	optimalLegs := s.buildOptimalLegs(terminalForOptimal, deliveryLocations, osrmResult, visits)

	// 16. Calculate optimal distance (WITHOUT return leg)
	osrmDistance := 0.0
	osrmDuration := 0

	for i := 0; i < len(optimalLegs)-1; i++ { // Exclude return leg
		osrmDistance += optimalLegs[i].OSRMDistance
		osrmDuration += optimalLegs[i].TravelDuration
	}

	// Fallback to OSRM legs if zero
	if osrmDistance == 0 && len(osrmResult.Trips) > 0 {
		for i := 0; i < len(osrmResult.Trips[0].Legs)-1; i++ {
			osrmDistance += osrmResult.Trips[0].Legs[i].Distance / 1000.0
			osrmDuration += int(osrmResult.Trips[0].Legs[i].Duration)
		}
	}

	// 17. Calculate metrics
	efficiency := 0.0
	if osrmDistance > 0 {
		efficiency = (osrmDistance / totalActualDistance) * 100
	}

	tripDuration := int(tripEndTime.Sub(tripStartTime).Seconds())
	avgSpeed := 0.0
	if tripDuration > 0 {
		avgSpeed = totalActualDistance / (float64(tripDuration) / 3600.0)
	}

	// 18. Find skipped deliveries
	visitedIDs := make(map[uint]bool)
	for _, visit := range visits {
		for _, containerID := range visit.ContainerTripIDs {
			visitedIDs[containerID] = true
		}
	}

	skipped := []uint{}
	for _, ct := range parentTrip.ContainerTrips {
		if !visitedIDs[ct.ID] {
			// Only count as skipped if location has coordinates
			var fm Models.FeeMapping
			err := s.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
				ct.Company, ct.Terminal, ct.DropOffPoint).First(&fm).Error

			if err == nil && fm.Latitude != 0 && fm.Longitude != 0 {
				skipped = append(skipped, ct.ID)
			}
		}
	}

	skippedJSON, _ := json.Marshal(skipped)

	// 19. Determine end location
	var endLocationLat, endLocationLng float64
	if returnedToBase {
		endLocationLat = returnTerminalLocation.Lat
		endLocationLng = returnTerminalLocation.Lng
	} else {
		lastVisit := visits[len(visits)-1]
		endLocationLat = lastVisit.Location.Lat
		endLocationLng = lastVisit.Location.Lng
	}

	// 20. Create summary
	summary := &Models.TripSummary{
		ParentTripID:      parentTripID,
		StartLocationLat:  startTerminalLocation.Lat,
		StartLocationLng:  startTerminalLocation.Lng,
		EndLocationLat:    endLocationLat,
		EndLocationLng:    endLocationLng,
		StartTime:         tripStartTime,
		EndTime:           tripEndTime,
		TripDuration:      tripDuration,
		ActualDistance:    totalActualDistance,
		OSRMDistance:      osrmDistance,
		DistanceDeviation: totalActualDistance - osrmDistance,
		RouteEfficiency:   efficiency,
		OSRMDuration:      osrmDuration,
		TimeDeviation:     tripDuration - osrmDuration,
		AverageSpeed:      avgSpeed,
		TotalDeliveries:   len(visits),
		SkippedDeliveries: string(skippedJSON),
		IsComplete:        len(skipped) == 0,
		ReturnedToBase:    returnedToBase,
		ReturnTime:        finalReturnTime,
		OSRMGeometry:      osrmGeometry,
	}

	// 21. Save to database
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(summary).Error; err != nil {
			return err
		}

		for i := range actualLegs {
			actualLegs[i].TripSummaryID = summary.ID
			if err := tx.Create(&actualLegs[i]).Error; err != nil {
				return err
			}
		}

		for i := range optimalLegs {
			optimalLegs[i].TripSummaryID = summary.ID
			if err := tx.Create(&optimalLegs[i]).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to save summary: %w", err)
	}

	log.Printf("✅ Summary saved - Distance: %.2fkm, Efficiency: %.1f%%, Deliveries: %d/%d",
		totalActualDistance, efficiency, len(visits), len(parentTrip.ContainerTrips))

	return summary, nil
}

// ============================================================================
// Helper Methods - REVISED
// ============================================================================

// findTripStart finds when the driver actually left the terminal to start deliveries
func (s *TripSummaryService) findTripStart(pings []Models.LocationPing, terminal GeoPoint, firstDeliveryTime time.Time, parentTripCreatedAt time.Time) time.Time {
	var lastTerminalPing time.Time
	wasAtTerminal := false

	// Work backwards from first delivery
	for _, ping := range pings {
		// Skip pings after first delivery
		if ping.TimeStamp.After(firstDeliveryTime) {
			continue
		}

		// Skip pings before parent trip was created (shouldn't use pings from before trip existed)
		if ping.TimeStamp.Before(parentTripCreatedAt.Add(-30 * time.Minute)) {
			continue
		}

		distance := haversine(ping.Lat, ping.Lng, terminal.Lat, terminal.Lng)

		if distance <= 0.15 { // 150m terminal radius
			wasAtTerminal = true
			lastTerminalPing = ping.TimeStamp
		} else if distance > 0.2 && wasAtTerminal {
			// Driver has left terminal - use last terminal ping
			return lastTerminalPing
		}
	}

	// If we found terminal pings, use the last one
	if !lastTerminalPing.IsZero() {
		return lastTerminalPing
	}

	// Fallback: Find first ping in our window
	for _, ping := range pings {
		if ping.TimeStamp.After(parentTripCreatedAt.Add(-30*time.Minute)) &&
			ping.TimeStamp.Before(firstDeliveryTime) {
			return ping.TimeStamp
		}
	}

	// Last resort: 1 hour before first delivery
	return firstDeliveryTime.Add(-1 * time.Hour)
}

// detectTerminalReturn checks if driver returned to ANY terminal after trip end
func (s *TripSummaryService) detectTerminalReturn(pings []Models.LocationPing, afterTime time.Time, windowEnd time.Time) (string, *time.Time) {
	// Get all terminals
	var terminals []Models.Terminal
	if err := s.DB.Find(&terminals).Error; err != nil {
		log.Printf("Failed to fetch terminals: %v", err)
		return "", nil
	}

	// EDGE CASE: Look for sustained terminal presence, not just a single ping
	terminalVisits := make(map[string][]time.Time) // terminal name -> ping times

	for _, ping := range pings {
		// Only check pings after trip end
		if ping.TimeStamp.Before(afterTime) || ping.TimeStamp.Equal(afterTime) {
			continue
		}

		// Don't check beyond window end
		if ping.TimeStamp.After(windowEnd) {
			break
		}

		// Check each terminal
		for _, terminal := range terminals {
			if terminal.Latitude == 0 || terminal.Longitude == 0 {
				continue
			}

			distance := haversine(ping.Lat, ping.Lng, terminal.Latitude, terminal.Longitude)
			if distance <= 0.15 { // 150m
				terminalVisits[terminal.Name] = append(terminalVisits[terminal.Name], ping.TimeStamp)
			}
		}
	}

	// Find terminal with earliest sustained presence (at least 2 pings)
	var earliestReturn time.Time
	var returnTerminal string

	for terminalName, pingTimes := range terminalVisits {
		if len(pingTimes) >= 2 {
			if earliestReturn.IsZero() || pingTimes[0].Before(earliestReturn) {
				earliestReturn = pingTimes[0]
				returnTerminal = terminalName
			}
		}
	}

	if !earliestReturn.IsZero() {
		return returnTerminal, &earliestReturn
	}

	return "", nil
}

// detectDeliveryVisits finds all delivery visits with improved edge case handling
func (s *TripSummaryService) detectDeliveryVisits(parentTrip Models.ParentTrip, pings []Models.LocationPing) []DeliveryVisit {
	potentialVisits := []DeliveryVisit{}

	for idx, containerTrip := range parentTrip.ContainerTrips {
		var feeMapping Models.FeeMapping
		err := s.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			containerTrip.Company, containerTrip.Terminal, containerTrip.DropOffPoint).
			First(&feeMapping).Error

		if err != nil {
			log.Printf("     Container %d: No coordinates available", idx+1)
			continue
		}
		// The pin lives on drop_off_points; this table no longer carries one.
		if err := Models.HydrateFeeMappingCoord(s.DB, &feeMapping); err != nil ||
			feeMapping.Latitude == 0 || feeMapping.Longitude == 0 {
			log.Printf("     Container %d: No coordinates available", idx+1)
			continue
		}

		dropOffLocation := GeoPoint{Lat: feeMapping.Latitude, Lng: feeMapping.Longitude}

		// Find geofence entry/exit
		var entryTime, exitTime time.Time
		inGeofence := false
		pingsInGeofence := 0

		for _, ping := range pings {
			distance := haversine(ping.Lat, ping.Lng, dropOffLocation.Lat, dropOffLocation.Lng)

			if distance <= 0.1 { // 100m geofence
				pingsInGeofence++
				if !inGeofence {
					entryTime = ping.TimeStamp
					inGeofence = true
				}
				exitTime = ping.TimeStamp
			} else if distance > 0.15 && inGeofence {
				// Left geofence (with buffer to avoid edge bouncing)
				break
			}
		}

		if entryTime.IsZero() {
			log.Printf("     Container %d (%s): Never entered geofence",
				idx+1, containerTrip.DropOffPoint)
			continue
		}

		dwellDuration := exitTime.Sub(entryTime)

		// EDGE CASE: Minimum dwell time adjusted based on number of pings
		minDwellTime := 30 * time.Minute
		if pingsInGeofence >= 3 && dwellDuration >= 20*time.Minute {
			// If we have good ping coverage, accept 20 min dwell
			minDwellTime = 20 * time.Minute
		}

		if dwellDuration >= minDwellTime {
			log.Printf("     Container %d: ✅ Visit confirmed (dwell: %.1f min, %d pings)",
				idx+1, dwellDuration.Minutes(), pingsInGeofence)

			potentialVisits = append(potentialVisits, DeliveryVisit{
				ContainerTripIDs: []uint{containerTrip.ID},
				Location:         dropOffLocation,
				LocationName:     containerTrip.DropOffPoint,
				ArrivalTime:      entryTime,
				DepartureTime:    exitTime,
				DwellTime:        int(dwellDuration.Seconds()),
			})
		} else {
			log.Printf("     Container %d: ❌ Dwell too short (%.1f min)",
				idx+1, dwellDuration.Minutes())
		}
	}

	// Merge visits at same location
	mergedVisits := []DeliveryVisit{}

	for _, visit := range potentialVisits {
		merged := false

		for i := range mergedVisits {
			distance := haversine(
				visit.Location.Lat, visit.Location.Lng,
				mergedVisits[i].Location.Lat, mergedVisits[i].Location.Lng,
			)

			// Same location and overlapping times
			timeOverlap := visit.ArrivalTime.Before(mergedVisits[i].DepartureTime.Add(10 * time.Minute))

			if distance < 0.015 && timeOverlap { // 15 meters
				// Merge visits
				if visit.ArrivalTime.Before(mergedVisits[i].ArrivalTime) {
					mergedVisits[i].ArrivalTime = visit.ArrivalTime
				}
				if visit.DepartureTime.After(mergedVisits[i].DepartureTime) {
					mergedVisits[i].DepartureTime = visit.DepartureTime
				}

				mergedVisits[i].ContainerTripIDs = append(mergedVisits[i].ContainerTripIDs, visit.ContainerTripIDs...)
				mergedVisits[i].DwellTime = int(mergedVisits[i].DepartureTime.Sub(mergedVisits[i].ArrivalTime).Seconds())

				merged = true
				log.Printf("     ✅ Merged visit at %s (now %d containers)",
					visit.LocationName, len(mergedVisits[i].ContainerTripIDs))
				break
			}
		}

		if !merged {
			mergedVisits = append(mergedVisits, visit)
		}
	}

	// Sort by arrival
	sort.Slice(mergedVisits, func(i, j int) bool {
		return mergedVisits[i].ArrivalTime.Before(mergedVisits[j].ArrivalTime)
	})

	return mergedVisits
}

func (s *TripSummaryService) buildDeliveryLegs(terminal GeoPoint, visits []DeliveryVisit, pings []Models.LocationPing, startTime time.Time) []Models.TripLeg {
	legs := []Models.TripLeg{}
	prevLocation := terminal
	prevTime := startTime
	prevName := "Terminal"
	sequence := 1

	for _, visit := range visits {
		legPings := filterPingsBetween(pings, prevTime, visit.ArrivalTime)
		distance := calculateDistanceFromPings(legPings)
		duration := int(visit.ArrivalTime.Sub(prevTime).Seconds())

		avgSpeed := 0.0
		if duration > 0 {
			avgSpeed = distance / (float64(duration) / 3600.0)
		}

		maxSpeed := calculateMaxSpeed(legPings)

		containerIDsJSON, _ := json.Marshal(visit.ContainerTripIDs)

		leg := Models.TripLeg{
			LegType:         "actual",
			SequenceNumber:  sequence,
			TripStructID:    &visit.ContainerTripIDs[0],
			ContainerIDs:    string(containerIDsJSON),
			FromLocationLat: prevLocation.Lat,
			FromLocationLng: prevLocation.Lng,
			ToLocationLat:   visit.Location.Lat,
			ToLocationLng:   visit.Location.Lng,
			FromName:        prevName,
			ToName:          visit.LocationName,
			DepartureTime:   prevTime,
			ArrivalTime:     visit.ArrivalTime,
			DwellTime:       visit.DwellTime,
			TravelDuration:  duration,
			ActualDistance:  distance,
			AverageSpeed:    avgSpeed,
			MaxSpeed:        maxSpeed,
		}

		// Get OSRM route
		osrmRoute, err := s.OSRMService.GetRoute(prevLocation, visit.Location)
		if err == nil && len(osrmRoute.Routes) > 0 {
			leg.OSRMDistance = osrmRoute.Routes[0].Distance / 1000.0
			leg.OSRMGeometry = osrmRoute.Routes[0].Geometry
		}

		legs = append(legs, leg)

		prevLocation = visit.Location
		prevTime = visit.DepartureTime
		prevName = visit.LocationName
		sequence++
	}

	return legs
}

func (s *TripSummaryService) buildReturnLeg(fromLocation GeoPoint, fromName string, toLocation GeoPoint, toName string, departureTime time.Time, arrivalTime time.Time, pings []Models.LocationPing, sequence int) Models.TripLeg {
	distance := calculateDistanceFromPings(pings)
	duration := int(arrivalTime.Sub(departureTime).Seconds())

	avgSpeed := 0.0
	if duration > 0 {
		avgSpeed = distance / (float64(duration) / 3600.0)
	}

	maxSpeed := calculateMaxSpeed(pings)

	leg := Models.TripLeg{
		LegType:         "actual",
		SequenceNumber:  sequence,
		TripStructID:    nil,
		FromLocationLat: fromLocation.Lat,
		FromLocationLng: fromLocation.Lng,
		ToLocationLat:   toLocation.Lat,
		ToLocationLng:   toLocation.Lng,
		FromName:        fromName,
		ToName:          fmt.Sprintf("Return to %s", toName),
		DepartureTime:   departureTime,
		ArrivalTime:     arrivalTime,
		DwellTime:       0,
		TravelDuration:  duration,
		ActualDistance:  distance,
		AverageSpeed:    avgSpeed,
		MaxSpeed:        maxSpeed,
	}

	osrmRoute, err := s.OSRMService.GetRoute(fromLocation, toLocation)
	if err == nil && len(osrmRoute.Routes) > 0 {
		leg.OSRMDistance = osrmRoute.Routes[0].Distance / 1000.0
		leg.OSRMGeometry = osrmRoute.Routes[0].Geometry
	}

	return leg
}

func (s *TripSummaryService) buildOptimalLegs(terminal GeoPoint, deliveries []GeoPoint, osrmResult *OSRMTripResponse, visits []DeliveryVisit) []Models.TripLeg {
	legs := []Models.TripLeg{}
	optimizedOrder := s.OSRMService.GetOptimizedOrder(osrmResult)

	prevLocation := terminal
	prevName := "Terminal"

	for i, deliveryIdx := range optimizedOrder {
		if deliveryIdx >= len(deliveries) || deliveryIdx >= len(visits) {
			continue
		}

		delivery := deliveries[deliveryIdx]
		visit := visits[deliveryIdx]

		containerIDsJSON, _ := json.Marshal(visit.ContainerTripIDs)

		leg := Models.TripLeg{
			LegType:         "optimal",
			SequenceNumber:  i + 1,
			TripStructID:    &visit.ContainerTripIDs[0],
			ContainerIDs:    string(containerIDsJSON),
			FromLocationLat: prevLocation.Lat,
			FromLocationLng: prevLocation.Lng,
			ToLocationLat:   delivery.Lat,
			ToLocationLng:   delivery.Lng,
			FromName:        prevName,
			ToName:          visit.LocationName,
			IsOptimal:       true,
		}

		if i < len(osrmResult.Trips[0].Legs) {
			osrmLeg := osrmResult.Trips[0].Legs[i]
			leg.OSRMDistance = osrmLeg.Distance / 1000.0
			leg.TravelDuration = int(osrmLeg.Duration)

			if len(osrmLeg.Steps) > 0 {
				leg.OSRMGeometry = osrmLeg.Steps[0].Geometry
			}
		}

		legs = append(legs, leg)
		prevLocation = delivery
		prevName = visit.LocationName
	}

	// Return leg
	returnLeg := Models.TripLeg{
		LegType:         "optimal",
		SequenceNumber:  len(optimizedOrder) + 1,
		FromLocationLat: prevLocation.Lat,
		FromLocationLng: prevLocation.Lng,
		ToLocationLat:   terminal.Lat,
		ToLocationLng:   terminal.Lng,
		FromName:        prevName,
		ToName:          "Terminal",
		IsOptimal:       true,
	}

	if len(optimizedOrder) < len(osrmResult.Trips[0].Legs) {
		osrmLeg := osrmResult.Trips[0].Legs[len(optimizedOrder)]
		returnLeg.OSRMDistance = osrmLeg.Distance / 1000.0
		returnLeg.TravelDuration = int(osrmLeg.Duration)

		if len(osrmLeg.Steps) > 0 {
			returnLeg.OSRMGeometry = osrmLeg.Steps[0].Geometry
		}
	}

	legs = append(legs, returnLeg)
	return legs
}

// ============================================================================
// Utility Functions
// ============================================================================

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

func filterPingsBetween(pings []Models.LocationPing, start, end time.Time) []Models.LocationPing {
	result := []Models.LocationPing{}
	for _, ping := range pings {
		if (ping.TimeStamp.After(start) || ping.TimeStamp.Equal(start)) &&
			(ping.TimeStamp.Before(end) || ping.TimeStamp.Equal(end)) {
			result = append(result, ping)
		}
	}
	return result
}

func calculateDistanceFromPings(pings []Models.LocationPing) float64 {
	if len(pings) < 2 {
		return 0
	}

	totalDistance := 0.0
	for i := 1; i < len(pings); i++ {
		distance := haversine(pings[i-1].Lat, pings[i-1].Lng, pings[i].Lat, pings[i].Lng)
		// EDGE CASE: Filter out unrealistic jumps (GPS errors)
		if distance < 5.0 { // Less than 5km between pings
			totalDistance += distance
		}
	}

	return totalDistance
}

func calculateMaxSpeed(pings []Models.LocationPing) float64 {
	if len(pings) < 2 {
		return 0
	}

	maxSpeed := 0.0
	for i := 1; i < len(pings); i++ {
		distance := haversine(pings[i-1].Lat, pings[i-1].Lng, pings[i].Lat, pings[i].Lng)
		duration := pings[i].TimeStamp.Sub(pings[i-1].TimeStamp).Seconds()

		if duration > 0 {
			speed := (distance / duration) * 3600

			// EDGE CASE: Ignore unrealistic speeds (GPS errors)
			if speed < 200 && speed > maxSpeed { // Max 200 km/h
				maxSpeed = speed
			}
		}
	}

	return maxSpeed
}

type DeliveryVisit struct {
	ContainerTripIDs []uint
	Location         GeoPoint
	LocationName     string
	ArrivalTime      time.Time
	DepartureTime    time.Time
	DwellTime        int
}
