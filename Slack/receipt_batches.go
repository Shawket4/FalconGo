package Slack

import (
	"Falcon/Models"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
)

// Receipt batches channel ID
const RECEIPT_BATCHES_CHANNEL_ID = "C09K0CN10HZ"

// SendReceiptBatchToSlack sends a receipt batch notification with images to Slack
func SendReceiptBatchToSlack(batch Models.ReceiptBatch, uploadDir string) error {
	log.Printf("=== Starting SendReceiptBatchToSlack for batch #%d ===", batch.ID)

	if err := godotenv.Load(".env"); err != nil {
		log.Printf("ERROR: Failed to load .env file: %v", err)
		return fmt.Errorf("error loading .env file: %v", err)
	}
	log.Println("DEBUG: .env file loaded successfully")

	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken == "" {
		log.Println("ERROR: SLACK_BOT_TOKEN not set in environment")
		return fmt.Errorf("SLACK_BOT_TOKEN not set")
	}
	log.Printf("DEBUG: Slack token found (length: %d)", len(slackToken))

	// Load driver information
	log.Printf("DEBUG: Loading driver with ID: %d", batch.DriverID)
	var driver Models.Driver
	if err := Models.DB.First(&driver, batch.DriverID).Error; err != nil {
		log.Printf("ERROR: Failed to load driver: %v", err)
		return fmt.Errorf("failed to load driver: %v", err)
	}
	log.Printf("DEBUG: Driver loaded successfully: %s", driver.Name)

	// Create Slack API client
	log.Println("DEBUG: Creating Slack API client")
	api := slack.New(slackToken)

	// Generate message text
	message := generateReceiptBatchMessage(batch, driver)
	log.Printf("DEBUG: Generated message:\n%s", message)

	// If no receipts, just send the message
	if len(batch.Receipts) == 0 {
		log.Println("DEBUG: No receipts in batch, sending message only")
		_, _, err := api.PostMessage(
			RECEIPT_BATCHES_CHANNEL_ID,
			slack.MsgOptionText(message, false),
		)
		if err != nil {
			log.Printf("ERROR: Failed to post message: %v", err)
			return err
		}
		log.Println("DEBUG: Message posted successfully")
		return nil
	}

	log.Printf("DEBUG: Uploading receipt batch #%d with %d images to Slack...", batch.ID, len(batch.Receipts))

	// Collect valid file paths
	var filePaths []string
	for i, receipt := range batch.Receipts {
		filePath := filepath.Join(uploadDir, receipt.ImagePath)
		log.Printf("DEBUG: Checking file %d: %s", i+1, filePath)

		if _, err := os.Stat(filePath); err != nil {
			log.Printf("WARNING: File not found: %s (error: %v)", filePath, err)
			continue
		}
		log.Printf("DEBUG: File exists: %s", filePath)
		filePaths = append(filePaths, filePath)
	}

	log.Printf("DEBUG: Found %d valid files out of %d receipts", len(filePaths), len(batch.Receipts))

	if len(filePaths) == 0 {
		log.Println("ERROR: No valid image files found")
		return fmt.Errorf("no valid image files found")
	}

	// Upload all files using UploadFileV2
	for i, filePath := range filePaths {
		log.Printf("DEBUG: Uploading file %d/%d: %s", i+1, len(filePaths), filePath)

		file, err := os.Open(filePath)
		if err != nil {
			log.Printf("WARNING: Could not open file %s: %v", filePath, err)
			continue
		}

		fileInfo, _ := file.Stat()

		params := slack.UploadFileV2Parameters{
			Channel:  RECEIPT_BATCHES_CHANNEL_ID,
			Reader:   file,
			Filename: filepath.Base(filePath),
			FileSize: int(fileInfo.Size()),
		}

		// Add message only to the first file
		if i == 0 {
			params.InitialComment = message
			log.Printf("DEBUG: Adding initial comment to first file")
		}

		log.Printf("DEBUG: Upload parameters: Channel=%s, Filename=%s, FileSize=%d", params.Channel, params.Filename, params.FileSize)

		summary, err := api.UploadFileV2(params)
		file.Close()

		if err != nil {
			log.Printf("ERROR: Failed to upload file %s: %v", filePath, err)
			if i == 0 {
				// If first file fails, abort
				return fmt.Errorf("failed to upload first file: %v", err)
			}
			continue
		}

		log.Printf("SUCCESS: Uploaded image %d/%d: %s (File ID: %s)", i+1, len(filePaths), filepath.Base(filePath), summary.ID)

		// Small delay between uploads to avoid rate limiting
		if i < len(filePaths)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	log.Printf("=== Receipt batch #%d uploaded to Slack successfully ===", batch.ID)
	return nil
}

// generateReceiptBatchMessage creates the Slack message for a receipt batch
func generateReceiptBatchMessage(batch Models.ReceiptBatch, driver Models.Driver) string {
	message := "*New Receipt Batch Uploaded*\n\n"
	message += fmt.Sprintf("*Batch ID:* #%d\n", batch.ID)
	message += fmt.Sprintf("*Driver:* %s\n", driver.Name)
	message += fmt.Sprintf("*Number of Receipts:* %d\n", len(batch.Receipts))
	message += fmt.Sprintf("*Status:* %s\n", batch.Status)
	message += fmt.Sprintf("*Uploaded:* %s\n", batch.ScannedAt.Format("January 2, 2006 - 15:04:05 MST"))
	message += "*View: https://apextransport.ddns.net/receipt-batch-manager"
	return message
}
