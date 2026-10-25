package middleware

import (
	"Falcon/Models"
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Money masking.
//
// Revenue is hidden below permission 3, for every company, on every surface.
// That is enforced here, once, rather than by each handler remembering to
// zero its own fields: MaskMoney sits on every route group that serves trips,
// fee mappings or statistics and scrubs the JSON response on its way out, so a
// field a handler adds tomorrow is covered the day it ships. Files (Excel,
// PDF) cannot be scrubbed after the fact; their handlers ask MoneyVisible and
// leave the money columns out.

// MoneyPermission is the level at which revenue, rates, rental and VAT become
// visible.
const MoneyPermission = 3

// moneyKeys are the JSON keys that carry money, normalised by moneyKey
// (lower case, underscores dropped), so `total_revenue`, `TotalRevenue` and
// `totalRevenue` are one entry. A key is money when its value is EGP or is the
// rate EGP is computed from (a fee mapping's fee: EGP per 1,000 L, or a
// Watanya band whose rate is public in the tariff).
var moneyKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range []string{
		// Stored per-trip pricing and the trip_revenue view.
		"revenue", "base_revenue", "revenue_vat",
		"allocated_rental", "allocated_vat", "allocated_total",
		// Fee mappings and the rates derived from them.
		"fee", "fees", "avg_fee", "fee_rate", "actual_fee", "rate_per_1000l", "per_km_rate",
		"expected_revenue", "expected_vat",
		// Statistics and analytics totals.
		"total_revenue", "total_fees", "avg_fees_per_day", "avg_revenue_per_day",
		"revenue_per_km", "car_rental", "rent", "rental", "rental_vat",
		"vat", "total_vat", "total_with_vat", "total_car_rent", "total_amount",
		// Receipt piles: per-receipt cost.
		"cost", "total_cost",
		// Watanya driver analytics: revenue per day against the fleet's.
		"efficiency",
	} {
		m[moneyKey(k)] = true
	}
	return m
}()

func moneyKey(k string) string {
	return strings.ToLower(strings.ReplaceAll(k, "_", ""))
}

// IsMoneyKey reports whether a JSON key is one MaskMoney scrubs.
func IsMoneyKey(k string) bool { return moneyKeys[moneyKey(k)] }

// MoneyVisible reports whether the caller may see money: an admin user at
// MoneyPermission or above. Drivers, anonymous callers and lower admins may
// not. Verify stores *Models.User, VerifyAny a Models.User; both are read.
func MoneyVisible(c *fiber.Ctx) bool {
	switch u := c.Locals("user").(type) {
	case *Models.User:
		return u != nil && u.Permission >= MoneyPermission
	case Models.User:
		return u.Permission >= MoneyPermission
	}
	return false
}

// MaskMoney scrubs money from JSON responses for callers below
// MoneyPermission. Mount it after the group's Verify, so the user is known.
func MaskMoney() fiber.Handler { return maskMoney(false) }

// StripMoney scrubs money from every JSON response, whoever the caller: the
// driver app shows no money to anyone.
func StripMoney() fiber.Handler { return maskMoney(true) }

func maskMoney(always bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := c.Next()
		if !always && MoneyVisible(c) {
			return err
		}
		resp := c.Response()
		if !bytes.HasPrefix(bytes.TrimSpace(resp.Header.ContentType()), []byte(fiber.MIMEApplicationJSON)) {
			return err
		}
		body := resp.Body()
		if len(bytes.TrimSpace(body)) == 0 {
			return err
		}
		out, scrubErr := ScrubMoneyJSON(body)
		if scrubErr != nil {
			// Fail closed: a body that cannot be read cannot be shown.
			resp.SetStatusCode(fiber.StatusInternalServerError)
			resp.SetBodyString(`{"message":"response withheld: it could not be checked for financial data"}`)
			return err
		}
		resp.SetBodyRaw(out)
		return err
	}
}

// ScrubMoneyJSON returns body with every money key's value zeroed: a number
// (or numeric string) becomes 0, an object or array under a money key has all
// of its numbers zeroed, and null stays null. Keys are kept, not dropped: the
// dashboard's schemas expect them, and 0 reads the same as the omitted field
// the statistics endpoints always sent below permission 3.
func ScrubMoneyJSON(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	v = scrub(v, false)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// scrub walks v; under a money key (zero) every number is zeroed.
func scrub(v any, zero bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = scrub(val, zero || IsMoneyKey(k))
		}
		return x
	case []any:
		for i := range x {
			x[i] = scrub(x[i], zero)
		}
		return x
	case json.Number:
		if zero {
			return json.Number("0")
		}
		return x
	case string:
		if zero {
			if _, err := json.Number(x).Float64(); err == nil {
				return json.Number("0")
			}
		}
		return x
	}
	return v
}
