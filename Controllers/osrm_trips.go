package Controllers

import (
	"Falcon/Models"
	"Falcon/services"
	"fmt"
	"net/http"

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

// fetchOSRMRoute asks etit-proxy, which owns the routing rules.
//
// This used to build its own http://localhost:5000/route/v1/driving URL. That
// call never applied the `exclude` for time-gated truck roads that etit-proxy
// applies, so a fee mapping's stored distance and the trip audit's expected
// distance disagreed for the same two points — the audit flagged trips against
// a number produced by different rules.
//
// restricted=true: a reference distance should describe a route the tanker may
// drive at any hour. See services.Route.
func fetchOSRMRoute(originLat, originLng, destLat, destLng float64) (distanceKm, durationMin float64, err error) {
	return services.Route(originLat, originLng, destLat, destLng, true)
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

	// The canonical pin lives in drop_off_points, and a pin set here applies to
	// every mapping sharing this drop-off name. The assignments above are
	// in-memory only now — Latitude/Longitude are gorm:"-" — so this write is
	// the one that persists.
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
	// Every mapping whose drop-off has a canonical pin — NOT the lat/lng cached
	// on fee_mappings. That cache is written once at create time and goes stale
	// the moment someone corrects a pin in the locations screen, so enriching
	// from it routed to yesterday's coordinates.
	if err := h.DB.
		Joins("JOIN drop_off_points ON drop_off_points.name = fee_mappings.drop_off_point").
		Where("drop_off_points.deleted_at IS NULL").
		Where("drop_off_points.latitude IS NOT NULL AND drop_off_points.longitude IS NOT NULL").
		Find(&mappings).Error; err != nil {
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

	dropOffCache := map[string]*Models.DropOffPoint{}
	loadDropOff := func(name string) (*Models.DropOffPoint, error) {
		if p, ok := dropOffCache[name]; ok {
			return p, nil
		}
		var p Models.DropOffPoint
		if err := h.DB.Where("name = ?", name).First(&p).Error; err != nil {
			return nil, err
		}
		if p.Latitude == nil || p.Longitude == nil {
			return nil, fmt.Errorf("unpinned")
		}
		dropOffCache[name] = &p
		return &p, nil
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

		// Drop-off coordinates from the canonical row, for the same reason the
		// query above selects on it.
		point, err := loadDropOff(m.DropOffPoint)
		if err != nil {
			r.Error = fmt.Sprintf("drop-off '%s' has no pin: %v", m.DropOffPoint, err)
			results = append(results, r)
			continue
		}

		osrmDist, osrmDuration, err := fetchOSRMRoute(
			terminal.Latitude, terminal.Longitude, *point.Latitude, *point.Longitude)
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
