package Models

import "gorm.io/gorm"

type PetroAppRecord struct {
	gorm.Model
	Branch                string  `json:"branch"`
	VehicleID             int     `json:"vehicle_id"`
	Vehicle               string  `json:"vehicle"`
	ShowVehicleProfile    bool    `json:"show_vehicle_profile"`
	InternalID            string  `json:"internal_id"`
	StructureNo           string  `json:"structure_no"`
	TripNumber            string  `json:"trip_number"`
	FuelTypeTitle         string  `json:"fuel_type_title"`
	Cost                  string  `json:"cost"`
	CostIncVAT            string  `json:"cost_inc_vat"`
	ServiceFee            string  `json:"service_fee"`
	ServiceFeeIncVAT      string  `json:"service_fee_inc_vat"`
	NumberOfLiters        string  `json:"number_of_liters"`
	Odo                   int     `json:"odo"`
	DelegateID            int     `json:"delegate_id"`
	DelegateName          string  `json:"delegate_name"`
	Date                  string  `json:"date"`
	Station               string  `json:"station"`
	StationBranch         string  `json:"station_branch"`
	Lat                   float64 `json:"lat"`
	Lng                   float64 `json:"lng"`
	VehiclePlateImage     string  `json:"vehicle_plate_image"`
	FuelPumpImage         string  `json:"fuel_pump_image"`
	OdometerImage         string  `json:"odometer_image"`
	VATPercent            string  `json:"vat_percent"`
	TotalBeforeVAT        string  `json:"total_before_vat"`
	VATAmount             string  `json:"vat_amount"`
	CustomerName          string  `json:"customer_name"`
	CustomerCommercialNo  string  `json:"customer_commercial_no"`
	CustomerTaxNo         string  `json:"customer_tax_no"`
	CustomerPostCode      string  `json:"customer_post_code"`
	StationName           string  `json:"station_name"`
	StationCommercialNo   string  `json:"station_commercial_no"`
	StationTaxNo          string  `json:"station_tax_no"`
	StationAddress        string  `json:"station_address"`
	Address               string  `json:"address"`
	SecondAddress         string  `json:"second_address"`
	Mobile                string  `json:"mobile"`
	Whatsapp              int64   `json:"whatsapp"`
	Email                 string  `json:"email"`
	PetroappVATNumber     string  `json:"petroapp_vat_number"`
	PetroappName          string  `json:"petroapp_name"`
	KilometersPerLiter    string  `json:"kilometers_per_liter"`
	LitersPer100Kilometer string  `json:"liters_per_100_kilometer"`
	FuelSourceTitle       string  `json:"fuel_source_title"`
	CompanyAddress        string  `json:"company_address"`
	PaymentStatus         string  `json:"payment_status"`
	IsSynced              bool    `json:"is_synced"`
}
