package Models

import "gorm.io/gorm"

type Driver struct {
	gorm.Model
	Name string `json:"name" validate:"required,min=3,max=20"`
	//Email        string `json:"email" gorm:"unique" validate:"required,email"`
	MobileNumber string `json:"mobile_number"`
	//PasswordInput               string `gorm:"-:migration" json:"password_input"`
	//Password                    []byte `json:"-"`
	IDLicenseExpirationDate     string    `json:"id_license_expiration_date"`
	DriverLicenseExpirationDate string    `json:"driver_license_expiration_date"`
	SafetyLicenseExpirationDate string    `json:"safety_license_expiration_date"`
	DrugTestExpirationDate      string    `json:"drug_test_expiration_date"`
	Transporter                 string    `json:"transporter"`
	IsApproved                  bool      `json:"is_approved"`
	IsInTrip                    bool      `json:"is_in_trip"`
	CriminalRecordImageName     string    `gorm:"default:'';not null" json:"criminal_record_image_name"`
	IDLicenseImageName          string    `gorm:"default:'';not null" json:"id_license_image_name"`
	DriverLicenseImageName      string    `gorm:"default:'';not null" json:"driver_license_image_name"`
	SafetyLicenseImageName      string    `gorm:"default:'';not null" json:"safety_license_image_name"`
	DrugTestImageName           string    `gorm:"default:'';not null" json:"drug_test_image_name"`
	IDLicenseImageNameBack      string    `gorm:"default:'';not null" json:"id_license_image_name_back"`
	DriverLicenseImageNameBack  string    `gorm:"default:'';not null" json:"driver_license_image_name_back"`
	SocialSecurityNo            string    `json:"social_security_number"`
	Loans                       []Loan    `json:"loans"`
	Expenses                    []Expense `json:"expense"`
}

type Expense struct {
	gorm.Model
	DriverID      uint    `json:"driver_id"`
	Cost          float64 `json:"cost"`
	Category      string  `json:"category"`
	Description   string  `json:"description"`
	Date          string  `json:"date"`
	PaymentMethod string  `json:"payment_method"`
	IsPaid        bool    `json:"is_paid" gorm:"not null;default:false"`
}

type Salary struct {
	gorm.Model
	DriverID      uint    `json:"driver_id"`
	DriverCost    float64 `json:"driver_cost"`
	TotalExpenses float64 `json:"total_expenses"`
	TotalLoans    float64 `json:"total_loans"`
	StartDate     string  `json:"start_date"`
	CloseDate     string  `json:"close_date"`
}

type Loan struct {
	gorm.Model
	DriverID uint    `json:"driver_id"`
	Amount   float64 `json:"amount"`
	Method   string  `json:"method"`
	Date     string  `json:"date"`
	IsPaid   bool    `json:"is_paid" gorm:"not null;default:false"`
}
