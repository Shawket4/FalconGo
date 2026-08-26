package Controllers

import (
	"Falcon/Models"
	"Falcon/services"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Terminals as first-class, picked-by-id entities.
//
// A trip's terminal used to be a free-text string matched against
// terminal_aliases; that is gone. Terminals now carry a per-company allowlist
// (terminal_companies) and per-(terminal, company) receipt-number serial
// patterns (terminal_receipt_patterns). The legacy `terminal` text columns on
// trips/parent_trips/fee_mappings stay populated with the canonical name
// during the transition so nothing downstream breaks.

// ---------------------------------------------------------------------------
// Catalog
// ---------------------------------------------------------------------------

// GetTerminalsCatalog serves GET /api/terminals.
//
// With ?company=X: the terminals allowed for that company (JOIN
// terminal_companies), each with the receipt pattern that applies to that
// company (a company-specific row wins over the NULL-company row).
//
// Without a company: every non-deleted terminal with its allowed_companies
// array, for the admin/curation view.
func (h *LocationsHandler) GetTerminalsCatalog(c *fiber.Ctx) error {
	company := strings.TrimSpace(c.Query("company"))

	if company == "" {
		var terminals []Models.Terminal
		if err := h.DB.Order("name").Find(&terminals).Error; err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		var allowlist []Models.TerminalCompany
		if err := h.DB.Find(&allowlist).Error; err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		companiesByTerminal := make(map[uint][]string)
		for _, a := range allowlist {
			companiesByTerminal[a.TerminalID] = append(companiesByTerminal[a.TerminalID], a.Company)
		}

		type terminalWithCompanies struct {
			Models.Terminal
			AllowedCompanies []string `json:"allowed_companies"`
		}
		out := make([]terminalWithCompanies, 0, len(terminals))
		for _, t := range terminals {
			companies := companiesByTerminal[t.ID]
			if companies == nil {
				companies = []string{}
			}
			out = append(out, terminalWithCompanies{Terminal: t, AllowedCompanies: companies})
		}
		return c.JSON(fiber.Map{"data": out})
	}

	var terminals []Models.Terminal
	if err := h.DB.
		Joins("JOIN terminal_companies tc ON tc.terminal_id = terminals.id").
		Where("tc.company = ?", company).
		Order("terminals.name").
		Find(&terminals).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	ids := make([]uint, 0, len(terminals))
	for _, t := range terminals {
		ids = append(ids, t.ID)
	}

	// Company-specific pattern wins over the NULL-company (all companies) one.
	patternByTerminal := make(map[uint]*Models.TerminalReceiptPattern)
	if len(ids) > 0 {
		var patterns []Models.TerminalReceiptPattern
		if err := h.DB.
			Where("terminal_id IN ? AND active = ? AND (company = ? OR company IS NULL)", ids, true, company).
			Find(&patterns).Error; err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		for i := range patterns {
			p := &patterns[i]
			existing := patternByTerminal[p.TerminalID]
			if existing == nil || (existing.Company == nil && p.Company != nil) {
				patternByTerminal[p.TerminalID] = p
			}
		}
	}

	type terminalForCompany struct {
		Models.Terminal
		ReceiptPattern *Models.TerminalReceiptPattern `json:"receipt_pattern"`
	}
	out := make([]terminalForCompany, 0, len(terminals))
	for _, t := range terminals {
		out = append(out, terminalForCompany{Terminal: t, ReceiptPattern: patternByTerminal[t.ID]})
	}
	return c.JSON(fiber.Map{"data": out})
}

// ResolveOrCreateTerminal serves POST /api/terminals {name, company}.
//
// Resolve-or-create-or-extend: an existing non-deleted terminal with that
// exact name is reused (its allowlist extended to the company when needed); a
// soft-deleted one is restored; otherwise a new unpinned terminal is created.
// The unique name constraint guarantees no duplicates either way.
func (h *LocationsHandler) ResolveOrCreateTerminal(c *fiber.Ctx) error {
	var body struct {
		Name    string `json:"name"`
		Company string `json:"company"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Company = strings.TrimSpace(body.Company)
	if body.Name == "" || body.Company == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "name and company are required"})
	}

	var terminal Models.Terminal
	err := h.DB.Where("name = ?", body.Name).First(&terminal).Error
	switch {
	case err == nil:
		// Exists: make sure the company is allowlisted.
	case err == gorm.ErrRecordNotFound:
		// A soft-deleted terminal still owns the unique name: restore it
		// rather than failing the insert.
		if h.DB.Unscoped().Where("name = ?", body.Name).First(&terminal).Error == nil {
			if err := h.DB.Unscoped().Model(&terminal).
				Update("deleted_at", nil).Error; err != nil {
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
			break
		}
		terminal = Models.Terminal{Name: body.Name}
		if err := h.DB.Create(&terminal).Error; err != nil {
			// Lost a race on the unique name -- the row exists now, use it.
			if h.DB.Where("name = ?", body.Name).First(&terminal).Error != nil {
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
		} else {
			if _, err := ensureTerminalCompany(h.DB, terminal.ID, body.Company); err != nil {
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
			return c.Status(http.StatusCreated).JSON(fiber.Map{
				"message":  "terminal created",
				"data":     terminal,
				"created":  true,
				"extended": false,
			})
		}
	default:
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	extended, err := ensureTerminalCompany(h.DB, terminal.ID, body.Company)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"message":  "terminal resolved",
		"data":     terminal,
		"created":  false,
		"extended": extended,
	})
}

// ensureTerminalCompany allowlists a company for a terminal. Returns whether a
// new allowlist row was actually inserted.
func ensureTerminalCompany(db *gorm.DB, terminalID uint, company string) (bool, error) {
	res := db.Exec(`
        INSERT INTO terminal_companies (terminal_id, company, created_at)
        VALUES (?, ?, NOW())
        ON CONFLICT (terminal_id, company) DO NOTHING
    `, terminalID, company)
	return res.RowsAffected > 0, res.Error
}

// ---------------------------------------------------------------------------
// Receipt patterns CRUD
// ---------------------------------------------------------------------------

func (h *LocationsHandler) loadTerminal(c *fiber.Ctx) (*Models.Terminal, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return nil, c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}
	var terminal Models.Terminal
	if err := h.DB.First(&terminal, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "terminal not found"})
		}
		return nil, c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return &terminal, nil
}

// GetTerminalReceiptPatterns serves GET /api/terminals/:id/receipt-patterns.
func (h *LocationsHandler) GetTerminalReceiptPatterns(c *fiber.Ctx) error {
	terminal, errResp := h.loadTerminal(c)
	if terminal == nil {
		return errResp
	}

	var patterns []Models.TerminalReceiptPattern
	if err := h.DB.Where("terminal_id = ?", terminal.ID).
		Order("company NULLS FIRST").
		Find(&patterns).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": patterns})
}

// UpsertTerminalReceiptPattern serves PUT /api/terminals/:id/receipt-patterns.
//
// Upserts on (terminal_id, company); company empty/null targets the
// all-companies row. The pattern must compile as a Go regex -- it is what
// receipt_valid is computed with.
func (h *LocationsHandler) UpsertTerminalReceiptPattern(c *fiber.Ctx) error {
	terminal, errResp := h.loadTerminal(c)
	if terminal == nil {
		return errResp
	}

	var body struct {
		Company     *string `json:"company"`
		Pattern     string  `json:"pattern"`
		Description string  `json:"description"`
		Active      *bool   `json:"active"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	body.Pattern = strings.TrimSpace(body.Pattern)
	if body.Pattern == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "pattern is required"})
	}
	if _, err := regexp.Compile(body.Pattern); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid regex: " + err.Error()})
	}

	// "" and null both mean the all-companies row (stored as NULL).
	if body.Company != nil && strings.TrimSpace(*body.Company) == "" {
		body.Company = nil
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}

	if err := h.DB.Exec(`
        INSERT INTO terminal_receipt_patterns
            (terminal_id, company, pattern, description, active, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, NOW(), NOW())
        ON CONFLICT (terminal_id, COALESCE(company, '')) DO UPDATE
           SET pattern     = EXCLUDED.pattern,
               description = EXCLUDED.description,
               active      = EXCLUDED.active,
               updated_at  = NOW()
    `, terminal.ID, body.Company, body.Pattern, body.Description, active).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	services.InvalidateReceiptPatternCache()

	var pattern Models.TerminalReceiptPattern
	if err := h.DB.Where("terminal_id = ? AND company IS NOT DISTINCT FROM ?", terminal.ID, body.Company).
		First(&pattern).Error; err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"message": "receipt pattern saved", "data": pattern})
}

// DeleteTerminalReceiptPattern serves
// DELETE /api/terminals/:id/receipt-patterns?company=X.
// An absent/empty company deletes the all-companies (NULL) row.
func (h *LocationsHandler) DeleteTerminalReceiptPattern(c *fiber.Ctx) error {
	terminal, errResp := h.loadTerminal(c)
	if terminal == nil {
		return errResp
	}

	var company *string
	if v := strings.TrimSpace(c.Query("company")); v != "" {
		company = &v
	}

	res := h.DB.Exec(`
        DELETE FROM terminal_receipt_patterns
         WHERE terminal_id = ? AND company IS NOT DISTINCT FROM ?
    `, terminal.ID, company)
	if res.Error != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "receipt pattern not found"})
	}
	services.InvalidateReceiptPatternCache()
	return c.JSON(fiber.Map{"message": "receipt pattern deleted"})
}

// ---------------------------------------------------------------------------
// Helpers used by the trip handlers
// ---------------------------------------------------------------------------

// resolveTerminalByName returns the non-deleted terminal with that exact
// (trimmed) name, or nil when there is none. Used to keep terminal_id
// populated for legacy string-only payloads.
func resolveTerminalByName(db *gorm.DB, name string) *Models.Terminal {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var terminal Models.Terminal
	if err := db.Where("name = ?", name).First(&terminal).Error; err != nil {
		return nil
	}
	return &terminal
}

// terminalAllowedForCompany reports whether (terminal, company) is in the
// terminal_companies allowlist.
func terminalAllowedForCompany(db *gorm.DB, terminalID uint, company string) (bool, error) {
	var count int64
	err := db.Model(&Models.TerminalCompany{}).
		Where("terminal_id = ? AND company = ?", terminalID, company).
		Count(&count).Error
	return count > 0, err
}
