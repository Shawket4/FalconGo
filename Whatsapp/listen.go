package Whatsapp

import (
	"Falcon/Constants"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
)

type GatewayStatus struct {
	IsConnected bool   `json:"is_connected"`
	IsLoggedIn  bool   `json:"is_logged_in"`
	JID         string `json:"jid"`
}

func fetchGatewayStatus() (*GatewayStatus, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	res, err := client.Get(Constants.WhatsappGoService + "/app/status")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var output struct {
		Results GatewayStatus `json:"results"`
	}
	if err = json.Unmarshal(body, &output); err != nil {
		return nil, err
	}
	return &output.Results, nil
}

// CheckWPLogin reports the gateway's live state from GoWA /app/status.
// /app/devices is not enough: a stored session still lists a device while
// the link to WhatsApp's servers is dead (e.g. the client-outdated outage).
func CheckWPLogin(c *fiber.Ctx) error {
	status, err := fetchGatewayStatus()
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "WhatsApp service unreachable",
		})
	}
	return c.Status(http.StatusOK).JSON(status)
}

// Reconnect asks GoWA to re-establish the WhatsApp link from the stored
// session, then reports the resulting status.
func Reconnect(c *fiber.Ctx) error {
	client := &http.Client{Timeout: 30 * time.Second}

	res, err := client.Get(Constants.WhatsappGoService + "/app/reconnect")
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "WhatsApp service unreachable",
		})
	}
	res.Body.Close()

	status, err := fetchGatewayStatus()
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "WhatsApp service unreachable",
		})
	}
	return c.Status(http.StatusOK).JSON(status)
}

func GetQRCode(c *fiber.Ctx) error {
	client := &http.Client{}
	method := "GET"

	urlLogin := Constants.WhatsappGoService + "/app/login"
	req, err := http.NewRequest(method, urlLogin, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create request",
		})
	}
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get QR link",
		})
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body) // Use io.ReadAll instead of ioutil.ReadAll
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to read response body",
		})
	}

	var output struct {
		Results struct {
			QRLink string `json:"qr_link"`
		} `json:"results"`
	}

	if err = json.Unmarshal(body, &output); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to parse response",
		})
	}

	// Get the QR code image
	req, err = http.NewRequest(method, output.Results.QRLink, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create QR request",
		})
	}

	res, err = client.Do(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get QR code",
		})
	}
	defer res.Body.Close()

	qrBody, err := io.ReadAll(res.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to read QR code",
		})
	}

	// Set proper headers for image download
	c.Set("Content-Disposition", "attachment; filename=qr.png")
	c.Set("Content-Type", "image/png")

	return c.Send(qrBody)
}

func SendMessage(phone, message string) error {
	client := &http.Client{}
	method := "POST"

	urlLogin := Constants.WhatsappGoService + "/send/message"
	dataStr := fmt.Sprintf(`{"phone": "%s", "message": "%s"}`, phone, message)
	fmt.Println(dataStr)
	data := []byte(dataStr)
	req, err := http.NewRequest(method, urlLogin, bytes.NewBuffer(data))

	if err != nil {
		fmt.Println(err)
		return err
	}
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println("response Body:", string(body))
	return nil
}
