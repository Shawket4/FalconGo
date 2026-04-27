package ETA

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
)

// Document represents the structure of each invoice document
type Document struct {
	PublicURL    string       `json:"publicUrl"`
	FreezeStatus FreezeStatus `json:"freezeStatus"`
	Index        string       `json:"index"`
	Type         string       `json:"type"`
	ID           string       `json:"id"`
	Score        interface{}  `json:"score"`
	Source       Source       `json:"source"`
	Highlight    Highlight    `json:"highlight"`
	Sorts        interface{}  `json:"sorts"`
}

type FreezeStatus struct {
	Frozen     bool        `json:"frozen"`
	Type       interface{} `json:"type"`
	Scope      interface{} `json:"scope"`
	ActionDate interface{} `json:"actionDate"`
	AuCode     interface{} `json:"auCode"`
	AuName     interface{} `json:"auName"`
}

type Source struct {
	UUID                        string      `json:"uuid"`
	SubmissionUUID              string      `json:"submissionUuid"`
	LongID                      string      `json:"longId"`
	InternalID                  string      `json:"internalId"`
	IssuerUserID                string      `json:"issuerUserId"`
	DocumentType                string      `json:"documentType"`
	DocumentTypeNameEn          string      `json:"documentTypeNameEn"`
	DocumentTypeNameAr          string      `json:"documentTypeNameAr"`
	DocumentTypeVersion         string      `json:"documentTypeVersion"`
	SubmissionDate              string      `json:"submissionDate"`
	IssueDate                   string      `json:"issueDate"`
	LastModifiedDateTimeUtc     string      `json:"lastModifiedDateTimeUtc"`
	CreationDateTimeUtc         string      `json:"creationDateTimeUtc"`
	SubmitterID                 string      `json:"submitterId"`
	SubmitterName               string      `json:"submitterName"`
	SubmitterBranchNo           string      `json:"submitterBranchNo"`
	IssuerTaxPayerID            string      `json:"issuerTaxPayerId"`
	IssuerType                  string      `json:"issuerType"`
	RecipientType               string      `json:"recipientType"`
	RecipientTypeNameEN         string      `json:"recipientTypeNameEN"`
	RecipientTypeNameAR         string      `json:"recipientTypeNameAR"`
	RecipientTaxPayerID         string      `json:"recipientTaxPayerId"`
	RecipientID                 string      `json:"recipientId"`
	RecipientName               string      `json:"recipientName"`
	IntermediaryRIN             interface{} `json:"intermediaryRIN"`
	IntermediaryName            interface{} `json:"intermediaryName"`
	DocumentStatusEN            string      `json:"documentStatusEN"`
	DocumentStatusAR            string      `json:"documentStatusAR"`
	TotalInvoiceAmount          float64     `json:"totalInvoiceAmount"`
	CancelRequestDate           interface{} `json:"cancelRequestDate"`
	RejectRequestDate           interface{} `json:"rejectRequestDate"`
	CancelRequestDelayedDate    interface{} `json:"cancelRequestDelayedDate"`
	RejectRequestDelayedDate    interface{} `json:"rejectRequestDelayedDate"`
	DeclineCancelRequestDate    interface{} `json:"declineCancelRequestDate"`
	DeclineRejectRequestDate    interface{} `json:"declineRejectRequestDate"`
	TotalSales                  float64     `json:"totalSales"`
	TotalDiscount               float64     `json:"totalDiscount"`
	NetAmount                   float64     `json:"netAmount"`
	MaxPrecision                int         `json:"maxPercision"`
	DocumentStatusReason        string      `json:"documentStatusReason"`
	CreatedByUserID             string      `json:"createdByUserId"`
	SubmissionChannel           string      `json:"submissionChannel"`
	FreezeType                  interface{} `json:"freezeType"`
	FreezeScope                 interface{} `json:"freezeScope"`
	FreezeDateTimeUTC           interface{} `json:"freezeDateTimeUTC"`
	UnfreezeDateTimeUTC         interface{} `json:"unfreezeDateTimeUTC"`
	AccountingUnitCode          interface{} `json:"accountingUnitCode"`
	AccountingUnitName          interface{} `json:"accountingUnitName"`
	LateSubmissionRequestNumber interface{} `json:"lateSubmissionRequestNumber"`
	SvcDlvDate                  interface{} `json:"svcDlvDate"`
	ActivityCode                string      `json:"activityCode"`
	TotalItemsDiscountAmount    float64     `json:"totalItemsDiscountAmount"`
	ExtraDiscountAmount         float64     `json:"extraDiscountAmount"`
	TotalValueDifference        float64     `json:"totalValueDifference"`
	TotalTaxableFees            float64     `json:"totalTaxableFees"`
	TotalNonTaxableFees         float64     `json:"totalNonTaxableFees"`
	NetWeight                   interface{} `json:"netWeight"`
	TaxTotals                   []TaxTotal  `json:"taxTotals"`
	LineItems                   []LineItem  `json:"lineItems"`
	Currencies                  []Currency  `json:"currencies"`
}

type TaxTotal struct {
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
}

type LineItem struct {
	Type           string      `json:"type"`
	Code           string      `json:"code"`
	Sales          float64     `json:"sales"`
	ValueDiff      float64     `json:"valueDiff"`
	TotalTaxes     float64     `json:"totalTaxes"`
	Quantity       float64     `json:"quantity"`
	UnitType       string      `json:"unitType"`
	DiscountAmount float64     `json:"discountAmount"`
	NetTotal       float64     `json:"netTotal"`
	ItemsDiscount  float64     `json:"itemsDiscount"`
	WeightUnitType interface{} `json:"weightUnitType"`
	WeightQuantity float64     `json:"weightQuantity"`
	Total          float64     `json:"total"`
	UnitValue      UnitValue   `json:"unitValue"`
	Taxes          []Tax       `json:"taxes"`
}

type UnitValue struct {
	Currency string  `json:"currency"`
	Sold     float64 `json:"sold"`
	EgpValue float64 `json:"egpValue"`
}

type Tax struct {
	Type    string  `json:"type"`
	Amount  float64 `json:"amount"`
	SubType string  `json:"subType"`
}

type Currency struct {
	Sold string  `json:"sold"`
	Rate float64 `json:"rate"`
}

type Highlight struct {
	RecipientID []string `json:"recipientId"`
}

type Metadata struct {
	TotalPages int     `json:"totalPages"`
	TotalCount int     `json:"totalCount"`
	MaxScore   float64 `json:"maxScore"`
}

type InvoiceData struct {
	Result   []Document `json:"result"`
	Metadata Metadata   `json:"metadata"`
}

type ConversionResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	InvoiceCount int    `json:"invoiceCount"`
	TotalCount   int    `json:"totalCount"`
	TotalPages   int    `json:"totalPages"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// convertJSONToExcel converts invoice JSON data to Excel format
func convertJSONToExcel(invoiceData *InvoiceData) (*bytes.Buffer, error) {
	// Create a new Excel file
	f := excelize.NewFile()

	// Create a new sheet named "Invoices"
	sheetName := "Invoices"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("error creating sheet: %v", err)
	}

	// Set the active sheet
	f.SetActiveSheet(index)

	// Define headers
	headers := []string{
		"Document UUID", "Internal ID", "Document Type", "Document Type (EN)",
		"Document Status (EN)", "Submission Date", "Issue Date", "Submitter Name",
		"Recipient Name", "Recipient ID", "Total Invoice Amount", "Total Sales",
		"Total Discount", "Net Amount", "Tax Amount", "Activity Code",
		"Submission Channel", "Public URL",
	}

	// Set headers in the first row
	for i, header := range headers {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, header)
	}

	// Style the header row
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#E6E6FA"},
			Pattern: 1,
		},
	})
	if err == nil {
		f.SetRowStyle(sheetName, 1, 1, headerStyle)
	}

	// Populate data rows
	for rowIndex, doc := range invoiceData.Result {
		row := rowIndex + 2 // Start from row 2 (after headers)

		// Parse dates for better formatting
		submissionDate, _ := time.Parse(time.RFC3339, doc.Source.SubmissionDate)
		issueDate, _ := time.Parse(time.RFC3339, doc.Source.IssueDate)

		// Calculate total tax amount
		var totalTaxAmount float64
		for _, tax := range doc.Source.TaxTotals {
			totalTaxAmount += tax.Amount
		}

		// Set cell values
		values := []interface{}{
			doc.Source.UUID,
			doc.Source.InternalID,
			doc.Source.DocumentType,
			doc.Source.DocumentTypeNameEn,
			doc.Source.DocumentStatusEN,
			submissionDate.Format("2006-01-02 15:04:05"),
			issueDate.Format("2006-01-02 15:04:05"),
			doc.Source.SubmitterName,
			doc.Source.RecipientName,
			doc.Source.RecipientID,
			doc.Source.TotalInvoiceAmount,
			doc.Source.TotalSales,
			doc.Source.TotalDiscount,
			doc.Source.NetAmount,
			totalTaxAmount,
			doc.Source.ActivityCode,
			doc.Source.SubmissionChannel,
			doc.PublicURL,
		}

		for colIndex, value := range values {
			cell := fmt.Sprintf("%c%d", 'A'+colIndex, row)
			f.SetCellValue(sheetName, cell, value)
		}
	}

	// Auto-fit columns
	for i := 0; i < len(headers); i++ {
		f.SetColWidth(sheetName, string('A'+rune(i)), string('A'+rune(i)), 15)
	}

	// Delete the default sheet if it exists and is not our target sheet
	if f.GetSheetName(0) != sheetName {
		f.DeleteSheet("Sheet1")
	}

	// Write to buffer
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("error writing Excel file to buffer: %v", err)
	}

	return &buf, nil
}

// Convert JSON file to Excel
func ConvertToExcel(c *fiber.Ctx) error {
	// Get the uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(ErrorResponse{
			Success: false,
			Error:   "No file provided. Please upload a JSON file.",
		})
	}

	// Check file extension
	fileExt := strings.ToLower(filepath.Ext(file.Filename))
	contentType := file.Header.Get("Content-Type")

	if fileExt != ".json" && contentType != "application/json" {
		return c.Status(400).JSON(ErrorResponse{
			Success: false,
			Error:   "Invalid file type. Please upload a JSON file.",
		})
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return c.Status(500).JSON(ErrorResponse{
			Success: false,
			Error:   "Failed to open uploaded file.",
		})
	}
	defer src.Close()

	// Read file content
	var invoiceData InvoiceData
	if err := json.NewDecoder(src).Decode(&invoiceData); err != nil {
		return c.Status(400).JSON(ErrorResponse{
			Success: false,
			Error:   "Invalid JSON format. Please check your file structure.",
		})
	}

	// Validate that we have invoice data
	if len(invoiceData.Result) == 0 {
		return c.Status(400).JSON(ErrorResponse{
			Success: false,
			Error:   "No invoice data found in the JSON file.",
		})
	}

	// Convert to Excel
	excelBuffer, err := convertJSONToExcel(&invoiceData)
	if err != nil {
		return c.Status(500).JSON(ErrorResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to convert to Excel: %v", err),
		})
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("invoices_export_%s.xlsx", timestamp)

	// Set headers for file download
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Set("Content-Length", fmt.Sprintf("%d", excelBuffer.Len()))

	// Return the Excel file
	return c.Send(excelBuffer.Bytes())
}

// Convert JSON data directly (via POST body)
func convertJSONData(c *fiber.Ctx) error {
	var invoiceData InvoiceData

	// Parse JSON from request body
	if err := c.BodyParser(&invoiceData); err != nil {
		return c.Status(400).JSON(ErrorResponse{
			Success: false,
			Error:   "Invalid JSON format in request body.",
		})
	}

	// Validate that we have invoice data
	if len(invoiceData.Result) == 0 {
		return c.Status(400).JSON(ErrorResponse{
			Success: false,
			Error:   "No invoice data found in the request.",
		})
	}

	// Convert to Excel
	excelBuffer, err := convertJSONToExcel(&invoiceData)
	if err != nil {
		return c.Status(500).JSON(ErrorResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to convert to Excel: %v", err),
		})
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("invoices_export_%s.xlsx", timestamp)

	// Set headers for file download
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Set("Content-Length", fmt.Sprintf("%d", excelBuffer.Len()))

	// Return the Excel file
	return c.Send(excelBuffer.Bytes())
}
