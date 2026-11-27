package services

import (
	"Falcon/Observability"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Road routing for FalconGo: every call goes to etit-proxy, which routes with
// Valhalla for the fleet's tanker (truck size, weight and hazmat, plus the
// time-of-day bans built into its map).
//
// Nothing here talks to a routing engine directly. FalconGo used to build its
// own OSRM URLs: first for fee mappings, which skipped the truck rules
// etit-proxy applied, and then for trip summaries, which used Madar's car
// OSRM. The same pair of points got different distances depending on who
// asked. One service owning "how far is A from B" is what stops that drift;
// adding a second caller here would bring it back.

// GeoPoint is a coordinate pair.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Route asks etit-proxy for the truck road distance between two points.
//
// `restricted` says whether the route must be legal at any hour. True is the
// conservative reading and the default for reference distances: it avoids
// every time-banned road. False means leaving now.
func Route(fromLat, fromLng, toLat, toLng float64, restricted bool) (distanceKm, durationMin float64, err error) {
	km, min, _, err := RouteWithGeometry(fromLat, fromLng, toLat, toLng, restricted)
	return km, min, err
}

// RouteCtx is Route, carrying the caller's trace to etit-proxy.
//
// Pass a request's context and the hop shows up on the same Sentry trace as
// the request that caused it; without it etit-proxy opens a trace of its own
// and the two halves of one operation are unconnected.
func RouteCtx(ctx context.Context, fromLat, fromLng, toLat, toLng float64, restricted bool) (distanceKm, durationMin float64, err error) {
	km, min, _, err := RouteWithGeometryCtx(ctx, fromLat, fromLng, toLat, toLng, restricted)
	return km, min, err
}

// RouteWithGeometry is Route plus the polyline, for callers that draw the road
// rather than only measuring it.
func RouteWithGeometry(fromLat, fromLng, toLat, toLng float64, restricted bool) (distanceKm, durationMin float64, geometry string, err error) {
	return RouteWithGeometryCtx(context.Background(), fromLat, fromLng, toLat, toLng, restricted)
}

// RouteWithGeometryCtx is RouteWithGeometry, carrying the caller's trace.
func RouteWithGeometryCtx(ctx context.Context, fromLat, fromLng, toLat, toLng float64, restricted bool) (distanceKm, durationMin float64, geometry string, err error) {
	return routeCall(ctx, map[string]any{
		"from_lat":   fromLat,
		"from_lng":   fromLng,
		"to_lat":     toLat,
		"to_lng":     toLng,
		"restricted": restricted,
	})
}

// RouteAt is the truck route for a truck leaving at departAt: time-of-day
// bans apply as they would at that moment. Use it to compare a leg that was
// actually driven against what was legal when it was driven.
func RouteAt(ctx context.Context, from, to GeoPoint, departAt time.Time) (distanceKm, durationMin float64, geometry string, err error) {
	return routeCall(ctx, map[string]any{
		"from_lat":  from.Lat,
		"from_lng":  from.Lng,
		"to_lat":    to.Lat,
		"to_lng":    to.Lng,
		"depart_at": departAt.Format(time.RFC3339),
	})
}

func routeCall(ctx context.Context, payload map[string]any) (distanceKm, durationMin float64, geometry string, err error) {
	var out struct {
		DistanceKm  float64 `json:"distanceKm"`
		DurationMin float64 `json:"durationMin"`
		Geometry    string  `json:"geometry"`
	}
	if err := callAdmin(ctx, "/admin/routing/route", payload, &out); err != nil {
		return 0, 0, "", err
	}
	if out.DistanceKm <= 0 {
		return 0, 0, "", fmt.Errorf("routing: proxy returned no usable route")
	}
	return out.DistanceKm, out.DurationMin, out.Geometry, nil
}

// OptimizedLeg is one hop of an optimized trip.
type OptimizedLeg struct {
	DistanceKm  float64 `json:"distanceKm"`
	DurationMin float64 `json:"durationMin"`
	Geometry    string  `json:"geometry"`
}

// OptimizedTrip is a trip with its stops in the best visiting order.
type OptimizedTrip struct {
	// Order lists indices into the stops passed in, in visiting order.
	Order []int `json:"order"`
	// Legs: start → first stop, …, last stop → end (len(Order)+1 hops).
	Legs        []OptimizedLeg `json:"legs"`
	DistanceKm  float64        `json:"distanceKm"`
	DurationMin float64        `json:"durationMin"`
	// Geometry is the whole trip as one polyline5.
	Geometry string `json:"geometry"`
}

// Optimize asks etit-proxy for the best order to visit stops, starting at
// start and ending at end, for a truck leaving at departAt (zero time: legal
// at any hour).
func Optimize(ctx context.Context, start GeoPoint, stops []GeoPoint, end GeoPoint, departAt time.Time) (*OptimizedTrip, error) {
	if len(stops) == 0 {
		return nil, fmt.Errorf("routing: optimize needs at least one stop")
	}
	payload := map[string]any{"start": start, "stops": stops, "end": end, "restricted": true}
	if !departAt.IsZero() {
		payload["depart_at"] = departAt.Format(time.RFC3339)
	}
	var out OptimizedTrip
	if err := callAdmin(ctx, "/admin/routing/optimize", payload, &out); err != nil {
		return nil, err
	}
	if len(out.Order) != len(stops) || len(out.Legs) != len(stops)+1 {
		return nil, fmt.Errorf("routing: optimize answered %d stops and %d legs for %d stops", len(out.Order), len(out.Legs), len(stops))
	}
	return &out, nil
}

// Router is the routing the trip summary needs; ProxyRouter is the real one.
type Router interface {
	Optimize(ctx context.Context, start GeoPoint, stops []GeoPoint, end GeoPoint, departAt time.Time) (*OptimizedTrip, error)
	RouteAt(ctx context.Context, from, to GeoPoint, departAt time.Time) (distanceKm, durationMin float64, geometry string, err error)
}

// ProxyRouter routes through etit-proxy.
type ProxyRouter struct{}

func (ProxyRouter) Optimize(ctx context.Context, start GeoPoint, stops []GeoPoint, end GeoPoint, departAt time.Time) (*OptimizedTrip, error) {
	return Optimize(ctx, start, stops, end, departAt)
}

func (ProxyRouter) RouteAt(ctx context.Context, from, to GeoPoint, departAt time.Time) (float64, float64, string, error) {
	return RouteAt(ctx, from, to, departAt)
}

// callAdmin POSTs payload to etit-proxy's token-gated admin surface.
func callAdmin(ctx context.Context, path string, payload any, out any) error {
	adminURL := os.Getenv("ETIT_PROXY_ADMIN_URL")
	adminToken := os.Getenv("ETIT_PROXY_ADMIN_TOKEN")
	if adminURL == "" || adminToken == "" {
		return fmt.Errorf("routing unavailable: ETIT_PROXY_ADMIN_URL/TOKEN not configured")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("routing: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adminURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("routing: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", adminToken)
	Observability.PropagateTrace(ctx, req)

	// Longer than the audit hook's 3s: this one is synchronous and a caller is
	// waiting on the number, where the hook is fire-and-forget.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("routing: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("routing: proxy returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("routing: decode: %w", err)
	}
	return nil
}
