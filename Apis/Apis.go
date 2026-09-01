package Apis

import (
	"Falcon/Controllers"
	"Falcon/Database"
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/Scrapper"
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/datatypes"
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

type CarResponse struct {
	Models.Car
	CarLicenseURL             string `json:"car_license_url,omitempty"`
	CarLicenseBackURL         string `json:"car_license_back_url,omitempty"`
	CalibrationLicenseURL     string `json:"calibration_license_url,omitempty"`
	CalibrationLicenseBackURL string `json:"calibration_license_back_url,omitempty"`
	TankLicenseURL            string `json:"tank_license_url,omitempty"`
	TankLicenseBackURL        string `json:"tank_license_back_url,omitempty"`
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

	// Build response with image URLs
	baseURL := c.BaseURL() // e.g., http://localhost:3000
	carsResponse := make([]CarResponse, len(cars))

	for i, car := range cars {
		carsResponse[i] = CarResponse{Car: car}

		if car.CarLicenseImageName != "" {
			carsResponse[i].CarLicenseURL = fmt.Sprintf("%s/images/car-licenses/%s", baseURL, car.CarLicenseImageName)
		}
		if car.CarLicenseImageNameBack != "" {
			carsResponse[i].CarLicenseBackURL = fmt.Sprintf("%s/images/car-licenses-back/%s", baseURL, car.CarLicenseImageNameBack)
		}
		if car.CalibrationLicenseImageName != "" {
			carsResponse[i].CalibrationLicenseURL = fmt.Sprintf("%s/images/calibration-licenses/%s", baseURL, car.CalibrationLicenseImageName)
		}
		if car.CalibrationLicenseImageNameBack != "" {
			carsResponse[i].CalibrationLicenseBackURL = fmt.Sprintf("%s/images/calibration-licenses-back/%s", baseURL, car.CalibrationLicenseImageNameBack)
		}
		if car.TankLicenseImageName != "" {
			carsResponse[i].TankLicenseURL = fmt.Sprintf("%s/images/tank-licenses/%s", baseURL, car.TankLicenseImageName)
		}
		if car.TankLicenseImageNameBack != "" {
			carsResponse[i].TankLicenseBackURL = fmt.Sprintf("%s/images/tank-licenses-back/%s", baseURL, car.TankLicenseImageNameBack)
		}
	}

	if limitStr != "" {
		limit, _ := strconv.Atoi(limitStr)
		page, _ := strconv.Atoi(pageStr)
		return c.JSON(fiber.Map{
			"data": carsResponse,
			"pagination": fiber.Map{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		})
	}

	return c.JSON(carsResponse)
}

func GetCar(c *fiber.Ctx) error {
	var input struct {
		ID uint `json:"id"`
	}
	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}
	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", input.ID).Find(&car).Error; err != nil {
		log.Println(err.Error())
		return err
	}
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
	var input struct {
		ID uint `json:"id"`
	}
	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}
	var driver Models.Driver
	if err := Models.DB.Model(&Models.Driver{}).Where("id = ?", input.ID).Find(&driver).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	return c.JSON(driver)
}

func GetTransporters(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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

// func RegisterCar(c *fiber.Ctx) error {
// 	Controllers.User(c)
// 	if Controllers.CurrentUser.Id != 0 {
// 		if Controllers.CurrentUser.Permission == 0 {
// 			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
// 		} else {
// 			db := Database.ConnectToDB()
// 			// Get the body of the request
// 			// Convert the body to a map
// 			var car Car
// 			formData := c.FormValue("request")
// 			// format formData into data map
// 			err := json.Unmarshal([]byte(formData), &car)
// 			if err != nil {
// 				log.Println(err)
// 			}
// 			// err := c.BodyParser(&car)
// 			// if err != nil {
// 			// 	log.Println(err.Error())
// 			// 	return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
// 			// }

// 			jsonCompartments, err := json.Marshal(car.Compartments)
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
// 			}
// 			if Controllers.CurrentUser.Permission >= 3 {
// 				car.IsApproved = 1
// 			} else {
// 				car.IsApproved = 0
// 			}
// 			if car.Transporter == "" {
// 				car.Transporter = Controllers.CurrentUser.Name
// 			}
// 			carLicense, err := c.FormFile("CarLicense")
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			// Save file to disk
// 			// Allow multipart form
// 			err = c.SaveFile(carLicense, fmt.Sprintf("./CarLicenses/%s", carLicense.Filename))
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			carLicenseBack, err := c.FormFile("CarLicenseBack")
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			// Save file to disk
// 			// Allow multipart form
// 			err = c.SaveFile(carLicenseBack, fmt.Sprintf("./CarLicenses/%s", carLicenseBack.Filename))
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}

// 			calibrationLicense, err := c.FormFile("CalibrationLicense")
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			// Save file to disk
// 			// Allow multipart form
// 			err = c.SaveFile(calibrationLicense, fmt.Sprintf("./CalibrationLicenses/%s", calibrationLicense.Filename))
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			calibrationLicenseBack, err := c.FormFile("CalibrationLicenseBack")
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			// Save file to disk
// 			// Allow multipart form
// 			err = c.SaveFile(calibrationLicenseBack, fmt.Sprintf("./CalibrationLicenses/%s", calibrationLicenseBack.Filename))
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}

// 			tankLicense, err := c.FormFile("TankLicense")
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			// Save file to disk
// 			// Allow multipart form
// 			err = c.SaveFile(tankLicense, fmt.Sprintf("./TankLicenses/%s", tankLicense.Filename))
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}

// 			tankLicenseBack, err := c.FormFile("TankLicenseBack")
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}
// 			// Save file to disk
// 			// Allow multipart form
// 			err = c.SaveFile(tankLicenseBack, fmt.Sprintf("./TankLicensesBack/%s", tankLicenseBack.Filename))
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.JSON(fiber.Map{
// 					"message": err.Error(),
// 					"file":    "save",
// 				})
// 			}

// 			// Insert the car into the database
// 			_, err = db.Exec("INSERT INTO `Cars` (`CarId`, `CarNoPlate`, `Transporter`, `TankCapacity`, `Compartments`, `LicenseExpirationDate`, `CalibrationExpirationDate`, `TankLicenseExpirationDate`, `IsApproved`, `CarLicenseImageName`, `CalibrationLicenseImageName`, `CarLicenseImageNameBack`, `CalibrationLicenseImageNameBack`, `TankLicenseImageName`, `TankLicenseImageNameBack`) VALUES (NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", car.CarNoPlate, car.Transporter, car.TankCapacity, jsonCompartments, car.LicenseExpirationDate, car.CalibrationExpirationDate, car.TankLicenseExpirationDate, car.IsApproved, carLicense.Filename, calibrationLicense.Filename, carLicenseBack.Filename, calibrationLicenseBack.Filename, tankLicense.Filename, tankLicenseBack.Filename)
// 			if err != nil {
// 				log.Println(err.Error())
// 				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
// 			}
// 			return c.JSON(
// 				fiber.Map{
// 					"Message":    "Car added successfully",
// 					"CarNoPlate": car.CarNoPlate,
// 				},
// 			)
// 		}
// 	} else {
// 		return c.JSON(fiber.Map{
// 			"message": "Not Logged In.",
// 		})
// 	}
// }

func RegisterCar(c *fiber.Ctx) error {
	var car Models.Car

	// Check Content-Type to determine how to parse the request
	contentType := string(c.Request().Header.ContentType())

	if strings.Contains(contentType, "application/json") {
		// Handle JSON request (no files)
		if err := c.BodyParser(&car); err != nil {
			log.Println("JSON parse error:", err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "Invalid request body",
			})
		}
	} else {
		// Handle multipart/form-data request (with files)
		formData := c.FormValue("request")
		if formData == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "Missing 'request' form field",
			})
		}

		if err := json.Unmarshal([]byte(formData), &car); err != nil {
			log.Println("Form data parse error:", err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "Invalid request body",
			})
		}

		// Handle file uploads (only for multipart requests)
		if err := handleFileUploads(c, &car); err != nil {
			return err
		}
	}

	// Convert compartments to JSON
	jsonCompartments, err := json.Marshal(car.Compartments)
	if err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusBadRequest).SendString("Invalid compartments data")
	}

	car.IsApproved = true

	car.Transporter = "Apex"

	car.JSONCompartments = datatypes.JSON(jsonCompartments)

	// Save to database
	if err := Models.DB.Save(&car).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to save car",
		})
	}

	return c.JSON(fiber.Map{
		"message":      "Car added successfully",
		"car_no_plate": car.CarNoPlate,
		"car_id":       car.ID,
	})
}

// Extract file handling logic into separate function
func handleFileUploads(c *fiber.Ctx, car *Models.Car) error {
	// Handle Car License Front
	if carLicense, err := c.FormFile("CarLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), carLicense.Filename)
		if err := c.SaveFile(carLicense, fmt.Sprintf("./CarLicenses/%s", filename)); err != nil {
			log.Println("Error saving CarLicense:", err)
			Observability.ReportFailedUpload("CarLicense", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to save car license image",
			})
		}
		car.CarLicenseImageName = filename
	}

	// Handle Car License Back
	if carLicenseBack, err := c.FormFile("CarLicenseBack"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), carLicenseBack.Filename)
		if err := c.SaveFile(carLicenseBack, fmt.Sprintf("./CarLicensesBack/%s", filename)); err != nil {
			log.Println("Error saving CarLicenseBack:", err)
			Observability.ReportFailedUpload("CarLicenseBack", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to save car license back image",
			})
		}
		car.CarLicenseImageNameBack = filename
	}

	// Handle Calibration License Front
	if calibrationLicense, err := c.FormFile("CalibrationLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), calibrationLicense.Filename)
		if err := c.SaveFile(calibrationLicense, fmt.Sprintf("./CalibrationLicenses/%s", filename)); err != nil {
			log.Println("Error saving CalibrationLicense:", err)
			Observability.ReportFailedUpload("CalibrationLicense", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to save calibration license image",
			})
		}
		car.CalibrationLicenseImageName = filename
	}

	// Handle Calibration License Back
	if calibrationLicenseBack, err := c.FormFile("CalibrationLicenseBack"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), calibrationLicenseBack.Filename)
		if err := c.SaveFile(calibrationLicenseBack, fmt.Sprintf("./CalibrationLicensesBack/%s", filename)); err != nil {
			log.Println("Error saving CalibrationLicenseBack:", err)
			Observability.ReportFailedUpload("CalibrationLicenseBack", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Failed to save calibration license back image",
			})
		}
		car.CalibrationLicenseImageNameBack = filename
	}

	// Handle Tank License (only for تريلا type)
	if car.CarType == "تريلا" || car.CarType == "Trailer" {
		if tankLicense, err := c.FormFile("TankLicense"); err == nil {
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), tankLicense.Filename)
			if err := c.SaveFile(tankLicense, fmt.Sprintf("./TankLicenses/%s", filename)); err != nil {
				log.Println("Error saving TankLicense:", err)
				Observability.ReportFailedUpload("TankLicense", err)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"message": "Failed to save tank license image",
				})
			}
			car.TankLicenseImageName = filename
		}

		if tankLicenseBack, err := c.FormFile("TankLicenseBack"); err == nil {
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), tankLicenseBack.Filename)
			if err := c.SaveFile(tankLicenseBack, fmt.Sprintf("./TankLicensesBack/%s", filename)); err != nil {
				log.Println("Error saving TankLicenseBack:", err)
				Observability.ReportFailedUpload("TankLicenseBack", err)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"message": "Failed to save tank license back image",
				})
			}
			car.TankLicenseImageNameBack = filename
		}
	}

	return nil
}

func RegisterTransporter(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else if Controllers.CurrentUser.Permission >= 1 && Controllers.CurrentUser.Permission != 4 {
			var Cars []Models.Car
			if err := Models.DB.Model(&Models.Car{}).Where("transporter = ?", Controllers.CurrentUser.Name).Find(&Cars).Error; err != nil {
				log.Println(err.Error())
				return err
			}

			for _, car := range Cars {
				var compartments []int
				err := json.Unmarshal(car.JSONCompartments, &compartments)
				if err != nil {
					log.Println(err.Error())
				}
				car.Compartments = compartments
			}

			return c.JSON(Cars)
		} else {
			var Cars []Models.Car
			if err := Models.DB.Model(&Models.Car{}).Find(&Cars).Error; err != nil {
				log.Println(err.Error())
				return err
			}

			for _, car := range Cars {
				var compartments []int
				err := json.Unmarshal(car.JSONCompartments, &compartments)
				if err != nil {
					log.Println(err.Error())
				}
				car.Compartments = compartments
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			query, err := db.Query("SELECT * FROM `Transporters` WHERE 1;")
			if err != nil {
				log.Println(err.Error())
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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

func UpdateCarDocuments(c *fiber.Ctx) error {
	var input Models.Car
	formData := c.FormValue("request")

	if err := json.Unmarshal([]byte(formData), &input); err != nil {
		log.Println(err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
		})
	}

	jsonCompartments, err := json.Marshal(input.Compartments)
	if err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusBadRequest).SendString("Invalid compartments data")
	}

	if input.Transporter == "" {
		input.Transporter = Controllers.CurrentUser.Name
	}

	var car Models.Car
	if err := Models.DB.Model(&Models.Car{}).Where("id = ?", input.ID).First(&car).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"message": "Car not found",
		})
	}

	// Update basic fields
	car.CarNoPlate = input.CarNoPlate
	car.CarType = input.CarType
	car.Transporter = input.Transporter
	car.TankCapacity = input.TankCapacity
	car.JSONCompartments = jsonCompartments
	car.LicenseExpirationDate = input.LicenseExpirationDate
	car.CalibrationExpirationDate = input.CalibrationExpirationDate
	car.TankLicenseExpirationDate = input.TankLicenseExpirationDate

	// Handle Car License Front update
	if carLicense, err := c.FormFile("CarLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), carLicense.Filename)
		if err := c.SaveFile(carLicense, fmt.Sprintf("./uploads/car-licenses/%s", filename)); err != nil {
			log.Println("Error saving CarLicense:", err)
			Observability.ReportFailedUpload("CarLicense", err)
		} else {
			if car.CarLicenseImageName != "" {
				os.Remove(fmt.Sprintf("./uploads/car-licenses/%s", car.CarLicenseImageName))
			}
			car.CarLicenseImageName = filename
		}
	}

	// Handle Car License Back update
	if carLicenseBack, err := c.FormFile("CarLicenseBack"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), carLicenseBack.Filename)
		if err := c.SaveFile(carLicenseBack, fmt.Sprintf("./uploads/car-licenses-back/%s", filename)); err != nil {
			log.Println("Error saving CarLicenseBack:", err)
			Observability.ReportFailedUpload("CarLicenseBack", err)
		} else {
			if car.CarLicenseImageNameBack != "" {
				os.Remove(fmt.Sprintf("./uploads/car-licenses-back/%s", car.CarLicenseImageNameBack))
			}
			car.CarLicenseImageNameBack = filename
		}
	}

	// Handle Calibration License Front update
	if calibrationLicense, err := c.FormFile("CalibrationLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), calibrationLicense.Filename)
		if err := c.SaveFile(calibrationLicense, fmt.Sprintf("./uploads/calibration-licenses/%s", filename)); err != nil {
			log.Println("Error saving CalibrationLicense:", err)
			Observability.ReportFailedUpload("CalibrationLicense", err)
		} else {
			if car.CalibrationLicenseImageName != "" {
				os.Remove(fmt.Sprintf("./uploads/calibration-licenses/%s", car.CalibrationLicenseImageName))
			}
			car.CalibrationLicenseImageName = filename
		}
	}

	// Handle Calibration License Back update
	if calibrationLicenseBack, err := c.FormFile("CalibrationLicenseBack"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), calibrationLicenseBack.Filename)
		if err := c.SaveFile(calibrationLicenseBack, fmt.Sprintf("./uploads/calibration-licenses-back/%s", filename)); err != nil {
			log.Println("Error saving CalibrationLicenseBack:", err)
			Observability.ReportFailedUpload("CalibrationLicenseBack", err)
		} else {
			if car.CalibrationLicenseImageNameBack != "" {
				os.Remove(fmt.Sprintf("./uploads/calibration-licenses-back/%s", car.CalibrationLicenseImageNameBack))
			}
			car.CalibrationLicenseImageNameBack = filename
		}
	}

	// Handle Tank License updates (only for تريلا type)
	if car.CarType == "تريلا" || car.CarType == "Trailer" {
		if tankLicense, err := c.FormFile("TankLicense"); err == nil {
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), tankLicense.Filename)
			if err := c.SaveFile(tankLicense, fmt.Sprintf("./uploads/tank-licenses/%s", filename)); err != nil {
				log.Println("Error saving TankLicense:", err)
				Observability.ReportFailedUpload("TankLicense", err)
			} else {
				if car.TankLicenseImageName != "" {
					os.Remove(fmt.Sprintf("./uploads/tank-licenses/%s", car.TankLicenseImageName))
				}
				car.TankLicenseImageName = filename
			}
		}

		if tankLicenseBack, err := c.FormFile("TankLicenseBack"); err == nil {
			filename := fmt.Sprintf("%d_%s", time.Now().Unix(), tankLicenseBack.Filename)
			if err := c.SaveFile(tankLicenseBack, fmt.Sprintf("./uploads/tank-licenses-back/%s", filename)); err != nil {
				log.Println("Error saving TankLicenseBack:", err)
				Observability.ReportFailedUpload("TankLicenseBack", err)
			} else {
				if car.TankLicenseImageNameBack != "" {
					os.Remove(fmt.Sprintf("./uploads/tank-licenses-back/%s", car.TankLicenseImageNameBack))
				}
				car.TankLicenseImageNameBack = filename
			}
		}
	}

	if err := Models.DB.Save(&car).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update car",
		})
	}

	return c.JSON(fiber.Map{
		"message": "Car updated successfully",
		"car_id":  car.ID,
	})
}

// Add this to your Driver handler file

func UpdateDriverDocuments(c *fiber.Ctx) error {
	var input Models.Driver
	formData := c.FormValue("request")

	if err := json.Unmarshal([]byte(formData), &input); err != nil {
		log.Println(err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
		})
	}

	if input.Transporter == "" {
		input.Transporter = Controllers.CurrentUser.Name
	}

	var driver Models.Driver
	if err := Models.DB.Model(&Models.Driver{}).Where("id = ?", input.ID).First(&driver).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"message": "Driver not found",
		})
	}

	// Update basic fields
	driver.Name = input.Name
	driver.MobileNumber = input.MobileNumber
	driver.Transporter = input.Transporter
	driver.SocialSecurityNo = input.SocialSecurityNo
	driver.IDLicenseExpirationDate = input.IDLicenseExpirationDate
	driver.DriverLicenseExpirationDate = input.DriverLicenseExpirationDate
	driver.SafetyLicenseExpirationDate = input.SafetyLicenseExpirationDate
	driver.DrugTestExpirationDate = input.DrugTestExpirationDate

	// Handle ID License Front update
	if idLicense, err := c.FormFile("IDLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), idLicense.Filename)
		if err := c.SaveFile(idLicense, fmt.Sprintf("./uploads/driver-id-licenses/%s", filename)); err != nil {
			log.Println("Error saving IDLicense:", err)
			Observability.ReportFailedUpload("IDLicense", err)
		} else {
			if driver.IDLicenseImageName != "" {
				os.Remove(fmt.Sprintf("./uploads/driver-id-licenses/%s", driver.IDLicenseImageName))
			}
			driver.IDLicenseImageName = filename
		}
	}

	// Handle ID License Back update
	if idLicenseBack, err := c.FormFile("IDLicenseBack"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), idLicenseBack.Filename)
		if err := c.SaveFile(idLicenseBack, fmt.Sprintf("./uploads/driver-id-licenses-back/%s", filename)); err != nil {
			log.Println("Error saving IDLicenseBack:", err)
			Observability.ReportFailedUpload("IDLicenseBack", err)
		} else {
			if driver.IDLicenseImageNameBack != "" {
				os.Remove(fmt.Sprintf("./uploads/driver-id-licenses-back/%s", driver.IDLicenseImageNameBack))
			}
			driver.IDLicenseImageNameBack = filename
		}
	}

	// Handle Driver License Front update
	if driverLicense, err := c.FormFile("DriverLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), driverLicense.Filename)
		if err := c.SaveFile(driverLicense, fmt.Sprintf("./uploads/driver-licenses/%s", filename)); err != nil {
			log.Println("Error saving DriverLicense:", err)
			Observability.ReportFailedUpload("DriverLicense", err)
		} else {
			if driver.DriverLicenseImageName != "" {
				os.Remove(fmt.Sprintf("./uploads/driver-licenses/%s", driver.DriverLicenseImageName))
			}
			driver.DriverLicenseImageName = filename
		}
	}

	// Handle Driver License Back update
	if driverLicenseBack, err := c.FormFile("DriverLicenseBack"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), driverLicenseBack.Filename)
		if err := c.SaveFile(driverLicenseBack, fmt.Sprintf("./uploads/driver-licenses-back/%s", filename)); err != nil {
			log.Println("Error saving DriverLicenseBack:", err)
			Observability.ReportFailedUpload("DriverLicenseBack", err)
		} else {
			if driver.DriverLicenseImageNameBack != "" {
				os.Remove(fmt.Sprintf("./uploads/driver-licenses-back/%s", driver.DriverLicenseImageNameBack))
			}
			driver.DriverLicenseImageNameBack = filename
		}
	}

	// Handle Safety License Front update
	if safetyLicense, err := c.FormFile("SafetyLicense"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), safetyLicense.Filename)
		if err := c.SaveFile(safetyLicense, fmt.Sprintf("./uploads/safety-licenses/%s", filename)); err != nil {
			log.Println("Error saving SafetyLicense:", err)
			Observability.ReportFailedUpload("SafetyLicense", err)
		} else {
			if driver.SafetyLicenseImageName != "" {
				os.Remove(fmt.Sprintf("./uploads/safety-licenses/%s", driver.SafetyLicenseImageName))
			}
			driver.SafetyLicenseImageName = filename
		}
	}

	// Handle Drug Test Front update
	if drugTest, err := c.FormFile("DrugTest"); err == nil {
		filename := fmt.Sprintf("%d_%s", time.Now().Unix(), drugTest.Filename)
		if err := c.SaveFile(drugTest, fmt.Sprintf("./uploads/drug-tests/%s", filename)); err != nil {
			log.Println("Error saving DrugTest:", err)
			Observability.ReportFailedUpload("DrugTest", err)
		} else {
			if driver.DrugTestImageName != "" {
				os.Remove(fmt.Sprintf("./uploads/drug-tests/%s", driver.DrugTestImageName))
			}
			driver.DrugTestImageName = filename
		}
	}

	if err := Models.DB.Save(&driver).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update driver",
		})
	}

	return c.JSON(fiber.Map{
		"message":   "Driver updated successfully",
		"driver_id": driver.ID,
		"driver":    driver,
	})
}

func EditTransporter(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission < 3 {
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

func ApproveRequest(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission < 3 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			var RequestData struct {
				TableName    string `json:"TableName"`
				ColumnIdName string `json:"ColumnIdName"`
				Id           int    `json:"Id"`
			}
			err := c.BodyParser(&RequestData)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			query := fmt.Sprintf("UPDATE `%s` SET `IsApproved` = 1 WHERE `%s` = %v;", RequestData.TableName, RequestData.ColumnIdName, RequestData.Id)
			_, err = db.Exec(query)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			return c.JSON(fiber.Map{
				"Message": "Request Approved",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func RejectRequest(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission < 3 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			db := Database.ConnectToDB()
			var RequestData struct {
				TableName    string `json:"TableName"`
				ColumnIdName string `json:"ColumnIdName"`
				Id           int    `json:"Id"`
			}
			err := c.BodyParser(&RequestData)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			query := fmt.Sprintf("DELETE FROM `%s` WHERE `%s`.`%s` = %v;", RequestData.TableName, RequestData.TableName, RequestData.ColumnIdName, RequestData.Id)
			_, err = db.Exec(query)
			if err != nil {
				log.Println(err.Error())
				return c.Status(fiber.StatusBadRequest).SendString("Invalid request body")
			}
			return c.JSON(fiber.Map{
				"Message": "Request Rejected",
			})
		}
	} else {
		return c.JSON(fiber.Map{
			"message": "Not Logged In.",
		})
	}
}

func GetNonDriverUsers(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission < 4 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission < 4 {
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

func GetVehicleStatus(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			var data struct {
				CarNoPlate string `json:"CarNoPlate"`
			}

			err := c.BodyParser(&data)

			if err != nil {
				log.Println(err.Error())
			}
			for _, s := range Scrapper.VehicleStatusList {
				if s.PlateNo == data.CarNoPlate {
					return c.JSON(s)
				}
			}
			return c.JSON(fiber.Map{
				"message": "Car Not Found",
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

func GetVehicleMapPoints(c *fiber.Ctx) error {
	return c.JSON(Scrapper.VehicleStatusList)
}

func GetLocations(c *fiber.Ctx) error {
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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

			var serviceEvent Models.Service
			if err := Models.DB.Model(&Models.Service{}).Where("id = ?", input.ID).Find(&serviceEvent).Error; err != nil {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
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
	Controllers.User(c)
	if Controllers.CurrentUser.Id != 0 {
		if Controllers.CurrentUser.Permission == 0 {
			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
		} else {
			limitStr := c.Query("limit")
			pageStr := c.Query("page", "1")
			carIDStr := c.Query("car_id")

			var serviceEvents []Models.Service
			var total int64

			db := Models.DB.Model(&Models.Service{})

			if Controllers.CurrentUser.Permission != 4 {
				db = db.Where("transporter = ?", Controllers.CurrentUser.Name)
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
