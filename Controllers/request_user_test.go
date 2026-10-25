package Controllers

import (
	"Falcon/Models"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRequestUserReadsWhatVerifyStored(t *testing.T) {
	app := fiber.New()
	var got []uint
	record := func(c *fiber.Ctx) error {
		got = append(got, RequestUser(c).Id)
		return nil
	}
	app.Get("/ptr", func(c *fiber.Ctx) error { c.Locals("user", &Models.User{Id: 7}); return c.Next() }, record)
	app.Get("/val", func(c *fiber.Ctx) error { c.Locals("user", Models.User{Id: 8}); return c.Next() }, record)
	app.Get("/none", record)
	for _, p := range []string{"/ptr", "/val", "/none"} {
		if _, err := app.Test(httptest.NewRequest("GET", p, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 3 || got[0] != 7 || got[1] != 8 || got[2] != 0 {
		t.Fatalf("RequestUser ids = %v; want [7 8 0] (unauthenticated is an empty user, never nil)", got)
	}
}
