package Controllers

import (
	"strconv"
	"time"

	"Falcon/Models" // Adjust the import path to match your project

	"github.com/gofiber/fiber/v2"
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
