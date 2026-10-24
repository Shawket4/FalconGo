package Controllers

import (
	"Falcon/Models"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const (
	aiAuditDefaultLimit = 100
	aiAuditMaxLimit     = 500
)

// parseAiAuditLimit reads ?limit=: absent means the default, above the
// maximum is clamped, and anything that is not a positive integer is refused.
func parseAiAuditLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return aiAuditDefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errors.New("limit must be a positive integer")
	}
	if n > aiAuditMaxLimit {
		n = aiAuditMaxLimit
	}
	return n, nil
}

// ListAiAuditLogs returns the most recent writes applied through the AI
// advisor, newest first, optionally narrowed to one suggestion.
// GET /api/ai-audit?suggestion_id=&limit=
func ListAiAuditLogs(c *fiber.Ctx) error {
	limit, err := parseAiAuditLimit(c.Query("limit"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": err.Error(),
		})
	}

	query := Models.DB.Model(&Models.AiAuditLog{})
	if sid := strings.TrimSpace(c.Query("suggestion_id")); sid != "" {
		query = query.Where("suggestion_id = ?", sid)
	}

	logs := []Models.AiAuditLog{}
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&logs).Error; err != nil {
		log.Println(err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to fetch AI audit logs",
		})
	}

	return c.JSON(fiber.Map{
		"logs":  logs,
		"count": len(logs),
		"limit": limit,
	})
}
