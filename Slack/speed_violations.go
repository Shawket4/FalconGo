package Slack

import (
	"Falcon/Models"
	"Falcon/Observability"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ParsedViolation struct {
	PlateNumber       string
	ViolationLocation string
	ViolationDate     time.Time
	FineAmount        float64
	RawMessage        string
}

func ParseViolationMessages(text string) ([]ParsedViolation, error) {
	log.Printf("🔍 ========== INTELLIGENT PARSING STARTING ==========")
	log.Printf("📄 Original text length: %d characters", len(text))

	// STEP 1: Aggressive text cleaning and normalization
	cleanedText := intelligentTextCleaning(text)

	// STEP 2: Try multiple strategies to find violation boundaries
	violationSegments := extractViolationSegments(cleanedText)

	if len(violationSegments) == 0 {
		log.Printf("❌ No violation segments found")
		return nil, fmt.Errorf("no violations found in message")
	}

	log.Printf("✅ Found %d potential violation segments", len(violationSegments))

	// STEP 3: Parse each segment with error recovery
	violations := make([]ParsedViolation, 0)

	for i, segment := range violationSegments {
		log.Printf("\n🔍 ========== PROCESSING SEGMENT %d/%d ==========", i+1, len(violationSegments))
		log.Printf("📝 Segment length: %d chars", len(segment))
		log.Printf("📝 Preview: %s...", segment[:minInt(150, len(segment))])

		// Validate segment has minimum required components
		if !isValidViolationSegment(segment) {
			log.Printf("⚠️  Segment %d failed validation, skipping", i+1)
			continue
		}

		violation, err := parseSingleViolationRobust(segment)
		if err != nil {
			log.Printf("❌ Failed to parse segment %d: %v", i+1, err)
			continue
		}

		log.Printf("✅ Successfully parsed violation %d: Plate=%s, Date=%s, Fine=%.2f, Location=%s",
			i+1, violation.PlateNumber, violation.ViolationDate.Format("2006-01-02"),
			violation.FineAmount, violation.ViolationLocation)

		violations = append(violations, *violation)
	}

	if len(violations) == 0 {
		return nil, fmt.Errorf("could not parse any violations from message")
	}

	log.Printf("\n✅ ========== SUCCESSFULLY PARSED %d VIOLATIONS ==========\n", len(violations))
	return violations, nil
}

// intelligentTextCleaning performs aggressive text normalization
func intelligentTextCleaning(text string) string {
	log.Printf("🧹 Starting intelligent text cleaning...")

	cleaned := text

	// 1. Remove all URL tags and clean up concatenated URLs
	// Handle: <https://ppo.gov.eg/trالنيابة> -> النيابة
	cleaned = regexp.MustCompile(`<https?://[^>]*?tr([\p{Arabic}]+)>\s*`).ReplaceAllString(cleaned, "\n$1\n")
	cleaned = regexp.MustCompile(`<https?://[^>]+>`).ReplaceAllString(cleaned, "")
	cleaned = regexp.MustCompile(`https?://[^\s]+`).ReplaceAllString(cleaned, "")

	// 2. Normalize whitespace but preserve line breaks
	cleaned = regexp.MustCompile(`[ \t]+`).ReplaceAllString(cleaned, " ")
	cleaned = regexp.MustCompile(`\n{3,}`).ReplaceAllString(cleaned, "\n\n")

	// 3. Remove zero-width characters and RTL/LTR marks
	cleaned = strings.ReplaceAll(cleaned, "\u200B", "") // Zero-width space
	cleaned = strings.ReplaceAll(cleaned, "\u200C", "") // Zero-width non-joiner
	cleaned = strings.ReplaceAll(cleaned, "\u200D", "") // Zero-width joiner
	cleaned = strings.ReplaceAll(cleaned, "\u200E", "") // Left-to-right mark
	cleaned = strings.ReplaceAll(cleaned, "\u200F", "") // Right-to-left mark
	cleaned = strings.ReplaceAll(cleaned, "\uFEFF", "") // Zero-width no-break space (BOM)

	// 4. Normalize Arabic characters
	// Handle different forms of the same letter
	cleaned = strings.ReplaceAll(cleaned, "أ", "ا")
	cleaned = strings.ReplaceAll(cleaned, "إ", "ا")
	cleaned = strings.ReplaceAll(cleaned, "آ", "ا")
	cleaned = strings.ReplaceAll(cleaned, "ة", "ه")
	cleaned = strings.ReplaceAll(cleaned, "ى", "ي")

	// 5. Fix common OCR/typing errors in header
	cleaned = strings.ReplaceAll(cleaned, "النيابه العامه", "النيابة العامة")
	cleaned = strings.ReplaceAll(cleaned, "النيابه العامة", "النيابة العامة")
	cleaned = strings.ReplaceAll(cleaned, "النيابة العامه", "النيابة العامة")

	log.Printf("✅ Text cleaning complete")
	return cleaned
}

// extractViolationSegments uses multiple strategies to find violation boundaries
func extractViolationSegments(text string) []string {
	log.Printf("🔍 Extracting violation segments using multiple strategies...")

	var segments []string

	// Strategy 1: Split by primary header marker
	segments = tryExtractByMarker(text, "النيابة العامة")
	if len(segments) > 0 {
		log.Printf("✅ Strategy 1 (primary marker) found %d segments", len(segments))
		return segments
	}

	// Strategy 2: Split by secondary markers (if header is partial)
	segments = tryExtractByMarker(text, "عزيزي المواطن")
	if len(segments) > 0 {
		log.Printf("✅ Strategy 2 (secondary marker) found %d segments", len(segments))
		return segments
	}

	// Strategy 3: Split by plate number pattern (رقم:)
	segments = tryExtractByPattern(text, `تم تسجيل مخالفة`)
	if len(segments) > 0 {
		log.Printf("✅ Strategy 3 (violation registration) found %d segments", len(segments))
		return segments
	}

	// Strategy 4: Look for date patterns as boundaries
	segments = tryExtractByDateBoundaries(text)
	if len(segments) > 0 {
		log.Printf("✅ Strategy 4 (date boundaries) found %d segments", len(segments))
		return segments
	}

	// Strategy 5: Fallback - treat entire message as one violation
	log.Printf("⚠️  All strategies failed, treating as single violation")
	return []string{text}
}

// tryExtractByMarker attempts to split text by a specific marker
func tryExtractByMarker(text string, marker string) []string {
	if !strings.Contains(text, marker) {
		return nil
	}

	parts := strings.Split(text, marker)
	if len(parts) <= 1 {
		return nil
	}

	segments := make([]string, 0)
	for i := 1; i < len(parts); i++ {
		segment := marker + parts[i]
		// Only include segments that look like violations
		if len(segment) > 100 { // Minimum reasonable length
			segments = append(segments, segment)
		}
	}

	return segments
}

// tryExtractByPattern attempts to split by regex pattern
func tryExtractByPattern(text string, pattern string) []string {
	re := regexp.MustCompile(pattern)
	indices := re.FindAllStringIndex(text, -1)

	if len(indices) == 0 {
		return nil
	}

	segments := make([]string, 0)
	for i, match := range indices {
		start := match[0]
		end := len(text)
		if i < len(indices)-1 {
			end = indices[i+1][0]
		}
		segment := text[start:end]
		if len(segment) > 100 {
			segments = append(segments, segment)
		}
	}

	return segments
}

// tryExtractByDateBoundaries uses date patterns to find boundaries
func tryExtractByDateBoundaries(text string) []string {
	// Find all dates in format YYYY-MM-DD
	datePattern := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	dates := datePattern.FindAllStringIndex(text, -1)

	if len(dates) <= 1 {
		return nil
	}

	// Use dates as rough boundaries, extending backwards to capture context
	segments := make([]string, 0)
	for i, dateMatch := range dates {
		// Start from 300 chars before the date (or beginning of text)
		start := maxInt(0, dateMatch[0]-300)

		// End at next date or end of text
		end := len(text)
		if i < len(dates)-1 {
			end = maxInt(dateMatch[1], dates[i+1][0]-200)
		}

		segment := text[start:end]
		if len(segment) > 100 {
			segments = append(segments, segment)
		}
	}

	return segments
}

// isValidViolationSegment checks if a segment has minimum required components
func isValidViolationSegment(segment string) bool {
	// Check for presence of key components (at least 3 out of 5)
	checks := []bool{
		strings.Contains(segment, "رقم") || strings.Contains(segment, "اللوحة"),
		strings.Contains(segment, "مكان") || strings.Contains(segment, "المخالفة"),
		strings.Contains(segment, "بتاريخ") || regexp.MustCompile(`\d{4}-\d{2}-\d{2}`).MatchString(segment),
		strings.Contains(segment, "بقيمه") || strings.Contains(segment, "بقيمة"),
		len(segment) > 100 && len(segment) < 5000, // Reasonable length
	}

	validCount := 0
	for _, check := range checks {
		if check {
			validCount++
		}
	}

	isValid := validCount >= 3
	log.Printf("  Validation: %d/5 checks passed -> %v", validCount, isValid)
	return isValid
}

// parseSingleViolationRobust is a more forgiving version of parseSingleViolation
func parseSingleViolationRobust(text string) (*ParsedViolation, error) {
	violation := &ParsedViolation{RawMessage: text}

	// Extract with multiple fallback attempts
	violation.PlateNumber = extractPlateNumberRobust(text)
	violation.ViolationLocation = extractLocationRobust(text)
	violation.ViolationDate = extractDateRobust(text)
	violation.FineAmount = extractFineAmountRobust(text)

	log.Printf("🔍 Extraction results:")
	log.Printf("  Plate: '%s'", violation.PlateNumber)
	log.Printf("  Location: '%s'", violation.ViolationLocation)
	log.Printf("  Date: '%s'", violation.ViolationDate.Format("2006-01-02"))
	log.Printf("  Fine: %.2f", violation.FineAmount)

	// Validate critical fields
	if violation.PlateNumber == "" {
		return nil, fmt.Errorf("plate number not found")
	}
	if violation.ViolationDate.IsZero() {
		return nil, fmt.Errorf("violation date not found")
	}
	if violation.FineAmount == 0 {
		return nil, fmt.Errorf("fine amount not found")
	}

	return violation, nil
}

// extractPlateNumberRobust uses multiple strategies to find plate number
func extractPlateNumberRobust(text string) string {
	// Try existing function first
	if plate := extractPlateNumber(text); plate != "" {
		return plate
	}

	// Fallback: Look for any Arabic letters + 4 digits pattern anywhere
	pattern := regexp.MustCompile(`[\p{Arabic}]{3,4}\d{4}`)
	if matches := pattern.FindAllString(text, -1); len(matches) > 0 {
		for _, match := range matches {
			if isValidPlateCandidate(match) {
				return NormalizePlateNumber(match)
			}
		}
	}

	return ""
}

// extractLocationRobust uses multiple strategies to find location
func extractLocationRobust(text string) string {
	// Try existing function first
	if location := extractLocation(text); location != "Unknown Location" {
		return location
	}

	// Fallback: Look for common location keywords
	locationKeywords := []string{"طريق", "شارع", "كوبري", "جسر", "محور", "الطريق الدائري", "ميدان"}
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		for _, keyword := range locationKeywords {
			if strings.Contains(line, keyword) && len(line) > 10 && len(line) < 200 {
				// Clean up the line
				line = regexp.MustCompile(`^[^:]*:`).ReplaceAllString(line, "")
				line = strings.TrimSpace(line)
				if line != "" {
					return line
				}
			}
		}
	}

	return "Unknown Location"
}

// extractDateRobust uses multiple strategies to find date
func extractDateRobust(text string) time.Time {
	// Try existing function first
	if date := extractDate(text); !date.IsZero() {
		return date
	}

	// Fallback: Try other date formats
	patterns := []string{
		`(\d{4})/(\d{2})/(\d{2})`,
		`(\d{2})-(\d{2})-(\d{4})`,
		`(\d{2})/(\d{2})/(\d{4})`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 3 {
			// Try to parse various formats
			dateStr := fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
			if date, err := time.Parse("2006-01-02", dateStr); err == nil {
				return date
			}
			dateStr = fmt.Sprintf("%s-%s-%s", matches[3], matches[2], matches[1])
			if date, err := time.Parse("2006-01-02", dateStr); err == nil {
				return date
			}
		}
	}

	return time.Time{}
}

// extractFineAmountRobust uses multiple strategies to find fine amount
func extractFineAmountRobust(text string) float64 {
	// Try existing function first
	if amount := extractFineAmount(text); amount > 0 {
		return amount
	}

	// Fallback: Look for any number that looks like a fine amount (100-10000 range)
	pattern := regexp.MustCompile(`(\d+\.?\d*)`)
	matches := pattern.FindAllString(text, -1)

	for _, match := range matches {
		if amount, err := strconv.ParseFloat(match, 64); err == nil {
			if amount >= 50 && amount <= 10000 { // Reasonable fine amount range
				return amount
			}
		}
	}

	return 0
}

// Helper functions
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func extractPlateNumber(text string) string {
	log.Printf("🔍 ========== PLATE EXTRACTION ==========")

	if strings.Contains(text, "رقم:") {
		parts := strings.Split(text, "رقم:")
		if len(parts) > 1 {
			afterMarker := parts[1]
			if idx := strings.Index(afterMarker, "\n"); idx > 0 {
				afterMarker = afterMarker[:idx]
			}
			if idx := strings.Index(afterMarker, "مكان"); idx > 0 {
				afterMarker = afterMarker[:idx]
			}
			extracted := strings.TrimSpace(afterMarker)
			log.Printf("🔍 After 'رقم:': '%s'", extracted)
			if plate := findPlateInString(extracted); plate != "" {
				log.Printf("🎯 SUCCESS: Found plate '%s'", plate)
				return plate
			}
		}
	}

	patterns := []string{
		`رقم:([^\n]*?)(?:\n|مكان|$)`,
		`اللوحة\s*المعدنية\s*رقم:([^\n]*?)(?:\n|مكان|$)`,
	}
	for i, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			captured := strings.TrimSpace(matches[1])
			log.Printf("🔍 Pattern %d captured: '%s'", i+1, captured)
			if plate := findPlateInString(captured); plate != "" {
				return plate
			}
		}
	}

	cleanText := strings.ReplaceAll(text, "\n", " ")
	cleanText = regexp.MustCompile(`\s+`).ReplaceAllString(cleanText, " ")
	if plate := findPlateInString(cleanText); plate != "" {
		return plate
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if plate := findPlateInString(line); plate != "" {
			return plate
		}
	}

	log.Printf("❌ PLATE EXTRACTION FAILED")
	return ""
}

func findPlateInString(s string) string {
	patterns := []string{
		`([ء-ي]{3,4}\d{4})`,
		`([ء-ي]\s*[ء-ي]\s*[ء-ي]\s*[ء-ي]?\s*\d\s*\d\s*\d\s*\d)`,
		`([\p{Arabic}]{3,4}\d{4})`,
		`([\p{Arabic}]\s*[\p{Arabic}]\s*[\p{Arabic}]\s*[\p{Arabic}]?\s*\d\s*\d\s*\d\s*\d)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllString(s, -1)
		for _, match := range matches {
			candidate := strings.TrimSpace(match)
			if isValidPlateCandidate(candidate) {
				normalized := NormalizePlateNumber(candidate)
				log.Printf("  ✅ Valid plate: '%s' -> '%s'", candidate, normalized)
				return normalized
			}
		}
	}

	tokens := strings.Fields(s)
	for _, token := range tokens {
		cleaned := regexp.MustCompile(`[^\p{Arabic}0-9]`).ReplaceAllString(token, "")
		if cleaned != "" && isValidPlateCandidate(cleaned) {
			return NormalizePlateNumber(cleaned)
		}
	}

	arabicParts := regexp.MustCompile(`[\p{Arabic}]+`).FindAllString(s, -1)
	digitParts := regexp.MustCompile(`\d+`).FindAllString(s, -1)
	if len(arabicParts) > 0 && len(digitParts) > 0 {
		for _, arab := range arabicParts {
			for _, digit := range digitParts {
				combined := arab + digit
				if isValidPlateCandidate(combined) {
					return NormalizePlateNumber(combined)
				}
			}
		}
	}
	return ""
}

func isValidPlateCandidate(s string) bool {
	cleaned := regexp.MustCompile(`[^\p{Arabic}0-9]`).ReplaceAllString(s, "")
	arabicMatches := regexp.MustCompile(`\p{Arabic}`).FindAllString(cleaned, -1)
	arabicCount := len(arabicMatches)
	digitMatches := regexp.MustCompile(`\d`).FindAllString(cleaned, -1)
	digitCount := len(digitMatches)
	return arabicCount >= 3 && arabicCount <= 4 && digitCount == 4
}

func NormalizePlateNumber(plate string) string {
	normalized := strings.ReplaceAll(plate, " ", "")
	diacritics := []string{"ً", "ٌ", "ٍ", "َ", "ُ", "ِ", "ّ", "ْ", "ـ"}
	for _, d := range diacritics {
		normalized = strings.ReplaceAll(normalized, d, "")
	}
	normalized = regexp.MustCompile(`[^\p{Arabic}0-9]`).ReplaceAllString(normalized, "")
	return strings.ToUpper(normalized)
}

func extractLocation(text string) string {
	log.Printf("🔍 ========== LOCATION EXTRACTION ==========")
	log.Printf("🔍 Input text length: %d chars", len(text))

	// Split into lines for line-by-line processing
	lines := strings.Split(text, "\n")
	log.Printf("🔍 Total lines: %d", len(lines))

	// First pass: Look for exact line containing مكان المخالفة or مكانالمخالفة
	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		log.Printf("🔍 Line %d: '%s'", i, trimmedLine)

		// Check if this line contains the location marker
		if strings.Contains(trimmedLine, "مكان") && strings.Contains(trimmedLine, ":") {
			log.Printf("✅ Found مكان marker in line %d", i)

			// Extract everything after the colon
			colonIdx := strings.Index(trimmedLine, ":")
			if colonIdx != -1 && colonIdx < len(trimmedLine)-1 {
				location := strings.TrimSpace(trimmedLine[colonIdx+1:])

				// Clean up the location
				location = strings.TrimRight(location, ": -")
				location = strings.TrimSpace(location)

				log.Printf("🔍 Extracted from line: '%s'", location)

				// Validate: must have actual content, not be a URL, reasonable length
				if location != "" &&
					len(location) > 5 &&
					len(location) < 300 &&
					!strings.Contains(location, "http") &&
					!strings.Contains(location, "//") &&
					!strings.Contains(location, "www.") {
					log.Printf("🎯 SUCCESS: Valid location found: '%s'", location)
					return location
				} else {
					log.Printf("⚠️  Validation failed: len=%d, has_slashes=%v",
						len(location), strings.Contains(location, "//"))
				}
			}
		}
	}

	// Second pass: Try regex patterns on the full text
	log.Printf("🔍 First pass failed, trying regex patterns...")

	patterns := []string{
		`مكان\s*المخالفة\s*:\s*([^\n]+)`,
		`مكانالمخالفة\s*:\s*([^\n]+)`,
		`مكان[^:]*:\s*([^\n]+)`,
	}

	for i, pattern := range patterns {
		log.Printf("🔍 Trying pattern %d: %s", i+1, pattern)
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			location := strings.TrimSpace(matches[1])

			// Clean up
			location = strings.TrimRight(location, ": -")
			location = strings.TrimSpace(location)

			log.Printf("🔍 Pattern %d captured: '%s'", i+1, location)

			// Validate
			if location != "" &&
				len(location) > 5 &&
				len(location) < 300 &&
				!strings.Contains(location, "http") &&
				!strings.Contains(location, "//") &&
				!strings.Contains(location, "www.") &&
				!strings.Contains(location, "ppo.gov") {
				log.Printf("🎯 SUCCESS: Valid location from regex: '%s'", location)
				return location
			}
		}
	}

	// Third pass: Look for Arabic text followed by numbers/dashes (typical of Egyptian locations)
	log.Printf("🔍 Trying location pattern matching...")

	// Look for lines that look like locations (contain طريق، شارع، الكيلو, etc.)
	locationKeywords := []string{"طريق", "شارع", "الكيلو", "كيلو", "اتجاه", "ميدان", "جسر", "كوبري"}

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Skip empty lines and lines with URLs
		if trimmedLine == "" || strings.Contains(trimmedLine, "http") || strings.Contains(trimmedLine, "//") {
			continue
		}

		// Check if line contains location keywords
		for _, keyword := range locationKeywords {
			if strings.Contains(trimmedLine, keyword) {
				log.Printf("🔍 Line %d contains location keyword '%s': '%s'", i, keyword, trimmedLine)

				// If this line has a colon, extract after it
				if colonIdx := strings.Index(trimmedLine, ":"); colonIdx != -1 {
					location := strings.TrimSpace(trimmedLine[colonIdx+1:])
					if len(location) > 5 {
						log.Printf("🎯 SUCCESS: Location from keyword search: '%s'", location)
						return location
					}
				} else if len(trimmedLine) > 10 {
					// The whole line might be the location
					log.Printf("🎯 SUCCESS: Whole line as location: '%s'", trimmedLine)
					return trimmedLine
				}
			}
		}
	}

	log.Printf("❌ LOCATION EXTRACTION FAILED - All methods exhausted")
	return "Unknown Location"
}

func extractDate(text string) time.Time {
	datePattern := regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
	if matches := datePattern.FindStringSubmatch(text); len(matches) > 3 {
		dateStr := fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
		if date, err := time.Parse("2006-01-02", dateStr); err == nil {
			return date
		}
	}
	return time.Time{}
}

func extractFineAmount(text string) float64 {
	patterns := []string{
		`بقيمه?\s*:?\s*(\d+(?:\.\d+)?)`,
		`بقيمة\s*:?\s*(\d+(?:\.\d+)?)`,
		`(\d+\.\d{2})`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			if amount, err := strconv.ParseFloat(matches[1], 64); err == nil && amount > 0 {
				return amount
			}
		}
	}
	return 0
}

func DetermineDriverAtViolationTime(car *Models.Car, violationTime time.Time) (*Models.DriverMatchResult, error) {
	if car == nil {
		return nil, fmt.Errorf("car is nil")
	}

	violationDateTime := time.Date(violationTime.Year(), violationTime.Month(), violationTime.Day(), 12, 0, 0, 0, violationTime.Location())
	startDate := violationDateTime.AddDate(0, 0, -1).Format("2006-01-02")
	endDate := violationDateTime.AddDate(0, 0, 1).Format("2006-01-02")

	var trips []Models.ParentTrip
	err := Models.DB.Where("car_id = ? AND date >= ? AND date <= ?", car.ID, startDate, endDate).
		Order("date ASC, created_at ASC").Find(&trips).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trips: %w", err)
	}

	if len(trips) == 0 {
		if car.DriverID == nil || *car.DriverID == 0 {
			return nil, fmt.Errorf("no driver assigned to car and no trips found")
		}
		return &Models.DriverMatchResult{
			DriverID: *car.DriverID, DriverName: car.Driver.Name, Confidence: 30.0,
			MatchMethod: "no_trips_found", Explanation: "No trips found near violation time",
		}, nil
	}

	if isSingleDriver(trips) {
		return &Models.DriverMatchResult{
			DriverID: trips[0].DriverID, DriverName: trips[0].DriverName, Confidence: 95.0,
			MatchMethod: "single_driver", Explanation: fmt.Sprintf("Only one driver (%s) found in trips", trips[0].DriverName),
		}, nil
	}

	var tripBefore, tripAfter *Models.ParentTrip
	var beforeGap, afterGap int
	violationDateStr := violationDateTime.Format("2006-01-02")

	for i := range trips {
		if trips[i].Date == violationDateStr {
			if tripBefore == nil {
				tripBefore = &trips[i]
			}
			tripAfter = &trips[i]
		} else if trips[i].Date < violationDateStr {
			tripBefore = &trips[i]
		} else if trips[i].Date > violationDateStr && tripAfter == nil {
			tripAfter = &trips[i]
		}
	}

	if tripBefore != nil {
		beforeDate, _ := time.Parse("2006-01-02", tripBefore.Date)
		beforeGap = int(violationDateTime.Sub(beforeDate).Minutes())
	}
	if tripAfter != nil {
		afterDate, _ := time.Parse("2006-01-02", tripAfter.Date)
		afterGap = int(afterDate.Sub(violationDateTime).Minutes())
	}

	if tripBefore != nil && tripAfter != nil && tripBefore.ID == tripAfter.ID {
		return &Models.DriverMatchResult{
			DriverID: tripBefore.DriverID, DriverName: tripBefore.DriverName, Confidence: 90.0,
			MatchMethod: "during_trip", TripBeforeID: &tripBefore.ID,
			TripBeforeGap: beforeGap, TripAfterGap: afterGap,
			Explanation: fmt.Sprintf("Violation during trip by %s", tripBefore.DriverName),
		}, nil
	}

	if tripBefore != nil && tripAfter != nil {
		if tripBefore.DriverID == tripAfter.DriverID {
			return &Models.DriverMatchResult{
				DriverID: tripBefore.DriverID, DriverName: tripBefore.DriverName, Confidence: 85.0,
				MatchMethod: "between_trips_same_driver", TripBeforeID: &tripBefore.ID, TripAfterID: &tripAfter.ID,
				TripBeforeGap: beforeGap, TripAfterGap: afterGap,
				Explanation: fmt.Sprintf("Same driver (%s) for trips before and after", tripBefore.DriverName),
			}, nil
		}
		if math.Abs(float64(beforeGap)) < math.Abs(float64(afterGap)) {
			return &Models.DriverMatchResult{
				DriverID: tripBefore.DriverID, DriverName: tripBefore.DriverName, Confidence: 60.0,
				MatchMethod: "between_trips_closer_before", TripBeforeID: &tripBefore.ID, TripAfterID: &tripAfter.ID,
				TripBeforeGap: beforeGap, TripAfterGap: afterGap,
				Explanation: fmt.Sprintf("Closer trip before by %s", tripBefore.DriverName),
			}, nil
		}
		return &Models.DriverMatchResult{
			DriverID: tripAfter.DriverID, DriverName: tripAfter.DriverName, Confidence: 60.0,
			MatchMethod: "between_trips_closer_after", TripBeforeID: &tripBefore.ID, TripAfterID: &tripAfter.ID,
			TripBeforeGap: beforeGap, TripAfterGap: afterGap,
			Explanation: fmt.Sprintf("Closer trip after by %s", tripAfter.DriverName),
		}, nil
	}

	if tripBefore != nil {
		return &Models.DriverMatchResult{
			DriverID: tripBefore.DriverID, DriverName: tripBefore.DriverName, Confidence: 70.0,
			MatchMethod: "only_before_trip", TripBeforeID: &tripBefore.ID, TripBeforeGap: beforeGap,
			Explanation: fmt.Sprintf("Only trip before found, driver: %s", tripBefore.DriverName),
		}, nil
	}

	if tripAfter != nil {
		return &Models.DriverMatchResult{
			DriverID: tripAfter.DriverID, DriverName: tripAfter.DriverName, Confidence: 65.0,
			MatchMethod: "only_after_trip", TripAfterID: &tripAfter.ID, TripAfterGap: afterGap,
			Explanation: fmt.Sprintf("Only trip after found, driver: %s", tripAfter.DriverName),
		}, nil
	}

	if car.DriverID == nil || *car.DriverID == 0 {
		return nil, fmt.Errorf("no driver assigned to car")
	}
	return &Models.DriverMatchResult{
		DriverID: *car.DriverID, DriverName: car.Driver.Name, Confidence: 40.0,
		MatchMethod: "fallback_assigned_driver", Explanation: "Using car's assigned driver",
	}, nil
}

func isSingleDriver(trips []Models.ParentTrip) bool {
	if len(trips) == 0 {
		return false
	}
	firstDriverID := trips[0].DriverID
	for _, trip := range trips[1:] {
		if trip.DriverID != firstDriverID {
			return false
		}
	}
	return true
}

func FindCarByPlate(plateNumber string) (*Models.Car, error) {
	normalizedInput := NormalizePlateNumber(plateNumber)
	log.Printf("🔍 Searching: '%s' (normalized: '%s')", plateNumber, normalizedInput)

	var car Models.Car
	err := Models.DB.Preload("Driver").Where("REPLACE(UPPER(car_no_plate), ' ', '') = ?", normalizedInput).First(&car).Error
	if err == nil {
		log.Printf("✅ Exact match: %s -> %s (ID: %d)", plateNumber, car.CarNoPlate, car.ID)
		return &car, nil
	}

	var allCars []Models.Car
	if err := Models.DB.Preload("Driver").Find(&allCars).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch cars: %w", err)
	}

	bestMatch := findBestPlateMatch(normalizedInput, allCars)
	if bestMatch != nil {
		similarity := calculatePlateSimilarity(normalizedInput, NormalizePlateNumber(bestMatch.CarNoPlate))
		log.Printf("✅ Fuzzy match: %s -> %s (ID: %d, %.0f%%)", plateNumber, bestMatch.CarNoPlate, bestMatch.ID, similarity*100)
		return bestMatch, nil
	}

	return nil, fmt.Errorf("car not found for plate: %s", plateNumber)
}

func findBestPlateMatch(inputPlate string, cars []Models.Car) *Models.Car {
	var bestMatch *Models.Car
	bestSimilarity := 0.0
	threshold := 0.75
	for i := range cars {
		carPlate := NormalizePlateNumber(cars[i].CarNoPlate)
		similarity := calculatePlateSimilarity(inputPlate, carPlate)
		if similarity > bestSimilarity && similarity >= threshold {
			bestSimilarity = similarity
			bestMatch = &cars[i]
		}
	}
	return bestMatch
}

func calculatePlateSimilarity(plate1, plate2 string) float64 {
	if plate1 == plate2 {
		return 1.0
	}
	levenshteinSim := 1.0 - (float64(levenshteinDistance(plate1, plate2)) / float64(maxInt(len(plate1), len(plate2))))
	letters1, numbers1 := extractLettersAndNumbers(plate1)
	letters2, numbers2 := extractLettersAndNumbers(plate2)

	lettersSim := 0.0
	if letters1 == letters2 {
		lettersSim = 1.0
	} else if len(letters1) > 0 && len(letters2) > 0 {
		lettersSim = 1.0 - (float64(levenshteinDistance(letters1, letters2)) / float64(maxInt(len(letters1), len(letters2))))
	}

	numbersSim := 0.0
	if numbers1 == numbers2 {
		numbersSim = 1.0
	} else if len(numbers1) > 0 && len(numbers2) > 0 {
		numbersSim = 1.0 - (float64(levenshteinDistance(numbers1, numbers2)) / float64(maxInt(len(numbers1), len(numbers2))))
	}

	componentSim := (lettersSim * 0.6) + (numbersSim * 0.4)
	return (levenshteinSim + componentSim) / 2.0
}

func extractLettersAndNumbers(plate string) (string, string) {
	var letters, numbers strings.Builder
	for _, char := range plate {
		if isArabicLetter(char) {
			letters.WriteRune(char)
		} else if char >= '0' && char <= '9' {
			numbers.WriteRune(char)
		}
	}
	return letters.String(), numbers.String()
}

func isArabicLetter(r rune) bool {
	return (r >= 0x0621 && r <= 0x064A) || (r >= 0x0660 && r <= 0x0669)
}

func levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)
	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}
	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			matrix[i][j] = minOfThree(matrix[i-1][j]+1, matrix[i][j-1]+1, matrix[i-1][j-1]+cost)
		}
	}
	return matrix[len1][len2]
}

func minOfThree(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func SaveViolationToDB(violation *ParsedViolation) error {
	car, err := FindCarByPlate(violation.PlateNumber)
	if err != nil {
		log.Printf("❌ Could not find car for plate %s: %v", violation.PlateNumber, err)
		return fmt.Errorf("car not found: %w", err)
	}

	driverMatch, err := DetermineDriverAtViolationTime(car, violation.ViolationDate)
	if err != nil {
		log.Printf("⚠️  Could not determine driver: %v", err)
		if car.DriverID == nil || *car.DriverID == 0 {
			return fmt.Errorf("cannot save violation: no driver could be determined")
		}
		driverMatch = &Models.DriverMatchResult{
			DriverID: *car.DriverID, DriverName: car.Driver.Name, Confidence: 50.0,
			MatchMethod: "car_assigned_driver", Explanation: "Fallback to car's assigned driver",
		}
	}

	// Convert int to *int for database fields
	var tripBeforeGap, tripAfterGap *int
	if driverMatch.TripBeforeGap != 0 {
		tripBeforeGap = &driverMatch.TripBeforeGap
	}
	if driverMatch.TripAfterGap != 0 {
		tripAfterGap = &driverMatch.TripAfterGap
	}

	speedViolation := Models.SpeedViolation{
		CarID:               car.ID,
		DriverID:            &driverMatch.DriverID,
		ViolationLocation:   violation.ViolationLocation,
		ViolationTime:       violation.ViolationDate,
		FineAmount:          violation.FineAmount,
		RawMessage:          violation.RawMessage,
		ProcessedAt:         time.Now(),
		DriverConfidence:    driverMatch.Confidence,
		NearestTripBeforeID: driverMatch.TripBeforeID,
		NearestTripAfterID:  driverMatch.TripAfterID,
		TripBeforeGap:       tripBeforeGap,
		TripAfterGap:        tripAfterGap,
		MatchMethod:         driverMatch.MatchMethod,
	}

	if err := Models.DB.Create(&speedViolation).Error; err != nil {
		return fmt.Errorf("failed to save violation: %w", err)
	}

	log.Printf("✅ Saved violation ID %d for car %s, driver: %s (%.1f%%, %s)",
		speedViolation.ID, car.CarNoPlate, driverMatch.DriverName,
		driverMatch.Confidence, driverMatch.MatchMethod)
	return nil
}

// UPDATE the duplicate detection in ProcessViolationMessage function
func ProcessViolationMessage(text string) error {
	violations, err := ParseViolationMessages(text)
	if err != nil {
		return err
	}

	log.Printf("📝 Parsed %d violation(s) from message", len(violations))

	for _, violation := range violations {
		// SIMPLIFIED DUPLICATE CHECK: Just compare the raw message text
		var existing Models.SpeedViolation
		err := Models.DB.Where("raw_message = ?", violation.RawMessage).First(&existing).Error

		if err == nil {
			log.Printf("⚠️  Violation already exists (duplicate raw message), skipping")
			continue
		}

		// If not found (err != nil), it's a new violation - save it
		if err := SaveViolationToDB(&violation); err != nil {
			log.Printf("❌ Failed to save violation for %s: %v", violation.PlateNumber, err)
			Observability.ReportJobFailure("speed_violations", "save_violation", err)
			continue
		}
	}
	return nil
}
