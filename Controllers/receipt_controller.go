// controllers/receipt_controller.go
package Controllers

import (
	"Falcon/Models"
	"Falcon/Slack"
	"Falcon/utils"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	// adjust to your project path

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ReceiptController struct {
	DB        *gorm.DB
	UploadDir string
}

func NewReceiptController(db *gorm.DB, uploadDir string) *ReceiptController {
	// Create upload directory if it doesn't exist
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create upload directory: %v", err))
	}

	return &ReceiptController{
		DB:        db,
		UploadDir: uploadDir,
	}
}

// UploadReceiptBatch - Driver uploads multiple receipt images
func (ctrl *ReceiptController) UploadReceiptBatch(c *fiber.Ctx) error {
	// Get the logged-in user from context
	driver, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "User not found in context",
		})
	}

	// Get uploaded files
	form, err := c.MultipartForm()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Failed to parse form data",
			"error":   err.Error(),
		})
	}

	files := form.File["receipt_images"]
	if len(files) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "At least one receipt image is required",
		})
	}

	// Start transaction
	tx := ctrl.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Create receipt batch
	batch := Models.ReceiptBatch{
		DriverID:  driver.ID,
		Status:    "pending",
		ScannedAt: time.Now(),
	}

	if err := tx.Create(&batch).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to create receipt batch",
			"error":   err.Error(),
		})
	}

	// Process each uploaded file
	var receipts []Models.Receipt
	var savedFiles []string

	for _, file := range files {
		// Generate unique filename
		ext := filepath.Ext(file.Filename)
		uniqueFilename := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), ext)
		filePath := filepath.Join(ctrl.UploadDir, uniqueFilename)

		// Save file to filesystem
		if err := c.SaveFile(file, filePath); err != nil {
			// Cleanup already saved files
			for _, savedFile := range savedFiles {
				os.Remove(savedFile)
			}
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to save receipt image",
				"error":   err.Error(),
			})
		}

		savedFiles = append(savedFiles, filePath)

		// Create receipt record
		receipt := Models.Receipt{
			BatchID:   batch.ID,
			ImagePath: uniqueFilename, // Store only filename, not full path
		}
		receipts = append(receipts, receipt)
	}

	// Save all receipt records
	if err := tx.Create(&receipts).Error; err != nil {
		// Cleanup saved files
		for _, savedFile := range savedFiles {
			os.Remove(savedFile)
		}
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to save receipt records",
			"error":   err.Error(),
		})
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		// Cleanup saved files
		for _, savedFile := range savedFiles {
			os.Remove(savedFile)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to commit transaction",
			"error":   err.Error(),
		})
	}

	// Load receipts for response
	batch.Receipts = receipts

	// Send notification to Slack with images
	batch.Receipts = receipts // Ensure receipts are loaded
	go func() {
		if err := Slack.SendReceiptBatchToSlack(batch, ctrl.UploadDir); err != nil {
			log.Printf("Error sending receipt batch to Slack: %v", err)
		} else {
			log.Printf("Receipt batch #%d sent to Slack successfully", batch.ID)
		}
	}()

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Receipt batch uploaded successfully",
		"data":    batch,
	})

}

// REPLACE your existing RegisterLocationPing function with this:

func (ctrl *ReceiptController) RegisterLocationPing(c *fiber.Ctx) error {
	// Get driver from context (set by VerifyDriver middleware)
	_, ok := c.Locals("driver").(Models.Driver)
	if !ok {
		return utils.UnauthorizedResponse(c, "Driver not found in context")
	}

	// Get session from context (set by RequireActiveSessionWithTabletMatch middleware)
	session, ok := c.Locals("session").(Models.DriverSession)
	if !ok {
		return utils.ForbiddenResponse(c, "No active session found")
	}

	// Parse location pings array
	var input struct {
		Locations []struct {
			Lat       float64   `json:"lat"`
			Lng       float64   `json:"lng"`
			TimeStamp time.Time `json:"time_stamp"`
			Speed     *float64  `json:"speed"`    // Optional
			Accuracy  *float64  `json:"accuracy"` // Optional
		} `json:"locations"`
	}

	if err := c.BodyParser(&input); err != nil {
		return utils.BadRequestResponse(c, "Invalid request body")
	}

	if len(input.Locations) == 0 {
		return utils.BadRequestResponse(c, "locations array cannot be empty")
	}

	// Load Cairo timezone
	cairoLocation, err := time.LoadLocation("Africa/Cairo")
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to load Cairo timezone")
	}

	// Create location pings with SessionID (not DriverID!)
	var locationPings []Models.LocationPing
	for _, loc := range input.Locations {
		cairoTime := loc.TimeStamp.In(cairoLocation)

		locationPings = append(locationPings, Models.LocationPing{
			SessionID: &session.ID, // ✅ Use pointer since SessionID is now nullable
			Lat:       loc.Lat,
			Lng:       loc.Lng,
			TimeStamp: cairoTime,
			Speed:     loc.Speed,
			Accuracy:  loc.Accuracy,
		})
	}

	// Batch insert
	if err := ctrl.DB.Create(&locationPings).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to save location pings")
	}

	return utils.CreatedResponse(c, fiber.Map{
		"message": "Location pings saved successfully",
		"count":   len(locationPings),
	})
}

// GetPendingBatches - Office gets all pending receipt batches
func (ctrl *ReceiptController) GetPendingBatches(c *fiber.Ctx) error {
	var batches []Models.ReceiptBatch

	// Query pending batches with driver info and receipts
	if err := ctrl.DB.
		Where("status = ?", "pending").
		Preload("Driver").
		Preload("Receipts").
		Order("scanned_at DESC").
		Find(&batches).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch pending batches",
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Pending receipt batches retrieved successfully",
		"data":    batches,
		"total":   len(batches),
	})
}

// GetBatchDetails - Office gets specific batch details
func (ctrl *ReceiptController) GetBatchDetails(c *fiber.Ctx) error {
	batchID, err := strconv.ParseUint(c.Params("batch_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid batch ID",
		})
	}

	var batch Models.ReceiptBatch
	if err := ctrl.DB.
		Preload("Driver").
		Preload("Receipts").
		First(&batch, batchID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Receipt batch not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch batch details",
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Batch details retrieved successfully",
		"data":    batch,
	})
}

// RejectBatch - Office rejects a receipt batch
func (ctrl *ReceiptController) RejectBatch(c *fiber.Ctx) error {
	batchID, err := strconv.ParseUint(c.Params("batch_id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid batch ID",
		})
	}

	var batch Models.ReceiptBatch
	if err := ctrl.DB.First(&batch, batchID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Receipt batch not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch batch",
			"error":   err.Error(),
		})
	}

	if batch.Status != "pending" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Can only reject pending batches",
		})
	}

	batch.Status = "rejected"
	if err := ctrl.DB.Save(&batch).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to reject batch",
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Batch rejected successfully",
		"data":    batch,
	})
}
