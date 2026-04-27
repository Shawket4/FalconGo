package utils

import (
	"Falcon/Models"
	"errors"
	"fmt"
	"log"

	"gorm.io/gorm"
)

// GetTabletByHWID retrieves a tablet by its hardware ID
func GetTabletByHWID(db *gorm.DB, hwid string) (*Models.Tablet, error) {
	var tablet Models.Tablet

	err := db.Where("hwid = ?", hwid).
		Preload("CurrentCar").
		First(&tablet).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("tablet not found")
		}
		return nil, err
	}

	return &tablet, nil
}

// RegisterTablet creates a new tablet record if it doesn't exist
func RegisterTablet(db *gorm.DB, hwid string, deviceName string, deviceType string) (*Models.Tablet, error) {
	var tablet Models.Tablet

	// Check if tablet exists (including soft-deleted ones)
	err := db.Unscoped().Where("hwid = ?", hwid).First(&tablet).Error

	if err == gorm.ErrRecordNotFound {
		// Tablet doesn't exist - create new one
		log.Printf("📱 Creating new tablet with HWID: %s", hwid)

		tablet = Models.Tablet{
			HWID:       hwid,
			DeviceName: deviceName,
			DeviceType: deviceType,
			Status:     "unassigned",
		}

		if err := db.Create(&tablet).Error; err != nil {
			log.Printf("❌ Failed to create tablet: %v", err)
			return nil, err
		}

		log.Printf("✅ Tablet created successfully - ID: %d, HWID: %s", tablet.ID, tablet.HWID)
		return &tablet, nil
	}

	if err != nil {
		// Some other database error
		return nil, err
	}

	// Tablet exists - check if it's soft-deleted
	if tablet.DeletedAt.Valid {
		// Tablet was soft-deleted - restore it

		// Update tablet info and clear deleted_at
		tablet.DeviceName = deviceName
		tablet.DeviceType = deviceType
		tablet.Status = "unassigned"
		tablet.DeletedAt = gorm.DeletedAt{} // Clear soft delete

		// Use Unscoped() to update soft-deleted record
		if err := db.Unscoped().Save(&tablet).Error; err != nil {
			return nil, err
		}

		return &tablet, nil
	}

	// Optional: Update device info in case it changed
	tablet.DeviceName = deviceName
	tablet.DeviceType = deviceType

	if err := db.Save(&tablet).Error; err != nil {
		log.Printf("❌ Failed to update tablet: %v", err)
		return nil, err
	}

	return &tablet, nil
}

// AssignTabletToCar assigns a tablet to a specific car
func AssignTabletToCar(db *gorm.DB, tabletID uint, carID uint) error {
	// Verify car exists
	var car Models.Car
	if err := db.First(&car, carID).Error; err != nil {
		return errors.New("car not found")
	}

	// Check if car already has a tablet assigned
	if car.TabletID != nil && *car.TabletID != tabletID {
		// Unassign the old tablet from this car
		oldTablet := Models.Tablet{}
		db.First(&oldTablet, *car.TabletID)
		oldTablet.CurrentCarID = nil
		oldTablet.Status = "unassigned"
		db.Save(&oldTablet)
	}

	// Check if tablet is already assigned to a different car
	var tablet Models.Tablet
	if err := db.First(&tablet, tabletID).Error; err != nil {
		return errors.New("tablet not found")
	}

	if tablet.CurrentCarID != nil && *tablet.CurrentCarID != carID {
		// Tablet is assigned to a different car - need to handle active sessions

		// Check for active sessions on this tablet
		var activeSessionCount int64
		db.Model(&Models.DriverSession{}).
			Where("tablet_id = ? AND end_time IS NULL", tabletID).
			Count(&activeSessionCount)

		if activeSessionCount > 0 {
			return errors.New("cannot reassign tablet with active sessions - end sessions first")
		}

		// Unassign tablet from old car
		oldCar := Models.Car{}
		db.First(&oldCar, *tablet.CurrentCarID)
		oldCar.TabletID = nil
		db.Save(&oldCar)
	}

	// Assign tablet to new car
	tablet.CurrentCarID = &carID
	tablet.Status = "assigned"
	if err := db.Save(&tablet).Error; err != nil {
		return err
	}

	// Update car's tablet reference
	car.TabletID = &tabletID
	if err := db.Save(&car).Error; err != nil {
		return err
	}

	return nil
}

// UnassignTabletFromCar removes a tablet's car assignment
func UnassignTabletFromCar(db *gorm.DB, tabletID uint) error {
	var tablet Models.Tablet
	if err := db.First(&tablet, tabletID).Error; err != nil {
		return errors.New("tablet not found")
	}

	// Check for active sessions
	var activeSessionCount int64
	db.Model(&Models.DriverSession{}).
		Where("tablet_id = ? AND end_time IS NULL", tabletID).
		Count(&activeSessionCount)

	if activeSessionCount > 0 {
		return errors.New("cannot unassign tablet with active sessions")
	}

	// Remove car's reference to this tablet
	if tablet.CurrentCarID != nil {
		var car Models.Car
		db.First(&car, *tablet.CurrentCarID)
		car.TabletID = nil
		db.Save(&car)
	}

	// Unassign tablet
	tablet.CurrentCarID = nil
	tablet.Status = "unassigned"

	if err := db.Save(&tablet).Error; err != nil {
		return err
	}

	return nil
}

// GetUnassignedTablets retrieves all tablets that are not assigned to any car
func GetUnassignedTablets(db *gorm.DB) ([]Models.Tablet, error) {
	var tablets []Models.Tablet

	err := db.Where("current_car_id IS NULL AND status != 'inactive'").
		Find(&tablets).Error

	if err != nil {
		return nil, err
	}

	return tablets, nil
}

// GetTabletStatus returns a human-readable status for a tablet
func GetTabletStatus(tablet *Models.Tablet) string {
	if tablet.Status == "inactive" {
		return "Inactive"
	}

	if tablet.CurrentCarID == nil {
		return "Unassigned"
	}

	return fmt.Sprintf("Assigned to Car ID: %d", *tablet.CurrentCarID)
}

// IsTabletAssignedToCar checks if a tablet is assigned to a specific car
func IsTabletAssignedToCar(db *gorm.DB, tabletID uint, carID uint) bool {
	var tablet Models.Tablet
	if err := db.First(&tablet, tabletID).Error; err != nil {
		return false
	}

	return tablet.CurrentCarID != nil && *tablet.CurrentCarID == carID
}

// GetCarByTabletHWID retrieves the car associated with a tablet HWID
func GetCarByTabletHWID(db *gorm.DB, hwid string) (*Models.Car, error) {
	var tablet Models.Tablet
	err := db.Where("hwid = ?", hwid).First(&tablet).Error

	if err != nil {
		return nil, errors.New("tablet not found")
	}

	if tablet.CurrentCarID == nil {
		return nil, errors.New("tablet not assigned to any car")
	}

	var car Models.Car
	err = db.First(&car, *tablet.CurrentCarID).Error

	if err != nil {
		return nil, errors.New("car not found")
	}

	return &car, nil
}
