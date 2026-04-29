package main

import (
	"fmt"
	"log"
	"os"

	"Falcon/Models"   // Ensure this matches your project structure
	"Falcon/AbstractFunctions" // Ensure this matches your project structure

	"github.com/joho/godotenv"
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const BatchSize = 8 // Hard limit for CPU TEI engine to prevent crashes

func main() {
	// 1. Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// 2. Build DSN from Environment Variables
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
	)

	// 3. Connect to Postgres
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	Models.DB = db

	// 4. Find items that haven't been vectorized yet
	var items []Models.InspectionItem
	if err := db.Where("embedding IS NULL").Find(&items).Error; err != nil {
		log.Fatalf("Failed to fetch items: %v", err)
	}

	total := len(items)
	log.Printf("Found %d items missing embeddings.", total)
	if total == 0 {
		return
	}

	successCount := 0

	// 5. Process in batches of 8
	for i := 0; i < total; i += BatchSize {
		end := i + BatchSize
		if end > total {
			end = total
		}

		batch := items[i:end]
		var texts []string

		for _, item := range batch {
			// Combine Service and Notes for better semantic context
			combined := item.Service
			if item.Notes != "" {
				combined += " - " + item.Notes
			}
			texts = append(texts, combined)
		}

		// Call your local TEI Docker container
		vectors, err := AbstractFunctions.GenerateEmbeddingsBatch(texts)
		if err != nil {
			log.Printf("ERROR on batch starting at index %d: %v", i, err)
			continue
		}

		// 6. Update database in a transaction
		tx := db.Begin()
		for j, item := range batch {
			if j >= len(vectors) {
				break
			}
			// Use pgvector-go to format the float32 array
			pgVec := pgvector.NewVector(vectors[j])
			if err := tx.Model(&item).UpdateColumn("embedding", pgVec).Error; err != nil {
				log.Printf("Failed to update item %d: %v", item.ID, err)
			} else {
				successCount++
			}
		}
		tx.Commit()

		log.Printf("Progress: %d/%d processed...", end, total)
	}

	log.Printf("Backfill complete! %d/%d records successfully embedded.", successCount, total)
}