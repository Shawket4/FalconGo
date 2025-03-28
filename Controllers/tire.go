package Controllers

import (
	"Falcon/Models"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"regexp"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gofiber/fiber/v2"
	"github.com/otiai10/gosseract/v2"
)

// GetAllTires fetches all tires in the system
func GetAllTires(c *fiber.Ctx) error {
	var tires []Models.Tire
	if err := Models.DB.Find(&tires).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusOK).JSON(tires)
}

// GetTire fetches a single tire by ID
func GetTire(c *fiber.Ctx) error {
	id := c.Params("id")
	var tire Models.Tire

	if err := Models.DB.First(&tire, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tire not found"})
	}

	return c.Status(fiber.StatusOK).JSON(tire)
}

// CreateTire creates a new tire
func CreateTire(c *fiber.Ctx) error {
	var tire Models.Tire
	if err := c.BodyParser(&tire); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if err := Models.DB.Create(&tire).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(tire)
}

// UpdateTire updates tire information
func UpdateTire(c *fiber.Ctx) error {
	id := c.Params("id")
	var tire Models.Tire

	// Check if the tire exists
	if err := Models.DB.First(&tire, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tire not found"})
	}

	// Bind the JSON request to the tire
	var updateData Models.Tire
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Update tire fields
	Models.DB.Model(&tire).Updates(updateData)
	return c.Status(fiber.StatusOK).JSON(tire)
}

// DeleteTire deletes a tire
func DeleteTire(c *fiber.Ctx) error {
	id := c.Params("id")
	var tire Models.Tire

	// Check if the tire exists
	if err := Models.DB.First(&tire, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tire not found"})
	}

	// Unassign the tire from any position
	Models.DB.Model(&Models.TirePosition{}).Where("tire_id = ?", id).Update("tire_id", nil)

	// Delete the tire
	Models.DB.Delete(&tire)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Tire deleted successfully"})
}

// SearchTires finds tires by serial, brand or model
func SearchTires(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Search query is required"})
	}

	var tires []Models.Tire
	result := Models.DB.Where("serial LIKE ? OR brand LIKE ? OR model LIKE ?",
		"%"+query+"%", "%"+query+"%", "%"+query+"%").Find(&tires)

	if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": result.Error.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(tires)
}

type OCRRequest struct {
	Image string `json:"image"` // base64 encoded image
}

type OCRResponse struct {
	Success bool   `json:"success"`
	DOT     string `json:"dot,omitempty"`
	Error   string `json:"error,omitempty"`
}

func DOTOCR(c *fiber.Ctx) error {
	var req OCRRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(OCRResponse{
			Success: false,
			Error:   "Invalid request body",
		})
	}

	// Check if image is provided
	if req.Image == "" {
		return c.Status(fiber.StatusBadRequest).JSON(OCRResponse{
			Success: false,
			Error:   "No image provided",
		})
	}

	// Process base64 image
	imageData := req.Image
	// Remove data:image/jpeg;base64, prefix if present
	if idx := strings.Index(imageData, ","); idx != -1 {
		imageData = imageData[idx+1:]
	}

	// Decode base64 image
	imgBytes, err := base64.StdEncoding.DecodeString(imageData)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(OCRResponse{
			Success: false,
			Error:   "Invalid image encoding",
		})
	}

	// Convert bytes to image
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(OCRResponse{
			Success: false,
			Error:   "Failed to decode image",
		})
	}

	// Preprocess image to enhance DOT visibility
	processedImg, err := preprocessImage(img)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(OCRResponse{
			Success: false,
			Error:   "Failed to preprocess image",
		})
	}

	// Save processed image to temporary file
	tmpfile, err := os.CreateTemp("", "tire-*.jpg")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(OCRResponse{
			Success: false,
			Error:   "Failed to create temporary file",
		})
	}
	defer os.Remove(tmpfile.Name())
	defer tmpfile.Close()

	if err := jpeg.Encode(tmpfile, processedImg, &jpeg.Options{Quality: 100}); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(OCRResponse{
			Success: false,
			Error:   "Failed to encode processed image",
		})
	}

	// Perform OCR using Tesseract
	dot, err := performOCR(tmpfile.Name())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(OCRResponse{
			Success: false,
			Error:   fmt.Sprintf("OCR failed: %v", err),
		})
	}

	// No DOT found
	if dot == "" {
		return c.Status(fiber.StatusOK).JSON(OCRResponse{
			Success: false,
			Error:   "No DOT number found in the image",
		})
	}

	return c.JSON(OCRResponse{
		Success: true,
		DOT:     dot,
	})
}

func preprocessImage(img image.Image) (image.Image, error) {
	// Step 1: Convert to grayscale
	grayImg := imaging.Grayscale(img)

	// Step 2: Increase contrast to make engravings more visible
	contrastedImg := imaging.AdjustContrast(grayImg, 50)

	// Step 3: Apply sharpening to enhance edges (engravings)
	sharpenedImg := imaging.Sharpen(contrastedImg, 2.0)

	// Step 4: Apply threshold to create binary image
	// This helps in isolating the text from background
	thresholdedImg := imaging.AdjustBrightness(sharpenedImg, 10)
	thresholdedImg = imaging.AdjustContrast(thresholdedImg, 40)

	return thresholdedImg, nil
}

func performOCR(imagePath string) (string, error) {
	client := gosseract.NewClient()
	defer client.Close()

	// Set Tesseract configurations for improved DOT detection
	client.SetImage(imagePath)

	// Configure Tesseract for DOT number recognition
	// We'll set it to only look for digits, letters, and spaces
	client.SetWhitelist("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ")

	// Set page segmentation mode to treat the image as a single line of text
	client.SetPageSegMode(gosseract.PSM_SINGLE_LINE)

	// Obtain the text from the image
	text, err := client.Text()
	if err != nil {
		return "", err
	}

	// Clean up the text
	text = strings.TrimSpace(text)

	// Look for DOT pattern
	dotPattern := regexp.MustCompile(`DOT\s*([A-Z0-9\s]+)`)
	matches := dotPattern.FindStringSubmatch(text)

	if len(matches) >= 2 {
		return fmt.Sprintf("DOT %s", strings.TrimSpace(matches[1])), nil
	}

	// If no DOT prefix found, try to extract just the pattern
	// DOT numbers typically follow a pattern like: plant code (2 chars) + date code (4 chars) + optional code
	altPattern := regexp.MustCompile(`([A-Z0-9]{2,3})\s*([A-Z0-9]{3,4})\s*([A-Z0-9]{3,4})`)
	altMatches := altPattern.FindStringSubmatch(text)

	if len(altMatches) >= 4 {
		return fmt.Sprintf("DOT %s %s %s",
			strings.TrimSpace(altMatches[1]),
			strings.TrimSpace(altMatches[2]),
			strings.TrimSpace(altMatches[3])), nil
	}

	// If no clear pattern detected, return the cleaned text
	// Let the client application decide how to interpret it
	if text != "" {
		return text, nil
	}

	return "", nil
}

// Helper function to save debug images during development
func saveDebugImage(img image.Image, name string) error {
	outFile, err := os.Create(name)
	if err != nil {
		return err
	}
	defer outFile.Close()

	return jpeg.Encode(outFile, img, &jpeg.Options{Quality: 90})
}
