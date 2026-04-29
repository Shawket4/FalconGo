package main

import (
	"log"
	"time"
	
	"Falcon/Models" 
	"Falcon/Services" 
	
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const BatchSize = 8 // Hard limit for CPU TEI engine

func main() {
	// Update with your actual DSN
	dsn := "host=localhost user=youruser password=yourpass dbname=yourdb port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), 
	})
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	Models.DB = db

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
	
	for i := 0; i < total; i += BatchSize {
		end := i + BatchSize
		if end > total {
			end = total
		}
		
		batch := items[i:end]
		var texts []string
		
		for _, item := range batch {
			combined := item.Service
			if item.Notes != "" {
				combined += " - " + item.Notes
			}
			texts = append(texts, combined)
		}

		vectors, err := Services.GenerateEmbeddingsBatch(texts)
		if err != nil {
			log.Printf("ERROR on batch %d: %v", i, err)
			continue
		}

		tx := db.Begin()
		for j, item := range batch {
			if j >= len(vectors) {
				break
			}
			pgVec := pgvector.NewVector(vectors[j])
			tx.Model(&item).UpdateColumn("embedding", pgVec)
			successCount++
		}
		tx.Commit()

		log.Printf("Processed %d/%d...", end, total)
	}

	log.Printf("Backfill complete! %d/%d embedded.", successCount, total)
}