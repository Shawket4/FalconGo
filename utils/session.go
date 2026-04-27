package utils

import (
	"Falcon/Models"
	"errors"
	"time"

	"gorm.io/gorm"
)

// GetActiveSession retrieves the active session for a driver
func GetActiveSession(db *gorm.DB, driverID uint) (*Models.DriverSession, error) {
	var session Models.DriverSession

	err := db.Where("driver_id = ? AND end_time IS NULL", driverID).
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		First(&session).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("no active session found")
		}
		return nil, err
	}

	return &session, nil
}

// GetActiveSessionByDriverAndTablet retrieves active session matching both driver and tablet
func GetActiveSessionByDriverAndTablet(db *gorm.DB, driverID uint, tabletID uint) (*Models.DriverSession, error) {
	var session Models.DriverSession

	err := db.Where("driver_id = ? AND tablet_id = ? AND end_time IS NULL", driverID, tabletID).
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		First(&session).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("no active session found for this driver and tablet")
		}
		return nil, err
	}

	return &session, nil
}

// HasActiveSession checks if a driver has an active session
func HasActiveSession(db *gorm.DB, driverID uint) bool {
	var count int64
	db.Model(&Models.DriverSession{}).
		Where("driver_id = ? AND end_time IS NULL", driverID).
		Count(&count)

	return count > 0
}

// CreateSession creates a new driver session
func CreateSession(db *gorm.DB, driverID uint, carID uint, tabletID uint) (*Models.DriverSession, error) {
	// Check if driver already has an active session
	if HasActiveSession(db, driverID) {
		return nil, errors.New("driver already has an active session")
	}

	// Verify tablet is assigned to the car
	var tablet Models.Tablet
	if err := db.First(&tablet, tabletID).Error; err != nil {
		return nil, errors.New("tablet not found")
	}

	if tablet.CurrentCarID == nil || *tablet.CurrentCarID != carID {
		return nil, errors.New("tablet is not assigned to this car")
	}

	session := Models.DriverSession{
		DriverID:  driverID,
		CarID:     carID,
		TabletID:  tabletID,
		StartTime: time.Now(),
	}

	if err := db.Create(&session).Error; err != nil {
		return nil, err
	}

	// Preload relationships
	db.Preload("Driver").Preload("Car").Preload("Tablet").First(&session, session.ID)

	return &session, nil
}

// EndSession ends an active session
func EndSession(db *gorm.DB, sessionID uint) error {
	now := time.Now()

	result := db.Model(&Models.DriverSession{}).
		Where("id = ? AND end_time IS NULL", sessionID).
		Update("end_time", now)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("session not found or already ended")
	}

	return nil
}

// EndDriverActiveSession ends the active session for a driver
func EndDriverActiveSession(db *gorm.DB, driverID uint) error {
	now := time.Now()

	result := db.Model(&Models.DriverSession{}).
		Where("driver_id = ? AND end_time IS NULL", driverID).
		Update("end_time", now)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("no active session found for driver")
	}

	return nil
}

// GetAllActiveSessions retrieves all currently active sessions
func GetAllActiveSessions(db *gorm.DB) ([]Models.DriverSession, error) {
	var sessions []Models.DriverSession

	err := db.Where("end_time IS NULL").
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		Order("start_time DESC").
		Find(&sessions).Error

	if err != nil {
		return nil, err
	}

	return sessions, nil
}

// GetDriverSessions retrieves all sessions for a specific driver with optional date filtering
func GetDriverSessions(db *gorm.DB, driverID uint, startDate *time.Time, endDate *time.Time) ([]Models.DriverSession, error) {
	query := db.Where("driver_id = ?", driverID)

	if startDate != nil {
		query = query.Where("start_time >= ?", startDate)
	}

	if endDate != nil {
		query = query.Where("start_time <= ?", endDate)
	}

	var sessions []Models.DriverSession
	err := query.
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		Order("start_time DESC").
		Find(&sessions).Error

	if err != nil {
		return nil, err
	}

	return sessions, nil
}

// GetCarSessions retrieves all sessions for a specific car
func GetCarSessions(db *gorm.DB, carID uint, startDate *time.Time, endDate *time.Time) ([]Models.DriverSession, error) {
	query := db.Where("car_id = ?", carID)

	if startDate != nil {
		query = query.Where("start_time >= ?", startDate)
	}

	if endDate != nil {
		query = query.Where("start_time <= ?", endDate)
	}

	var sessions []Models.DriverSession
	err := query.
		Preload("Driver").
		Preload("Car").
		Preload("Tablet").
		Order("start_time DESC").
		Find(&sessions).Error

	if err != nil {
		return nil, err
	}

	return sessions, nil
}

// ValidateSessionForLocationPing validates that a session can receive location pings
func ValidateSessionForLocationPing(db *gorm.DB, sessionID uint, driverID uint, tabletID uint) error {
	var session Models.DriverSession

	err := db.First(&session, sessionID).Error
	if err != nil {
		return errors.New("session not found")
	}

	// Check session is active
	if session.EndTime != nil {
		return errors.New("session has ended")
	}

	// Check driver matches
	if session.DriverID != driverID {
		return errors.New("session does not belong to this driver")
	}

	// Check tablet matches
	if session.TabletID != tabletID {
		return errors.New("session tablet does not match request tablet")
	}

	return nil
}
