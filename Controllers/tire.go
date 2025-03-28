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
	"time"

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
	Success        bool    `json:"success"`
	DOT            string  `json:"dot,omitempty"`
	Error          string  `json:"error,omitempty"`
	ProcessingTime float64 `json:"processingTime,omitempty"` // in milliseconds
	RawText        string  `json:"rawText,omitempty"`        // for debugging
}

func DOTOCR(c *fiber.Ctx) error {
	startTime := time.Now()

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
			Error:   fmt.Sprintf("Failed to decode image: %v", err),
		})
	}

	// Preprocess image to enhance DOT visibility
	processedImg := preprocessImage(img)

	// Save processed image to a temporary file for OCR
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
	tmpfile.Close() // Close before gosseract reads it

	// Try multiple OCR configurations for better results
	var bestText string
	var dot string

	// Try different PSM modes (Page Segmentation Modes)
	psmModes := []gosseract.PageSegMode{
		gosseract.PSM_SINGLE_LINE, // 7
		gosseract.PSM_SINGLE_WORD, // 8
		gosseract.PSM_AUTO,        // 3
		gosseract.PSM_SINGLE_CHAR, // 10
	}

	for _, psm := range psmModes {
		text, err := performOCR(tmpfile.Name(), psm)
		if err != nil {
			continue
		}

		// Try to extract DOT number
		extractedDOT := extractDOTNumber(text)
		if extractedDOT != "" {
			dot = extractedDOT
			bestText = text
			break
		}

		// Save the text for future tries if no DOT found yet
		if bestText == "" {
			bestText = text
		}
	}

	// If no DOT found yet, try one more time with a broader whitelist
	if dot == "" && bestText == "" {
		text, _ := performOCRWithBroadWhitelist(tmpfile.Name())
		bestText = text
		dot = extractDOTNumber(text)
	}

	// Calculate processing time
	processingTime := float64(time.Since(startTime).Milliseconds())

	// No DOT found
	if dot == "" {
		return c.Status(fiber.StatusOK).JSON(OCRResponse{
			Success:        false,
			Error:          "No DOT number found in the image",
			ProcessingTime: processingTime,
			RawText:        bestText,
		})
	}

	return c.JSON(OCRResponse{
		Success:        true,
		DOT:            dot,
		ProcessingTime: processingTime,
		RawText:        bestText,
	})
}

func preprocessImage(img image.Image) image.Image {
	// Step 1: Convert to grayscale
	grayImg := imaging.Grayscale(img)

	// Step 2: Increase contrast to make engravings more visible
	contrastedImg := imaging.AdjustContrast(grayImg, 80)

	// Step 3: Apply sharpening to enhance edges (engravings)
	sharpenedImg := imaging.Sharpen(contrastedImg, 2.5)

	// Step 4: Apply brightness adjustment for better text recognition
	brightImg := imaging.AdjustBrightness(sharpenedImg, 15)

	// Return the processed image
	return brightImg
}

func performOCR(imagePath string, pageSegMode gosseract.PageSegMode) (string, error) {
	client := gosseract.NewClient()
	defer client.Close()

	// Set image path
	if err := client.SetImage(imagePath); err != nil {
		return "", fmt.Errorf("failed to set image: %v", err)
	}

	// Configure Tesseract for DOT number recognition
	client.SetLanguage("eng")
	client.SetPageSegMode(pageSegMode)
	client.SetWhitelist("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ")

	// Get the text
	text, err := client.Text()
	if err != nil {
		return "", fmt.Errorf("OCR processing failed: %v", err)
	}

	return text, nil
}

func performOCRWithBroadWhitelist(imagePath string) (string, error) {
	client := gosseract.NewClient()
	defer client.Close()

	// Set image path
	if err := client.SetImage(imagePath); err != nil {
		return "", fmt.Errorf("failed to set image: %v", err)
	}

	// Configure Tesseract with a broader whitelist and different PSM
	client.SetLanguage("eng")
	client.SetPageSegMode(gosseract.PSM_AUTO)
	// No whitelist restriction - allow all characters

	// Get the text
	text, err := client.Text()
	if err != nil {
		return "", fmt.Errorf("OCR processing failed: %v", err)
	}

	return text, nil
}

func extractDOTNumber(text string) string {
	// Clean up the text
	text = strings.TrimSpace(text)

	// Look for explicit DOT pattern
	dotPattern := regexp.MustCompile(`(?i)DOT\s*([A-Z0-9\s]+)`)
	matches := dotPattern.FindStringSubmatch(text)

	if len(matches) >= 2 {
		return fmt.Sprintf("DOT %s", strings.TrimSpace(matches[1]))
	}

	// General pattern for DOT numbers: typically a mix of letters and numbers in grouped patterns
	// Format is typically: plant code (1-3 chars) + date code (3-4 chars) + optional code

	// Common DOT patterns:
	// - 3 characters, 2 characters, 3-4 numbers
	// - 2-3 characters, 3-4 numbers, 1-4 characters

	// Try to match pattern like "XX YY ZZZZ" or "XXX YY ZZZZ"
	dotFormatA := regexp.MustCompile(`([A-Z0-9]{1,3})\s*([A-Z0-9]{1,2})\s*([A-Z0-9]{3,4})`)
	matchesA := dotFormatA.FindStringSubmatch(text)

	if len(matchesA) >= 4 {
		return fmt.Sprintf("DOT %s %s %s", matchesA[1], matchesA[2], matchesA[3])
	}

	// Try to match pattern like "XXX YYYY ZZZ"
	dotFormatB := regexp.MustCompile(`([A-Z0-9]{2,3})\s*([A-Z0-9]{3,4})\s*([A-Z0-9]{1,4})`)
	matchesB := dotFormatB.FindStringSubmatch(text)

	if len(matchesB) >= 4 {
		return fmt.Sprintf("DOT %s %s %s", matchesB[1], matchesB[2], matchesB[3])
	}

	// No standard pattern found, but we can still look for DOT-like patterns
	// This is a more generic approach without hardcoding specific numbers

	// If text contains at least 2 groups of alphanumerics with 2+ chars each, it might be a DOT
	potentialGroups := regexp.MustCompile(`[A-Z0-9]{2,}`)
	groups := potentialGroups.FindAllString(text, -1)

	if len(groups) >= 2 {
		// Just format whatever we found as a DOT number
		result := "DOT"
		for _, group := range groups {
			result += " " + group
		}
		return result
	}

	return ""
}
