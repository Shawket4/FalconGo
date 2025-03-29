package Controllers

import (
	"Falcon/Models"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gofiber/fiber/v2"
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
	Success        bool     `json:"success"`
	Text           string   `json:"text,omitempty"`
	Error          string   `json:"error,omitempty"`
	ProcessingTime float64  `json:"processingTime,omitempty"` // in milliseconds
	RawResults     []string `json:"rawResults,omitempty"`     // for debugging
}

// EngravedTextOCR processes images with engraved text (like tire markings)
func EngravedTextOCR(c *fiber.Ctx) error {
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
			Error:   fmt.Sprintf("Invalid image encoding: %v", err),
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

	// Create a temporary directory for processing
	tempDir, err := ioutil.TempDir("", "engraved-ocr-*")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(OCRResponse{
			Success: false,
			Error:   "Failed to create temporary directory",
		})
	}
	defer os.RemoveAll(tempDir)

	// Process the image with techniques optimized for engraved text
	processedImages := []struct {
		path string
		name string
	}{
		{processHighContrast(img, tempDir, "high_contrast"), "High Contrast"},
		{processEdgeEnhanced(img, tempDir, "edge_enhanced"), "Edge Enhanced"},
		{processBinarized(img, tempDir, "binarized"), "Binarized"},
	}

	// Try OCR on each processed image
	var allResults []string
	var bestText string
	var bestConfidence float64

	for _, processedImg := range processedImages {
		if processedImg.path == "" {
			continue
		}

		// Try different OCR configurations
		ocrConfigs := []struct {
			name    string
			psmMode string
		}{
			{"Single Line", "7"},
			{"Single Word", "8"},
			{"Single Char", "10"},
		}

		for _, config := range ocrConfigs {
			// Run OCR on the processed image
			text, confidence, err := runOCR(processedImg.path, config.psmMode)
			if err != nil {
				log.Printf("OCR failed for %s with config %s: %v", processedImg.name, config.name, err)
				continue
			}

			if text != "" {
				result := fmt.Sprintf("[%s, %s, Confidence: %.2f]: %s",
					processedImg.name, config.name, confidence, text)
				allResults = append(allResults, result)

				// Keep track of the highest confidence result
				if confidence > bestConfidence {
					bestText = text
					bestConfidence = confidence
				}
			}
		}
	}

	// Calculate processing time
	processingTime := float64(time.Since(startTime).Milliseconds())

	// Check if we found any text
	if bestText == "" {
		return c.Status(fiber.StatusOK).JSON(OCRResponse{
			Success:        false,
			Error:          "No text recognized in the image",
			ProcessingTime: processingTime,
			RawResults:     allResults,
		})
	}

	// Clean up the result - remove extra spaces and special chars
	bestText = cleanOCRResult(bestText)

	return c.JSON(OCRResponse{
		Success:        true,
		Text:           bestText,
		ProcessingTime: processingTime,
		RawResults:     allResults,
	})
}

// High contrast processing for engraved text
func processHighContrast(img image.Image, tempDir, name string) string {
	// Convert to grayscale
	grayImg := imaging.Grayscale(img)

	// Resize 3x to improve OCR on small text
	resizedImg := imaging.Resize(grayImg, grayImg.Bounds().Dx()*3, 0, imaging.Lanczos)

	// Apply extreme contrast
	contrastImg := imaging.AdjustContrast(resizedImg, 150)

	// Apply sharpening to emphasize text
	sharpImg := imaging.Sharpen(contrastImg, 3.0)

	// Save processed image
	outputPath := filepath.Join(tempDir, name+".png")
	err := imaging.Save(sharpImg, outputPath)
	if err != nil {
		log.Printf("Failed to save image: %v", err)
		return ""
	}

	return outputPath
}

// Edge enhancement for engraved text
func processEdgeEnhanced(img image.Image, tempDir, name string) string {
	// Convert to grayscale
	grayImg := imaging.Grayscale(img)

	// Resize for better processing
	resizedImg := imaging.Resize(grayImg, grayImg.Bounds().Dx()*3, 0, imaging.Lanczos)

	// Apply contrast
	contrastImg := imaging.AdjustContrast(resizedImg, 100)

	// Sharpen to make edges more visible
	sharpImg := imaging.Sharpen(contrastImg, 2.0)

	// Enhance edges by sharpening multiple times
	edgeImg := imaging.Sharpen(sharpImg, 3.0)
	edgeImg = imaging.Sharpen(edgeImg, 2.0)

	// Increase contrast
	finalImg := imaging.AdjustContrast(edgeImg, 60)

	// Save processed image
	outputPath := filepath.Join(tempDir, name+".png")
	err := imaging.Save(finalImg, outputPath)
	if err != nil {
		log.Printf("Failed to save processed image: %v", err)
		return ""
	}

	return outputPath
}

// Binarization for engraved text
func processBinarized(img image.Image, tempDir, name string) string {
	// Convert to grayscale
	grayImg := imaging.Grayscale(img)

	// Resize for better OCR
	resizedImg := imaging.Resize(grayImg, grayImg.Bounds().Dx()*3, 0, imaging.Lanczos)

	// Apply contrast
	contrastImg := imaging.AdjustContrast(resizedImg, 120)

	// Create binary image with a higher threshold for embossed text
	bounds := contrastImg.Bounds()
	binaryImg := image.NewGray(bounds)

	// Use a high threshold specifically for embossed text
	threshold := uint8(160)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.GrayModel.Convert(contrastImg.At(x, y)).(color.Gray)
			if pixel.Y > threshold {
				binaryImg.Set(x, y, color.White)
			} else {
				binaryImg.Set(x, y, color.Black)
			}
		}
	}

	// Save processed image
	outputPath := filepath.Join(tempDir, name+".png")
	file, err := os.Create(outputPath)
	if err != nil {
		log.Printf("Failed to create file: %v", err)
		return ""
	}
	defer file.Close()

	if err := jpeg.Encode(file, binaryImg, &jpeg.Options{Quality: 100}); err != nil {
		log.Printf("Failed to save image: %v", err)
		return ""
	}

	return outputPath
}

// Run OCR on processed image
func runOCR(imagePath, psmMode string) (string, float64, error) {
	// Create a temporary output file base (tesseract will add .txt)
	outputBase := imagePath + ".out"

	// Build command arguments
	args := []string{
		imagePath,
		outputBase,
		"-l", "eng",
		"--psm", psmMode,
		"--dpi", "300",
		"-c", "tessedit_char_whitelist=ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ",
		"-c", "tessedit_do_invert=0", // Assume text is dark on light
		"-c", "textord_min_linesize=1.5", // Help for engraved text
		"-c", "textord_max_noise_size=8", // Ignore small noise
	}

	// Execute command
	cmd := exec.Command("tesseract", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("tesseract error: %v, stderr: %s", err, stderr.String())
	}

	// Read the output file
	outputPath := outputBase + ".txt"
	content, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read OCR output: %v", err)
	}

	// Clean up the temporary output file
	os.Remove(outputPath)

	// Get confidence estimate
	confidence := estimateConfidence(imagePath, psmMode)

	return strings.TrimSpace(string(content)), confidence, nil
}

// Estimate OCR confidence
func estimateConfidence(imagePath, psmMode string) float64 {
	// This is a simplified approach. For a proper implementation,
	// you'd need to run Tesseract with special parameters to get the actual confidence.

	// Create a temporary output file base for confidence checking
	outputBase := imagePath + ".conf"

	args := []string{
		imagePath,
		outputBase,
		"-l", "eng",
		"--psm", psmMode,
		"--dpi", "300",
		"tsv", // Output format for confidence data
	}

	cmd := exec.Command("tesseract", args...)
	if err := cmd.Run(); err != nil {
		return 0
	}

	// Read the TSV output
	outputPath := outputBase + ".tsv"
	content, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return 0
	}

	// Clean up
	os.Remove(outputPath)

	// Parse TSV to get confidence
	lines := strings.Split(string(content), "\n")
	var totalConf float64
	var count int

	for i, line := range lines {
		if i == 0 { // Skip header line
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) >= 11 { // Field 10 has confidence
			var conf float64
			fmt.Sscanf(fields[10], "%f", &conf)
			totalConf += conf
			count++
		}
	}

	if count > 0 {
		return totalConf / float64(count)
	}

	return 0
}

// Clean up OCR result
func cleanOCRResult(text string) string {
	// Remove newlines
	text = strings.ReplaceAll(text, "\n", " ")

	// Remove multiple spaces
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}

	return strings.TrimSpace(text)
}
