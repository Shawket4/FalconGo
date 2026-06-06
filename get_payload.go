package main

import (
	"encoding/json"
	"fmt"
	"log"
	"Falcon/Models"
)

func main() {
	Models.Connect()
	var record Models.PetroAppRecord
	if err := Models.DB.First(&record).Error; err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(record, "", "  ")
	fmt.Println(string(b))
}
