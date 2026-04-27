package Scrapper

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
)

const (
	baseURL = "https://etit-fms.etit-eg.com"
)

// ClientConfig holds configuration for creating authenticated clients
type ClientConfig struct {
	Username string
	Password string
}

// Authenticated client types that can be used by other packages
type AuthenticatedClients struct {
	HttpClient *http.Client
	Collector  *colly.Collector
}

// AuthenticityToken holds the CSRF token
type AuthenticityToken struct {
	Token string
}

// getToken retrieves the authentication token from the login page
func getToken(client *http.Client) (AuthenticityToken, error) {
	loginURL := baseURL

	response, err := client.Get(loginURL)
	if err != nil {
		return AuthenticityToken{}, fmt.Errorf("error fetching response: %w", err)
	}
	defer response.Body.Close()

	document, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return AuthenticityToken{}, fmt.Errorf("error loading HTTP response body: %w", err)
	}

	token, exists := document.Find("input[name='__VIEWSTATE']").Attr("value")
	if !exists {
		return AuthenticityToken{}, fmt.Errorf("could not find token in page")
	}

	authenticityToken := AuthenticityToken{
		Token: token,
	}

	return authenticityToken, nil
}

// Login creates an authenticated HTTP client and colly Collector
func Login(config ClientConfig) (*AuthenticatedClients, error) {
	// Create HTTP client with cookie jar and TLS configuration
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("error creating cookie jar: %w", err)
	}

	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	httpClient := &http.Client{
		Jar:       jar,
		Transport: customTransport,
	}

	// Get authentication token
	authenticityToken, err := getToken(httpClient)
	if err != nil {
		return nil, err
	}

	// Create a custom colly collector with modified transport
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	// Set a custom transport that ignores TLS errors
	collector.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		// Disable HTTP/2 which can cause issues with some servers
		ForceAttemptHTTP2: false,
	})

	// Use existing cookie jar
	collector.SetCookieJar(jar)

	// Login with HTTP client first to establish cookies
	loginURL := baseURL + "/"
	data := url.Values{
		"ScriptManager1":          {"UpdatePanel1|lg_AltairLogin$LoginButton"},
		"__EVENTTARGET":           {"lg_AltairLogin$LoginButton"},
		"__VIEWSTATE":             {authenticityToken.Token},
		"__VIEWSTATEGENERATOR":    {"0C2F32F0"},
		"lg_AltairLogin$UserName": {config.Username},
		"lg_AltairLogin$Password": {config.Password},
	}

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failed with status: %d", resp.StatusCode)
	}
	// The HTTP client is now authenticated with cookies - use these cookies for the collector
	log.Println("HTTP client successfully authenticated")

	// Try a test request with the collector to verify it works
	err = collector.Visit(baseURL + "/WebPages/UpdateTransportersData.aspx")
	if err != nil {
		log.Println("Warning: Collector test request failed:", err)
		log.Println("Continuing with HTTP client only...")
		// Continue anyway as we can still use the HTTP client
	} else {
		log.Println("Collector successfully authenticated")
	}

	return &AuthenticatedClients{
		HttpClient: httpClient,
		Collector:  collector,
	}, nil
}

// GetClients returns authenticated clients with credentials from config
func GetClients(username, password string) (*AuthenticatedClients, error) {
	config := ClientConfig{
		Username: username,
		Password: password,
	}

	return Login(config)
}
