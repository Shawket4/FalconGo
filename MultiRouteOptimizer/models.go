package MultiRouteOptimizer

type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// RouteRequest is the structure of the incoming request
type RouteRequest struct {
	Start        Point   `json:"start"`
	End          Point   `json:"end"`
	Waypoints    []Point `json:"waypoints"`
	Optimization string  `json:"optimization,omitempty"` // "time" or "distance", defaults to "distance"
	TravelMode   string  `json:"travelMode,omitempty"`   // For Google Maps URL generation
	Algorithm    string  `json:"algorithm,omitempty"`    // "nearest", "2opt", "simulated", "genetic"
}

// RouteResponse is the structure of the API response
type RouteResponse struct {
	OptimalRoute      []Point `json:"optimalRoute"`
	TotalDistance     float64 `json:"totalDistance"`
	EstimatedDuration float64 `json:"estimatedDuration"` // In seconds, based on average speed
	GoogleMapsURL     string  `json:"googleMapsUrl"`
	Algorithm         string  `json:"algorithm"`
}

// Earth radius in kilometers
const earthRadius = 6371.0

// Average speeds in km/h for different travel modes
var AverageSpeeds = map[string]float64{
	"driving":   60.0,
	"walking":   5.0,
	"bicycling": 15.0,
	"transit":   30.0,
}
