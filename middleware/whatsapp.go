package middleware

import (
	"Falcon/Constants"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

func CheckWPLoginMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		client := &http.Client{}
		method := "GET"

		url := Constants.WhatsappGoService + "/app/devices"
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to create request",
			})
		}
		req.Header.Add("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to check login status",
			})
		}
		defer res.Body.Close()

		body, err := io.ReadAll(res.Body) // Use io.ReadAll instead of ioutil.ReadAll
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to read response",
			})
		}

		var output struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Results []struct {
				Name   string `json:"name"`
				Device string `json:"device"`
			} `json:"results"`
		}

		if err = json.Unmarshal(body, &output); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to parse response",
			})
		}

		// Check if user is logged in
		if len(output.Results) == 0 {
			return c.Status(3004).JSON(fiber.Map{
				"error": "Not logged in to WhatsApp",
			})
		}

		// Continue to next handler
		return c.Next()
	}
}
