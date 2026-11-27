package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A stand-in etit-proxy that records what it was asked.
func fakeProxy(t *testing.T, handler func(path string, body map[string]any) (int, any)) *[]map[string]any {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["_path"] = r.URL.Path
		seen = append(seen, body)
		status, out := handler(r.URL.Path, body)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ETIT_PROXY_ADMIN_URL", srv.URL)
	t.Setenv("ETIT_PROXY_ADMIN_TOKEN", "tok")
	return &seen
}

func TestRouteAtSendsTheDepartureTime(t *testing.T) {
	seen := fakeProxy(t, func(string, map[string]any) (int, any) {
		return 200, map[string]any{"distanceKm": 87.9, "durationMin": 74.2, "geometry": "abc"}
	})
	cairo := time.FixedZone("EET", 2*3600)
	at := time.Date(2026, 11, 28, 13, 0, 0, 0, cairo)

	km, min, geom, err := RouteAt(context.Background(), GeoPoint{30.146, 31.286}, GeoPoint{30.7865, 31.0004}, at)
	if err != nil {
		t.Fatal(err)
	}
	if km != 87.9 || min != 74.2 || geom != "abc" {
		t.Fatalf("got %v %v %q", km, min, geom)
	}
	got := (*seen)[0]
	if got["_path"] != "/admin/routing/route" || got["depart_at"] != "2026-11-28T13:00:00+02:00" {
		t.Fatalf("request: %v", got)
	}
	if _, ok := got["restricted"]; ok {
		t.Fatalf("a timed route must not also ask for any-hour: %v", got)
	}
}

func TestReferenceRoutesStayLegalAtAnyHour(t *testing.T) {
	seen := fakeProxy(t, func(string, map[string]any) (int, any) {
		return 200, map[string]any{"distanceKm": 10, "durationMin": 12, "geometry": ""}
	})
	if _, _, err := Route(30, 31, 30.1, 31.1, true); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]; got["restricted"] != true || got["depart_at"] != nil {
		t.Fatalf("request: %v", got)
	}
}

func TestOptimizeReadsOrderLegsAndGeometry(t *testing.T) {
	seen := fakeProxy(t, func(string, map[string]any) (int, any) {
		return 200, map[string]any{
			"order": []int{1, 0},
			"legs": []map[string]any{
				{"distanceKm": 43.1, "durationMin": 32, "geometry": "a"},
				{"distanceKm": 69.3, "durationMin": 56, "geometry": "b"},
				{"distanceKm": 68.6, "durationMin": 61, "geometry": "c"},
			},
			"distanceKm": 181.0, "durationMin": 149, "geometry": "whole",
		}
	})
	start := GeoPoint{30.146, 31.286}
	trip, err := Optimize(context.Background(), start, []GeoPoint{{30.7865, 31.0004}, {30.466, 31.1858}}, start, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(trip.Order) != 2 || trip.Order[0] != 1 || trip.Geometry != "whole" || trip.Legs[2].DistanceKm != 68.6 {
		t.Fatalf("trip: %+v", trip)
	}
	got := (*seen)[0]
	if got["_path"] != "/admin/routing/optimize" || got["restricted"] != true || got["depart_at"] != nil {
		t.Fatalf("request: %v", got)
	}
	if stops := got["stops"].([]any); len(stops) != 2 || stops[0].(map[string]any)["lng"] != 31.0004 {
		t.Fatalf("stops: %v", got["stops"])
	}
}

func TestOptimizeRejectsAnAnswerThatLostAStop(t *testing.T) {
	fakeProxy(t, func(string, map[string]any) (int, any) {
		return 200, map[string]any{"order": []int{0}, "legs": []map[string]any{{"distanceKm": 1}, {"distanceKm": 1}}}
	})
	_, err := Optimize(context.Background(), GeoPoint{}, []GeoPoint{{1, 1}, {2, 2}}, GeoPoint{}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "for 2 stops") {
		t.Fatalf("want a stop-count error, got %v", err)
	}
}

func TestRoutingSurfacesProxyErrorsAndMissingConfig(t *testing.T) {
	fakeProxy(t, func(string, map[string]any) (int, any) {
		return 500, map[string]any{"error": "internal server error"}
	})
	if _, _, err := Route(30, 31, 30.1, 31.1, true); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want the proxy's 500, got %v", err)
	}
	t.Setenv("ETIT_PROXY_ADMIN_URL", "")
	if _, err := Optimize(context.Background(), GeoPoint{}, []GeoPoint{{1, 1}}, GeoPoint{}, time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("want not-configured, got %v", err)
	}
}

func TestOptimalLegsFollowTheOptimizedOrder(t *testing.T) {
	terminal := GeoPoint{30.146, 31.286}
	deliveries := []GeoPoint{{30.7865, 31.0004}, {30.466, 31.1858}}
	visits := []DeliveryVisit{
		{ContainerTripIDs: []uint{11}, Location: deliveries[0], LocationName: "Tanta"},
		{ContainerTripIDs: []uint{12}, Location: deliveries[1], LocationName: "Banha"},
	}
	optimal := &OptimizedTrip{
		Order: []int{1, 0},
		Legs: []OptimizedLeg{
			{DistanceKm: 48.2, DurationMin: 41.5, Geometry: "t-b"},
			{DistanceKm: 43.1, DurationMin: 32, Geometry: "b-t"},
			{DistanceKm: 92.0, DurationMin: 75, Geometry: "t-home"},
		},
	}
	legs := (&TripSummaryService{}).buildOptimalLegs(terminal, deliveries, optimal, visits)
	if len(legs) != 3 {
		t.Fatalf("want 2 deliveries + return, got %d", len(legs))
	}
	if legs[0].ToName != "Banha" || legs[0].FromName != "Terminal" || legs[0].OSRMDistance != 48.2 ||
		legs[0].TravelDuration != 2490 || legs[0].OSRMGeometry != "t-b" || *legs[0].TripStructID != 12 {
		t.Fatalf("first leg: %+v", legs[0])
	}
	if legs[1].FromName != "Banha" || legs[1].ToName != "Tanta" || legs[1].SequenceNumber != 2 {
		t.Fatalf("second leg: %+v", legs[1])
	}
	ret := legs[2]
	if ret.ToName != "Terminal" || ret.FromName != "Tanta" || ret.OSRMDistance != 92.0 || ret.SequenceNumber != 3 {
		t.Fatalf("return leg: %+v", ret)
	}
}
