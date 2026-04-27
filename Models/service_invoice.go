package Models

import (
	"time"

	"gorm.io/gorm"
)

type ServiceInvoice struct {
	gorm.Model
	CarID           uint      `json:"car_id" gorm:"not null;index"`
	DriverName      string    `json:"driver_name" gorm:"size:255;not null"`
	Date            time.Time `json:"date" gorm:"not null"`
	MeterReading    int64     `json:"meter_reading" gorm:"not null"`
	PlateNumber     string    `json:"plate_number" gorm:"size:50;not null;index"`
	Supervisor      string    `json:"supervisor" gorm:"size:255;not null"`
	OperatingRegion string    `json:"operating_region" gorm:"size:255;not null"`

	// Relationships
	Car             Car              `json:"car,omitempty" gorm:"foreignKey:CarID"`
	InspectionItems []InspectionItem `json:"inspection_items,omitempty" gorm:"foreignKey:ServiceInvoiceID;constraint:OnDelete:CASCADE"`
}

// InspectionItem represents individual inspection line items
type InspectionItem struct {
	gorm.Model
	ServiceInvoiceID uint   `json:"service_invoice_id" gorm:"not null;index"`
	Service          string `json:"service" gorm:"size:500"`
	Notes            string `json:"notes" gorm:"type:text"`
	ItemOrder        int    `json:"item_order" gorm:"not null;default:0"`

	// Relationship
	ServiceInvoice ServiceInvoice `json:"service_invoice,omitempty" gorm:"foreignKey:ServiceInvoiceID"`
}

type ServiceInvoiceRequest struct {
	CarID           uint                    `json:"car_id" binding:"required"`
	DriverName      string                  `json:"driver_name" binding:"required"`
	Date            string                  `json:"date" binding:"required"`
	MeterReading    int64                   `json:"meter_reading" binding:"required"`
	PlateNumber     string                  `json:"plate_number" binding:"required"`
	Supervisor      string                  `json:"supervisor" binding:"required"`
	OperatingRegion string                  `json:"operating_region" binding:"required"`
	InspectionItems []InspectionItemRequest `json:"inspection_items"`
}

type InspectionItemRequest struct {
	Service string `json:"service"`
	Notes   string `json:"notes"`
}
