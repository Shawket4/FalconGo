package Models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Car struct {
	gorm.Model
	TabletID                  *uint            `json:"tablet_id" gorm:"index"`
	Tablet                    *Tablet          `json:"tablet,omitempty" gorm:"foreignKey:TabletID"`
	CarNoPlate                string           `json:"car_no_plate"`
	CarType                   string           `json:"car_type"`
	Transporter               string           `json:"transporter"`
	TankCapacity              int              `json:"tank_capacity"`
	Compartments              []int            `gorm:"-" json:"compartments"`
	JSONCompartments          datatypes.JSON   `json:"json_compartments"`
	LicenseExpirationDate     string           `json:"license_expiration_date"`
	CalibrationExpirationDate string           `json:"calibration_expiration_date"`
	TankLicenseExpirationDate string           `json:"tank_license_expiration_date"`
	IsInTrip                  bool             `json:"is_in_trip"`
	IsApproved                bool             `json:"is_approved"`
	Location                  string           `json:"location"`
	Latitude                  string           `json:"lat"`
	Longitude                 string           `json:"long"`
	LocationTimeStamp         string           `json:"location_time_stamp"`
	EngineStatus              string           `json:"engine_status"`
	Speed                     int              `json:"speed"`
	LastFuelOdometer          int              `json:"last_fuel_odometer"`
	LastOilChangeID           int              `json:"last_oil_change_id"`
	Mileage                   float64          `json:"mileage"`
	DriverID                  *uint            `json:"driver_id" gorm:"index"`
	Driver                    *Driver          `json:"driver" gorm:"foreignKey:DriverID"`
	EtitCarID                 string           `json:"etit_car_id"`
	ServiceInvoices           []ServiceInvoice `json:"service_invoices,omitempty" gorm:"foreignKey:CarID"`
	OperatingCompany          string           `json:"operating_company"`
	OperatingArea             string           `json:"operating_area"`
	GeoFence                  string           `json:"geo_fence"`
	SlackStatus               string           `json:"slack_status"`
	LastUpdatedSlackStatus    time.Time        `json:"last_updated_slack_status"`

	// Image fields
	CarLicenseImageName             string `json:"car_license_image_name"`
	CarLicenseImageNameBack         string `json:"car_license_image_name_back"`
	CalibrationLicenseImageName     string `json:"calibration_license_image_name"`
	CalibrationLicenseImageNameBack string `json:"calibration_license_image_name_back"`
	TankLicenseImageName            string `json:"tank_license_image_name"`
	TankLicenseImageNameBack        string `json:"tank_license_image_name_back"`
}

func (Car) TableName() string {
	return "cars"
}

// CompartmentList decodes the persisted compartment layout. JSONCompartments
// is the stored column; the Compartments field is transient and is only
// populated for the wire. A car with no layout configured yet returns an empty
// slice rather than an error, so callers can treat "not configured" and
// "configured as nothing" the same way.
func (c *Car) CompartmentList() ([]int, error) {
	raw := c.JSONCompartments
	if len(raw) == 0 || string(raw) == "null" {
		return []int{}, nil
	}
	var compartments []int
	if err := json.Unmarshal(raw, &compartments); err != nil {
		return nil, err
	}
	if compartments == nil {
		compartments = []int{}
	}
	return compartments, nil
}

func RecalculateMileage(carID uint) error {
	var car Car
	if err := DB.First(&car, carID).Error; err != nil {
		return err
	}

	var maxMileage float64

	// 1. Check Fuel Events
	var maxFuelOdo float64
	DB.Model(&FuelEvent{}).Where("car_id = ?", carID).Select("MAX(odometer_after)").Scan(&maxFuelOdo)
	if maxFuelOdo > maxMileage {
		maxMileage = maxFuelOdo
	}

	// 2. Check Oil Changes
	var maxOilOdo float64
	DB.Model(&OilChange{}).Where("car_id = ?", carID).Select("MAX(current_odometer)").Scan(&maxOilOdo)
	if maxOilOdo > maxMileage {
		maxMileage = maxOilOdo
	}

	// 3. Check Service Invoices (Manual Overrides)
	var maxServiceOdo float64
	DB.Model(&ServiceInvoice{}).Where("car_id = ?", carID).Select("MAX(meter_reading)").Scan(&maxServiceOdo)
	if maxServiceOdo > maxMileage {
		maxMileage = maxServiceOdo
	}

	// Update Car Mileage
	return DB.Model(&car).Update("mileage", maxMileage).Error
}
