package Apis

import (
	"Falcon/Controllers"
	"Falcon/Database"
	"Falcon/Models"
	"Falcon/Observability"
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type Car struct {
	CarId                           int    `json:"CarId"`
	CarNoPlate                      string `json:"CarNoPlate"`
	Transporter                     string `json:"Transporter"`
	TankCapacity                    int    `json:"TankCapacity"`
	Compartments                    []int  `json:"Compartments"`
	LicenseExpirationDate           string `json:"LicenseExpirationDate"`
	CalibrationExpirationDate       string `json:"CalibrationExpirationDate"`
	TankLicenseExpirationDate       string `json:"TankLicenseExpirationDate"`
	CarLicenseImageName             string `json:"CarLicenseImageName"`
	CalibrationLicenseImageName     string `json:"CalibrationLicenseImageName"`
	CarLicenseImageNameBack         string `json:"CarLicenseImageNameBack"`
	CalibrationLicenseImageNameBack string `json:"CalibrationLicenseImageNameBack"`
	TankLicenseImageName            string `json:"TankLicenseImageName"`
	TankLicenseImageNameBack        string `json:"TankLicenseImageNameBack"`
	IsInTrip                        string `json:"IsInTrip"`
	IsApproved                      int    `json:"IsApproved"`
}

type Driver struct {
	DriverId                   int    `json:"DriverId"`
	Name                       string `json:"Name"`
	Email                      string `json:"Email"`
	MobileNumber               string `json:"MobileNumber"`
	LicenseExpirationDate      string `json:"LicenseExpirationDate"`
	SafetyExpirationDate       string `json:"SafetyExpirationDate"`
	DrugTestExpirationDate     string `json:"DrugTestExpirationDate"`
	IsApproved                 int    `json:"IsApproved"`
	Transporter                string `json:"Transporter"`
	DriverLicenseImageName     string `json:"DriverLicenseImageName"`
	SafetyLicenseImageName     string `json:"SafetyLicenseImageName"`
	DrugTestImageName          string `json:"DrugTestImageName"`
	DriverLicenseImageNameBack string `json:"DriverLicenseImageNameBack"`
	SafetyLicenseImageNameBack string `json:"SafetyLicenseImageNameBack"`
	DrugTestImageNameBack      string `json:"DrugTestImageNameBack"`
}

type Transporter struct {
	Id           int                 `json:"TransporterId"`
	Name         string              `json:"TransporterName"`
	PhoneNumbers []map[string]string `json:"TransporterPhones"`
}

type User struct {
	Id           int    `json:"Id"`
	Name         string `json:"Name"`
	Email        string `json:"Email"`
	Permission   int    `json:"Permission"`
	MobileNumber string `json:"Mobile_Number"`
}

func GetCars(c *fiber.Ctx) error {
	limitStr := c.Query("limit")
	pageStr := c.Query("page", "1")

	var cars []Models.Car
	var total int64

	db := Models.DB.Model(&Models.Car{}).Preload("Driver")

	if limitStr != "" {
		limit, _ := strconv.Atoi(limitStr)
		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		offset := (page - 1) * limit

		db.Count(&total)
		if err := db.Offset(offset).Limit(limit).Find(&cars).Error; err != nil {
			log.Println(err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to fetch cars",
			})
		}
	} else {
		if err := db.Find(&cars).Error; err != nil {
			log.Println(err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to fetch cars",
			})
		}
	}

	// Decode the stored layout so every client sees one shape —
	// compartments: [] — instead of having to cope with a null column, a
	// JSON string, and an array.
	for i := range cars {
		compartments, err := cars[i].CompartmentList()
		if err != nil {
			log.Printf("car %d has an unreadable compartment layout: %v", cars[i].ID, err)
			compartments = []int{}
		}
		cars[i].Compartments = compartments
	}

	if limitStr != "" {
		limit, _ := strconv.Atoi(limitStr)
		page, _ := strconv.Atoi(pageStr)
		return c.JSON(fiber.Map{
			"data": cars,
			"pagination": fiber.Map{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		})
	}

	return c.JSON(cars)
}

func GetCar(c *fiber.Ctx) error {
	// The route is GET /api/cars/:id. Body-parsing an id here meant a plain GET
	// resolved to id 0 and, with Find rather than First, returned an empty car
	// with a 200. The param is the identifier; the body is a fallback for the
	// older callers that sent one.
	var input struct {
		ID uint `json:"id"`
	}
	if id, err := strconv.Atoi(c.Params("id")); err == nil && id > 0 {
		input.ID = uint(id)
	} else if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Missing car id",
		})
	}
	if input.ID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Missing car id",
		})
	}

	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", input.ID).First(&car).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Car not found",
			})
		}
		log.Println(err.Error())
		return err
	}

	compartments, err := car.CompartmentList()
	if err != nil {
		log.Printf("car %d has an unreadable compartment layout: %v", car.ID, err)
		compartments = []int{}
	}
	car.Compartments = compartments

	return c.JSON(car)
}

func SetCarDriverPair(c *fiber.Ctx) error {
	var input struct {
		CarID    uint `json:"car_id"`
		DriverID uint `json:"driver_id"`
	}
	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", input.CarID).Update("driver_id", input.DriverID).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	c.Status(http.StatusOK)
	return c.JSON(fiber.Map{
		"message": "Updated Successfully",
	})
}

func GetDrivers(c *fiber.Ctx) error {

	var drivers []Models.Driver

	if err := Models.DB.Model(&Models.Driver{}).Find(&drivers).Error; err != nil {
		log.Println("Database error:", err.Error())
		return c.Status(500).JSON(fiber.Map{
			"error":   "Database error",
			"details": err.Error(),
		})
	}

	fmt.Printf("Found %d drivers\n", len(drivers))
	return c.Status(200).JSON(drivers)
}

func GetDriver(c *fiber.Ctx) error {
	// The route is GET /api/drivers/:id. Body-parsing an id here meant a plain GET
	// resolved to id 0 and, with Find rather than First, returned an empty driver
	// with a 200. The param is the identifier; the body is a fallback for the
	// older callers that sent one.
	var input struct {
		ID uint `json:"id"`
	}
	if id, err := strconv.Atoi(c.Params("id")); err == nil && id > 0 {
		input.ID = uint(id)
	} else if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Missing driver id",
		})
	}
	if input.ID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Missing driver id",
		})
	}

	var driver Models.Driver
	if err := Models.DB.Model(&Models.Driver{}).Where("id = ?", input.ID).First(&driver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"message": "Driver not found",
			})
		}
		log.Println(err.Error())
		return err
	}
	return c.JSON(driver)
}

func GetTransporters(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var Transporters []string
			if err := Models.DB.Model(&Models.User{}).Where("permission >= 2").Select("name").Find(&Transporters).Error; err != nil {
				log.Println(err.Error())
				return err
			}
			return c.JSON(Transporters)
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func DaysBetweenDates(Date1, Date2 string) int {
	// Convert string to time
	t1, _ := time.Parse("2006-01-02", Date1)
	t2, _ := time.Parse("2006-01-02", Date2)
	// Calculate days between dates
	days := t2.Sub(t1).Hours() / 24
	return int(days)
}

func RegisterTransporter(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			// Get the body of the request
			var data Transporter
			err := c.BodyParser(&data)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			// Insert the transporter into the database
			// Encode the transporter mobile numbers to json
			jsonPhoneNumbers, err := json.Marshal(data.PhoneNumbers)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}

			_, err = db.Exec("INSERT INTO `Transporters` (`TransporterId`, `TransporterName`, `TransporterMobilePhone`) VALUES (NULL, ?, ?);", data.Name, jsonPhoneNumbers)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			//
			return c.JSON(
				fiber.Map{
					"Message": "Transporter added successfully",
					"Name":    data.Name,
				},
			)
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func GetCarProfileData(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else if currentUser.Permission >= 1 && currentUser.Permission != 4 {
			var Cars []Models.Car
			if err := Models.DB.Model(&Models.Car{}).Where("transporter = ?", currentUser.Name).Find(&Cars).Error; err != nil {
				log.Println(err.Error())
				return err
			}

			for i := range Cars {
				compartments, err := Cars[i].CompartmentList()
				if err != nil {
					log.Println(err.Error())
				}
				Cars[i].Compartments = compartments
			}

			return c.JSON(Cars)
		} else {
			var Cars []Models.Car
			if err := Models.DB.Model(&Models.Car{}).Find(&Cars).Error; err != nil {
				log.Println(err.Error())
				return err
			}

			for i := range Cars {
				compartments, err := Cars[i].CompartmentList()
				if err != nil {
					log.Println(err.Error())
				}
				Cars[i].Compartments = compartments
			}
			return c.JSON(Cars)
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func GetDriverProfileData(c *fiber.Ctx) error {
	var Drivers []Models.Driver
	//query, err := db.Query("SELECT `id`, `name`, `email`, `mobile_number`, `driver_license_expiration_date`, `safety_license_expiration_date`, `drug_test_expiration_date`, `IsApproved`, `Transporter`, `DriverLicenseImageName`, `SafetyLicenseImageName`, `DrugTestImageName`, `DriverLicenseImageNameBack`, `SafetyLicenseImageNameBack`, `DrugTestImageNameBack` FROM `users` WHERE permission = 0 AND `IsApproved` = 1 AND `Transporter` = ?", Controllers.CurrentUser.Name)
	//if err != nil {
	//	log.Println(err.Error())
	//}
	//defer query.Close()
	//
	//for query.Next() {
	//	var driver Driver
	//	err = query.Scan(&driver.DriverId, &driver.Name, &driver.Email, &driver.MobileNumber, &driver.LicenseExpirationDate, &driver.SafetyExpirationDate, &driver.DrugTestExpirationDate, &driver.IsApproved, &driver.Transporter, &driver.DriverLicenseImageName, &driver.SafetyLicenseImageName, &driver.DrugTestImageName, &driver.DriverLicenseImageNameBack, &driver.SafetyLicenseImageNameBack, &driver.DrugTestImageNameBack)
	//	if err != nil {
	//		log.Println(err.Error())
	//	}
	//	Drivers = append(Drivers, driver)
	//db := Database.ConnectToDB()
	if err := Models.DB.Model(&Models.Driver{}).Find(&Drivers).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	//query, err := db.Query("SELECT `id`, `name`, `email`, `mobile_number`, `driver_license_expiration_date`, `safety_license_expiration_date`, `drug_test_expiration_date`, `IsApproved`, `Transporter`, `DriverLicenseImageName`, `SafetyLicenseImageName`, `DrugTestImageName`, `DriverLicenseImageNameBack`, `SafetyLicenseImageNameBack`, `DrugTestImageNameBack` FROM `users` WHERE permission = 0 AND `IsApproved` = 1;")
	//if err != nil {
	//	log.Println(err.Error())
	//}
	//defer query.Close()
	//
	//for query.Next() {
	//	var driver Driver
	//	err = query.Scan(&driver.DriverId, &driver.Name, &driver.Email, &driver.MobileNumber, &driver.LicenseExpirationDate, &driver.SafetyExpirationDate, &driver.DrugTestExpirationDate, &driver.IsApproved, &driver.Transporter, &driver.DriverLicenseImageName, &driver.SafetyLicenseImageName, &driver.DrugTestImageName, &driver.DriverLicenseImageNameBack, &driver.SafetyLicenseImageNameBack, &driver.DrugTestImageNameBack)
	//	if err != nil {
	//		log.Println(err.Error())
	//	}
	//	Drivers = append(Drivers, driver)
	//}
	return c.JSON(Drivers)
}

func GetTransporterProfileData(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			query, err := db.Query("SELECT * FROM `Transporters` WHERE 1;")
			if err != nil {
				log.Println(err.Error())
				Observability.ReportJobFailure("transporters", "query", err)
			}
			defer query.Close()
			var Transporters []Transporter
			for query.Next() {
				var Transporter Transporter
				var jsonData string
				err = query.Scan(&Transporter.Id, &Transporter.Name, &jsonData)
				if err != nil {
					log.Println(err.Error())
				}
				err = json.Unmarshal([]byte(jsonData), &Transporter.PhoneNumbers)
				if err != nil {
					log.Println(err.Error())
				}
				Transporters = append(Transporters, Transporter)
			}
			return c.JSON(Transporters)
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func DeleteDriver(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var Driver struct {
				ID uint `json:"ID"`
			}
			if err := c.BodyParser(&Driver); err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			if err := Models.DB.Delete(&Models.Driver{}, Driver.ID).Error; err != nil {
				log.Println(err.Error())
				return err
			}
			//_, err = db.Exec("DELETE FROM `drivers` WHERE `name` = ?;", Driver.Name)
			//if err != nil {
			//	log.Println(err.Error())
			//	return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			//}
			return c.JSON(fiber.Map{
				"Message": "Driver deleted successfully",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func DeleteCar(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var Car struct {
				ID uint `json:"ID"`
			}
			if err := c.BodyParser(&Car); err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			if err := Models.DB.Delete(&Models.Car{}, Car.ID).Error; err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}

			return c.JSON(fiber.Map{
				"Message": "Car deleted successfully",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func EditTransporter(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			// Get Request Data
			var transporter Transporter
			err := c.BodyParser(&transporter)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			jsonPhoneNumbers, err := json.Marshal(transporter.PhoneNumbers)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			_, err = db.Exec("UPDATE `Transporters` SET `TransporterName` = ?, `TransporterMobilePhone` = ? WHERE `TransporterId` = ?;", transporter.Name, jsonPhoneNumbers, transporter.Id)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			return c.JSON(fiber.Map{
				"Message": "Transporter updated successfully",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func GetPendingRequests(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission < 3 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			Cars, err := db.Query("SELECT `CarId`, `CarNoPlate`, `Transporter`, `TankCapacity`, `Compartments`, `LicenseExpirationDate`, `CalibrationExpirationDate`, `TankLicenseExpirationDate`, `IsApproved`, `CarLicenseImageName`, `CalibrationLicenseImageName`, `TankLicenseImageName`, `CarLicenseImageNameBack`, `CalibrationLicenseImageNameBack`, `TankLicenseImageName` FROM `Cars` WHERE `IsApproved` = 0;")
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			defer Cars.Close()
			var CarsArray []Car
			for Cars.Next() {
				var car Car
				var jsonData string
				err := Cars.Scan(&car.CarId, &car.CarNoPlate, &car.Transporter, &car.TankCapacity, &jsonData, &car.LicenseExpirationDate, &car.CalibrationExpirationDate, &car.TankLicenseExpirationDate, &car.IsApproved, &car.CarLicenseImageName, &car.CalibrationLicenseImageName, &car.TankLicenseImageName, &car.CarLicenseImageNameBack, &car.CalibrationLicenseImageNameBack, &car.TankLicenseImageNameBack)
				json.Unmarshal([]byte(jsonData), &car.Compartments)
				if err != nil {
					log.Println(err.Error())
				}
				if err != nil {
					log.Println(err.Error())
					return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
				}
				CarsArray = append(CarsArray, car)
			}
			// Get Pending Driver Requests
			Drivers, err := db.Query("SELECT `id`, `name`, `email`, `mobile_number`, `driver_license_expiration_date`, `safety_license_expiration_date`, `drug_test_expiration_date`, `IsApproved`, `Transporter`, `DriverLicenseImageName`, `SafetyLicenseImageName`, `DrugTestImageName`, `DriverLicenseImageNameBack`, `SafetyLicenseImageNameBack`, `DrugTestImageNameBack` FROM `users` WHERE `IsApproved` = 0 AND permission = 0;")
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			defer Drivers.Close()
			var DriversArray []Driver
			for Drivers.Next() {
				var driver Driver
				err := Drivers.Scan(&driver.DriverId, &driver.Name, &driver.Email, &driver.MobileNumber, &driver.LicenseExpirationDate, &driver.SafetyExpirationDate, &driver.DrugTestExpirationDate, &driver.IsApproved, &driver.Transporter, &driver.DriverLicenseImageName, &driver.SafetyLicenseImageName, &driver.DrugTestImageName, &driver.DriverLicenseImageNameBack, &driver.SafetyLicenseImageNameBack, &driver.DrugTestImageNameBack)
				if err != nil {
					log.Println(err.Error())
					return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
				}
				DriversArray = append(DriversArray, driver)
			}
			return c.JSON(fiber.Map{
				"Cars":    CarsArray,
				"Drivers": DriversArray,
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

// ApproveRequest and RejectRequest are the legacy approval queue. They took
// the table and column names from the request body and spliced them into SQL
// (RejectRequest was an arbitrary DELETE), against the retired MySQL database
// that Database.ConnectToDB still points at. Nothing on Postgres needs them:
// drivers are created and updated already approved (RegisterDriver,
// UpdateDriver), and cars carry their own is_approved. They answer 410 so the
// endpoint can never be turned back into an injection point by accident.
func legacyApprovalGone(c *fiber.Ctx) error {
	return c.Status(fiber.StatusGone).JSON(fiber.Map{
		"message": "The legacy approval queue has been retired; drivers are approved on creation.",
	})
}

func ApproveRequest(c *fiber.Ctx) error {
	return legacyApprovalGone(c)
}

func RejectRequest(c *fiber.Ctx) error {
	return legacyApprovalGone(c)
}

func GetNonDriverUsers(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission < 4 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			Users, err := db.Query("SELECT `id`, `name`, `email`, `permission`, `mobile_number` FROM `users` WHERE `IsApproved` = 1 AND permission > 0 AND permission < 4;")
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			defer Users.Close()
			var UsersArray []User
			for Users.Next() {
				var user User
				err := Users.Scan(&user.Id, &user.Name, &user.Email, &user.Permission, &user.MobileNumber)
				if err != nil {
					log.Println(err.Error())
					return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
				}
				UsersArray = append(UsersArray, user)
			}
			return c.JSON(fiber.Map{
				"Users": UsersArray,
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func UpdateTempPermission(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission < 4 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			var RequestData struct {
				Id int `json:"Id"`
			}
			err := c.BodyParser(&RequestData)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			query := fmt.Sprintf("UPDATE `users` SET permission=(@temp:=permission), permission = permission2, permission2 = @temp WHERE `id` = %v;", RequestData.Id)
			_, err = db.Exec(query)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			return c.JSON(fiber.Map{
				"Message": "Permission Updated",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

type Trip struct {
	TripId            int             `json:"TripId"`
	HasDriver         bool            `json:"HasDriver"`
	DriverName        string          `json:"DriverName"`
	Date              string          `json:"Date"`
	CarNoPlate        string          `json:"CarNoPlate"`
	PickUpPoint       string          `json:"PickUpPoint"`
	NoOfDropOffPoints int             `json:"NoOfDropOffPoints"`
	DropOffPoints     []string        `json:"DropOffPoints"`
	Compartments      [][]interface{} `json:"Compartments"`
}

func UpdateCarStatusByID(CarID uint) (*Models.Car, error) {
	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", CarID).Find(&car).Error; err != nil {
		log.Println(err.Error())
		return nil, err
	}
	car.IsInTrip = true
	if err := Models.DB.Save(&car).Error; err != nil {
		log.Println(err.Error())
		return nil, err
	}
	return &car, nil
}

// Functions to manage fee mappings

// CreateFeeMapping creates a new fee mapping for a company
// func CalculateRevenue(trip *Models.TripStruct) (float64, error) {
// 	var totalRevenue float64 = 0

// 	switch trip.Company {
// 	case "TAQA", "Watanya":
// 		// Distance-based pricing
// 		trip.DistanceBasedPricing = true

// 		// Calculate revenue based on mileage for each drop-off point
// 		for _, dropOff := range trip.StepCompleteTime.DropOffPoints {
// 			// Fetch distance from database or mapping service
// 			distance, err := GetDistanceBetweenPoints(trip.StepCompleteTime.Terminal.TerminalName, dropOff.LocationName)
// 			if err != nil {
// 				return 0, err
// 			}

// 			// Calculate fee based on distance and capacity
// 			pointRevenue := distance * trip.FeePerKilometer * float64(dropOff.Capacity)
// 			totalRevenue += pointRevenue

// 			// Store the distance for reference
// 			dropOff.Distance = distance
// 		}

// 		// Update trip mileage with total calculated distance
// 		var totalDistance float64 = 0
// 		for _, dropOff := range trip.StepCompleteTime.DropOffPoints {
// 			totalDistance += dropOff.Distance
// 		}
// 		trip.Mileage = totalDistance

// 	case "Petrol Arrows":
// 		// Flat-rate pricing
// 		trip.DistanceBasedPricing = false

// 		// Load fee maps from database
// 		fees, err := GetFeeMapsByCompany(trip.Company)
// 		if err != nil {
// 			return 0, err
// 		}
// 		trip.FlatRateFees = fees

// 		// Calculate revenue based on predefined fees for each drop-off point
// 		for i, dropOff := range trip.StepCompleteTime.DropOffPoints {
// 			fee, err := LookupFee(fees, trip.StepCompleteTime.Terminal.TerminalName, dropOff.LocationName)
// 			if err != nil {
// 				return 0, err
// 			}

// 			// Calculate point revenue based on flat fee and capacity
// 			pointRevenue := fee * float64(dropOff.Capacity)
// 			totalRevenue += pointRevenue

// 			// Store the fee rate for reference
// 			trip.StepCompleteTime.DropOffPoints[i].FeeRate = fee
// 		}

// 		// Store flat rate fees as JSON
// 		feesJSON, err := json.Marshal(trip.FlatRateFees)
// 		if err != nil {
// 			return 0, err
// 		}
// 		trip.FlatRateFeesDB = feesJSON

// 	default:
// 		return 0, fmt.Errorf("unsupported company type: %s", trip.Company)
// 	}

// 	return totalRevenue, nil
// }

// Helper function to get distance between terminal and location

// Helper function to get fee maps by company

// Updated CreateCarTrip function

// Updated EditCarTrip function

func GetLocations(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			// db := Database.ConnectToDB()

			// defer db.Close()
			// CustomerQuery, err := db.Query("SELECT `CustomerName` FROM `Customers` WHERE 1;")

			// if err != nil {
			// 	log.Println(err.Error())
			// }

			// var Customers []string

			// defer CustomerQuery.Close()
			// for CustomerQuery.Next() {
			// 	var customer string
			// 	err = CustomerQuery.Scan(&customer)
			// 	if err != nil {
			// 		log.Println(err.Error())
			// 	}
			// 	Customers = append(Customers, customer)
			// }
			// TerminalQuery, err := db.Query("SELECT `TerminalName` FROM `Terminals` WHERE 1;")

			// if err != nil {
			// 	log.Println(err.Error())
			// }

			// var Terminals []string

			// defer TerminalQuery.Close()
			// for TerminalQuery.Next() {
			// 	var terminal string
			// 	err = TerminalQuery.Scan(&terminal)
			// 	if err != nil {
			// 		log.Println(err.Error())
			// 	}
			// 	Terminals = append(Terminals, terminal)
			// }
			var locations []Models.Location

			if err := Models.DB.Model(&Models.Location{}).Find(&locations).Error; err != nil {
				log.Println(err)
				return err
			}

			var terminals []Models.Terminal

			if err := Models.DB.Model(&Models.Terminal{}).Find(&terminals).Error; err != nil {
				log.Println(err)
				return err
			}

			return c.JSON(fiber.Map{
				"Customers": locations,
				"Terminals": terminals,
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func CreateLocation(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var input struct {
				Name string `json:"name"`
			}
			if err := c.BodyParser(&input); err != nil {
				log.Println(err)
				return err
			}
			var location Models.Location
			location.Name = input.Name
			if err := Models.DB.Create(&location).Error; err != nil {
				log.Println(err)
				return c.JSON(err)
			}
			return c.JSON(fiber.Map{"message": "Success"})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func CreateTerminal(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var input struct {
				Name string `json:"name"`
			}
			if err := c.BodyParser(&input); err != nil {
				log.Println(err)
				return err
			}
			var terminal Models.Terminal
			terminal.Name = input.Name
			if err := Models.DB.Create(&terminal).Error; err != nil {
				log.Println(err)
				return c.JSON(err)
			}
			return c.JSON(fiber.Map{"message": "Success"})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func CreateServiceEvent(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var input Models.Service
			formData := c.FormValue("request")
			if err := json.Unmarshal([]byte(formData), &input); err != nil {
				log.Println(err)
				return err
			}
			var car Models.Car
			if err := Models.DB.Model(&Models.Car{}).Where("id = ?", input.CarID).Find(&car).Error; err != nil {
				log.Println(err.Error())
				return err
			}
			input.CarNoPlate = car.CarNoPlate

			// proofFile, err := c.FormFile("ProofFile")
			// if err != nil {
			// 	log.Println(err.Error())
			// 	return c.JSON(fiber.Map{
			// 		"message": err.Error(),
			// 		"file":    "save",
			// 	})
			// }

			// err = c.SaveFile(proofFile, fmt.Sprintf("./ServiceProofs/%s", proofFile.Filename))
			// if err != nil {
			// 	log.Println(err.Error())
			// 	return c.JSON(fiber.Map{
			// 		"message": err.Error(),
			// 		"file":    "save",
			// 	})
			// }

			// input.ProofImageName = proofFile.Filename
			_, err := input.Add()
			if err != nil {
				log.Println(err)
				return err
			}

			return c.JSON(fiber.Map{
				"message": "Service Event Registered Successfully.",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func EditServiceEvent(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var input Models.Service
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

			// First, not Find: Find leaves a missing or zero ID as an empty
			// struct, which Edit then saves as a brand-new row.
			if input.ID == 0 {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Service event not found"})
			}
			var serviceEvent Models.Service
			if err := Models.DB.First(&serviceEvent, input.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Service event not found"})
				}
				log.Println(err.Error())
				return err
			}
			serviceEvent.CarID = input.CarID
			serviceEvent.CarNoPlate = input.CarNoPlate
			serviceEvent.DriverName = input.DriverName
			serviceEvent.ServiceType = input.ServiceType
			serviceEvent.DateOfService = input.DateOfService
			serviceEvent.OdometerReading = input.OdometerReading
			serviceEvent.ProofImageName = input.ProofImageName
			_, err := serviceEvent.Edit()
			if err != nil {
				log.Println(err)
				return err
			}

			return c.JSON(fiber.Map{
				"message": "Service Event Updated Successfully.",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func DeleteServiceEvent(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var input struct {
				ID uint `json:"ID"`
			}
			if err := c.BodyParser(&input); err != nil {
				log.Println(err.Error())
				return err
			}
			var serviceEvent Models.Service
			if err := Models.DB.Model(&Models.Service{}).Where("id = ?", input.ID).Find(&serviceEvent).Error; err != nil {
				log.Println(err.Error())
				return err
			}
			_, err := serviceEvent.Delete()
			if err != nil {
				log.Println(err.Error())
				return err
			}
			return c.JSON(fiber.Map{
				"message": "Service Event Deleted Successfully",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func GetAllServiceEvents(c *fiber.Ctx) error {
	currentUser := Controllers.RequestUser(c)
	if currentUser.Id != 0 {
		if currentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			limitStr := c.Query("limit")
			pageStr := c.Query("page", "1")
			carIDStr := c.Query("car_id")

			var serviceEvents []Models.Service
			var total int64

			db := Models.DB.Model(&Models.Service{})

			if currentUser.Permission != 4 {
				db = db.Where("transporter = ?", currentUser.Name)
			}

			if carIDStr != "" {
				carID, _ := strconv.Atoi(carIDStr)
				db = db.Where("car_id = ?", carID)
			}

			if limitStr != "" {
				limit, _ := strconv.Atoi(limitStr)
				page, _ := strconv.Atoi(pageStr)
				if page < 1 {
					page = 1
				}
				offset := (page - 1) * limit

				db.Count(&total)
				if err := db.Order("date_of_service DESC").Offset(offset).Limit(limit).Find(&serviceEvents).Error; err != nil {
					log.Println(err.Error())
					return err
				}

				return c.JSON(fiber.Map{
					"data": serviceEvents,
					"pagination": fiber.Map{
						"page":       page,
						"limit":      limit,
						"total":      total,
						"totalPages": (total + int64(limit) - 1) / int64(limit),
					},
				})
			} else {
				if err := db.Order("date_of_service DESC").Find(&serviceEvents).Error; err != nil {
					log.Println(err.Error())
					return err
				}
				return c.JSON(fiber.Map{
					"ServiceEvents": serviceEvents,
				})
			}
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func createTarAndGz(fileNames map[string]string, buffer io.Writer) error {
	gzipWriter := gzip.NewWriter(buffer)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()
	for filepath, filename := range fileNames {
		err := addToTar(tarWriter, filename, filepath)
		if err != nil {
			return err
		}
	}
	return nil
}

func addToTar(tarWriter *tar.Writer, filename string, filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = filename
	err = tarWriter.WriteHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return err
	}

	return nil
}

// func GetCarExpenses(c *fiber.Ctx) error {

// 	var input struct {
// 		CarID    uint   `json:"car_id"`
// 		DateFrom string `json:"date_from"`
// 		DateTo   string `json:"date_to"`
// 	}

// 	if err := c.BodyParser(&input); err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	var TotalExpenses float64
// 	var TotalRevenue float64
// 	var FuelEvents []Models.FuelEvent
// 	var OilChanges []Models.OilChange
// 	var ServiceEvents []Models.Service
// 	var Trips []Models.TripStruct

// 	DateFrom, err := AbstractFunctions.ParseDate(input.DateFrom)

// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	DateTo, err := AbstractFunctions.ParseDate(input.DateTo)
// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	if err := Models.DB.Model(&Models.FuelEvent{}).Where("car_id = ?", input.CarID).Where("date BETWEEN ? AND ?", DateFrom, DateTo).Find(&FuelEvents).Error; err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	if err := Models.DB.Model(&Models.OilChange{}).Where("car_id = ?", input.CarID).Where("date BETWEEN ? AND ?", DateFrom, DateTo).Find(&OilChanges).Error; err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	if err := Models.DB.Model(&Models.Service{}).Where("car_id = ?", input.CarID).Where("date_of_service BETWEEN ? AND ?", DateFrom, DateTo).Find(&ServiceEvents).Error; err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	if err := Models.DB.Model(&Models.TripStruct{}).Where("car_id = ?", input.CarID).Where("date BETWEEN ? AND ?", DateFrom, DateTo).Find(&Trips).Error; err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	for _, fuelEvent := range FuelEvents {
// 		TotalExpenses += fuelEvent.Price
// 	}

// 	for _, oilChange := range OilChanges {
// 		TotalExpenses += oilChange.Cost
// 	}

// 	for _, serviceEvent := range ServiceEvents {
// 		TotalExpenses += serviceEvent.Cost
// 	}

// 	for _, trip := range Trips {
// 		TotalRevenue += trip.Revenue
// 	}

// 	file := GenerateExpensesExcelFile(TotalCarExpenses{TotalExpenses: TotalExpenses, TotalRevenue: TotalRevenue, FuelEvents: FuelEvents, ServiceEvents: ServiceEvents, OilChanges: OilChanges, Trips: Trips})
// 	var filename string = fmt.Sprintf("./Expenses %v %s:%s.xlsx", input.CarID, input.DateFrom, input.DateTo)
// 	err = file.SaveAs(filename)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	return c.SendFile(filename, true)
// }

// UpdateCarLastFuelOdometer updates the car's last_fuel_odometer based on the most recent FuelEvent
func SyncCarLastFuelOdometer(c *fiber.Ctx) error {
	log.Println("Starting car last fuel odometer update process...")

	// Get all cars
	var cars []Models.Car
	if err := Models.DB.Find(&cars).Error; err != nil {
		log.Printf("Error fetching cars: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to fetch cars",
			"error":   err.Error(),
		})
	}

	updatedCount := 0
	errorCount := 0
	results := make(map[string]interface{})

	for _, car := range cars {
		// Get the most recent FuelEvent for this car
		var lastFuelEvent Models.FuelEvent
		err := Models.DB.Where("car_no_plate = ?", car.CarNoPlate).
			Order("date DESC, created_at DESC").
			First(&lastFuelEvent).Error

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				log.Printf("No fuel events found for car %s", car.CarNoPlate)
				results[car.CarNoPlate] = map[string]interface{}{
					"status":  "no_fuel_events",
					"message": "No fuel events found for this car",
				}
				continue
			}
			log.Printf("Error fetching fuel events for car %s: %v", car.CarNoPlate, err)
			results[car.CarNoPlate] = map[string]interface{}{
				"status":  "error",
				"message": "Database error",
				"error":   err.Error(),
			}
			errorCount++
			continue
		}

		// Update the car's last_fuel_odometer
		if err := Models.DB.Model(&car).Update("last_fuel_odometer", lastFuelEvent.OdometerAfter).Error; err != nil {
			log.Printf("Error updating car %s last_fuel_odometer: %v", car.CarNoPlate, err)
			Observability.ReportJobFailure("fuel_odometer_sync", "update_last_fuel_odometer", err)
			results[car.CarNoPlate] = map[string]interface{}{
				"status":  "error",
				"message": "Failed to update odometer",
				"error":   err.Error(),
			}
			errorCount++
			continue
		}

		// Check if the odometer was actually updated
		var updatedCar Models.Car
		Models.DB.First(&updatedCar, car.ID)

		results[car.CarNoPlate] = map[string]interface{}{
			"status":            "updated",
			"previous_odometer": car.LastFuelOdometer,
			"new_odometer":      lastFuelEvent.OdometerAfter,
			"fuel_event_date":   lastFuelEvent.Date,
			"fuel_event_id":     lastFuelEvent.ID,
		}

		log.Printf("Updated car %s last_fuel_odometer from %d to %d (FuelEvent ID: %d, Date: %s)",
			car.CarNoPlate, car.LastFuelOdometer, lastFuelEvent.OdometerAfter, lastFuelEvent.ID, lastFuelEvent.Date)
		updatedCount++
	}

	log.Printf("Car last fuel odometer update completed: %d updated, %d errors", updatedCount, errorCount)

	return c.JSON(fiber.Map{
		"success":       true,
		"message":       fmt.Sprintf("Updated %d cars, %d errors", updatedCount, errorCount),
		"updated_count": updatedCount,
		"error_count":   errorCount,
		"results":       results,
	})
}

func SyncCarMileages(c *fiber.Ctx) error {
	log.Println("Starting global car mileage recalculation process...")

	var cars []Models.Car
	if err := Models.DB.Find(&cars).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to fetch cars",
			"error":   err.Error(),
		})
	}

	updatedCount := 0
	errorCount := 0

	for _, car := range cars {
		if err := Models.RecalculateMileage(car.ID); err != nil {
			log.Printf("Error recalculating mileage for car %s: %v", car.CarNoPlate, err)
			Observability.ReportJobFailure("fuel_odometer_sync", "recalculate_mileage", err)
			errorCount++
		} else {
			updatedCount++
		}
	}

	return c.JSON(fiber.Map{
		"success":       true,
		"message":       fmt.Sprintf("Recalculated mileage for %d cars, %d errors", updatedCount, errorCount),
		"updated_count": updatedCount,
		"error_count":   errorCount,
	})
}
