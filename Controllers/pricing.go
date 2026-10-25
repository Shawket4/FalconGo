package Controllers

import (
	"Falcon/Models"
	"Falcon/pricing"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// The stored-pricing API. Reads are permission 3 (revenue is financial data);
// every write — a new rule version, deleting a future one, a re-price — is
// permission 4. Routes: FiberConfig/Routes.go, under /api/pricing.

// pricingCompany is one company's versions in GET /api/pricing/rules.
type pricingCompany struct {
	Company string `json:"company"`
	// The version in force today (Cairo), or 0 when every version is future.
	CurrentRuleID uint                 `json:"current_rule_id"`
	Rules         []Models.PricingRule `json:"rules"`
}

// GetPricingRules is GET /api/pricing/rules: every version with its bands,
// grouped by company, newest first.
func GetPricingRules(c *fiber.Ctx) error {
	rules, err := pricing.ListRules(Models.DB)
	if err != nil {
		log.Printf("pricing: list rules: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not load pricing rules"})
	}
	today := time.Now().In(pricing.Cairo).Format("2006-01-02")
	out := []pricingCompany{}
	for _, r := range rules {
		if len(out) == 0 || out[len(out)-1].Company != r.Company {
			out = append(out, pricingCompany{Company: r.Company, Rules: []Models.PricingRule{}})
		}
		g := &out[len(out)-1]
		g.Rules = append(g.Rules, r)
		// Newest first, so the first version not in the future is current.
		if g.CurrentRuleID == 0 && r.EffectiveFrom <= today {
			g.CurrentRuleID = r.ID
		}
	}
	return c.JSON(fiber.Map{"companies": out})
}

// CreatePricingRule is POST /api/pricing/rules: a new effective-dated version
// for a company. Versions are append-only. A version dated in the past does
// not move trips already priced — the response says how many it would, and
// POST /api/pricing/reprice moves them.
func CreatePricingRule(c *fiber.Ctx) error {
	var in pricing.RuleInput
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	var id uint
	var stale int64
	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		id, stale, err = pricing.CreateRule(tx, in, RequestUser(c).Id)
		return err
	})
	var verr *pricing.ValidationError
	switch {
	case errors.As(err, &verr):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": verr.Msg})
	case errors.Is(err, pricing.ErrDuplicateVersion):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case err != nil:
		log.Printf("pricing: create rule: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not create the pricing rule"})
	}
	rules, err := pricing.ListRules(Models.DB)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not load pricing rules"})
	}
	var created Models.PricingRule
	for _, r := range rules {
		if r.ID == id {
			created = r
		}
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"rule": created,
		// Live trips on/after effective_from priced under another version.
		// Non-zero means a re-price over that range is needed to apply it.
		"unrepriced_trips": stale,
	})
}

// DeletePricingRule is DELETE /api/pricing/rules/:id — only for a version
// that has not taken effect yet and has priced nothing.
func DeletePricingRule(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}
	err = Models.DB.Transaction(func(tx *gorm.DB) error {
		return pricing.DeleteFutureRule(tx, uint(id), time.Now())
	})
	switch {
	case errors.Is(err, pricing.ErrRuleNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, pricing.ErrRuleNotFuture), errors.Is(err, pricing.ErrRuleInUse):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case err != nil:
		log.Printf("pricing: delete rule %d: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not delete the pricing rule"})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// truckMonthCompanyTotal is one company's line under the rows.
type truckMonthCompanyTotal struct {
	Company   string  `json:"company"`
	Cars      int     `json:"cars"`
	CarDays   int     `json:"car_days"`
	Rental    float64 `json:"rental"`
	RentalVAT float64 `json:"rental_vat"`
	Frozen    bool    `json:"frozen"`
}

// GetTruckMonths is GET /api/pricing/truck-months?month=YYYY-MM[&company=].
func GetTruckMonths(c *fiber.Ctx) error {
	month := strings.TrimSpace(c.Query("month"))
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "month must be YYYY-MM"})
	}
	company := strings.TrimSpace(c.Query("company"))
	rows, err := pricing.ListTruckMonths(Models.DB, start.Format("2006-01-02"), company)
	if err != nil {
		log.Printf("pricing: truck months %s: %v", month, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not load truck-months"})
	}
	totals := []truckMonthCompanyTotal{}
	seenCar := map[string]bool{}
	for _, r := range rows {
		if len(totals) == 0 || totals[len(totals)-1].Company != r.Company {
			totals = append(totals, truckMonthCompanyTotal{Company: r.Company})
		}
		t := &totals[len(totals)-1]
		if key := r.Company + "\x00" + r.CarNoPlate; !seenCar[key] {
			seenCar[key] = true
			t.Cars++
			t.CarDays += r.MonthDaysWorked
		}
		t.Rental += r.Rental
		t.RentalVAT += r.RentalVAT
		t.Frozen = t.Frozen || r.FrozenAt != nil
	}
	return c.JSON(fiber.Map{"month": month, "rows": rows, "totals": totals})
}

// repriceInput is the body of POST /api/pricing/reprice.
type repriceInput struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Company       string `json:"company"`
	IncludeFrozen bool   `json:"include_frozen"`
}

// RepricePricing is POST /api/pricing/reprice: re-price trips dated in
// [from, to] under the rules effective on their dates, and recompute their
// truck-months. One transaction: it applies wholly or not at all.
func RepricePricing(c *fiber.Ctx) error {
	var in repriceInput
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	from, errFrom := time.Parse("2006-01-02", in.From)
	to, errTo := time.Parse("2006-01-02", in.To)
	if errFrom != nil || errTo != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "from and to must be YYYY-MM-DD"})
	}
	if to.Before(from) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "to is before from"})
	}
	var res pricing.RepriceResult
	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		res, err = pricing.Reprice(tx, in.From, in.To, strings.TrimSpace(in.Company), in.IncludeFrozen)
		return err
	})
	var ferr *pricing.FrozenError
	if errors.As(err, &ferr) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error":         "the range includes frozen months; resend with include_frozen: true to re-open and re-freeze them",
			"frozen_months": ferr.Months,
		})
	}
	if err != nil {
		log.Printf("pricing: reprice %s..%s %q: %v", in.From, in.To, in.Company, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not re-price"})
	}
	log.Printf("💰 reprice %s..%s %q by user %d: %d trips (%d changed, %d unpriced), %d months; total %.2f -> %.2f",
		in.From, in.To, in.Company, RequestUser(c).Id, res.TripsRepriced, res.TripsChanged,
		res.PricingErrors, len(res.MonthsRecomputed), res.Before.Total, res.After.Total)
	return c.JSON(res)
}
