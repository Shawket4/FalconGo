package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Route asks etit-proxy for the road distance between two points.
//
// It exists to stop this codebase talking to OSRM directly. FalconGo used to
// build its own http://localhost:5000/route/v1/driving URL in three places,
// none of which applied the `exclude` for time-gated truck roads that
// etit-proxy applies when routing a tanker. Same OSRM instance, different
// parameters, different answers — so a fee mapping's stored distance and the
// trip audit's expected distance disagreed for the same pair of points, and
// nothing said why.
//
// Routing through the proxy means the exclude class, the retry-without-exclude
// fallback and the base URL have exactly one definition. Adding a second
// caller here would recreate the drift this removes.
//
// `restricted` says whether the time-gated roads must be avoided. True is the
// conservative reading and the default for reference distances: a route that
// avoids restricted roads is legal at any hour, one that uses them is not.
func Route(fromLat, fromLng, toLat, toLng float64, restricted bool) (distanceKm, durationMin float64, err error) {
	km, min, _, err := RouteWithGeometry(fromLat, fromLng, toLat, toLng, restricted)
	return km, min, err
}

// RouteWithGeometry is Route plus the polyline, for callers that draw the road
// rather than only measuring it.
func RouteWithGeometry(fromLat, fromLng, toLat, toLng float64, restricted bool) (distanceKm, durationMin float64, geometry string, err error) {
	adminURL := os.Getenv("ETIT_PROXY_ADMIN_URL")
	adminToken := os.Getenv("ETIT_PROXY_ADMIN_TOKEN")
	if adminURL == "" || adminToken == "" {
		return 0, 0, "", fmt.Errorf("routing unavailable: ETIT_PROXY_ADMIN_URL/TOKEN not configured")
	}

	payload, err := json.Marshal(map[string]any{
		"from_lat":   fromLat,
		"from_lng":   fromLng,
		"to_lat":     toLat,
		"to_lng":     toLng,
		"restricted": restricted,
	})
	if err != nil {
		return 0, 0, "", fmt.Errorf("routing: marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, adminURL+"/admin/routing/route", bytes.NewReader(payload))
	if err != nil {
		return 0, 0, "", fmt.Errorf("routing: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", adminToken)

	// Longer than the audit hook's 3s: this one is synchronous and a caller is
	// waiting on the number, where the hook is fire-and-forget.
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("routing: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, 0, "", fmt.Errorf("routing: proxy returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var out struct {
		DistanceKm  float64 `json:"distanceKm"`
		DurationMin float64 `json:"durationMin"`
		Geometry    string  `json:"geometry"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0, "", fmt.Errorf("routing: decode: %w", err)
	}
	if out.DistanceKm <= 0 {
		return 0, 0, "", fmt.Errorf("routing: proxy returned no usable route")
	}
	return out.DistanceKm, out.DurationMin, out.Geometry, nil
}
