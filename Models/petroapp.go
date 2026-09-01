package Models

import "gorm.io/gorm"

type PetroAppStation struct {
	gorm.Model
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}
