package Controllers

import (
	"Falcon/Models"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type OSRMEnrichmentHandler struct {
	DB *gorm.DB
}

func NewOSRMEnrichmentHandler(db *gorm.DB) *OSRMEnrichmentHandler {
	return &OSRMEnrichmentHandler{DB: db}
}

type osrmResponse struct {
	Code   string      `json:"code"`
	Routes []osrmRoute `json:"routes"`
}

type osrmRoute struct {
	Distance float64 `json:"distance"`
	Duration float64 `json:"duration"`
}

func fetchOSRMRoute(originLat, originLng, destLat, destLng float64) (distanceKm, durationMin float64, err error) {
	url := fmt.Sprintf(
		"http://localhost:5000/route/v1/driving/%.6f,%.6f;%.6f,%.6f?overview=false",
		originLng, originLat, destLng, destLat,
	)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("OSRM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("OSRM returned status %d", resp.StatusCode)
	}

	var result osrmResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0, fmt.Errorf("OSRM decode error: %w", err)
	}
	if result.Code != "Ok" || len(result.Routes) == 0 {
		return 0, 0, fmt.Errorf("no OSRM route found")
	}

	return result.Routes[0].Distance / 1000, result.Routes[0].Duration / 60, nil
}

// PUT /fee-mappings/:id/location
// Body: { "lat": 30.0444, "lng": 31.2357 }
func (h *OSRMEnrichmentHandler) UpdateDropOffLocation(c *fiber.Ctx) error {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	var body struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if body.Lat == 0 && body.Lng == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "lat and lng are required"})
	}

	var mapping Models.FeeMapping
	if err := h.DB.First(&mapping, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "fee mapping not found"})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var terminal Models.Terminal
	if err := h.DB.Where("name = ?", mapping.Terminal).First(&terminal).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error":    "terminal not found in DB",
				"terminal": mapping.Terminal,
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if terminal.Latitude == 0 && terminal.Longitude == 0 {
		return c.Status(http.StatusUnprocessableEntity).JSON(fiber.Map{
			"error":    "terminal has no coordinates set",
			"terminal": terminal.Name,
		})
	}

	osrmDist, osrmDuration, err := fetchOSRMRoute(terminal.Latitude, terminal.Longitude, body.Lat, body.Lng)
	if err != nil {
		return c.Status(http.StatusBadGateway).JSON(fiber.Map{
			"error":   "OSRM route calculation failed",
			"details": err.Error(),
		})
	}

	mapping.Latitude = body.Lat
	mapping.Longitude = body.Lng
	mapping.OSRMDistance = osrmDist
	mapping.OSRMDuration = osrmDuration

	if err := h.DB.Save(&mapping).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// The canonical pin lives in drop_off_points; a pin set here applies to
	// every mapping sharing this drop-off name. Legacy per-mapping columns
	// are kept in sync during the transition.
	if err := syncDropOffPin(h.DB, mapping.DropOffPoint, body.Lat, body.Lng, "manual"); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message": "Drop-off location and OSRM data updated",
		"data": fiber.Map{
			"id":                 mapping.ID,
			"company":            mapping.Company,
			"terminal":           mapping.Terminal,
			"drop_off_point":     mapping.DropOffPoint,
			"lat":                mapping.Latitude,
			"lng":                mapping.Longitude,
			"manual_distance_km": mapping.Distance,
			"osrm_distance_km":   osrmDist,
			"osrm_duration_min":  osrmDuration,
			"discrepancy_km":     mapping.Distance - osrmDist,
		},
	})
}

// POST /fee-mappings/enrich-osrm
// Refreshes OSRM data for all mappings that have lat/lng set.
func (h *OSRMEnrichmentHandler) BulkEnrichOSRM(c *fiber.Ctx) error {
	var mappings []Models.FeeMapping
	if err := h.DB.Where("latitude != 0 AND longitude != 0").Find(&mappings).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if len(mappings) == 0 {
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"message": "No mappings with coordinates found",
		})
	}

	terminalCache := map[string]*Models.Terminal{}
	loadTerminal := func(name string) (*Models.Terminal, error) {
		if t, ok := terminalCache[name]; ok {
			return t, nil
		}
		var t Models.Terminal
		if err := h.DB.Where("name = ?", name).First(&t).Error; err != nil {
			return nil, err
		}
		terminalCache[name] = &t
		return &t, nil
	}

	type result struct {
		ID           uint    `json:"id"`
		DropOffPoint string  `json:"drop_off_point"`
		OSRMDistance float64 `json:"osrm_distance_km,omitempty"`
		OSRMDuration float64 `json:"osrm_duration_min,omitempty"`
		Discrepancy  float64 `json:"discrepancy_km,omitempty"`
		Error        string  `json:"error,omitempty"`
	}

	results := make([]result, 0, len(mappings))

	for i := range mappings {
		m := &mappings[i]
		r := result{ID: m.ID, DropOffPoint: m.DropOffPoint}

		terminal, err := loadTerminal(m.Terminal)
		if err != nil {
			r.Error = fmt.Sprintf("terminal '%s' not found: %v", m.Terminal, err)
			results = append(results, r)
			continue
		}
		if terminal.Latitude == 0 && terminal.Longitude == 0 {
			r.Error = fmt.Sprintf("terminal '%s' has no coordinates", m.Terminal)
			results = append(results, r)
			continue
		}

		osrmDist, osrmDuration, err := fetchOSRMRoute(terminal.Latitude, terminal.Longitude, m.Latitude, m.Longitude)
		if err != nil {
			r.Error = fmt.Sprintf("OSRM error: %v", err)
			results = append(results, r)
			continue
		}

		m.OSRMDistance = osrmDist
		m.OSRMDuration = osrmDuration
		if err := h.DB.Save(m).Error; err != nil {
			r.Error = fmt.Sprintf("DB save error: %v", err)
			results = append(results, r)
			continue
		}

		r.OSRMDistance = osrmDist
		r.OSRMDuration = osrmDuration
		r.Discrepancy = m.Distance - osrmDist
		results = append(results, r)
	}

	succeeded := 0
	for _, r := range results {
		if r.Error == "" {
			succeeded++
		}
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"message":   "Bulk OSRM enrichment complete",
		"processed": len(mappings),
		"succeeded": succeeded,
		"failed":    len(mappings) - succeeded,
		"results":   results,
	})
}

func parseUintParam(c *fiber.Ctx, param string) (uint64, error) {
	var v uint64
	_, err := fmt.Sscanf(c.Params(param), "%d", &v)
	return v, err
}
