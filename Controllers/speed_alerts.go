package Controllers

import (
	"Falcon/Models"
	"log"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

func GetVehicleSpeedViolations(c *fiber.Ctx) error {
	var Alerts []Models.SpeedAlert
	var Cars []Models.Car

	if err := Models.DB.Model(&Models.Car{}).Find(&Cars).Error; err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return c.JSON(fiber.Map{"error": err})
	}

	if err := Models.DB.Model(&Models.SpeedAlert{}).Find(&Alerts).Error; err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return c.JSON(fiber.Map{"error": err})
	}
	var outputMap map[string][]Models.SpeedAlert = make(map[string][]Models.SpeedAlert)
	for _, alert := range Alerts {
		var car_plate string
		for _, car := range Cars {
			if car.EtitCarID == alert.VehicleID {
				car_plate = car.CarNoPlate
			}
		}
		if car_plate != "" {
			outputMap[car_plate] = append(outputMap[car_plate], alert)
		}
	}
	return c.JSON(outputMap)
}
