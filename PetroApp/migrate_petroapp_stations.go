package PetroApp

import (
	"Falcon/Models"
	"encoding/json"
	"fmt"
	"os"
)

func MigrateStations() {
	data, err := os.ReadFile("./coordinates.json")
	if err != nil {
		panic(err)
	}
	var stations []Models.PetroAppStation
	err = json.Unmarshal(data, &stations)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(stations))
	if err := Models.DB.Model(&Models.PetroAppStation{}).Create(stations).Error; err != nil {
		panic(err)
	}
}
