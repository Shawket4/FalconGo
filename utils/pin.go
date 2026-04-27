package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// GenerateRandomPIN generates a random 4-digit PIN
func GenerateRandomPIN() (string, error) {
	// Generate random number between 1000 and 9999
	n, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return "", err
	}

	pin := fmt.Sprintf("%04d", n.Int64()+1000)
	return pin, nil
}

// HashPIN hashes a PIN using bcrypt
func HashPIN(pin string) string {
	hashedPIN, _ := bcrypt.GenerateFromPassword([]byte(pin), 1)
	return string(hashedPIN)
}

// ValidatePIN compares a plain PIN with a hashed PIN
func ValidatePIN(plainPIN string, hashedPIN string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPIN), []byte(plainPIN))
	return err == nil
}
