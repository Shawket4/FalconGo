package Controllers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"Falcon/Models"
	"Falcon/services"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Receipt piles — its own module, not part of the Watanya fee report.
//
// The fee report answers "what do we bill Watanya for this range". This
// answers "how do I file the paper that came back". Same company, same date
// range, entirely different document: one is a billing statement with terminals
// and fee bands, the other is a filing plan with letters and tick boxes. They
// are kept apart so neither has to carry the other's columns, and so this one
// can be exported from its own screen rather than from the report screen.
//
// Watanya-only, deliberately. Other companies' receipts do not come back as a
// heap to be filed by Arabic first letter, and the letter rules in
// services.LetterOf are Arabic-specific.

const pilesCompany = "Watanya"

// The level at which the money on this screen becomes visible. The filing plan
// itself -- which boxes, which letters, which names, where the paper is -- is
// open to anyone who can see trips at all, because it is warehouse work rather
// than accounting. Matches the threshold GetTripStatistics already uses.
const pilesFinancialPermission = 3

// hasFinancialAccess reports whether this caller may see fees and costs.
func hasFinancialAccess(c *fiber.Ctx) bool {
	user, ok := c.Locals("user").(*Models.User)
	return ok && user.Permission >= pilesFinancialPermission
}

// receiptStatusByTrip maps each trip id to where its paper currently is.
//
// "Latest" is by received_at and skips soft-deleted steps, matching the rule
// the trips list uses: a deleted step is a correction, and letting it count as
// the latest resurrects the very state someone deleted -- a deleted Garage
// step after a live Office step made an office-received trip read as in-garage.
func (h *ReceiptPilesHandler) receiptStatusByTrip(tripIDs []uint) (map[uint]string, error) {
	out := make(map[uint]string, len(tripIDs))
	if len(tripIDs) == 0 {
		return out, nil
	}

	var steps []Models.ReceiptStep
	if err := h.DB.
		Select("trip_id, location, received_at").
		Where("trip_id IN ? AND deleted_at IS NULL", tripIDs).
		Order("trip_id, received_at").
		Find(&steps).Error; err != nil {
		return nil, err
	}

	// Ordered by received_at ascending, so the last write per trip wins.
	for _, step := range steps {
		switch step.Location {
		case "Office":
			out[step.TripID] = services.ReceiptAtOffice
		case "Garage":
			out[step.TripID] = services.ReceiptAtGarage
		default:
			out[step.TripID] = services.ReceiptNotFiled
		}
	}
	return out, nil
}

type ReceiptPilesHandler struct {
	DB *gorm.DB
}

func NewReceiptPilesHandler(db *gorm.DB) *ReceiptPilesHandler {
	return &ReceiptPilesHandler{DB: db}
}

// pileRequest is shared by the JSON and export endpoints so the two can never
// disagree about what a given range and mode mean.
type pileRequest struct {
	StartDate string
	EndDate   string
	Mode      string
	PileCount int
}

// parsePileRequest reads the query string. Both endpoints are GETs — the plan
// is a pure function of the range, so it is cacheable and shareable as a URL.
func parsePileRequest(c *fiber.Ctx) (pileRequest, error) {
	req := pileRequest{
		StartDate: strings.TrimSpace(c.Query("start_date")),
		EndDate:   strings.TrimSpace(c.Query("end_date")),
		Mode:      strings.TrimSpace(c.Query("mode")),
	}
	if req.StartDate == "" || req.EndDate == "" {
		return req, fmt.Errorf("start_date and end_date are required")
	}
	for _, d := range []string{req.StartDate, req.EndDate} {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return req, fmt.Errorf("dates must be YYYY-MM-DD, got %q", d)
		}
	}
	if req.StartDate > req.EndDate {
		return req, fmt.Errorf("start_date is after end_date")
	}

	switch req.Mode {
	case "", services.ModeBalanced:
		req.Mode = services.ModeBalanced
	case services.ModeLetter:
	default:
		return req, fmt.Errorf("mode must be %q or %q", services.ModeBalanced, services.ModeLetter)
	}

	// piles is advisory: absent or zero means "derive it from this range", which
	// is what keeps the split balanced as the range grows.
	if raw := strings.TrimSpace(c.Query("piles")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > services.MaxPiles {
			return req, fmt.Errorf("piles must be between 1 and %d", services.MaxPiles)
		}
		req.PileCount = n
	}
	return req, nil
}

// plan runs the query and the grouping. Shared by both endpoints.
func (h *ReceiptPilesHandler) plan(req pileRequest) (services.PilePlan, error) {
	var trips []Models.TripStruct
	// Only the columns the plan reads. The fee report needs capacity and
	// mappings; a filing plan needs the name on the sheet and nothing else.
	err := h.DB.
		Select("receipt_no, drop_off_point, terminal, date, car_no_plate, driver_name").
		Where("company = ? AND date BETWEEN ? AND ?", pilesCompany, req.StartDate, req.EndDate).
		Find(&trips).Error
	if err != nil {
		return services.PilePlan{}, err
	}

	plan := services.BuildPilePlan(trips, req.Mode, req.PileCount)
	plan.StartDate = req.StartDate
	plan.EndDate = req.EndDate
	return plan, nil
}

// GetDropOffDetail serves one drop-off's receipts, split per terminal, in the
// exact order the Watanya fee report prints them.
//
// A separate call rather than part of the plan: the plan already carries every
// receipt in the range as a flat list per drop-off, and inlining the priced,
// per-terminal breakdown for all ~100 drop-offs would multiply that payload for
// a view of one. The drawer asks when it opens.
func (h *ReceiptPilesHandler) GetDropOffDetail(c *fiber.Ctx) error {
	req, err := parsePileRequest(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	name := strings.TrimSpace(c.Query("drop_off_point"))
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "drop_off_point is required",
		})
	}

	var trips []Models.TripStruct
	err = h.DB.
		Select("id, receipt_no, drop_off_point, terminal, date, car_no_plate, driver_name, tank_capacity, mileage").
		Where("company = ? AND drop_off_point = ? AND date BETWEEN ? AND ?",
			pilesCompany, name, req.StartDate, req.EndDate).
		Find(&trips).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error: " + err.Error(),
		})
	}

	// The fee mappings for this drop-off only. The report indexes every
	// Watanya mapping; one drop-off needs a handful.
	var mappings []Models.FeeMapping
	if err := h.DB.Where("company = ? AND drop_off_point = ?", pilesCompany, name).
		Find(&mappings).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve fee mappings: " + err.Error(),
		})
	}
	feeByKey := make(map[string]services.FeeInfo, len(mappings))
	for _, m := range mappings {
		feeByKey[m.Terminal+"|"+m.DropOffPoint] = services.FeeInfo{
			Distance:  m.Distance,
			FeeIndex:  int(m.Fee),
			ActualFee: services.WatanyaBandRates[int(m.Fee)],
		}
	}

	tripIDs := make([]uint, 0, len(trips))
	for _, t := range trips {
		tripIDs = append(tripIDs, t.ID)
	}
	statusByTrip, err := h.receiptStatusByTrip(tripIDs)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve receipt steps: " + err.Error(),
		})
	}

	detail := services.BuildDropOffDetail(
		name, trips, feeByKey, statusByTrip, hasFinancialAccess(c),
	)
	detail.StartDate = req.StartDate
	detail.EndDate = req.EndDate
	return c.JSON(detail)
}

// GetReceiptPiles serves the plan as JSON, for the dashboard module.
func (h *ReceiptPilesHandler) GetReceiptPiles(c *fiber.Ctx) error {
	req, err := parsePileRequest(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	plan, err := h.plan(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error: " + err.Error(),
		})
	}
	return c.JSON(plan)
}

// ExportReceiptPiles serves the same plan as a workbook.
//
// The bytes go straight out rather than through a temp file: nothing else
// reads the file, and a file would need cleaning up on every error path.
func (h *ReceiptPilesHandler) ExportReceiptPiles(c *fiber.Ctx) error {
	req, err := parsePileRequest(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	plan, err := h.plan(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error: " + err.Error(),
		})
	}

	blob, err := services.RenderPilePlanExcel(plan)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to build workbook: " + err.Error(),
		})
	}

	filename := fmt.Sprintf("receipt_piles_%s_%s.xlsx",
		strings.ReplaceAll(req.StartDate, "-", ""),
		strings.ReplaceAll(req.EndDate, "-", ""))
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	return c.Send(blob)
}
