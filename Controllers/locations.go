package Controllers

import (
	"Falcon/Models"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// LocationsHandler serves the canonical-locations system: terminals with
// aliases and radius overrides, and drop_off_points as the single source of
// truth for drop-off pins (set once, referenced everywhere by name).
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

	var aliases []Models.TerminalAlias
	if err := h.DB.Find(&aliases).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	aliasesByTerminal := make(map[uint][]Models.TerminalAlias)
	for _, a := range aliases {
		a.Terminal = Models.Terminal{}
		aliasesByTerminal[a.TerminalID] = append(aliasesByTerminal[a.TerminalID], a)
	}

	type terminalWithAliases struct {
		Models.Terminal
		Aliases []Models.TerminalAlias `json:"aliases"`
	}
	out := make([]terminalWithAliases, 0, len(terminals))
	for _, t := range terminals {
		out = append(out, terminalWithAliases{Terminal: t, Aliases: aliasesByTerminal[t.ID]})
	}
	return c.JSON(fiber.Map{"data": out})
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
	return c.JSON(fiber.Map{"message": "terminal updated", "data": terminal})
}

func (h *LocationsHandler) CreateTerminalAlias(c *fiber.Ctx) error {
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
		Alias string `json:"alias"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	body.Alias = strings.TrimSpace(body.Alias)
	if body.Alias == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "alias is required"})
	}

	alias := Models.TerminalAlias{Alias: body.Alias, TerminalID: terminal.ID}
	if err := h.DB.Create(&alias).Error; err != nil {
		return c.Status(http.StatusConflict).JSON(fiber.Map{"error": "alias already exists or could not be created"})
	}
	return c.Status(http.StatusCreated).JSON(fiber.Map{"message": "alias created", "data": alias})
}

func (h *LocationsHandler) DeleteTerminalAlias(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}
	res := h.DB.Unscoped().Delete(&Models.TerminalAlias{}, id)
	if res.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "alias not found"})
	}
	return c.JSON(fiber.Map{"message": "alias deleted"})
}

// ---------------------------------------------------------------------------
// Drop-off points
// ---------------------------------------------------------------------------

// GetDropOffPoints lists drop-off points. Query params:
//   - q:       substring filter on name
//   - missing: "true" returns only points without a pin
func (h *LocationsHandler) GetDropOffPoints(c *fiber.Ctx) error {
	query := h.DB.Model(&Models.DropOffPoint{}).Order("name")
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	if c.Query("missing") == "true" {
		query = query.Where("latitude IS NULL")
	}

	var points []Models.DropOffPoint
	if err := query.Find(&points).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": points})
}

func (h *LocationsHandler) CreateDropOffPoint(c *fiber.Ctx) error {
	var body struct {
		Name      string   `json:"name"`
		Latitude  *float64 `json:"lat"`
		Longitude *float64 `json:"long"`
		RadiusM   *float64 `json:"radius_m"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "name is required"})
	}

	point := Models.DropOffPoint{
		Name:      body.Name,
		Latitude:  body.Latitude,
		Longitude: body.Longitude,
		RadiusM:   body.RadiusM,
	}
	if body.Latitude != nil {
		point.PinSource = "manual"
	}
	if err := h.DB.Create(&point).Error; err != nil {
		return c.Status(http.StatusConflict).JSON(fiber.Map{"error": "drop-off point already exists or could not be created"})
	}
	if point.Latitude != nil && point.Longitude != nil {
		if err := syncDropOffPin(h.DB, point.Name, *point.Latitude, *point.Longitude, point.PinSource); err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	}
	return c.Status(http.StatusCreated).JSON(fiber.Map{"message": "drop-off point created", "data": point})
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
		Latitude  *float64 `json:"lat"`
		Longitude *float64 `json:"long"`
		RadiusM   *float64 `json:"radius_m"`
		ClearRadius bool   `json:"clear_radius"`
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
	if body.Latitude != nil && body.Longitude != nil {
		// Mirror the new pin onto legacy fee_mappings columns.
		if err := h.DB.Exec(`
            UPDATE fee_mappings
               SET latitude = ?, longitude = ?, updated_at = NOW()
             WHERE deleted_at IS NULL AND drop_off_point = ?
        `, *body.Latitude, *body.Longitude, point.Name).Error; err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	}
	return c.JSON(fiber.Map{"message": "drop-off point updated", "data": point})
}

func (h *LocationsHandler) DeleteDropOffPoint(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
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

// syncDropOffPin upserts the canonical pin for a drop-off name and mirrors it
// onto all fee_mappings rows sharing that name, so legacy readers of the
// per-mapping columns stay correct during the transition.
func syncDropOffPin(db *gorm.DB, name string, lat, lng float64, source string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	err := db.Exec(`
        INSERT INTO drop_off_points (created_at, updated_at, name, latitude, longitude, pin_source)
        VALUES (NOW(), NOW(), ?, ?, ?, ?)
        ON CONFLICT (name) DO UPDATE
           SET latitude = EXCLUDED.latitude,
               longitude = EXCLUDED.longitude,
               pin_source = EXCLUDED.pin_source,
               updated_at = NOW(),
               deleted_at = NULL
    `, name, lat, lng, source).Error
	if err != nil {
		return err
	}
	return db.Exec(`
        UPDATE fee_mappings
           SET latitude = ?, longitude = ?, updated_at = NOW()
         WHERE deleted_at IS NULL AND drop_off_point = ?
    `, lat, lng, name).Error
}

// ensureDropOffPointExists creates an unpinned drop_off_points row for a new
// drop-off name so it shows up in the needs-attention inbox. No-op when the
// row already exists.
func ensureDropOffPointExists(db *gorm.DB, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	db.Exec(`
        INSERT INTO drop_off_points (created_at, updated_at, name, pin_source)
        VALUES (NOW(), NOW(), ?, '')
        ON CONFLICT (name) DO NOTHING
    `, name)
}

// ---------------------------------------------------------------------------
// Needs-attention inbox (DB-derived part)
// ---------------------------------------------------------------------------

// GetLocationsInbox returns the work the dashboard inbox can derive from the
// database alone: drop-off points without a pin, and terminal name strings
// (from trips and fee_mappings) that resolve to neither a terminal nor an
// alias. GPS pin suggestions are served separately by the etit-proxy
// calibration API and merged client-side.
func (h *LocationsHandler) GetLocationsInbox(c *fiber.Ctx) error {
	var unpinned []Models.DropOffPoint
	if err := h.DB.Where("latitude IS NULL").Order("name").Find(&unpinned).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	type unknownTerminal struct {
		Name     string `json:"name"`
		TripRows int64  `json:"trip_rows"`
	}
	var unknown []unknownTerminal
	err := h.DB.Raw(`
        SELECT src.name, SUM(src.trip_rows) AS trip_rows
          FROM (
            SELECT terminal AS name, COUNT(*) AS trip_rows
              FROM trips
             WHERE deleted_at IS NULL AND terminal <> ''
             GROUP BY terminal
            UNION ALL
            SELECT terminal AS name, 0 AS trip_rows
              FROM fee_mappings
             WHERE deleted_at IS NULL AND terminal <> ''
             GROUP BY terminal
          ) src
         WHERE NOT EXISTS (SELECT 1 FROM terminals t
                            WHERE t.deleted_at IS NULL AND t.name = src.name)
           AND NOT EXISTS (SELECT 1 FROM terminal_aliases a
                            WHERE a.deleted_at IS NULL AND a.alias = src.name)
         GROUP BY src.name
         ORDER BY trip_rows DESC
    `).Scan(&unknown).Error
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"unpinned_dropoffs": unpinned,
		"unknown_terminals": unknown,
	})
}
