package pricing

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"Falcon/Models"

	"gorm.io/gorm"
)

// RuleInput is the body of a new rule version.
type RuleInput struct {
	Company        string   `json:"company"`
	EffectiveFrom  string   `json:"effective_from"`
	Method         string   `json:"method"`
	PerKmRate      *float64 `json:"per_km_rate"`
	VatRate        *float64 `json:"vat_rate"`
	RentalMode     string   `json:"rental_mode"`
	RentalAmount   *float64 `json:"rental_amount"`
	RentalFullDays *int     `json:"rental_full_days"`
	Notes          string   `json:"notes"`
	Bands          []struct {
		Band         int     `json:"band"`
		RatePer1000L float64 `json:"rate_per_1000l"`
	} `json:"bands"`
}

// ValidationError is a refused rule; its message is safe to show.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ErrDuplicateVersion is a second version for the same company and date.
var ErrDuplicateVersion = errors.New("a version for this company and effective_from already exists")

// Validate checks a rule's method-specific fields and normalises it.
func (in *RuleInput) Validate() error {
	bad := func(f string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(f, a...)} }
	in.Company = strings.TrimSpace(in.Company)
	if in.Company == "" {
		return bad("company is required")
	}
	if _, err := time.Parse("2006-01-02", in.EffectiveFrom); err != nil {
		return bad("effective_from must be YYYY-MM-DD")
	}
	if in.VatRate == nil || *in.VatRate < 0 || *in.VatRate >= 1 {
		return bad("vat_rate is required, as a fraction in [0, 1) (0.14 for 14%%)")
	}
	switch in.Method {
	case Models.PricingPerKm:
		if in.PerKmRate == nil || *in.PerKmRate <= 0 {
			return bad("per_km needs a positive per_km_rate")
		}
		if len(in.Bands) > 0 {
			return bad("bands are only for band_per_1000l")
		}
	case Models.PricingPer1000LFee:
		if in.PerKmRate != nil {
			return bad("per_km_rate is only for per_km")
		}
		if len(in.Bands) > 0 {
			return bad("bands are only for band_per_1000l")
		}
	case Models.PricingBandPer1000L:
		if in.PerKmRate != nil {
			return bad("per_km_rate is only for per_km")
		}
		if len(in.Bands) == 0 {
			return bad("band_per_1000l needs at least one band")
		}
		seen := map[int]bool{}
		for _, b := range in.Bands {
			if b.Band < 1 {
				return bad("band numbers start at 1, got %d", b.Band)
			}
			if seen[b.Band] {
				return bad("band %d appears twice", b.Band)
			}
			if b.RatePer1000L <= 0 {
				return bad("band %d needs a positive rate_per_1000l", b.Band)
			}
			seen[b.Band] = true
		}
	default:
		return bad("method must be per_1000l_fee, band_per_1000l or per_km")
	}
	if in.RentalMode == "" {
		in.RentalMode = Models.RentalNone
	}
	switch in.RentalMode {
	case Models.RentalNone:
		if in.RentalAmount != nil || in.RentalFullDays != nil {
			return bad("rental_amount and rental_full_days need a rental_mode")
		}
	case Models.RentalMonthlyTaper:
		if in.RentalAmount == nil || *in.RentalAmount <= 0 {
			return bad("monthly_taper needs a positive rental_amount (per car-month)")
		}
		if in.RentalFullDays == nil || *in.RentalFullDays < 1 || *in.RentalFullDays > 31 {
			return bad("monthly_taper needs rental_full_days between 1 and 31")
		}
	case Models.RentalPerCarDay:
		if in.RentalAmount == nil || *in.RentalAmount <= 0 {
			return bad("per_car_day needs a positive rental_amount (per car-day)")
		}
		if in.RentalFullDays != nil {
			return bad("rental_full_days is only for monthly_taper")
		}
	default:
		return bad("rental_mode must be none, monthly_taper or per_car_day")
	}
	return nil
}

// ListRules returns every rule version with its bands, newest first within
// each company, companies alphabetically.
func ListRules(db *gorm.DB) ([]Models.PricingRule, error) {
	var rules []Models.PricingRule
	err := db.Raw(`
        SELECT id, company, to_char(effective_from, 'YYYY-MM-DD') AS effective_from,
               method, per_km_rate::float8 AS per_km_rate, vat_rate::float8 AS vat_rate,
               rental_mode, rental_amount::float8 AS rental_amount, rental_full_days,
               notes, created_by_user_id, created_at
          FROM pricing_rules
         ORDER BY company, effective_from DESC`).Scan(&rules).Error
	if err != nil || len(rules) == 0 {
		return rules, err
	}
	var bands []struct {
		RuleID       uint
		Band         int
		RatePer1000L float64 `gorm:"column:rate_per_1000l"`
	}
	if err := db.Raw(`SELECT rule_id, band, rate_per_1000l::float8 AS rate_per_1000l
                        FROM pricing_bands ORDER BY rule_id, band`).Scan(&bands).Error; err != nil {
		return rules, err
	}
	byRule := map[uint][]Models.PricingBand{}
	for _, b := range bands {
		byRule[b.RuleID] = append(byRule[b.RuleID], Models.PricingBand{RuleID: b.RuleID, Band: b.Band, RatePer1000L: b.RatePer1000L})
	}
	for i := range rules {
		rules[i].Bands = byRule[rules[i].ID]
		if rules[i].Bands == nil {
			rules[i].Bands = []Models.PricingBand{}
		}
	}
	return rules, nil
}

// CreateRule appends a new version. It never touches an existing one. It
// returns the new id and how many live trips dated on or after effective_from
// are priced under some other version — the trips a re-price would move.
func CreateRule(tx *gorm.DB, in RuleInput, userID uint) (uint, int64, error) {
	if err := in.Validate(); err != nil {
		return 0, 0, err
	}
	var exists int64
	if err := tx.Raw(`SELECT COUNT(*) FROM pricing_rules WHERE company = ? AND effective_from = ?::date`,
		in.Company, in.EffectiveFrom).Scan(&exists).Error; err != nil {
		return 0, 0, err
	}
	if exists > 0 {
		return 0, 0, ErrDuplicateVersion
	}
	var id uint
	if err := tx.Raw(`
        INSERT INTO pricing_rules
            (company, effective_from, method, per_km_rate, vat_rate, rental_mode,
             rental_amount, rental_full_days, notes, created_by_user_id)
        VALUES (?, ?::date, ?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING id`,
		in.Company, in.EffectiveFrom, in.Method, in.PerKmRate, *in.VatRate, in.RentalMode,
		in.RentalAmount, in.RentalFullDays, strings.TrimSpace(in.Notes), userID).Scan(&id).Error; err != nil {
		return 0, 0, err
	}
	for _, b := range in.Bands {
		if err := tx.Exec(`INSERT INTO pricing_bands (rule_id, band, rate_per_1000l) VALUES (?, ?, ?)`,
			id, b.Band, b.RatePer1000L).Error; err != nil {
			return 0, 0, err
		}
	}
	var stale int64
	err := tx.Raw(`SELECT COUNT(*) FROM trips
                    WHERE deleted_at IS NULL AND company = ? AND date >= ?
                      AND pricing_rule_id IS DISTINCT FROM ?`,
		in.Company, in.EffectiveFrom, id).Scan(&stale).Error
	return id, stale, err
}

// ErrRuleInUse and ErrRuleNotFuture refuse a version delete.
var (
	ErrRuleNotFound  = errors.New("pricing rule not found")
	ErrRuleNotFuture = errors.New("only a version whose effective_from is in the future can be deleted")
	ErrRuleInUse     = errors.New("this version has already priced trips or truck-months")
)

// DeleteFutureRule deletes a version that has not taken effect yet (its
// effective_from is after today, Cairo) and has priced nothing.
func DeleteFutureRule(tx *gorm.DB, id uint, now time.Time) error {
	var r struct{ EffectiveFrom string }
	res := tx.Raw(`SELECT to_char(effective_from, 'YYYY-MM-DD') AS effective_from
                     FROM pricing_rules WHERE id = ?`, id).Scan(&r)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	if r.EffectiveFrom <= now.In(Cairo).Format("2006-01-02") {
		return ErrRuleNotFuture
	}
	var used int64
	if err := tx.Raw(`SELECT (SELECT COUNT(*) FROM trips WHERE pricing_rule_id = ?)
                           + (SELECT COUNT(*) FROM truck_month_charges WHERE rule_id = ?)`,
		id, id).Scan(&used).Error; err != nil {
		return err
	}
	if used > 0 {
		return ErrRuleInUse
	}
	return tx.Exec(`DELETE FROM pricing_rules WHERE id = ?`, id).Error
}

// TruckMonth is one truck_month_charges row as the API returns it.
type TruckMonth struct {
	ID              uint       `json:"id"`
	CarNoPlate      string     `json:"car_no_plate"`
	CarID           *uint      `json:"car_id"`
	Company         string     `json:"company"`
	Terminal        string     `json:"terminal"`
	Month           string     `json:"month"`
	DaysWorked      int        `json:"days_worked"`
	MonthDaysWorked int        `json:"month_days_worked"`
	ChargeableDays  float64    `json:"chargeable_days"`
	Rental          float64    `json:"rental"`
	RentalVAT       float64    `json:"rental_vat"`
	RuleID          *uint      `json:"rule_id"`
	ComputedAt      time.Time  `json:"computed_at"`
	FrozenAt        *time.Time `json:"frozen_at"`
}

// ListTruckMonths returns month's (YYYY-MM-01) rows, optionally for one company.
func ListTruckMonths(db *gorm.DB, month, company string) ([]TruckMonth, error) {
	rows := []TruckMonth{}
	err := db.Raw(`
        SELECT id, car_no_plate, car_id, company, terminal,
               to_char(month, 'YYYY-MM-DD') AS month, days_worked, month_days_worked,
               chargeable_days::float8 AS chargeable_days, rental::float8 AS rental,
               rental_vat::float8 AS rental_vat, rule_id, computed_at, frozen_at
          FROM truck_month_charges
         WHERE month = ?::date AND (? = '' OR company = ?)
         ORDER BY company, car_no_plate, terminal`, month, company, company).Scan(&rows).Error
	return rows, err
}

// CompanyRule is the version a report describes a company by: how it is
// priced and, for per-km companies, at what rate.
type CompanyRule struct {
	Company   string
	RuleID    uint
	Method    string
	PerKmRate float64
}

// RulesAt returns each company's version in force on date (YYYY-MM-DD; ""
// means the newest), or, for a company whose first version starts later, that
// first version. Statistics group a company's trips by its method; the money
// itself always comes from what each trip stored.
func RulesAt(db *gorm.DB, date string) (map[string]CompanyRule, error) {
	if date == "" {
		date = "9999-12-31"
	}
	var rows []CompanyRule
	err := db.Raw(`
        SELECT DISTINCT ON (company) company, id AS rule_id, method,
               COALESCE(per_km_rate, 0)::float8 AS per_km_rate
          FROM pricing_rules
         ORDER BY company,
                  (to_char(effective_from, 'YYYY-MM-DD') <= ?) DESC,
                  CASE WHEN to_char(effective_from, 'YYYY-MM-DD') <= ? THEN effective_from END DESC NULLS LAST,
                  effective_from ASC`, date, date).Scan(&rows).Error
	out := make(map[string]CompanyRule, len(rows))
	for _, r := range rows {
		out[r.Company] = r
	}
	return out, err
}
