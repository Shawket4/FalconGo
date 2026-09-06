package Controllers

import (
	"Falcon/Models"
	"Falcon/services"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// LocationsHandler serves the canonical-locations system: terminals with
// radius overrides, per-company allowlists and receipt serial patterns
// (see terminals.go), and drop_off_points as the single source of truth for
// drop-off pins (set once, referenced everywhere by name).
type LocationsHandler struct {
	DB *gorm.DB
}

func NewLocationsHandler(db *gorm.DB) *LocationsHandler {
	return &LocationsHandler{DB: db}
}

// ---------------------------------------------------------------------------
// Terminals
// ---------------------------------------------------------------------------

func (h *LocationsHandler) GetTerminals(c *fiber.Ctx) error {
	var terminals []Models.Terminal
	if err := h.DB.Order("name").Find(&terminals).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": terminals})
}

func (h *LocationsHandler) UpdateTerminal(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	var terminal Models.Terminal
	if err := h.DB.First(&terminal, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "terminal not found"})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var body struct {
		Address   *string  `json:"address"`
		Latitude  *float64 `json:"lat"`
		Longitude *float64 `json:"long"`
		RadiusM   *float64 `json:"radius_m"`
		// Distinguishes "radius_m: null" (clear override) from field absent.
		ClearRadius bool `json:"clear_radius"`
		// "manual" (default) or "gps_suggested" when accepting an inbox suggestion.
		PinSource string `json:"pin_source"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	updates := map[string]interface{}{}
	if body.Address != nil {
		updates["address"] = *body.Address
	}
	if body.Latitude != nil && body.Longitude != nil {
		updates["latitude"] = *body.Latitude
		updates["longitude"] = *body.Longitude
		source := body.PinSource
		if source != "gps_suggested" {
			source = "manual"
		}
		updates["pin_source"] = source
	}
	if body.RadiusM != nil {
		updates["radius_m"] = *body.RadiusM
	} else if body.ClearRadius {
		updates["radius_m"] = nil
	}
	if len(updates) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "nothing to update"})
	}

	if err := h.DB.Model(&terminal).Updates(updates).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// A moved pin or resized fence changes what the trip auditor sees —
	// re-audit the recent trips departing this terminal.
	if (body.Latitude != nil && body.Longitude != nil) || body.RadiusM != nil || body.ClearRadius {
		services.NotifyLocationRescan(h.DB, "terminal", terminal.ID, "")
	}
	return c.JSON(fiber.Map{"message": "terminal updated", "data": terminal})
}

// ---------------------------------------------------------------------------
// Drop-off points
// ---------------------------------------------------------------------------

// GetDropOffPoints lists drop-off points with server-side pagination.
// Query params:
//   - q:        substring filter on name
//   - missing:  "true" returns only points without a pin
//   - page:     1-based page number (default 1)
//   - per_page: page size (default 25, max 200)
//
// Drop-off points are never created here: their existence flows from fee
// mappings (see Controllers/fee.go); this page only manages geography.
func (h *LocationsHandler) GetDropOffPoints(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(c.Query("per_page", "25"))
	if perPage < 1 {
		perPage = 25
	}
	if perPage > 200 {
		perPage = 200
	}

	filtered := func() *gorm.DB {
		query := h.DB.Model(&Models.DropOffPoint{})
		if q := strings.TrimSpace(c.Query("q")); q != "" {
			query = query.Where("name ILIKE ?", "%"+q+"%")
		}
		if c.Query("missing") == "true" {
			query = query.Where("latitude IS NULL")
		}
		return query
	}

	var total int64
	if err := filtered().Count(&total).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var points []Models.DropOffPoint
	if err := filtered().Order("name").
		Limit(perPage).Offset((page - 1) * perPage).
		Find(&points).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"items":    points,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

func (h *LocationsHandler) UpdateDropOffPoint(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	var point Models.DropOffPoint
	if err := h.DB.First(&point, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "drop-off point not found"})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var body struct {
		Latitude    *float64 `json:"lat"`
		Longitude   *float64 `json:"long"`
		RadiusM     *float64 `json:"radius_m"`
		ClearRadius bool     `json:"clear_radius"`
		// "manual" (default) or "gps_suggested" when accepting an inbox suggestion.
		PinSource string `json:"pin_source"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	updates := map[string]interface{}{}
	if body.Latitude != nil && body.Longitude != nil {
		updates["latitude"] = *body.Latitude
		updates["longitude"] = *body.Longitude
		source := body.PinSource
		if source != "gps_suggested" {
			source = "manual"
		}
		updates["pin_source"] = source
	}
	if body.RadiusM != nil {
		updates["radius_m"] = *body.RadiusM
	} else if body.ClearRadius {
		updates["radius_m"] = nil
	}
	if len(updates) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "nothing to update"})
	}

	if err := h.DB.Model(&point).Updates(updates).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// No pin is mirrored onto fee_mappings: its latitude/longitude columns were
	// dropped once drop_off_points became the canonical home for a pin, and the
	// mapping's coordinates are hydrated from here at serve time
	// (Models.HydrateFeeMappingCoords). Writing them back turned every save on
	// this endpoint into a 500 — after the pin had already been committed.

	// A moved pin or resized fence changes what the trip auditor sees —
	// re-audit the recent trips delivering to this point.
	if (body.Latitude != nil && body.Longitude != nil) || body.RadiusM != nil || body.ClearRadius {
		services.NotifyLocationRescan(h.DB, "dropoff", 0, point.Name)
	}
	return c.JSON(fiber.Map{"message": "drop-off point updated", "data": point})
}

// DeleteDropOffPoint removes a drop-off point, but only when no live fee
// mapping still references its name — fee mappings own drop-off existence, so
// deleting a referenced point would just resurrect it (unpinned) on the next
// mapping write.
func (h *LocationsHandler) DeleteDropOffPoint(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	var point Models.DropOffPoint
	if err := h.DB.First(&point, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "drop-off point not found"})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var references int64
	if err := h.DB.Model(&Models.FeeMapping{}).
		Where("drop_off_point = ?", point.Name).
		Count(&references).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if references > 0 {
		return c.Status(http.StatusConflict).JSON(fiber.Map{
			"error":      "drop-off point is still referenced by fee mappings; delete those mappings first",
			"references": references,
		})
	}

	res := h.DB.Delete(&Models.DropOffPoint{}, id)
	if res.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "drop-off point not found"})
	}
	return c.JSON(fiber.Map{"message": "drop-off point deleted"})
}

// syncDropOffPin upserts the canonical pin for a drop-off name. Nothing is
// mirrored onto fee_mappings: that table's latitude/longitude columns were
// dropped when drop_off_points became the canonical home for a pin, and its
// coordinates are hydrated from here at serve time
// (Models.HydrateFeeMappingCoords).
func syncDropOffPin(db *gorm.DB, name string, lat, lng float64, source string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return db.Exec(`
        INSERT INTO drop_off_points (created_at, updated_at, name, latitude, longitude, pin_source)
        VALUES (NOW(), NOW(), ?, ?, ?, ?)
        ON CONFLICT (name) DO UPDATE
           SET latitude = EXCLUDED.latitude,
               longitude = EXCLUDED.longitude,
               pin_source = EXCLUDED.pin_source,
               updated_at = NOW(),
               deleted_at = NULL
    `, name, lat, lng, source).Error
}

// ensureDropOffPointExists guarantees a drop_off_points row for a drop-off
// name referenced by a fee mapping (fee mappings own drop-off existence). A
// new name gets an unpinned row (so it shows up in the needs-attention inbox),
// a soft-deleted row is restored with its old pin intact, and a live row is
// left untouched.
func ensureDropOffPointExists(db *gorm.DB, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	db.Exec(`
        INSERT INTO drop_off_points (created_at, updated_at, name, pin_source)
        VALUES (NOW(), NOW(), ?, '')
        ON CONFLICT (name) DO UPDATE
           SET deleted_at = NULL,
               updated_at = NOW()
         WHERE drop_off_points.deleted_at IS NOT NULL
    `, name)
}

// ---------------------------------------------------------------------------
// Needs-attention inbox (DB-derived part)
// ---------------------------------------------------------------------------

// GetLocationsInbox returns the work the dashboard inbox can derive from the
// database alone: drop-off points without a pin. Terminals are picked by id
// now (see terminals.go), so there is no "unknown terminal names" section any
// more. GPS pin suggestions are served separately by the etit-proxy
// calibration API and merged client-side.
// The unpinned list is capped; total_unpinned carries the real count.
func (h *LocationsHandler) GetLocationsInbox(c *fiber.Ctx) error {
	const inboxCap = 50

	var totalUnpinned int64
	if err := h.DB.Model(&Models.DropOffPoint{}).
		Where("latitude IS NULL").
		Count(&totalUnpinned).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var unpinned []Models.DropOffPoint
	if err := h.DB.Where("latitude IS NULL").Order("name").
		Limit(inboxCap).Find(&unpinned).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"unpinned_dropoffs": unpinned,
		"total_unpinned":    totalUnpinned,
	})
}
