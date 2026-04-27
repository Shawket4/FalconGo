package utils

import (
	"Falcon/Models"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
)

const SecretKey = "secret" // TODO: Move to environment variable

// CustomClaims extends JWT claims with user type and IDs
type CustomClaims struct {
	UserType   string `json:"user_type"`  // "admin_user" or "driver"
	UserID     uint   `json:"user_id"`    // User ID for admin users
	DriverID   uint   `json:"driver_id"`  // Driver ID for drivers
	Permission int    `json:"permission"` // Permission level for admin users
	jwt.RegisteredClaims
}

// GenerateAdminUserToken generates a JWT token for admin users
func GenerateAdminUserToken(user Models.User) (string, error) {
	claims := CustomClaims{
		UserType:   "admin_user",
		UserID:     user.Id,
		DriverID:   0,
		Permission: user.Permission,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    strconv.Itoa(int(user.Id)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24 * 31)), // 31 days
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(SecretKey))
}

// GenerateDriverToken generates a JWT token for drivers
func GenerateDriverToken(driverID uint) (string, error) {
	claims := CustomClaims{
		UserType: "driver",
		UserID:   0,
		DriverID: driverID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    strconv.Itoa(int(driverID)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24 * 365)), // 31 days
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(SecretKey))
}

// ParseToken parses and validates a JWT token
func ParseToken(tokenString string) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(SecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	// Check expiration
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("token has expired")
	}

	return claims, nil
}

// ValidateDriverToken validates a token and ensures it's a driver token
func ValidateDriverToken(tokenString string) (uint, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return 0, err
	}

	if claims.UserType != "driver" {
		return 0, errors.New("token is not a driver token")
	}

	if claims.DriverID == 0 {
		return 0, errors.New("invalid driver ID in token")
	}

	return claims.DriverID, nil
}

// ValidateAdminUserToken validates a token and ensures it's an admin user token
func ValidateAdminUserToken(tokenString string) (uint, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return 0, err
	}

	if claims.UserType != "admin_user" {
		return 0, errors.New("token is not an admin user token")
	}

	if claims.UserID == 0 {
		return 0, errors.New("invalid user ID in token")
	}

	return claims.UserID, nil
}

// GetTokenType returns the user type from a token without full validation
func GetTokenType(tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(SecretKey), nil
	})

	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	return claims.UserType, nil
}

func GetUserIDFromContext(c *fiber.Ctx) uint {
	user := c.Locals("user").(*Models.User)
	return user.Id
}

// GetUserFromContext extracts full user object from context
func GetUserFromContext(c *fiber.Ctx) *Models.User {
	return c.Locals("user").(*Models.User)
}
