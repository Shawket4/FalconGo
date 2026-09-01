package workers

import (
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/services"
	"log"
	"math"
	"time"

	"gorm.io/gorm"
)

type TripSummaryWorker struct {
	DB                 *gorm.DB
	TripSummaryService *services.TripSummaryService
	ticker             *time.Ticker
}

func NewTripSummaryWorker(db *gorm.DB) *TripSummaryWorker {
	return &TripSummaryWorker{
		DB:                 db,
		TripSummaryService: services.NewTripSummaryService(db),
		ticker:             time.NewTicker(15 * time.Minute),
	}
}

func (w *TripSummaryWorker) Start() {
	log.Println("🚀 Trip Summary Worker started - runs every 15 minutes")

	// Run immediately
	go func() {
		defer Observability.Guard("trip_summary_worker")
		w.processPendingTrips()
	}()

	// Then periodically. Each tick is guarded on its own: a panic in one run
	// reports and is dropped, the loop survives, and the API server — which
	// an unrecovered panic here would have taken down with it — keeps serving.
	go func() {
		for range w.ticker.C {
			func() {
				defer Observability.Guard("trip_summary_worker")
				w.processPendingTrips()
			}()
		}
	}()
}

func (w *TripSummaryWorker) Stop() {
	w.ticker.Stop()
	log.Println("⏹️  Trip Summary Worker stopped")
}

func (w *TripSummaryWorker) processPendingTrips() {
	log.Println("🔄 Processing pending trip summaries...")

	// Find ParentTrips without summaries (Watanya only)
	var pendingTrips []Models.ParentTrip

	err := w.DB.
		Preload("ContainerTrips").
		Joins("LEFT JOIN trip_summaries ON trip_summaries.parent_trip_id = parent_trips.id").
		Where("trip_summaries.id IS NULL").
		Where("parent_trips.company = ?", "Watanya").
		Where("parent_trips.session_id IS NOT NULL AND parent_trips.session_id > 0"). // CRITICAL: Must have session
		Order("parent_trips.created_at ASC").
		Find(&pendingTrips).Error

	if err != nil {
		log.Printf("❌ Error fetching pending trips: %v", err)
		// The job boundary: this returns and the worker does nothing for the
		// next fifteen minutes, looking exactly like a run with no work.
		Observability.ReportJobFailure("trip_summary_worker", "load_pending", err)
		return
	}

	if len(pendingTrips) == 0 {
		log.Println("✅ No pending trips to process")
		return
	}

	log.Printf("📊 Found %d pending Watanya trips with sessions", len(pendingTrips))

	processed := 0
	skipped := 0
	errors := 0

	for _, trip := range pendingTrips {
		readyStatus := w.isTripReadyForSummary(trip)

		switch readyStatus {
		case TripReady:
			log.Printf("⚙️  Generating summary for ParentTrip ID: %d (Session: %d)", trip.ID, trip.SessionID)

			summary, err := w.TripSummaryService.GenerateSummary(trip.ID)
			if err != nil {
				log.Printf("❌ Failed to generate summary for trip %d: %v", trip.ID, err)
				errors++
				continue
			}

			log.Printf("✅ Summary generated for ParentTrip ID: %d (Distance: %.2fkm, Deliveries: %d, Returned: %v)",
				trip.ID, summary.ActualDistance, summary.TotalDeliveries, summary.ReturnedToBase)
			processed++

		case TripInProgress:
			log.Printf("   ⏳ Trip %d still in progress", trip.ID)
			skipped++

		case TripNoSession:
			log.Printf("   ⚠️  Trip %d has no session", trip.ID)
			skipped++

		case TripNoPings:
			log.Printf("   ⚠️  Trip %d has no location pings yet", trip.ID)
			skipped++
		}
	}

	log.Printf("✅ Processed %d trips, skipped %d, errors %d", processed, skipped, errors)

	// Individual failures are logged per trip and stay there — one bad trip is
	// ordinary. A run where every attempt failed is not: summaries have simply
	// stopped being produced, and the counters are the only place that shows it.
	if errors > 0 && processed == 0 {
		Observability.ReportBatchFailure("trip_summary_worker", errors, errors+skipped)
	}
}

// ============================================================================
// Trip Readiness Logic - REVISED
// ============================================================================

type TripReadiness int

const (
	TripReady TripReadiness = iota
	TripInProgress
	TripNoSession
	TripNoPings
)

func (w *TripSummaryWorker) isTripReadyForSummary(trip Models.ParentTrip) TripReadiness {
	// 1. CRITICAL: Must have session
	if trip.SessionID == nil || *trip.SessionID == 0 {
		return TripNoSession
	}

	// 2. Get session info
	var session Models.DriverSession
	if err := w.DB.First(&session, trip.SessionID).Error; err != nil {
		log.Printf("   ⚠️  Session %d not found", trip.SessionID)
		return TripNoSession
	}

	// 3. Get terminal
	var terminal Models.Terminal
	if err := w.DB.Where("name = ?", trip.Terminal).First(&terminal).Error; err != nil {
		log.Printf("   ⚠️  Terminal not found: %s", trip.Terminal)
		return TripInProgress
	}

	terminalLocation := GeoPoint{Lat: terminal.Latitude, Lng: terminal.Longitude}

	// 4. Get session pings (not driver pings!)
	var sessionPings []Models.LocationPing
	err := w.DB.Where("session_id = ?", trip.SessionID).
		Order("time_stamp ASC").
		Find(&sessionPings).Error

	if err != nil || len(sessionPings) == 0 {
		return TripNoPings
	}

	// 5. EDGE CASE: Filter pings relevant to THIS parent trip
	// If there's a NEXT parent trip in same session, don't include those pings
	var nextTrip Models.ParentTrip
	hasNextTrip := false
	var tripWindowEnd time.Time

	err = w.DB.Where("session_id = ? AND created_at > ? AND id != ?",
		trip.SessionID, trip.CreatedAt, trip.ID).
		Order("created_at ASC").
		First(&nextTrip).Error

	if err == nil {
		hasNextTrip = true
		tripWindowEnd = nextTrip.CreatedAt
	} else if session.EndTime != nil {
		tripWindowEnd = *session.EndTime
	} else {
		tripWindowEnd = time.Now()
	}

	// Filter pings to this trip's window
	relevantPings := []Models.LocationPing{}
	for _, ping := range sessionPings {
		if ping.TimeStamp.After(trip.CreatedAt.Add(-30*time.Minute)) &&
			ping.TimeStamp.Before(tripWindowEnd) {
			relevantPings = append(relevantPings, ping)
		}
	}

	if len(relevantPings) < 5 {
		return TripNoPings
	}

	// 6. Check if driver has left terminal
	hasLeftTerminal := false
	for _, ping := range relevantPings {
		distance := haversine(ping.Lat, ping.Lng, terminalLocation.Lat, terminalLocation.Lng)
		if distance > 0.2 { // 200m
			hasLeftTerminal = true
			break
		}
	}

	if !hasLeftTerminal {
		return TripInProgress
	}

	// 7. Check completion status
	// Trip is ready if ANY of these is true:
	// A) Session has ended
	// B) There's a next parent trip (this one is done)
	// C) All deliveries completed AND driver returned to terminal
	// D) All deliveries completed AND no movement for 2+ hours

	// A) Session ended
	if session.EndTime != nil {
		log.Printf("   ✅ Trip %d ready: Session ended", trip.ID)
		return TripReady
	}

	// B) Next trip exists
	if hasNextTrip {
		log.Printf("   ✅ Trip %d ready: Next trip started", trip.ID)
		return TripReady
	}

	// C) Check deliveries + terminal return
	allDeliveriesCompleted := w.checkAllDeliveriesCompleted(trip, relevantPings)

	if allDeliveriesCompleted {
		// Check terminal return
		lastPing := relevantPings[len(relevantPings)-1]
		distanceFromTerminal := haversine(lastPing.Lat, lastPing.Lng, terminalLocation.Lat, terminalLocation.Lng)

		if distanceFromTerminal <= 0.15 {
			// Verify sustained presence at terminal
			recentTerminalPings := 0
			lookbackTime := time.Now().Add(-10 * time.Minute)

			for i := len(relevantPings) - 1; i >= 0; i-- {
				if relevantPings[i].TimeStamp.Before(lookbackTime) {
					break
				}
				dist := haversine(relevantPings[i].Lat, relevantPings[i].Lng, terminalLocation.Lat, terminalLocation.Lng)
				if dist <= 0.15 {
					recentTerminalPings++
				}
			}

			if recentTerminalPings >= 2 {
				log.Printf("   ✅ Trip %d ready: All deliveries done + returned to terminal", trip.ID)
				return TripReady
			}
		}

		// D) Check for extended inactivity
		lastPingTime := relevantPings[len(relevantPings)-1].TimeStamp
		inactiveDuration := time.Since(lastPingTime)

		if inactiveDuration >= 2*time.Hour {
			log.Printf("   ✅ Trip %d ready: All deliveries done + inactive for %.1f hours",
				trip.ID, inactiveDuration.Hours())
			return TripReady
		}

		// EDGE CASE: If deliveries done but driver still moving, check if they're heading back
		isHeadingToTerminal := w.isHeadingTowardsTerminal(relevantPings, terminalLocation)
		if isHeadingToTerminal && inactiveDuration >= 30*time.Minute {
			log.Printf("   ✅ Trip %d ready: Deliveries done + heading to terminal", trip.ID)
			return TripReady
		}
	}

	return TripInProgress
}

// checkAllDeliveriesCompleted verifies all deliveries with coordinates have been visited
func (w *TripSummaryWorker) checkAllDeliveriesCompleted(trip Models.ParentTrip, pings []Models.LocationPing) bool {
	deliveriesWithCoords := 0
	deliveriesVisited := 0

	for _, containerTrip := range trip.ContainerTrips {
		var feeMapping Models.FeeMapping
		err := w.DB.Where("company = ? AND terminal = ? AND drop_off_point = ?",
			containerTrip.Company, containerTrip.Terminal, containerTrip.DropOffPoint).
			First(&feeMapping).Error

		if err != nil {
			continue
		}
		// The pin lives on drop_off_points; this table no longer carries one.
		if err := Models.HydrateFeeMappingCoord(w.DB, &feeMapping); err != nil ||
			feeMapping.Latitude == 0 || feeMapping.Longitude == 0 {
			continue
		}

		deliveriesWithCoords++

		dropOffLocation := GeoPoint{Lat: feeMapping.Latitude, Lng: feeMapping.Longitude}

		// Check if visited
		var entryTime, exitTime time.Time
		inGeofence := false
		pingsInGeofence := 0

		for _, ping := range pings {
			distance := haversine(ping.Lat, ping.Lng, dropOffLocation.Lat, dropOffLocation.Lng)

			if distance <= 0.1 {
				pingsInGeofence++
				if !inGeofence {
					entryTime = ping.TimeStamp
					inGeofence = true
				}
				exitTime = ping.TimeStamp
			} else if distance > 0.15 && inGeofence {
				break
			}
		}

		if !entryTime.IsZero() {
			dwellDuration := exitTime.Sub(entryTime)

			// Accept shorter dwell if good ping coverage
			minDwell := 30 * time.Minute
			if pingsInGeofence >= 3 {
				minDwell = 20 * time.Minute
			}

			if dwellDuration >= minDwell {
				deliveriesVisited++
			}
		}
	}

	return deliveriesWithCoords > 0 && deliveriesVisited >= deliveriesWithCoords
}

// isHeadingTowardsTerminal checks if recent movement is towards terminal
func (w *TripSummaryWorker) isHeadingTowardsTerminal(pings []Models.LocationPing, terminal GeoPoint) bool {
	if len(pings) < 5 {
		return false
	}

	// Compare distance to terminal: last 3 pings vs 3 pings before that
	recentPings := pings[len(pings)-3:]
	olderPings := pings[len(pings)-6 : len(pings)-3]

	recentAvgDist := 0.0
	for _, ping := range recentPings {
		recentAvgDist += haversine(ping.Lat, ping.Lng, terminal.Lat, terminal.Lng)
	}
	recentAvgDist /= float64(len(recentPings))

	olderAvgDist := 0.0
	for _, ping := range olderPings {
		olderAvgDist += haversine(ping.Lat, ping.Lng, terminal.Lat, terminal.Lng)
	}
	olderAvgDist /= float64(len(olderPings))

	// Heading to terminal if recent distance is meaningfully less
	return recentAvgDist < olderAvgDist-0.5 // At least 500m closer
}

// ============================================================================
// Utilities
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

type GeoPoint struct {
	Lat float64
	Lng float64
}
