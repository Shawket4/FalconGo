package Controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"Falcon/Models" // Adjust the import path to match your project

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

// CreateServiceInvoice creates a new service invoice
// POST /api/service-invoices
func CreateServiceInvoice(c *fiber.Ctx) error {
	var req Models.ServiceInvoiceRequest

	// Parse request body
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid request body",
			"message": err.Error(),
		})
	}

	// Validate required fields
	if req.CarID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Validation failed",
			"message": "car_id is required",
		})
	}

	if req.DriverName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Validation failed",
			"message": "driver_name is required",
		})
	}

	// Parse date
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid date format",
			"message": "Date must be in YYYY-MM-DD format",
		})
	}

	// Check if car exists
	var car Models.Car
	if err := Models.DB.First(&car, req.CarID).Error; err != nil {
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

	// Create service invoice
	invoice := &Models.ServiceInvoice{
		CarID:           req.CarID,
		DriverName:      req.DriverName,
		Date:            date,
		MeterReading:    req.MeterReading,
		PlateNumber:     req.PlateNumber,
		Supervisor:      req.Supervisor,
		OperatingRegion: req.OperatingRegion,
	}

	// Begin transaction
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Transaction error",
			"message": tx.Error.Error(),
		})
	}

	// Create the service invoice
	if err := tx.Create(invoice).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to create service invoice",
			"message": err.Error(),
		})
	}

	// Create inspection items
	for i, item := range req.InspectionItems {
		if item.Service != "" || item.Notes != "" {
			inspectionItem := Models.InspectionItem{
				ServiceInvoiceID: invoice.ID,
				Service:          item.Service,
				Notes:            item.Notes,
				ItemOrder:        i + 1,
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

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to commit transaction",
			"message": err.Error(),
		})
	}

	// Reload with relationships
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

	// Parse date
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid date format",
			"message": "Date must be in YYYY-MM-DD format",
		})
	}

	// Begin transaction
	tx := Models.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Transaction error",
			"message": tx.Error.Error(),
		})
	}

	// Update service invoice
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

	// Delete existing inspection items
	if err := tx.Where("service_invoice_id = ?", invoice.ID).Delete(&Models.InspectionItem{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to delete existing inspection items",
			"message": err.Error(),
		})
	}

	// Create new inspection items
	for i, item := range req.InspectionItems {
		if item.Service != "" || item.Notes != "" {
			inspectionItem := Models.InspectionItem{
				ServiceInvoiceID: invoice.ID,
				Service:          item.Service,
				Notes:            item.Notes,
				ItemOrder:        i + 1,
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

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to commit transaction",
			"message": err.Error(),
		})
	}

	// Reload with relationships
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
// CONFIGURATION
// ============================================================================

const (
	GroqAPIURL       = "https://api.groq.com/openai/v1/chat/completions"
	GroqModel        = "llama-3.3-70b-versatile" // Updated to valid Groq model
	MaxBatchSize     = 40
	MaxRetries       = 3
	RetryBaseDelay   = 500 * time.Millisecond
	RequestTimeout   = 45 * time.Second
	BatchDelay       = 600 * time.Millisecond
	MaxQueryLength   = 200
	MaxServiceLength = 120
	MaxNotesLength   = 180
	CacheTTL         = 10 * time.Minute
	MaxTokens        = 1500
)

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
}

type InspectionItemRef struct {
	ID      uint   `json:"id"`
	Service string `json:"service"`
	Notes   string `json:"notes,omitempty"`
}

type LLMMatchedItem struct {
	ID     uint   `json:"id"`
	Reason string `json:"reason"`
	Score  int    `json:"score,omitempty"` // 1-3: 1=direct, 2=related, 3=indirect
}

type LLMSearchResponse struct {
	Matches []LLMMatchedItem `json:"matches"`
}

type InspectionItemResponse struct {
	ID        uint    `json:"id"`
	Service   string  `json:"service"`
	Notes     string  `json:"notes,omitempty"`
	ItemOrder int     `json:"item_order"`
	Matched   bool    `json:"matched"`
	Reason    *string `json:"reason,omitempty"`
	Score     *int    `json:"score,omitempty"` // 1=direct match, 2=related, 3=indirectly related
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
	NormalizedQry string            `json:"normalized_query,omitempty"`
	TotalMatches  int               `json:"total_matches"`
	InvoiceCount  int               `json:"invoice_count"`
	Results       []InvoiceResponse `json:"results"`
	SearchTime    string            `json:"search_time"`
	FromCache     bool              `json:"from_cache,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}

// ============================================================================
// SIMPLE CACHE
// ============================================================================

type CacheEntry struct {
	Matches   []LLMMatchedItem
	ExpiresAt time.Time
}

type SearchCache struct {
	mu    sync.RWMutex
	items map[string]CacheEntry
}

var cache = &SearchCache{items: make(map[string]CacheEntry)}

func (c *SearchCache) Get(key string) ([]LLMMatchedItem, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.items[key]
	if !exists || time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	return entry.Matches, true
}

func (c *SearchCache) Set(key string, matches []LLMMatchedItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = CacheEntry{
		Matches:   matches,
		ExpiresAt: time.Now().Add(CacheTTL),
	}
}

func (c *SearchCache) GenerateKey(query string, itemIDs []uint) string {
	h := sha256.New()
	h.Write([]byte(query))
	for _, id := range itemIDs {
		h.Write([]byte(fmt.Sprintf("-%d", id)))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ============================================================================
// QUERY NORMALIZATION & VALIDATION
// ============================================================================

var arabicNormalizer = strings.NewReplacer(
	"أ", "ا", "إ", "ا", "آ", "ا", "ٱ", "ا",
	"ة", "ه", "ى", "ي", "ؤ", "و", "ئ", "ي",
)

var commonAliases = map[string][]string{
	"فرامل":    {"برايك", "بريك", "brake", "brakes", "فرملة"},
	"كمبروسر":  {"compressor", "ضاغط", "كباس", "كمبريسور"},
	"موتور":    {"محرك", "ماكينة", "engine", "انجن"},
	"فتيس":     {"جيربوكس", "gearbox", "قير", "ناقل"},
	"كاوتش":    {"اطار", "عجل", "tire", "tyre", "اطارات"},
	"سوست":     {"ياي", "spring", "نابض", "زمبرك"},
	"دريكسيون": {"مقود", "steering", "توجيه", "ستيرنج"},
	"رادياتير": {"radiator", "مبرد", "تبريد"},
	"دينامو":   {"alternator", "مولد", "شحن"},
	"بطارية":   {"battery", "بطاريات", "اكوم"},
	"زيت":      {"oil", "اويل", "تزييت", "شحم"},
}

func normalizeQuery(query string) string {
	q := strings.TrimSpace(query)
	q = arabicNormalizer.Replace(q)
	q = strings.ToLower(q)
	// Remove extra spaces
	space := regexp.MustCompile(`\s+`)
	q = space.ReplaceAllString(q, " ")
	return q
}

func expandQuery(query string) []string {
	normalized := normalizeQuery(query)
	terms := []string{normalized}

	for key, aliases := range commonAliases {
		if strings.Contains(normalized, normalizeQuery(key)) {
			terms = append(terms, aliases...)
		}
		for _, alias := range aliases {
			if strings.Contains(normalized, normalizeQuery(alias)) {
				terms = append(terms, key)
				terms = append(terms, aliases...)
				break
			}
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	unique := []string{}
	for _, t := range terms {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}
	return unique
}

func validateRequest(req *SearchServiceRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("EMPTY_QUERY: Search query is required")
	}
	if utf8.RuneCountInString(req.Query) > MaxQueryLength {
		return fmt.Errorf("QUERY_TOO_LONG: Query exceeds %d characters", MaxQueryLength)
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > 200 {
		req.Limit = 200
	}
	return nil
}

// ============================================================================
// MAIN HANDLER
// ============================================================================

func SearchServiceInvoices(c *fiber.Ctx) error {
	startTime := time.Now()
	log.Printf("[Search] === New search request ===")

	var req SearchServiceRequest
	if err := c.QueryParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
			Success: false, Error: "Invalid query parameters", Code: "INVALID_PARAMS",
		})
	}

	log.Printf("[Search] Query: '%s', PlateNumber: '%s'", req.Query, req.PlateNumber)

	if err := validateRequest(&req); err != nil {
		code := strings.Split(err.Error(), ":")[0]
		return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
			Success: false, Error: err.Error(), Code: code,
		})
	}

	normalizedQuery := normalizeQuery(req.Query)
	expandedTerms := expandQuery(req.Query)
	log.Printf("[Search] Normalized: '%s', Expanded: %v", normalizedQuery, expandedTerms)

	// Build query
	query := Models.DB.Model(&Models.InspectionItem{}).
		Joins("JOIN service_invoices ON service_invoices.id = inspection_items.service_invoice_id").
		Where("service_invoices.deleted_at IS NULL")

	if req.PlateNumber != "" {
		query = query.Where("service_invoices.plate_number = ?", req.PlateNumber)
	}
	if req.CarID > 0 {
		query = query.Where("service_invoices.car_id = ?", req.CarID)
	}
	if req.FromDate != "" {
		query = query.Where("service_invoices.date >= ?", req.FromDate)
	}
	if req.ToDate != "" {
		query = query.Where("service_invoices.date <= ?", req.ToDate)
	}

	log.Printf("[Search] Fetching inspection items from DB...")
	var items []Models.InspectionItem
	if err := query.Limit(1000).Find(&items).Error; err != nil {
		log.Printf("[Search] DB error: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
			Success: false, Error: "Database query failed", Code: "DB_ERROR",
		})
	}
	log.Printf("[Search] Found %d inspection items", len(items))

	if len(items) == 0 {
		return c.JSON(SearchResponse{
			Success:      true,
			Query:        req.Query,
			TotalMatches: 0,
			InvoiceCount: 0,
			Results:      []InvoiceResponse{},
			SearchTime:   time.Since(startTime).String(),
		})
	}

	// Build refs for LLM
	itemRefs := make([]InspectionItemRef, len(items))
	itemIDs := make([]uint, len(items))
	for i, item := range items {
		service, notes := truncateText(item.Service, MaxServiceLength), truncateText(item.Notes, MaxNotesLength)
		itemRefs[i] = InspectionItemRef{ID: item.ID, Service: service, Notes: notes}
		itemIDs[i] = item.ID
	}

	// Check cache
	cacheKey := cache.GenerateKey(normalizedQuery, itemIDs)
	matches, fromCache := cache.Get(cacheKey)
	log.Printf("[Search] Cache key: %s, hit: %v", cacheKey, fromCache)

	if !fromCache {
		log.Printf("[Search] Calling LLM...")
		var err error
		matches, err = queryGroqWithRetry(c.Context(), normalizedQuery, expandedTerms, itemRefs)
		if err != nil {
			log.Printf("[Search] LLM failed: %v, trying fallback", err)
			// Fallback to basic text matching
			matches = fallbackTextMatch(normalizedQuery, expandedTerms, itemRefs)
			if len(matches) == 0 {
				return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
					Success: false, Error: "Search service unavailable: " + err.Error(), Code: "LLM_ERROR",
				})
			}
		}
		cache.Set(cacheKey, matches)
	}

	log.Printf("[Search] Got %d matches", len(matches))

	if len(matches) == 0 {
		return c.JSON(SearchResponse{
			Success:       true,
			Query:         req.Query,
			NormalizedQry: normalizedQuery,
			TotalMatches:  0,
			InvoiceCount:  0,
			Results:       []InvoiceResponse{},
			SearchTime:    time.Since(startTime).String(),
			FromCache:     fromCache,
		})
	}

	// Build lookup maps
	reasonMap := make(map[uint]string)
	scoreMap := make(map[uint]int)
	matchedIDs := make([]uint, len(matches))
	for i, m := range matches {
		matchedIDs[i] = m.ID
		reasonMap[m.ID] = m.Reason
		if m.Score > 0 {
			scoreMap[m.ID] = m.Score
		} else {
			scoreMap[m.ID] = 2 // Default to "related"
		}
	}

	// Get invoice IDs
	var invoiceIDs []uint
	Models.DB.Model(&Models.InspectionItem{}).
		Where("id IN ?", matchedIDs).
		Distinct("service_invoice_id").
		Pluck("service_invoice_id", &invoiceIDs)

	// Fetch full invoices
	var invoices []Models.ServiceInvoice
	err := Models.DB.Where("id IN ?", invoiceIDs).
		Preload("InspectionItems", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Car").
		Order("date DESC").
		Limit(req.Limit).
		Find(&invoices).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
			Success: false, Error: "Failed to fetch invoices", Code: "DB_ERROR",
		})
	}

	// Build response
	results := make([]InvoiceResponse, len(invoices))
	totalMatches := 0

	for i, inv := range invoices {
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
			if reason, ok := reasonMap[item.ID]; ok {
				items[j].Matched = true
				items[j].Reason = &reason
				score := scoreMap[item.ID]
				items[j].Score = &score
				matchCount++
				totalMatches++
			}
		}

		results[i] = InvoiceResponse{
			ID:              inv.ID,
			Date:            inv.Date,
			MeterReading:    inv.MeterReading,
			PlateNumber:     inv.PlateNumber,
			DriverName:      inv.DriverName,
			Supervisor:      inv.Supervisor,
			OperatingRegion: inv.OperatingRegion,
			InspectionItems: items,
			MatchCount:      matchCount,
		}

		if inv.Car.ID != 0 {
			results[i].Car = &CarSummary{
				ID:           inv.Car.ID,
				PlateNumber:  inv.Car.CarNoPlate,
				CarType:      inv.Car.CarType,
				Transporter:  inv.Car.Transporter,
				TankCapacity: inv.Car.TankCapacity,
			}
		}
	}

	// Sort by match count descending
	sortByMatchCount(results)

	return c.JSON(SearchResponse{
		Success:       true,
		Query:         req.Query,
		NormalizedQry: normalizedQuery,
		TotalMatches:  totalMatches,
		InvoiceCount:  len(results),
		Results:       results,
		SearchTime:    time.Since(startTime).String(),
		FromCache:     fromCache,
	})
}

// ============================================================================
// LLM QUERY WITH RETRY
// ============================================================================

func queryGroqWithRetry(ctx context.Context, query string, expandedTerms []string, items []InspectionItemRef) ([]LLMMatchedItem, error) {
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("ERROR: Failed to load .env file: %v", err)
	}
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY not set")
	}

	log.Printf("[Search] Starting LLM query for '%s' with %d items", query, len(items))

	var allMatches []LLMMatchedItem
	seenIDs := make(map[uint]bool)

	totalBatches := (len(items) + MaxBatchSize - 1) / MaxBatchSize

	for start := 0; start < len(items); start += MaxBatchSize {
		end := min(start+MaxBatchSize, len(items))
		batch := items[start:end]
		batchNum := (start / MaxBatchSize) + 1

		log.Printf("[Search] Processing batch %d/%d (%d items)", batchNum, totalBatches, len(batch))

		var matches []LLMMatchedItem
		var err error

		for attempt := 0; attempt < MaxRetries; attempt++ {
			log.Printf("[Search] Batch %d, attempt %d/%d", batchNum, attempt+1, MaxRetries)

			matches, err = queryGroqBatch(ctx, apiKey, query, expandedTerms, batch)
			if err == nil {
				log.Printf("[Search] Batch %d succeeded with %d matches", batchNum, len(matches))
				break
			}

			log.Printf("[Search] Batch %d attempt %d failed: %v", batchNum, attempt+1, err)

			if strings.Contains(err.Error(), "rate_limit") || strings.Contains(err.Error(), "429") {
				delay := RetryBaseDelay * time.Duration(attempt+1) * 2
				log.Printf("[Search] Rate limited, waiting %v", delay)
				time.Sleep(delay)
				continue
			}
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "context") {
				delay := RetryBaseDelay * time.Duration(attempt+1)
				log.Printf("[Search] Timeout, waiting %v", delay)
				time.Sleep(delay)
				continue
			}
			break // Non-retryable error
		}

		if err != nil {
			log.Printf("[Search] Batch %d failed after all retries: %v", batchNum, err)
			if len(allMatches) > 0 {
				return allMatches, nil // Return partial results
			}
			return nil, err
		}

		for _, m := range matches {
			if !seenIDs[m.ID] {
				seenIDs[m.ID] = true
				allMatches = append(allMatches, m)
			}
		}

		if end < len(items) {
			time.Sleep(BatchDelay)
		}
	}

	log.Printf("[Search] Completed with %d total matches", len(allMatches))
	return allMatches, nil
}

func queryGroqBatch(ctx context.Context, apiKey string, query string, expandedTerms []string, items []InspectionItemRef) ([]LLMMatchedItem, error) {
	itemsJSON, _ := json.Marshal(items)
	termsStr := strings.Join(expandedTerms, ", ")

	prompt := fmt.Sprintf(`Search: "%s"
Related terms: [%s]

Items:
%s

Find ALL matching items. Return JSON: {"matches": [{"id": N, "reason": "brief", "score": 1-3}]}
Score: 1=direct match, 2=related component, 3=indirectly related`, query, termsStr, string(itemsJSON))

	reqBody := map[string]interface{}{
		"model": GroqModel,
		"messages": []map[string]string{
			{"role": "system", "content": truckMechanicSystemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.1,
		"max_tokens":  MaxTokens,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[Groq] Sending request (%d bytes)", len(jsonBody))

	// Create a NEW context with timeout, don't inherit from fiber
	reqCtx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", GroqAPIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: RequestTimeout}

	log.Printf("[Groq] Executing HTTP request...")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Groq] HTTP request failed: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[Groq] Response status: %d", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[Groq] Response body (%d bytes): %s", len(body), truncateText(string(body), 500))

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate_limit exceeded")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, truncateText(string(body), 200))
	}

	var groqResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &groqResp); err != nil {
		return nil, fmt.Errorf("response parse failed: %w", err)
	}

	if groqResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", groqResp.Error.Message)
	}

	if len(groqResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response - no choices")
	}

	content := extractJSON(groqResp.Choices[0].Message.Content)
	log.Printf("[Groq] Extracted JSON: %s", truncateText(content, 300))

	var result LLMSearchResponse
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("JSON parse failed: %w, content: %s", err, truncateText(content, 100))
	}

	return result.Matches, nil
}

// ============================================================================
// FALLBACK TEXT MATCHING
// ============================================================================

func fallbackTextMatch(query string, expandedTerms []string, items []InspectionItemRef) []LLMMatchedItem {
	var matches []LLMMatchedItem

	for _, item := range items {
		normalizedService := normalizeQuery(item.Service)
		normalizedNotes := normalizeQuery(item.Notes)
		combined := normalizedService + " " + normalizedNotes

		for _, term := range expandedTerms {
			if strings.Contains(combined, normalizeQuery(term)) {
				matches = append(matches, LLMMatchedItem{
					ID:     item.ID,
					Reason: "Text match: " + term,
					Score:  1,
				})
				break
			}
		}
	}

	return matches
}

// ============================================================================
// UTILITIES
// ============================================================================

func truncateText(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}

func extractJSON(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start != -1 && end > start {
		return content[start : end+1]
	}
	return content
}

func sortByMatchCount(results []InvoiceResponse) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].MatchCount > results[i].MatchCount {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// SYSTEM PROMPT
// ============================================================================

const truckMechanicSystemPrompt = `Expert truck mechanic for petroleum tankers. Find service records related to the search query.

SYSTEM RELATIONSHIPS:

BRAKES(فرامل،برايك،تيل،طنبورة): chambers(جهاز فرامل،شامبر), S-cams(كامات،رجلاش), drums(طنبورة،هوبات), springs(سوستة فرامل), ABS, lines(خراطيم فرامل). Connected to AIR system.

AIR(هواء،كمبروسر،ضاغط): compressor(كمبروسر،كباس), dryer(درير،مجفف), tanks(خزان هواء), valves(بلف هواء،صمام هواء), bags(باجات،وسائد،ممبرين), fittings(كبسة هواء،خراطيم هواء), air filter(فلتر هواء). Powers BRAKES.

ENGINE(موتور،محرك،ماكينة): turbo(تربو،شاحن), fuel(بخاخات،طرمبة سولار،فلتر سولار،انجكترات), oil(زيت موتور،فلتر زيت), cooling(رادياتير،مروحة،ترموستات), belts(سير،سيور), intake(فلتر هواء), exhaust(شكمان)

OIL/LUBRICATION(زيت،تشحيم،شحم): engine oil(زيت موتور،زيت ماتور), oil filter(فلتر زيت), greasing(تشحيم،شحم), gearbox oil(زيت فتيس), differential oil(زيت كرونة), power steering(زيت باور)

TRANSMISSION(فتيس،جيربوكس): clutch(دبرياج), gearbox(تروس), driveshaft(كردان،صليبة), differential(كرونة), axles(محاور،اكسل،دبل)

SUSPENSION(سوست،تعليق): springs(سوست،ريشور), air bags(باجات هواء), shocks(مساعدين), bushings(جلب،جلبة)

STEERING(دريكسيون،مقود): power steering(باور،طرمبة باور), tie rods(تيروات،مقصات)

WHEELS(كاوتش،عجل): tires(كاوتش،اطار), rims(جنط), bearings(رولمان،بلي)

TRAILER(مقطورة): fifth wheel(صينية،طبق), landing gear(ارجل), tanker valves(بلف تحميل،بلف تفريغ،محبس)

SCORING:
1 = DIRECT: Service text contains the search term or its direct synonym
2 = RELATED: Same system component (زيت→تشحيم، فرامل→كامات)  
3 = CONNECTED: Different system that mechanically depends on search term (فرامل→كمبروسر)

STRICT RULES:
- Score 3 should be RARE - only when there's a real mechanical dependency
- Do NOT match unrelated systems
- Do NOT force connections that don't exist
- When in doubt, DON'T match

EXAMPLES:
- Query "زيت": Match زيت موتور(1), فلتر زيت(1), تشحيم(2). Do NOT match هواء, فرامل, كاوتش
- Query "فرامل": Match تيل(1), كامات(2), رجلاش(2), كمبروسر(3). Do NOT match زيت, كاوتش
- Query "هواء": Match فلتر هواء(1), كمبروسر(1), درير(2), باجات(2). Do NOT match زيت, كاوتش

REASON: Write brief Arabic explanation (max 10 words). Do NOT copy service text.

Return ONLY: {"matches":[{"id":N,"reason":"brief","score":1-3}]}
Empty array [] if nothing matches.`
