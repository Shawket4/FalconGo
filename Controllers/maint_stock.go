package Controllers

import (
	"Falcon/Models"
	"encoding/json"
	"log"
	"math"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// The stock routes maint-rust (the garage app's backend) calls with the
// user's relayed token. maint-rust keeps a local ledger and mirrors every
// movement here, debits and credits alike, then refreshes its caches from
// the on-hand these routes compute. Both sides are idempotent on ref_id.

// maintStockInput is the body of POST /api/maint-stock/debit — maint-rust's
// stock_movements row as to_jsonb renders it, trimmed to these fields.
type maintStockInput struct {
	Kind        string   `json:"kind"`
	Brand       *string  `json:"brand"`
	Model       *string  `json:"model"`
	Size        *string  `json:"size"`
	OilType     *string  `json:"oil_type"`
	Qty         *int     `json:"qty"`
	Liters      *float64 `json:"liters"`
	RefID       string   `json:"ref_id"`
	WorkOrderID *string  `json:"work_order_id"`
}

// validateMaintStock turns the body into a movement, or says why it is not one.
func validateMaintStock(in maintStockInput) (Models.MaintStockMovement, string) {
	ref, err := uuid.Parse(strings.TrimSpace(in.RefID))
	if err != nil {
		return Models.MaintStockMovement{}, "ref_id must be a uuid"
	}
	m := Models.MaintStockMovement{
		RefID:   ref.String(),
		Kind:    in.Kind,
		Brand:   in.Brand,
		Model:   in.Model,
		Size:    in.Size,
		OilType: in.OilType,
		Qty:     in.Qty,
		Liters:  in.Liters,
	}
	switch in.Kind {
	case Models.MaintStockTireDebit, Models.MaintStockTireCredit:
		if in.Qty == nil || *in.Qty <= 0 {
			return m, in.Kind + " needs qty > 0"
		}
	case Models.MaintStockOilDebit, Models.MaintStockOilCredit:
		if in.Liters == nil || math.IsNaN(*in.Liters) || math.IsInf(*in.Liters, 0) || *in.Liters <= 0 {
			return m, in.Kind + " needs liters > 0"
		}
	default:
		return m, "kind must be one of tire_debit, oil_debit, tire_credit, oil_credit"
	}
	if in.WorkOrderID != nil && strings.TrimSpace(*in.WorkOrderID) != "" {
		wo, err := uuid.Parse(strings.TrimSpace(*in.WorkOrderID))
		if err != nil {
			return m, "work_order_id must be a uuid"
		}
		s := wo.String()
		m.WorkOrderID = &s
	}
	return m, ""
}

// RecordMaintStockMovement is POST /api/maint-stock/debit. Despite the path it
// takes all four kinds: maint-rust's backlog sweep mirrors its credits here
// too, and refusing one would park that row at the head of its 100-oldest
// backlog forever. A replayed ref_id is a 200 that changes nothing.
func RecordMaintStockMovement(c *fiber.Ctx) error {
	var in maintStockInput
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	m, problem := validateMaintStock(in)
	if problem != "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": problem})
	}
	m.RecordedByUserID = RequestUser(c).Id

	if _, err := Models.RecordMaintStockMovement(Models.DB, m); err != nil {
		log.Printf("maint stock: recording %s %s: %v", m.Kind, m.RefID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not record the stock movement"})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// GetMaintTireStock is GET /api/maint-stock/tires.
func GetMaintTireStock(c *fiber.Ctx) error {
	rows, err := Models.MaintTireStock(Models.DB)
	if err != nil {
		log.Printf("maint stock: tire on-hand: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not compute tire stock"})
	}
	return c.JSON(rows)
}

// GetMaintOilStock is GET /api/maint-stock/oil.
func GetMaintOilStock(c *fiber.Ctx) error {
	rows, err := Models.MaintOilStock(Models.DB)
	if err != nil {
		log.Printf("maint stock: oil on-hand: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not compute oil stock"})
	}
	return c.JSON(rows)
}
