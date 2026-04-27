// Services/PaySlipAuditService.go - UPDATE GetAuditTrail
package services

import (
	"Falcon/Models"
	"encoding/json"

	"gorm.io/gorm"
)

type PaySlipAuditService struct {
	DB *gorm.DB
}

// LogAction creates an audit entry for a pay slip action
func (pas *PaySlipAuditService) LogAction(paySlipID uint, action string, changedBy uint, changeDetails interface{}, ipAddress, userAgent string) error {
	detailsJSON, _ := json.Marshal(changeDetails)

	audit := Models.PaySlipAudit{
		PaySlipID:     paySlipID,
		Action:        action,
		ChangedBy:     changedBy,
		ChangeDetails: string(detailsJSON),
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
	}

	return pas.DB.Create(&audit).Error
}

// Services/PaySlipAuditService.go - UPDATE GetAuditTrail
func (pas *PaySlipAuditService) GetAuditTrail(paySlipID uint) ([]Models.PaySlipAudit, error) {
	var audits []Models.PaySlipAudit
	err := pas.DB.Where("pay_slip_id = ?", paySlipID).
		Order("created_at DESC").
		Find(&audits).Error

	if err != nil {
		return nil, err
	}

	// Manually load users for each audit
	for i := range audits {
		if audits[i].ChangedBy > 0 {
			var user Models.User
			if pas.DB.First(&user, audits[i].ChangedBy).Error == nil {
				audits[i].User = &user
			}
		}
	}

	return audits, nil
}
