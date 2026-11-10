package Controllers

import (
	"Falcon/Models"
	"Falcon/documents"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// A driver's profile: name, phone, transporter, ID number. The papers and
// their dates belong to the documents package. The four expiry dates are
// still accepted here, because the AI advisor and older clients send them:
// a date sent is taken into the matching document (documents.AdoptLegacyDates)
// and a date left out is left alone.

// ProfileBody is the JSON of a profile request: the body itself, or the
// "request" field of a multipart form, which is how the first clients sent it.
func ProfileBody(c *fiber.Ctx) []byte {
	if strings.Contains(string(c.Request().Header.ContentType()), fiber.MIMEApplicationJSON) {
		return c.Body()
	}
	return []byte(c.FormValue("request"))
}

// ProfileError answers a failed profile write: a refused date as a 400 with
// its code, anything else as a logged 500.
func ProfileError(c *fiber.Ctx, what string, err error) error {
	var verr *documents.ValidationError
	if errors.As(err, &verr) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": verr.Msg, "error": verr.Msg, "code": verr.Code,
		})
	}
	log.Printf("%s: %v", what, err)
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"message": "Failed to " + what, "error": "Failed to " + what,
	})
}

// driverProfileInput is a driver edit. A nil field was not sent and is left
// as it is: the advisor sends whole rows, the dashboard only what its form holds.
type driverProfileInput struct {
	ID                          uint    `json:"ID"`
	Name                        *string `json:"name"`
	MobileNumber                *string `json:"mobile_number"`
	Transporter                 *string `json:"transporter"`
	SocialSecurityNo            *string `json:"social_security_number"`
	IDLicenseExpirationDate     *string `json:"id_license_expiration_date"`
	DriverLicenseExpirationDate *string `json:"driver_license_expiration_date"`
	SafetyLicenseExpirationDate *string `json:"safety_license_expiration_date"`
	DrugTestExpirationDate      *string `json:"drug_test_expiration_date"`
}

// RegisterDriver is POST /api/drivers.
func RegisterDriver(c *fiber.Ctx) error {
	var data Models.Driver
	if err := json.Unmarshal(ProfileBody(c), &data); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	data.ID = 0
	data.Name = strings.TrimSpace(data.Name)
	if data.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "The driver's name is required"})
	}
	for _, date := range []string{data.IDLicenseExpirationDate, data.DriverLicenseExpirationDate, data.SafetyLicenseExpirationDate, data.DrugTestExpirationDate} {
		if err := documents.CheckLegacyDate(date); err != nil {
			return ProfileError(c, "save the driver", err)
		}
	}
	data.IsApproved = true
	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&data).Error; err != nil {
			return err
		}
		return documents.AdoptLegacyDates(tx, documents.OwnerDriver, data.ID, documentsActor(c))
	})
	if err != nil {
		return ProfileError(c, "save the driver", err)
	}
	return c.JSON(fiber.Map{
		"message":   "Driver Created Successfully",
		"driver_id": data.ID,
	})
}

// UpdateDriver is PUT /api/drivers/:id (and PUT /api/drivers/documents/:id,
// the path the advisor knows it by). Only the columns sent change; the PIN is
// never touched, so an edit cannot wipe a driver's tablet login.
func UpdateDriver(c *fiber.Ctx) error {
	var in driverProfileInput
	if err := json.Unmarshal(ProfileBody(c), &in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	// The route's id wins; the body's is for the callers that only send that.
	id, ok := uintParam(c, "id")
	if !ok {
		id = in.ID
	}
	var driver Models.Driver
	if err := Models.DB.First(&driver, id).Error; err != nil || id == 0 {
		if id == 0 || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Driver not found"})
		}
		return ProfileError(c, "load the driver", err)
	}

	set := map[string]any{"is_approved": true}
	text := func(column string, value *string) {
		if value != nil {
			set[column] = strings.TrimSpace(*value)
		}
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "The driver's name is required"})
	}
	for _, date := range []*string{in.IDLicenseExpirationDate, in.DriverLicenseExpirationDate, in.SafetyLicenseExpirationDate, in.DrugTestExpirationDate} {
		if date != nil {
			if err := documents.CheckLegacyDate(*date); err != nil {
				return ProfileError(c, "update the driver", err)
			}
		}
	}
	text("name", in.Name)
	text("mobile_number", in.MobileNumber)
	text("social_security_no", in.SocialSecurityNo)
	// An empty transporter has always meant "not given", never "clear it".
	if in.Transporter != nil && strings.TrimSpace(*in.Transporter) != "" {
		text("transporter", in.Transporter)
	}
	text("id_license_expiration_date", in.IDLicenseExpirationDate)
	text("driver_license_expiration_date", in.DriverLicenseExpirationDate)
	text("safety_license_expiration_date", in.SafetyLicenseExpirationDate)
	text("drug_test_expiration_date", in.DrugTestExpirationDate)

	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Models.Driver{}).Where("id = ?", id).Updates(set).Error; err != nil {
			return err
		}
		if err := documents.AdoptLegacyDates(tx, documents.OwnerDriver, id, documentsActor(c)); err != nil {
			return err
		}
		return tx.First(&driver, id).Error
	})
	if err != nil {
		return ProfileError(c, "update the driver", err)
	}
	return c.JSON(fiber.Map{
		"message":   "Driver updated successfully",
		"driver_id": driver.ID,
		"driver":    driver,
	})
}
