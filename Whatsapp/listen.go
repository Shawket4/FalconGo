package Whatsapp

import (
	"Falcon/Constants"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

func CheckWPLogin(c *fiber.Ctx) error {
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
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"error": "Not logged in to WhatsApp",
		})
	}
	return c.Status(http.StatusOK).JSON(nil)
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
