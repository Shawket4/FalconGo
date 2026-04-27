package Models

import "gorm.io/gorm"

type Tablet struct {
	gorm.Model
	HWID         string `gorm:"column:hwid;uniqueIndex;not null" json:"hwid"`
	DeviceName   string `json:"device_name"`
	DeviceType   string `json:"device_type"` // "android" or "ios"
	CurrentCarID *uint  `json:"current_car_id" gorm:"index"`
	CurrentCar   *Car   `json:"current_car,omitempty" gorm:"foreignKey:CurrentCarID"`
	Status       string `json:"status" gorm:"default:'unassigned'"` // unassigned, assigned, inactive

	// Relationships
	Sessions []DriverSession `json:"sessions,omitempty" gorm:"foreignKey:TabletID"`
}

// TableName specifies the table name for Tablet model
func (Tablet) TableName() string {
	return "tablets"
}
