package Models

import "gorm.io/gorm"

type Driver struct {
	gorm.Model

	// Authentication
	PIN *string `json:"-" gorm:"uniqueIndex;size:60"`

	// Basic Info
	Name         string `json:"name" validate:"required,min=3,max=100"`
	MobileNumber string `json:"mobile_number"`
	Transporter  string `json:"transporter"`

	// License & Documents - Expiration Dates
	IDLicenseExpirationDate     string `json:"id_license_expiration_date"`
	DriverLicenseExpirationDate string `json:"driver_license_expiration_date"`
	SafetyLicenseExpirationDate string `json:"safety_license_expiration_date"`
	DrugTestExpirationDate      string `json:"drug_test_expiration_date"`
	SocialSecurityNo            string `json:"social_security_number"`

	// Document Images - Front and Back (matching Car model pattern)
	IDLicenseImageName         string `gorm:"default:''" json:"id_license_image_name"`
	IDLicenseImageNameBack     string `gorm:"default:''" json:"id_license_image_name_back"`
	DriverLicenseImageName     string `gorm:"default:''" json:"driver_license_image_name"`
	DriverLicenseImageNameBack string `gorm:"default:''" json:"driver_license_image_name_back"`
	SafetyLicenseImageName     string `gorm:"default:''" json:"safety_license_image_name"`
	DrugTestImageName          string `gorm:"default:''" json:"drug_test_image_name"`

	// Status
	IsApproved bool `json:"is_approved" gorm:"default:false"`

	// Relationships
	Loans    []Loan          `json:"loans,omitempty"`
	Expenses []Expense       `json:"expenses,omitempty"`
	Sessions []DriverSession `json:"sessions,omitempty" gorm:"foreignKey:DriverID"`
}

// TableName specifies the table name for Driver model
func (Driver) TableName() string {
	return "drivers"
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
