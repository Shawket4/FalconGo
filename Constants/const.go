package Constants

import (
	"os"
	"log"
	"github.com/joho/godotenv"
)

// const SMBPath = "/samba/main/"
const SMBPath = "./Temp/"

const WhatsappGoService = "http://localhost:3000"

const WhatsAppAlertNumber = "+201061856523"
const WhatsAppGPIDFuel = "120363420442360151@g.us"
const WhatsAppGPIDEtit = "120363418908976957@g.us"

var JWTSecret string

var PetroAppServiceURL string
var PetroAppServiceAPIKey string
func Init() {

	if err := godotenv.Load(".env"); err != nil {
		log.Printf("⚠️  Error loading .env file: %v", err)
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET environment variable is not set — refusing to start")
	}
	JWTSecret = secret

	PetroAppServiceURL = os.Getenv("PETROAPP_SERVICE_URL")
	PetroAppServiceAPIKey = os.Getenv("PETROAPP_SERVICE_API_KEY")
}
