package Apis

import (
	"Falcon/Controllers"
	"Falcon/Models"
	"Falcon/documents"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// A car's profile: plate, type, capacity, and what its licence says about the
// vehicle (chassis, engine, payload, colour, and the trailer's own plate,
// chassis, payload and colour). The papers and their dates belong to the documents package. The
// three expiry dates are still accepted here, because the AI advisor and
// older clients send them: a date sent is taken into the matching document
// (documents.AdoptLegacyDates) and a date left out is left alone.

// carProfileInput is a car edit. A nil field was not sent and is left as it
// is: the advisor sends whole rows, the dashboard only what its form holds.
type carProfileInput struct {
	ID                 uint     `json:"ID"`
	CarNoPlate         *string  `json:"car_no_plate"`
	CarType            *string  `json:"car_type"`
	Transporter        *string  `json:"transporter"`
	TankCapacity       *int     `json:"tank_capacity"`
	Compartments       []int    `json:"compartments"`
	ChassisNo          *string  `json:"chassis_no"`
	EngineNo           *string  `json:"engine_no"`
	EngineCC           *int     `json:"engine_cc"`
	PayloadTons        *float64 `json:"payload_tons"`
	Color              *string  `json:"color"`
	TrailerNoPlate     *string  `json:"trailer_no_plate"`
	TrailerChassisNo   *string  `json:"trailer_chassis_no"`
	TrailerPayloadTons *float64 `json:"trailer_payload_tons"`
	TrailerColor       *string  `json:"trailer_color"`

	LicenseExpirationDate     *string `json:"license_expiration_date"`
	CalibrationExpirationDate *string `json:"calibration_expiration_date"`
	TankLicenseExpirationDate *string `json:"tank_license_expiration_date"`
}

func carActor(c *fiber.Ctx) documents.Actor {
	user := Controllers.RequestUser(c)
	return documents.Actor{ID: user.Id, Name: user.Name}
}

func badCar(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"message": message})
}

// RegisterCar is POST /api/cars. A body that names an existing car by ID has
// always been taken as an edit of it, and still is.
func RegisterCar(c *fiber.Ctx) error {
	raw := Controllers.ProfileBody(c)
	var car Models.Car
	if err := json.Unmarshal(raw, &car); err != nil {
		log.Println("car body parse error:", err)
		return badCar(c, "Invalid request body")
	}
	if car.ID != 0 {
		return updateCar(c, car.ID, raw)
	}
	car.CarNoPlate = strings.TrimSpace(car.CarNoPlate)
	if car.CarNoPlate == "" {
		return badCar(c, "The plate number is required")
	}
	if car.EngineCC < 0 || car.PayloadTons < 0 || car.TrailerPayloadTons < 0 {
		return badCar(c, "Engine size and payload cannot be negative")
	}
	for _, date := range []string{car.LicenseExpirationDate, car.CalibrationExpirationDate, car.TankLicenseExpirationDate} {
		if err := documents.CheckLegacyDate(date); err != nil {
			return Controllers.ProfileError(c, "save car", err)
		}
	}
	car.IsApproved = true
	car.Transporter = "Apex"
	// Compartments is transient: the stored column is json_compartments. Only
	// write a layout the caller sent, or a car posted without one would store
	// a literal null where the trip form expects a list.
	if car.Compartments != nil {
		layout, err := json.Marshal(car.Compartments)
		if err != nil {
			return badCar(c, "Invalid compartments data")
		}
		car.JSONCompartments = datatypes.JSON(layout)
	}

	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&car).Error; err != nil {
			return err
		}
		return documents.AdoptLegacyDates(tx, documents.OwnerCar, car.ID, carActor(c))
	})
	if err != nil {
		return Controllers.ProfileError(c, "save car", err)
	}
	return c.JSON(fiber.Map{
		"message":      "Car added successfully",
		"car_no_plate": car.CarNoPlate,
		"car_id":       car.ID,
	})
}

// UpdateCar is PUT /api/cars/:id.
func UpdateCar(c *fiber.Ctx) error {
	raw := Controllers.ProfileBody(c)
	var probe struct {
		ID uint `json:"ID"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		log.Println("car body parse error:", err)
		return badCar(c, "Invalid request body")
	}
	// The route's id wins; the body's is for the callers that only send that.
	id := probe.ID
	if p, err := c.ParamsInt("id"); err == nil && p > 0 {
		id = uint(p)
	}
	return updateCar(c, id, raw)
}

func updateCar(c *fiber.Ctx, id uint, raw []byte) error {
	var in carProfileInput
	if err := json.Unmarshal(raw, &in); err != nil {
		log.Println("car body parse error:", err)
		return badCar(c, "Invalid request body")
	}
	var car Models.Car
	if err := Models.DB.First(&car, id).Error; err != nil || id == 0 {
		if id == 0 || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"message": "Car not found"})
		}
		return Controllers.ProfileError(c, "load car", err)
	}

	set := map[string]any{}
	text := func(column string, value *string) {
		if value != nil {
			set[column] = strings.TrimSpace(*value)
		}
	}
	if in.CarNoPlate != nil && strings.TrimSpace(*in.CarNoPlate) == "" {
		return badCar(c, "The plate number is required")
	}
	if (in.EngineCC != nil && *in.EngineCC < 0) ||
		(in.PayloadTons != nil && *in.PayloadTons < 0) ||
		(in.TrailerPayloadTons != nil && *in.TrailerPayloadTons < 0) {
		return badCar(c, "Engine size and payload cannot be negative")
	}
	for _, date := range []*string{in.LicenseExpirationDate, in.CalibrationExpirationDate, in.TankLicenseExpirationDate} {
		if date != nil {
			if err := documents.CheckLegacyDate(*date); err != nil {
				return Controllers.ProfileError(c, "update car", err)
			}
		}
	}
	text("car_no_plate", in.CarNoPlate)
	text("car_type", in.CarType)
	// An empty transporter has always meant "not given", never "clear it".
	if in.Transporter != nil && strings.TrimSpace(*in.Transporter) != "" {
		text("transporter", in.Transporter)
	}
	if in.TankCapacity != nil {
		set["tank_capacity"] = *in.TankCapacity
	}
	// Absent compartments means "leave the layout alone", not "erase it".
	if in.Compartments != nil {
		layout, err := json.Marshal(in.Compartments)
		if err != nil {
			return badCar(c, "Invalid compartments data")
		}
		set["json_compartments"] = datatypes.JSON(layout)
	}
	text("chassis_no", in.ChassisNo)
	text("engine_no", in.EngineNo)
	if in.EngineCC != nil {
		set["engine_cc"] = *in.EngineCC
	}
	if in.PayloadTons != nil {
		set["payload_tons"] = *in.PayloadTons
	}
	text("color", in.Color)
	text("trailer_no_plate", in.TrailerNoPlate)
	text("trailer_color", in.TrailerColor)
	text("trailer_chassis_no", in.TrailerChassisNo)
	if in.TrailerPayloadTons != nil {
		set["trailer_payload_tons"] = *in.TrailerPayloadTons
	}
	text("license_expiration_date", in.LicenseExpirationDate)
	text("calibration_expiration_date", in.CalibrationExpirationDate)
	text("tank_license_expiration_date", in.TankLicenseExpirationDate)

	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		if len(set) > 0 {
			if err := tx.Model(&Models.Car{}).Where("id = ?", id).Updates(set).Error; err != nil {
				return err
			}
		}
		return documents.AdoptLegacyDates(tx, documents.OwnerCar, id, carActor(c))
	})
	if err != nil {
		return Controllers.ProfileError(c, "update car", err)
	}
	return c.JSON(fiber.Map{
		"message": "Car updated successfully",
		"car_id":  id,
	})
}
