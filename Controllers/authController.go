package Controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"Falcon/Models"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

const SecretKey = "secret"

func RegisterDriver(c *fiber.Ctx) error {
	var data Models.Driver
	formData := c.FormValue("request")
	if err := json.Unmarshal([]byte(formData), &data); err != nil {
		log.Println(err)
		return err
	}

	//data.Password, _ = bcrypt.GenerateFromPassword([]byte(data.PasswordInput), 14)
	//data.PasswordInput = ""
	//if data.Transporter == "" {
	//	data.Transporter = CurrentUser.Name
	//}
	// driverLicense, err := c.FormFile("DriverLicense")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(driverLicense, fmt.Sprintf("./DriverLicenses/%s", driverLicense.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }

	// safetyLicense, err := c.FormFile("SafetyLicense")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(safetyLicense, fmt.Sprintf("./SafetyLicenses/%s", safetyLicense.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }

	// drugTest, err := c.FormFile("DrugTest")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(drugTest, fmt.Sprintf("./DrugTests/%s", drugTest.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }

	// IDLicenseFront, err := c.FormFile("IDLicenseFront")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(IDLicenseFront, fmt.Sprintf("./IDLicenses/%s", IDLicenseFront.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }

	// IDLicenseBack, err := c.FormFile("IDLicenseBack")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(IDLicenseBack, fmt.Sprintf("./IDLicensesBack/%s", IDLicenseBack.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }

	// CriminalRecord, err := c.FormFile("CriminalRecord")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(CriminalRecord, fmt.Sprintf("./CriminalRecords/%s", CriminalRecord.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }

	// data.IDLicenseImageName = IDLicenseFront.Filename
	// data.IDLicenseImageNameBack = IDLicenseBack.Filename
	// data.DriverLicenseImageName = driverLicense.Filename
	// data.CriminalRecordImageName = CriminalRecord.Filename
	// data.SafetyLicenseImageName = safetyLicense.Filename
	// data.DrugTestImageName = drugTest.Filename
	data.IsApproved = true
	if err := Models.DB.Create(&data).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": fmt.Sprintf("Error On Saving User To DB: %v", err.Error()),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Driver Created Successfully",
	})
}

func UpdateDriver(c *fiber.Ctx) error {
	var input Models.Driver
	formData := c.FormValue("request")
	if err := json.Unmarshal([]byte(formData), &input); err != nil {
		log.Println(err)
		return err
	}
	var driver Models.Driver
	if err := Models.DB.Model(&Models.Driver{}).Where("id = ?", input.ID).Find(&driver).Error; err != nil {
		log.Println(err.Error())
		return err
	}
	driver.Name = input.Name
	driver.MobileNumber = input.MobileNumber
	driver.IDLicenseExpirationDate = input.IDLicenseExpirationDate
	driver.DriverLicenseExpirationDate = input.DriverLicenseExpirationDate
	driver.SafetyLicenseExpirationDate = input.SafetyLicenseExpirationDate
	driver.DrugTestExpirationDate = input.DrugTestExpirationDate
	//uploadedFiles := c.FormValue("files")
	//var uploadedFilesBool map[string]bool
	//if err := json.Unmarshal([]byte(uploadedFiles), &uploadedFilesBool); err != nil {
	//	log.Println(err)
	//	return err
	//}
	//if uploadedFilesBool["DriverLicense"] {
	// driverLicense, err := c.FormFile("DriverLicense")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(driverLicense, fmt.Sprintf("./DriverLicenses/%s", driverLicense.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driver.DriverLicenseImageName = driverLicense.Filename

	// safetyLicense, err := c.FormFile("SafetyLicense")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(safetyLicense, fmt.Sprintf("./SafetyLicenses/%s", safetyLicense.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driver.SafetyLicenseImageName = safetyLicense.Filename

	// drugTest, err := c.FormFile("DrugTest")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(drugTest, fmt.Sprintf("./DrugTests/%s", drugTest.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driver.DrugTestImageName = drugTest.Filename

	// IDLicenseFront, err := c.FormFile("IDLicenseFront")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(IDLicenseFront, fmt.Sprintf("./IDLicenses/%s", IDLicenseFront.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driver.IDLicenseImageName = IDLicenseFront.Filename

	// IDLicenseBack, err := c.FormFile("IDLicenseBack")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(IDLicenseBack, fmt.Sprintf("./IDLicensesBack/%s", IDLicenseBack.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driver.IDLicenseImageNameBack = IDLicenseBack.Filename

	// CriminalRecord, err := c.FormFile("CriminalRecord")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(CriminalRecord, fmt.Sprintf("./CriminalRecords/%s", CriminalRecord.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driver.CriminalRecordImageName = CriminalRecord.Filename

	// if CurrentUser.Permission >= 1 {
	driver.IsApproved = true
	// }
	if err := Models.DB.Save(&driver).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": fmt.Sprintf("Error On Saving User To DB: %v", err.Error()),
		})
	}
	return c.JSON(fiber.Map{
		"message": "Driver Created Successfully",
	})
}

func FetchUsers(c *fiber.Ctx) error {
	var users []Models.User
	if err := Models.DB.Find(&users).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": fmt.Sprintf("Error On Fetching Users From DB: %v", err.Error()),
		})
	}
	return c.JSON(users)
}

func RegisterUser(c *fiber.Ctx) error {
	var data map[string]string

	if err := c.BodyParser(&data); err != nil {
		log.Println(err.Error())
		return err
	}

	password, _ := bcrypt.GenerateFromPassword([]byte(data["password"]), 14)
	var user Models.User
	permissionlevel, err := strconv.Atoi(data["permission"])
	if err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"message": err.Error(),
		})
	}
	user.Permission = permissionlevel

	// user := Models.User{
	// 	Name:                        data["name"],
	// 	Email:                       data["email"],
	// 	Password:                    password,
	// 	Permission:                  permissionlevel,
	// 	DriverLicenseExpirationDate: data["DriverLicenseExpirationDate"],
	// 	SafetyLicenseExpirationDate: data["SafetyLicenseExpirationDate"],
	// 	DrugTestExpirationDate:      data["DrugTestExpirationDate"],
	// 	MobileNumber:                data["mobile_number"],
	// }
	user.Name = data["name"]
	user.Email = data["email"]
	user.Password = password
	user.IsApproved = 1
	// if CurrentUser.Permission >= 3 {
	// 	user.IsApproved = 1
	// } else {
	// 	user.IsApproved = 0
	// }
	// driverLicense, err := c.FormFile("DriverLicense")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(driverLicense, fmt.Sprintf("./DriverLicenses/%s", driverLicense.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// driverLicenseBack, err := c.FormFile("DriverLicenseBack")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(driverLicenseBack, fmt.Sprintf("./DriverLicenses/%s", driverLicenseBack.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// safetyLicense, err := c.FormFile("SafetyLicense")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(safetyLicense, fmt.Sprintf("./SafetyLicenses/%s", safetyLicense.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// safetyLicenseBack, err := c.FormFile("SafetyLicenseBack")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(safetyLicenseBack, fmt.Sprintf("./SafetyLicenses/%s", safetyLicenseBack.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// drugTest, err := c.FormFile("DrugTest")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(drugTest, fmt.Sprintf("./DrugTests/%s", drugTest.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// drugTestBack, err := c.FormFile("DrugTestBack")
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // Save file to disk
	// // Allow multipart form
	// err = c.SaveFile(drugTestBack, fmt.Sprintf("./DrugTests/%s", drugTestBack.Filename))
	// if err != nil {
	// 	log.Println(err.Error())
	// 	return c.JSON(fiber.Map{
	// 		"message": err.Error(),
	// 		"file":    "save",
	// 	})
	// }
	// // user.Image = file.Filename
	// user.DriverLicenseImageName = driverLicense.Filename
	// user.DriverLicenseImageNameBack = driverLicenseBack.Filename
	// user.SafetyLicenseImageName = safetyLicense.Filename
	// user.SafetyLicenseImageNameBack = safetyLicenseBack.Filename
	// user.DrugTestImageName = drugTest.Filename
	// user.DrugTestImageNameBack = drugTestBack.Filename

	// Database.DB.Create(&user)

	// Database.DB.Create(&user)
	Models.DB.Create(&user)

	return c.JSON(user)
}

func UpdateUser(c *fiber.Ctx) error {
	var input Models.User

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}

	var user Models.User
	Models.DB.Where("id = ?", input.Id).First(&user)

	if user.Id == 0 {
		return c.JSON(fiber.Map{
			"message": "User not found",
		})
	}

	user.Name = input.Name
	user.Email = input.Email
	user.Permission = input.Permission
	user.IsApproved = input.IsApproved

	if err := Models.DB.Save(&user).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": fmt.Sprintf("Error On Saving User To DB: %v", err.Error()),
		})
	}

	return c.JSON(fiber.Map{
		"message": "User updated successfully",
	})
}

func DeleteUser(c *fiber.Ctx) error {
	var input Models.User
	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return err
	}

	if err := Models.DB.Where("id = ?", input.Id).Delete(&Models.User{}).Error; err != nil {
		log.Println(err.Error())
		return c.JSON(fiber.Map{
			"error": fmt.Sprintf("Error On Deleting User From DB: %v", err.Error()),
		})
	}

	return c.JSON(fiber.Map{
		"message": "User deleted successfully",
	})
}

// } else {
// 	return c.JSON(fiber.Map{
// 		"message": "Not Logged In.",
// 	})
// }

func Login(c *fiber.Ctx) error {
	var data map[string]string

	if err := c.BodyParser(&data); err != nil {
		return err
	}

	var user Models.User
	Models.DB.Where("email = ?", data["email"]).First(&user)

	if user.Id == 0 {
		c.Status(fiber.StatusNotFound)
		return c.JSON(fiber.Map{
			"error": "User not found",
		})
	}

	// pp, _ := bcrypt.GenerateFromPassword([]byte(data["password"]), 14)

	err := bcrypt.CompareHashAndPassword(user.Password, []byte(data["password"]))

	if err != nil {
		c.Status(fiber.StatusBadRequest)
		return c.JSON(fiber.Map{
			"error": "Invalid password",
		})
	}

	// Create token
	claims := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    strconv.Itoa(int(user.Id)),
		ExpiresAt: jwt.NewNumericDate(time.Unix(time.Now().Add(time.Hour*24*31).Unix(), 0)), // 1 day
	})

	// time.Now().Add(time.Hour * 24).Unix()

	token, err := claims.SignedString([]byte(SecretKey))

	if err != nil {
		c.Status(fiber.StatusInternalServerError)
		return c.JSON(fiber.Map{
			"error": "could not login",
		})
	}

	// Create cookie
	cookie := fiber.Cookie{
		Name:     "jwt",
		Value:    token,
		Expires:  time.Now().Add(time.Hour * 24),
		HTTPOnly: true,
	}

	c.Cookie(&cookie)

	// return c.JSON(token)
	fmt.Println("Success")
	return c.JSON(fiber.Map{
		"jwt":        token,
		"success":    "success message",
		"permission": user.Permission,
		"name":       user.Name,
		"email":      user.Email,
	})
}

var CurrentUser *Models.User

func User(c *fiber.Ctx) error {
	var user Models.User
	user.Id = 0
	CurrentUser = &user
	cookie := c.Cookies("jwt")

	token, err := jwt.ParseWithClaims(cookie, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(SecretKey), nil
	})

	if err != nil {
		c.Status(fiber.StatusUnauthorized)
		fmt.Println("Error", err)
		return c.JSON(fiber.Map{
			"message": "Unauthenticated",
		})
	}

	claims := token.Claims.(*jwt.RegisteredClaims)

	Models.DB.Where("id = ?", claims.Issuer).First(&user)
	CurrentUser = &user
	return c.JSON(user)
}

func ValidateToken(c *fiber.Ctx) error {
	// Extract token from Authorization header
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Authorization header is required",
		})
	}

	// Check if the format is "Bearer <token>"
	headerParts := strings.Split(authHeader, " ")
	if len(headerParts) != 2 || headerParts[0] != "Bearer" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Invalid authorization format",
		})
	}

	tokenString := headerParts[1]

	// Parse and validate the token
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(SecretKey), nil
	})

	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Invalid or expired token",
			"error":   err.Error(),
		})
	}

	// Verify token is valid
	if !token.Valid {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Invalid token",
		})
	}

	// Extract claims
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Invalid token claims",
		})
	}

	// Optional: Verify the token hasn't expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Token has expired",
		})
	}

	// Verify user exists in database
	var user Models.User
	result := Models.DB.Where("id = ?", claims.Issuer).First(&user)

	if result.Error != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "User not found",
		})
	}

	// Token is valid
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Valid token",
		"user_id": claims.Issuer,
	})
}

func Logout(c *fiber.Ctx) error {
	// Remove cookie
	// -time.Hour = expires before one hour
	cookie := fiber.Cookie{
		Name:     "jwt",
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: true,
	}

	c.Cookie(&cookie)

	return c.JSON(fiber.Map{
		"message": "success",
	})
}
