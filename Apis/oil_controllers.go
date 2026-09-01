package Apis

import (
	"Falcon/Models"
	"Falcon/Observability"
	"errors"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func CreateOilChange(c *fiber.Ctx) error {
	var input Models.OilChange
	if err := c.BodyParser(&input); err != nil {
		return err
	}
	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", input.CarID).Find(&car).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	input.CarNoPlate = car.CarNoPlate
	authorUser := c.Locals("user").(*Models.User)
	input.Author = authorUser.Name
	input, err := input.Add()
	if err != nil {
		log.Println(err)
		return err
	}

	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", car.ID).Update("last_oil_change_id", input.ID).Error; err != nil {
		log.Println(err)
		return err
	}
	if err := Models.RecalculateMileage(car.ID); err != nil {
		log.Println("Error updating car mileage:", err)
		Observability.ReportJobFailure("oil_change", "recalculate_mileage", err)
	}

	return c.JSON(fiber.Map{
		"message": "Oil Change Registered Successfully.",
	})
}

func EditOilChange(c *fiber.Ctx) error {
	var input Models.OilChange
	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}
	// formData := c.FormValue("request")
	// if err := json.Unmarshal([]byte(formData), &input); err != nil {
	// 	log.Println(err)
	// 	return err
	// }
	var car Models.Car
	if err := Models.DB.Where("id = ?", input.CarID).Find(&car).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	input.CarNoPlate = car.CarNoPlate

	var oilChange Models.OilChange
	if err := Models.DB.Model(&Models.OilChange{}).Where("id = ?", input.ID).Find(&oilChange).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	oilChange.CarID = input.CarID
	oilChange.CarNoPlate = input.CarNoPlate
	oilChange.DriverName = input.DriverName
	oilChange.CurrentOdometer = input.CurrentOdometer
	oilChange.Date = input.Date
	oilChange.SuperVisor = input.SuperVisor
	oilChange.Mileage = input.Mileage
	oilChange.OdometerAtChange = input.OdometerAtChange
	// Copied explicitly, like every other field here: this handler edits a
	// row it loaded from the database, so anything not assigned keeps its old
	// value and the edit is silently dropped. Unticking a filter has to
	// survive, which rules out the usual `Updates` shortcut too — GORM skips
	// false as a zero value; `Edit()` does a full Save.
	oilChange.OilFilterChanged = input.OilFilterChanged
	oilChange.FuelFilterChanged = input.FuelFilterChanged
	oilChange.WaterFilterChanged = input.WaterFilterChanged
	overWriterUser := c.Locals("user").(*Models.User)
	oilChange.Overwriter = overWriterUser.Name
	if _, err := oilChange.Edit(); err != nil {
		log.Println(err)
		return err
	}
	if err := Models.RecalculateMileage(car.ID); err != nil {
		log.Println("Error updating car mileage:", err)
		Observability.ReportJobFailure("oil_change", "recalculate_mileage", err)
	}

	return c.JSON(fiber.Map{
		"message": "Oil Change Updated Successfully.",
	})
}

func DeleteOilChange(c *fiber.Ctx) error {
	// Get ID from URL parameter instead of request body
	idParam := c.Params("id")
	if idParam == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "ID parameter is required",
		})
	}

	// Convert string to uint
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid ID format",
		})
	}

	var oilChange Models.OilChange
	if err := Models.DB.Model(&Models.OilChange{}).Where("id = ?", uint(id)).Find(&oilChange).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Error finding oil change record",
		})
	}

	// Check if record exists
	if oilChange.ID == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"message": "Oil change record not found",
		})
	}

	if _, err = oilChange.Delete(); err != nil {
		log.Println(err)
		return err
	}
	if err := Models.RecalculateMileage(oilChange.CarID); err != nil {
		log.Println("Error updating car mileage:", err)
		Observability.ReportJobFailure("oil_change", "recalculate_mileage", err)
	}

	return c.JSON(fiber.Map{
		"message": "Oil Change Deleted Successfully",
	})
}

func GetAllOilChanges(c *fiber.Ctx) error {
	var oilChanges []Models.OilChange

	if err := Models.DB.Model(&Models.OilChange{}).Find(&oilChanges).Error; err != nil {
		log.Println(err.Error())
		return err
	}

	return c.JSON(
		oilChanges,
	)
}

func GetOilChange(c *fiber.Ctx) error {
	id := c.Params("oilChangeId")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Missing oil change ID",
		})
	}

	var oilChange Models.OilChange
	query := Models.DB.Model(&Models.OilChange{}).Where("id = ?", id)

	if err := query.First(&oilChange).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Oil change not found",
			})
		}
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Error retrieving oil change",
		})
	}

	return c.JSON(oilChange)
}
