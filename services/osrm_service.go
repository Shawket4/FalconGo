package services

import (
	"Falcon/Models"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type OSRMService struct {
	BaseURL string
}

// NewOSRMService talks to OSRM directly, and is the one caller that still
// does.
//
// It uses OSRM's /trip/v1 service — travelling-salesman ordering for
// multi-drop rounds — which etit-proxy's client does not expose; that client
// covers /route/v1 and /match/v1 only. Everything that MEASURES a distance now
// goes through services.Route so the time-gated exclude is applied once; this
// one ORDERS stops, where that rule matters less.
//
// The base URL is read from the same OSRM_URL etit-proxy uses, so the two
// cannot end up pointed at different instances by accident.
func NewOSRMService() *OSRMService {
	base := os.Getenv("OSRM_URL")
	if base == "" {
		base = "http://localhost:5000"
	}
	return &OSRMService{BaseURL: base}
}

// ============================================================================
// Response Structs
// ============================================================================

type OSRMTripResponse struct {
	Code      string         `json:"code"`
	Trips     []OSRMTrip     `json:"trips"`
	Waypoints []OSRMWaypoint `json:"waypoints"`
}

type OSRMTrip struct {
	Distance float64   `json:"distance"` // meters
	Duration float64   `json:"duration"` // seconds
	Geometry string    `json:"geometry"` // encoded polyline
	Legs     []OSRMLeg `json:"legs"`
}

type OSRMLeg struct {
	Distance float64    `json:"distance"` // meters
	Duration float64    `json:"duration"` // seconds
	Steps    []OSRMStep `json:"steps"`
}

type OSRMStep struct {
	Distance float64 `json:"distance"`
	Duration float64 `json:"duration"`
	Geometry string  `json:"geometry"`
	Name     string  `json:"name"`
}

type OSRMWaypoint struct {
	WaypointIndex int       `json:"waypoint_index"`
	TripsIndex    int       `json:"trips_index"`
	Location      []float64 `json:"location"` // [lng, lat]
	Name          string    `json:"name"`
	Distance      float64   `json:"distance"`
}

type OSRMMatchResponse struct {
	Code      string         `json:"code"`
	Matchings []OSRMMatching `json:"matchings"`
}

type OSRMMatching struct {
	Distance   float64   `json:"distance"` // meters
	Duration   float64   `json:"duration"` // seconds
	Geometry   string    `json:"geometry"`
	Confidence float64   `json:"confidence"`
	Legs       []OSRMLeg `json:"legs"`
}

type OSRMTableResponse struct {
	Code         string         `json:"code"`
	Distances    [][]float64    `json:"distances"` // meters
	Durations    [][]float64    `json:"durations"` // seconds
	Sources      []OSRMWaypoint `json:"sources"`
	Destinations []OSRMWaypoint `json:"destinations"`
}

type OSRMRouteResponse struct {
	Code   string      `json:"code"`
	Routes []OSRMRoute `json:"routes"`
}

type OSRMRoute struct {
	Distance float64   `json:"distance"` // meters
	Duration float64   `json:"duration"` // seconds
	Geometry string    `json:"geometry"`
	Legs     []OSRMLeg `json:"legs"`
}

// ============================================================================
// GeoPoint Helper
// ============================================================================

type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// ============================================================================
// Main Methods
// ============================================================================

// CalculateOptimalRoundTrip calculates the optimal route visiting all delivery points
// and returning to the terminal (round trip)
func (s *OSRMService) CalculateOptimalRoundTrip(terminal GeoPoint, deliveries []GeoPoint) (*OSRMTripResponse, error) {
	// Build coordinates: terminal;delivery1;delivery2;...
	coords := fmt.Sprintf("%.6f,%.6f", terminal.Lng, terminal.Lat)
	for _, d := range deliveries {
		coords += fmt.Sprintf(";%.6f,%.6f", d.Lng, d.Lat)
	}

	// Build URL with correct parameters
	url := fmt.Sprintf(
		"%s/trip/v1/driving/%s?source=first&roundtrip=true&steps=true&geometries=polyline",
		s.BaseURL,
		coords,
	)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OSRM request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM response: %w", err)
	}

	var result OSRMTripResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM returned error code: %s", result.Code)
	}

	if len(result.Trips) == 0 {
		return nil, fmt.Errorf("OSRM returned no trips")
	}

	return &result, nil
}

// MatchRoute matches actual GPS pings to the road network
// Uses Models.LocationPing directly
func (s *OSRMService) MatchRoute(pings []Models.LocationPing) (*OSRMMatchResponse, error) {
	if len(pings) < 2 {
		return nil, fmt.Errorf("need at least 2 location pings to match route")
	}

	coordinates := [][]float64{}
	timestamps := []int64{}

	for _, ping := range pings {
		coordinates = append(coordinates, []float64{ping.Lng, ping.Lat})
		timestamps = append(timestamps, ping.TimeStamp.Unix())
	}

	// Build request body
	requestBody := map[string]interface{}{
		"coordinates": coordinates,
		"timestamps":  timestamps,
		"geometries":  "polyline",
		"overview":    "full",
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/match/v1/driving", s.BaseURL)
	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("OSRM match request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM match response: %w", err)
	}

	var result OSRMMatchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM match response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM match returned error code: %s", result.Code)
	}

	if len(result.Matchings) == 0 {
		return nil, fmt.Errorf("OSRM returned no route matches")
	}

	return &result, nil
}

// CalculateDistanceMatrix calculates distances and durations between all point pairs
func (s *OSRMService) CalculateDistanceMatrix(points []GeoPoint) (*OSRMTableResponse, error) {
	if len(points) < 2 {
		return nil, fmt.Errorf("need at least 2 points for distance matrix")
	}

	// Build coordinates
	coords := fmt.Sprintf("%.6f,%.6f", points[0].Lng, points[0].Lat)
	for i := 1; i < len(points); i++ {
		coords += fmt.Sprintf(";%.6f,%.6f", points[i].Lng, points[i].Lat)
	}

	url := fmt.Sprintf("%s/table/v1/driving/%s", s.BaseURL, coords)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OSRM table request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM table response: %w", err)
	}

	var result OSRMTableResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM table response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM table returned error code: %s", result.Code)
	}

	return &result, nil
}

// GetRoute calculates a simple route between two points (no optimization)
func (s *OSRMService) GetRoute(from, to GeoPoint) (*OSRMRouteResponse, error) {
	url := fmt.Sprintf(
		"%s/route/v1/driving/%.6f,%.6f;%.6f,%.6f?steps=true&geometries=polyline",
		s.BaseURL,
		from.Lng, from.Lat,
		to.Lng, to.Lat,
	)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OSRM route request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM route response: %w", err)
	}

	var result OSRMRouteResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM route response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM route returned error code: %s", result.Code)
	}

	if len(result.Routes) == 0 {
		return nil, fmt.Errorf("OSRM returned no routes")
	}

	return &result, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

// GetOptimizedOrder returns the optimized visit order from OSRM trip response
// Returns indices relative to the original deliveries array (0-based)
func (s *OSRMService) GetOptimizedOrder(response *OSRMTripResponse) []int {
	order := []int{}

	for _, wp := range response.Waypoints {
		// Skip terminal (waypoint_index 0)
		if wp.WaypointIndex > 0 {
			// Convert to 0-based delivery index
			order = append(order, wp.WaypointIndex-1)
		}
	}

	return order
}

// GetTotalDistance returns total distance in kilometers
func (s *OSRMService) GetTotalDistance(response *OSRMTripResponse) float64 {
	if len(response.Trips) == 0 {
		return 0
	}
	return response.Trips[0].Distance / 1000.0 // Convert to km
}

// GetTotalDuration returns total duration in seconds
func (s *OSRMService) GetTotalDuration(response *OSRMTripResponse) int {
	if len(response.Trips) == 0 {
		return 0
	}
	return int(response.Trips[0].Duration)
}
