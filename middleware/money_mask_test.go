package middleware

import (
	"Falcon/Models"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestScrubMoneyJSON(t *testing.T) {
	in := `{"message":"ok","data":[{"id":7,"revenue":1220.5,"base_revenue":1220.5,"revenue_vat":null,
	"fee":"30.5","distance":120,"tank_capacity":40000,"pricing_error":"band 3 has no rate",
	"TotalRevenue":9,"totalRevenue":9,"parent_trip":{"total_revenue":3,"receipt_batch":{"id":2}}}],
	"mappings":{"طنطا":{"fee":4,"distance":96,"expected_revenue":{"base":1,"vat":[2,3]}}},
	"meta":{"total":5,"page":1},"name":"<a&b>","big":12345678901234567890}`
	out, err := ScrubMoneyJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"big":12345678901234567890,"data":[{"TotalRevenue":0,"base_revenue":0,"distance":120,"fee":0,"id":7,` +
		`"parent_trip":{"receipt_batch":{"id":2},"total_revenue":0},"pricing_error":"band 3 has no rate",` +
		`"revenue":0,"revenue_vat":null,"tank_capacity":40000,"totalRevenue":0}],` +
		`"mappings":{"طنطا":{"distance":96,"expected_revenue":{"base":0,"vat":[0,0]},"fee":0}},` +
		`"message":"ok","meta":{"page":1,"total":5},"name":"<a&b>"}`
	if string(out) != want {
		t.Errorf("scrubbed:\n%s\nwant:\n%s", out, want)
	}
	if _, err := ScrubMoneyJSON([]byte(`{"a":`)); err == nil {
		t.Error("a truncated body scrubbed without error")
	}
}

func maskApp(mw fiber.Handler, user any, handler fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Get("/x", func(c *fiber.Ctx) error {
		if user != nil {
			c.Locals("user", user)
		}
		return c.Next()
	}, mw, handler)
	return app
}

func body(t *testing.T, app *fiber.App) (int, string) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestMaskMoneyByPermission(t *testing.T) {
	trip := func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"revenue": 10, "id": 1}) }
	cases := []struct {
		name string
		user any
		mw   fiber.Handler
		want string
	}{
		{"anonymous", nil, MaskMoney(), `{"id":1,"revenue":0}`},
		{"permission 1", &Models.User{Permission: 1}, MaskMoney(), `{"id":1,"revenue":0}`},
		{"permission 2 (VerifyAny's value)", Models.User{Permission: 2}, MaskMoney(), `{"id":1,"revenue":0}`},
		{"permission 3", &Models.User{Permission: 3}, MaskMoney(), `{"id":1,"revenue":10}`},
		{"permission 4 (VerifyAny's value)", Models.User{Permission: 4}, MaskMoney(), `{"id":1,"revenue":10}`},
		{"driver app, permission 4", &Models.User{Permission: 4}, StripMoney(), `{"id":1,"revenue":0}`},
	}
	for _, c := range cases {
		code, got := body(t, maskApp(c.mw, c.user, trip))
		if code != 200 || got != c.want {
			t.Errorf("%s: %d %s; want %s", c.name, code, got, c.want)
		}
	}

	// Files pass through untouched: their handlers leave the money out.
	file := func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		return c.SendString(`{"revenue":10}`)
	}
	if _, got := body(t, maskApp(MaskMoney(), nil, file)); got != `{"revenue":10}` {
		t.Errorf("non-JSON body rewritten: %s", got)
	}
	// A JSON body that cannot be parsed is withheld, not passed through.
	broken := func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
		return c.SendString(`{"revenue":10`)
	}
	if code, got := body(t, maskApp(MaskMoney(), nil, broken)); code != 500 || got == `{"revenue":10` {
		t.Errorf("unparseable JSON = %d %s; want it withheld with a 500", code, got)
	}
}
