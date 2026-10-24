package Apis

import (
	"Falcon/Models"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// carAssignmentInput is the body of PATCH /api/cars/:id/assignment. Both
// fields are optional; an absent one is left as it is.
type carAssignmentInput struct {
	OperatingCompany *string `json:"operating_company"`
	OperatingArea    *string `json:"operating_area"`
}

// columns validates the body and returns the column updates it asks for.
// Values are trimmed; a field that is sent must not be blank, and a body that
// sends neither field is refused rather than treated as a no-op.
func (in carAssignmentInput) columns() (map[string]interface{}, error) {
	updates := map[string]interface{}{}
	for column, value := range map[string]*string{
		"operating_company": in.OperatingCompany,
		"operating_area":    in.OperatingArea,
	} {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed == "" {
			return nil, errors.New(column + " must not be blank")
		}
		updates[column] = trimmed
	}
	if len(updates) == 0 {
		return nil, errors.New("send operating_company and/or operating_area")
	}
	return updates, nil
}

// UpdateCarAssignment sets a car's operating company and/or area.
// PATCH /api/cars/:id/assignment
func UpdateCarAssignment(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid car id",
		})
	}

	var input carAssignmentInput
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
		})
	}
	updates, err := input.columns()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": err.Error(),
		})
	}

	var car Models.Car
	if err := Models.DB.Where("id = ?", id).First(&car).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Car not found",
			})
		}
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to load car",
		})
	}

	if err := Models.DB.Model(&car).Updates(updates).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update car assignment",
		})
	}
	// Answer with what is stored now, not with what was asked for.
	if err := Models.DB.First(&car, car.ID).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Car assignment updated but could not be read back",
		})
	}

	return c.JSON(fiber.Map{
		"id":                car.ID,
		"car_no_plate":      car.CarNoPlate,
		"operating_company": car.OperatingCompany,
		"operating_area":    car.OperatingArea,
	})
}
