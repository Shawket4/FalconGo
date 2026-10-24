package Controllers

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"Falcon/Models"
	"Falcon/utils"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error On Saving User To DB",
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
	driver.IsApproved = true
	driver.PIN = nil
	if err := Models.DB.Save(&driver).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error On Saving User To DB",
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error On Fetching Users From DB",
		})
	}
	return c.JSON(users)
}

// parsePermission reads a permission level sent either as a number (4) or as
// a string ("4") — the dashboard's select holds strings — and keeps it on the
// 1–4 ladder that middleware.Verify checks against.
func parsePermission(raw json.RawMessage) (int, error) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	level, err := strconv.Atoi(text)
	if err != nil || level < 1 || level > 4 {
		return 0, fiber.NewError(fiber.StatusBadRequest, "permission must be 1, 2, 3 or 4")
	}
	return level, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

func RegisterUser(c *fiber.Ctx) error {
	var data struct {
		Name       string          `json:"name"`
		Email      string          `json:"email"`
		Password   string          `json:"password"`
		Permission json.RawMessage `json:"permission"`
	}

	if err := c.BodyParser(&data); err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	data.Name = strings.TrimSpace(data.Name)
	data.Email = strings.TrimSpace(data.Email)
	if data.Name == "" || data.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name and email are required"})
	}
	if len(data.Password) < 6 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Password must be at least 6 characters"})
	}
	permissionlevel, err := parsePermission(data.Permission)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	password, err := bcrypt.GenerateFromPassword([]byte(data.Password), 14)
	if err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error hashing password"})
	}
	var user Models.User
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
	user.Name = data.Name
	user.Email = data.Email
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
	if err := Models.DB.Create(&user).Error; err != nil {
		if isUniqueViolation(err) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "A user with this email already exists"})
		}
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error On Saving User To DB"})
	}

	return c.JSON(user)
}

// UpdateUser changes only the fields the request carries. The dashboard's
// edit form sends name, email, permission and an optional new password; a
// field it leaves out (IsApproved, an empty password) keeps its stored value
// instead of being zeroed.
func UpdateUser(c *fiber.Ctx) error {
	var input struct {
		Id         uint            `json:"id"`
		Name       *string         `json:"name"`
		Email      *string         `json:"email"`
		Password   *string         `json:"password"`
		Permission json.RawMessage `json:"permission"`
		IsApproved *int            `json:"IsApproved"`
	}

	if err := c.BodyParser(&input); err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var user Models.User
	if input.Id == 0 || Models.DB.Where("id = ?", input.Id).First(&user).Error != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		user.Name = strings.TrimSpace(*input.Name)
	}
	if input.Email != nil && strings.TrimSpace(*input.Email) != "" {
		user.Email = strings.TrimSpace(*input.Email)
	}
	if len(input.Permission) > 0 && string(input.Permission) != "null" {
		level, err := parsePermission(input.Permission)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		user.Permission = level
	}
	if input.IsApproved != nil {
		user.IsApproved = *input.IsApproved
	}
	if input.Password != nil && *input.Password != "" {
		if len(*input.Password) < 6 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Password must be at least 6 characters"})
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*input.Password), 14)
		if err != nil {
			log.Println(err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error hashing password"})
		}
		user.Password = hash
	}

	if err := Models.DB.Save(&user).Error; err != nil {
		if isUniqueViolation(err) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "A user with this email already exists"})
		}
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error On Saving User To DB",
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error On Deleting User From DB",
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

	err := bcrypt.CompareHashAndPassword(user.Password, []byte(data["password"]))

	if err != nil {
		c.Status(fiber.StatusBadRequest)
		return c.JSON(fiber.Map{
			"error": "Invalid password",
		})
	}

	// Generate token using the new utility function
	token, err := utils.GenerateAdminUserToken(user)

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
		Expires:  time.Now().Add(time.Hour * 24 * 31), // Match token expiry
		HTTPOnly: true,
	}

	c.Cookie(&cookie)

	return c.JSON(fiber.Map{
		"jwt":        token,
		"success":    "success message",
		"permission": user.Permission,
		"name":       user.Name,
		"email":      user.Email,
		"id":         user.Id,
		"driver_id":  user.DriverID,
	})
}

var CurrentUser *Models.User

func User(c *fiber.Ctx) error {
	cookie := c.Cookies("jwt")
	if cookie == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Unauthenticated",
		})
	}

	// Use the new ParseToken utility
	claims, err := utils.ParseToken(cookie)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Invalid or expired token",
		})
	}

	// Check if it's an admin user
	if claims.UserType != "admin_user" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"message": "Invalid user type",
		})
	}

	var user Models.User
	Models.DB.Where("id = ?", claims.UserID).First(&user)

	if user.Id == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"message": "User not found",
		})
	}

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

	// Use the new ParseToken utility
	claims, err := utils.ParseToken(tokenString)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"message": "Invalid or expired token",
			"error":   err.Error(),
		})
	}

	// Verify user exists based on type
	if claims.UserType == "admin_user" {
		var user Models.User
		result := Models.DB.Where("id = ?", claims.UserID).First(&user)
		if result.Error != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "User not found",
			})
		}
	} else if claims.UserType == "driver" {
		var driver Models.Driver
		result := Models.DB.Where("id = ?", claims.DriverID).First(&driver)
		if result.Error != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"message": "Driver not found",
			})
		}
	}

	// Token is valid
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":   "Valid token",
		"user_type": claims.UserType,
		"user_id":   claims.UserID,
		"driver_id": claims.DriverID,
	})
}

func Logout(c *fiber.Ctx) error {
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
