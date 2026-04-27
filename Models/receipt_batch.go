package Models

import (
	"time"

	"gorm.io/gorm"
)

type ReceiptBatch struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	DriverID         uint           `gorm:"not null;index" json:"driver_id"`
	Driver           Driver         `gorm:"foreignKey:DriverID" json:"driver,omitempty"`
	Status           string         `gorm:"type:varchar(20);default:'pending'" json:"status"` // pending, assigned, rejected
	ScannedAt        time.Time      `gorm:"autoCreateTime" json:"scanned_at"`
	AssignedToTripID *uint          `gorm:"index" json:"assigned_to_trip_id"`
	Receipts         []Receipt      `gorm:"foreignKey:BatchID" json:"receipts"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

type Receipt struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	BatchID   uint           `gorm:"not null;index" json:"batch_id"`
	ImagePath string         `gorm:"type:varchar(500);not null" json:"image_path"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
