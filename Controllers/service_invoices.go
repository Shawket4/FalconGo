package Controllers

import (
	"strconv"
	"time"

	"Falcon/Models" // Adjust the import path to match your project
	"Falcon/AbstractFunctions"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"github.com/pgvector/pgvector-go"
)

// CreateServiceInvoice creates a new service invoice
// POST /api/service-invoices
func CreateServiceInvoice(c *fiber.Ctx) error {
	var req Models.ServiceInvoiceRequest

	// 1. Parse request body
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid request body",
			"message": err.Error(),
		})
	}

	// 2. GENERATE VECTORS FIRST (Outside the DB Transaction)
	// We do this first so the DB isn't locked while waiting for the AI engine
	embeddings, err := generateEmbeddingsForItems(req.InspectionItems)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to generate search vectors",
			"details": err.Error(),
		})
	}

	// 3. Validation
	if req.CarID == 0 || req.DriverName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Validation failed",
			"message": "car_id and driver_name are required",
		})
	}

	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid date format",
			"message": "Date must be in YYYY-MM-DD format",
		})
	}

	// 4. Begin transaction
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Transaction failed",
		})
	}

	// 5. Create the Service Invoice (The Parent)
	// We do this first so that invoice.ID is populated for the children
	invoice := &Models.ServiceInvoice{
		CarID:           req.CarID,
		DriverName:      req.DriverName,
		Date:            date,
		MeterReading:    req.MeterReading,
		PlateNumber:     req.PlateNumber,
		Supervisor:      req.Supervisor,
		OperatingRegion: req.OperatingRegion,
	}

	if err := tx.Create(invoice).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create service invoice",
		})
	}

	// 6. Create Inspection Items (The Children)
	embeddingIdx := 0
	for i, item := range req.InspectionItems {
		if item.Service != "" || item.Notes != "" {
			inspectionItem := Models.InspectionItem{
				ServiceInvoiceID: invoice.ID, // Links to the newly created parent
				Service:          item.Service,
				Notes:            item.Notes,
				ItemOrder:        i + 1,
			}

			// Assign the pre-calculated vector
			if embeddingIdx < len(embeddings) {
				inspectionItem.Embedding = pgvector.NewVector(embeddings[embeddingIdx])
				embeddingIdx++
			}

			if err := tx.Create(&inspectionItem).Error; err != nil {
				tx.Rollback()
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to create inspection items",
				})
			}
		}
	}

	// 7. Finalize the Database write
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to commit transaction",
		})
	}

	// 8. Reload everything for the response
	Models.DB.Preload("Car").Preload("InspectionItems", func(db *gorm.DB) *gorm.DB {
		return db.Order("item_order ASC")
	}).First(invoice, invoice.ID)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Service invoice created successfully",
		"data":    invoice,
	})
}

// GetServiceInvoice retrieves a service invoice by ID
// GET /api/service-invoices/:id
func GetServiceInvoice(c *fiber.Ctx) error {
	id := c.Params("id")
	invoiceID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid ID",
			"message": "ID must be a valid number",
		})
	}

	var invoice Models.ServiceInvoice
	err = Models.DB.Preload("Car").Preload("InspectionItems", func(db *gorm.DB) *gorm.DB {
		return db.Order("item_order ASC")
	}).First(&invoice, uint(invoiceID)).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   "Service invoice not found",
				"message": "The specified service invoice does not exist",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Database error",
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Service invoice retrieved successfully",
		"data":    invoice,
	})
}

// GetServiceInvoicesByCarID retrieves all service invoices for a specific car
// GET /api/cars/:carId/service-invoices
func GetServiceInvoicesByCarID(c *fiber.Ctx) error {
	carID := c.Params("carId")
	carIDUint, err := strconv.ParseUint(carID, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid Car ID",
			"message": "Car ID must be a valid number",
		})
	}

	// Check if car exists
	var car Models.Car
	if err := Models.DB.First(&car, uint(carIDUint)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   "Car not found",
				"message": "The specified car does not exist",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Database error",
			"message": err.Error(),
		})
	}

	var invoices []Models.ServiceInvoice
	err = Models.DB.Where("car_id = ?", uint(carIDUint)).
		Preload("InspectionItems", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Order("date DESC").
		Find(&invoices).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Database error",
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Service invoices retrieved successfully",
		"data":    invoices,
		"count":   len(invoices),
	})
}

// GetAllServiceInvoices retrieves all service invoices with pagination
// GET /api/service-invoices?page=1&limit=10
func GetAllServiceInvoices(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit

	var invoices []Models.ServiceInvoice
	var total int64

	// Get total count
	Models.DB.Model(&Models.ServiceInvoice{}).Count(&total)

	// Get paginated results
	err := Models.DB.Preload("Car").Preload("InspectionItems", func(db *gorm.DB) *gorm.DB {
		return db.Order("item_order ASC")
	}).Order("date DESC").Offset(offset).Limit(limit).Find(&invoices).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Database error",
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Service invoices retrieved successfully",
		"data":    invoices,
		"pagination": fiber.Map{
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": (total + int64(limit) - 1) / int64(limit),
		},
	})
}

// UpdateServiceInvoice updates an existing service invoice
// PUT /api/service-invoices/:id
func UpdateServiceInvoice(c *fiber.Ctx) error {
	id := c.Params("id")
	invoiceID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid ID",
			"message": "ID must be a valid number",
		})
	}

	var req Models.ServiceInvoiceRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid request body",
			"message": err.Error(),
		})
	}

	// 1. Check if invoice exists
	var invoice Models.ServiceInvoice
	if err := Models.DB.First(&invoice, uint(invoiceID)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   "Service invoice not found",
				"message": "The specified service invoice does not exist",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Database error",
			"message": err.Error(),
		})
	}

	// 2. Parse date
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid date format",
			"message": "Date must be in YYYY-MM-DD format",
		})
	}

	// 3. Generate vectors for all items in the request
	// We do this BEFORE starting the transaction to avoid holding DB locks 
	// while waiting for the TEI Docker container to respond.
	embeddings, err := generateEmbeddingsForItems(req.InspectionItems)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to generate search vectors",
			"details": err.Error(),
		})
	}

	// 4. Begin transaction
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Transaction error",
			"message": tx.Error.Error(),
		})
	}

	// 5. Update main invoice fields
	invoice.DriverName = req.DriverName
	invoice.Date = date
	invoice.MeterReading = req.MeterReading
	invoice.PlateNumber = req.PlateNumber
	invoice.Supervisor = req.Supervisor
	invoice.OperatingRegion = req.OperatingRegion

	if err := tx.Save(&invoice).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to update service invoice",
			"message": err.Error(),
		})
	}

	// 6. Delete existing inspection items
	if err := tx.Where("service_invoice_id = ?", invoice.ID).Delete(&Models.InspectionItem{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to delete existing inspection items",
			"message": err.Error(),
		})
	}

	// 7. Create new inspection items with their vectors
	embeddingIdx := 0
	for i, item := range req.InspectionItems {
		if item.Service != "" || item.Notes != "" {
			inspectionItem := Models.InspectionItem{
				ServiceInvoiceID: invoice.ID,
				Service:          item.Service,
				Notes:            item.Notes,
				ItemOrder:        i + 1,
			}

			// Assign the corresponding vector from our batch results
			if embeddingIdx < len(embeddings) {
				inspectionItem.Embedding = pgvector.NewVector(embeddings[embeddingIdx])
				embeddingIdx++
			}

			if err := tx.Create(&inspectionItem).Error; err != nil {
				tx.Rollback()
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   "Failed to create inspection items",
					"message": err.Error(),
				})
			}
		}
	}

	// 8. Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to commit transaction",
			"message": err.Error(),
		})
	}

	// 9. Reload with relationships for the response
	Models.DB.Preload("Car").Preload("InspectionItems", func(db *gorm.DB) *gorm.DB {
		return db.Order("item_order ASC")
	}).First(&invoice, invoice.ID)

	return c.JSON(fiber.Map{
		"message": "Service invoice updated successfully",
		"data":    invoice,
	})
}

// DeleteServiceInvoice deletes a service invoice
// DELETE /api/service-invoices/:id
func DeleteServiceInvoice(c *fiber.Ctx) error {
	id := c.Params("id")
	invoiceID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid ID",
			"message": "ID must be a valid number",
		})
	}

	// Check if invoice exists
	var invoice Models.ServiceInvoice
	if err := Models.DB.First(&invoice, uint(invoiceID)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   "Service invoice not found",
				"message": "The specified service invoice does not exist",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Database error",
			"message": err.Error(),
		})
	}

	// Delete the service invoice (inspection items will be deleted due to CASCADE)
	if err := Models.DB.Delete(&invoice).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to delete service invoice",
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Service invoice deleted successfully",
	})
}


// ============================================================================
// REQUEST/RESPONSE TYPES
// ============================================================================

type SearchServiceRequest struct {
	Query       string `json:"query" query:"query"`
	PlateNumber string `json:"plate_number" query:"plate_number"`
	CarID       uint   `json:"car_id" query:"car_id"`
	FromDate    string `json:"from_date" query:"from_date"`
	ToDate      string `json:"to_date" query:"to_date"`
	Limit       int    `json:"limit" query:"limit"`
	Page        int    `json:"page" query:"page"`
}


type InspectionItemResponse struct {
	ID        uint    `json:"id"`
	Service   string  `json:"service"`
	Notes     string  `json:"notes,omitempty"`
	ItemOrder int     `json:"item_order"`
	Matched   bool    `json:"matched"`
	Reason    *string `json:"reason,omitempty"`
	Score     *int    `json:"score,omitempty"`
	Distance  float64 `json:"distance,omitempty"`
	MatchType string  `json:"match_type,omitempty"` // "semantic", "keyword", or "both"
}

type InvoiceResponse struct {
	ID              uint                     `json:"id"`
	Date            time.Time                `json:"date"`
	MeterReading    int64                    `json:"meter_reading"`
	PlateNumber     string                   `json:"plate_number"`
	DriverName      string                   `json:"driver_name"`
	Supervisor      string                   `json:"supervisor,omitempty"`
	OperatingRegion string                   `json:"operating_region,omitempty"`
	Car             *CarSummary              `json:"car,omitempty"`
	InspectionItems []InspectionItemResponse `json:"inspection_items"`
	MatchCount      int                      `json:"match_count"`
}

type CarSummary struct {
	ID           uint   `json:"id"`
	PlateNumber  string `json:"plate_number"`
	CarType      string `json:"car_type"`
	Transporter  string `json:"transporter"`
	TankCapacity int    `json:"tank_capacity,omitempty"`
}

type SearchResponse struct {
	Success       bool              `json:"success"`
	Query         string            `json:"query"`
	TotalMatches  int               `json:"total_matches"`
	InvoiceCount  int               `json:"invoice_count"`
	Results       []InvoiceResponse `json:"results"`
	SearchTime    string            `json:"search_time"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}


// ============================================================================
// MAIN HANDLER
// ============================================================================

func SearchServiceInvoices(c *fiber.Ctx) error {
	startTime := time.Now()

	var req SearchServiceRequest
	if err := c.QueryParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{Success: false, Error: "Invalid params"})
	}

	if req.Query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{Success: false, Error: "Query required"})
	}

	limitProvided := c.Query("limit") != ""
	if limitProvided && req.Limit <= 0 {
		req.Limit = 10 // default if limit provided but invalid
	}

	// 1. Convert user query to vector
	queryVector, err := AbstractFunctions.GenerateEmbedding(req.Query)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Success: false, Error: "Embedding failed"})
	}
	pgVec := pgvector.NewVector(queryVector)

	// 2. Hybrid Search: Vector Distance + Keyword Match
	searchTerm := "%" + req.Query + "%"
	type MatchedItem struct {
		Models.InspectionItem
		Distance       float64 `gorm:"column:distance"`
		IsKeywordMatch bool    `gorm:"column:is_keyword_match"`
	}
	var matches []MatchedItem

	dbQuery := Models.DB.Table("inspection_items").
		Select(`
			inspection_items.*, 
			inspection_items.embedding <=> ? AS distance,
			(inspection_items.service ILIKE ? OR inspection_items.notes ILIKE ?) AS is_keyword_match
		`, pgVec, searchTerm, searchTerm).
		Joins("JOIN service_invoices ON service_invoices.id = inspection_items.service_invoice_id").
		Where("service_invoices.deleted_at IS NULL").
		Where(`
			(inspection_items.embedding <=> ? < 0.45) OR 
			(inspection_items.service ILIKE ? OR inspection_items.notes ILIKE ?)
		`, pgVec, searchTerm, searchTerm)

	if req.PlateNumber != "" {
		dbQuery = dbQuery.Where("service_invoices.plate_number = ?", req.PlateNumber)
	}
	if req.CarID > 0 {
		dbQuery = dbQuery.Where("service_invoices.car_id = ?", req.CarID)
	}

	// Order by distance (semantic) but prioritize exact keyword matches by boosting them
	// We fetch more than we need to allow for grouping and sorting in memory
	fetchLimit := 500
	if limitProvided && req.Limit > 0 {
		fetchLimit = (req.Page + 1) * req.Limit * 2
		if fetchLimit < 500 {
			fetchLimit = 500
		}
	}

	if err := dbQuery.Order("is_keyword_match DESC, distance ASC").Limit(fetchLimit).Find(&matches).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Success: false, Error: "DB search failed"})
	}

	if len(matches) == 0 {
		return c.JSON(SearchResponse{Success: true, Query: req.Query, TotalMatches: 0, Results: []InvoiceResponse{}})
	}

	// 3. Extract Invoice IDs & fetch full relationships
	var invoiceIDs []uint
	matchInfoMap := make(map[uint]struct {
		Distance       float64
		IsKeywordMatch bool
	})

	for _, m := range matches {
		matchInfoMap[m.ID] = struct {
			Distance       float64
			IsKeywordMatch bool
		}{m.Distance, m.IsKeywordMatch}

		exists := false
		for _, id := range invoiceIDs {
			if id == m.ServiceInvoiceID {
				exists = true
				break
			}
		}
		if !exists {
			invoiceIDs = append(invoiceIDs, m.ServiceInvoiceID)
		}
	}

	var invoices []Models.ServiceInvoice
	Models.DB.Where("id IN ?", invoiceIDs).
		Preload("InspectionItems", func(db *gorm.DB) *gorm.DB { return db.Order("item_order ASC") }).
		Preload("Car").
		Order("date DESC").
		Find(&invoices)

	// 4. Build Final Response
	var results []InvoiceResponse
	totalMatches := 0

	for _, inv := range invoices {
		items := make([]InspectionItemResponse, len(inv.InspectionItems))
		matchCount := 0

		for j, item := range inv.InspectionItems {
			items[j] = InspectionItemResponse{
				ID:        item.ID,
				Service:   item.Service,
				Notes:     item.Notes,
				ItemOrder: item.ItemOrder,
				Matched:   false,
			}

			if info, exists := matchInfoMap[item.ID]; exists {
				items[j].Matched = true
				items[j].Distance = info.Distance

				// Determine Match Type
				isSemantic := info.Distance < 0.45
				if isSemantic && info.IsKeywordMatch {
					items[j].MatchType = "both"
				} else if info.IsKeywordMatch {
					items[j].MatchType = "keyword"
				} else {
					items[j].MatchType = "semantic"
				}

				matchCount++
				totalMatches++
			}
		}

		var carSummary *CarSummary
		if inv.Car.ID != 0 {
			carSummary = &CarSummary{
				ID:           inv.Car.ID,
				PlateNumber:  inv.Car.CarNoPlate,
				CarType:      inv.Car.CarType,
				Transporter:  inv.Car.Transporter,
				TankCapacity: inv.Car.TankCapacity,
			}
		}

		results = append(results, InvoiceResponse{
			ID:              inv.ID,
			Date:            inv.Date,
			MeterReading:    inv.MeterReading,
			PlateNumber:     inv.PlateNumber,
			DriverName:      inv.DriverName,
			Supervisor:      inv.Supervisor,
			OperatingRegion: inv.OperatingRegion,
			Car:             carSummary,
			InspectionItems: items,
			MatchCount:      matchCount,
		})
	}

	sortByMatchCount(results)

	totalResults := len(results)
	if limitProvided {
		page := req.Page
		if page < 1 {
			page = 1
		}
		start := (page - 1) * req.Limit
		end := start + req.Limit

		if start > totalResults {
			results = []InvoiceResponse{}
		} else {
			if end > totalResults {
				end = totalResults
			}
			results = results[start:end]
		}

		return c.JSON(fiber.Map{
			"success":       true,
			"query":         req.Query,
			"total_matches": totalMatches,
			"invoice_count": totalResults,
			"results":       results,
			"search_time":   time.Since(startTime).String(),
			"pagination": fiber.Map{
				"page":       page,
				"limit":      req.Limit,
				"total":      totalResults,
				"totalPages": (totalResults + req.Limit - 1) / req.Limit,
			},
		})
	}

	return c.JSON(SearchResponse{
		Success:      true,
		Query:        req.Query,
		TotalMatches: totalMatches,
		InvoiceCount: totalResults,
		Results:      results,
		SearchTime:   time.Since(startTime).String(),
	})
}



// ============================================================================
// UTILITIES
// ============================================================================


func sortByMatchCount(results []InvoiceResponse) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].MatchCount > results[i].MatchCount {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}


func generateEmbeddingsForItems(items []Models.InspectionItemRequest) ([][]float32, error) {
	var texts []string
	for _, item := range items {
		if item.Service != "" || item.Notes != "" {
			combined := item.Service + " " + item.Notes
			texts = append(texts, combined)
		}
	}

	if len(texts) == 0 {
		return nil, nil
	}

	// Use the Batch function we created in AbstractFunctions/Services
	return AbstractFunctions.GenerateEmbeddingsBatch(texts)
}