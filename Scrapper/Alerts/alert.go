package Alerts

import (
	"Falcon/Models"
	"log"
)

func StoreUniqueAlerts(alerts []Models.SpeedAlert) error {
	if err := Models.DB.Model(&Models.SpeedAlert{}).Create(&alerts).Error; err != nil {
		log.Println(err)
		return err
	}
	return ProcessAlertsWithHighestExceed(alerts, Models.DB)
}
