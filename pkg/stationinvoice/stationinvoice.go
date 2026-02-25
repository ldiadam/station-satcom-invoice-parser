package stationinvoice

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Invoice struct {
	InvoiceNumber string `json:"invoice_number"`
	InvoiceDate   string `json:"invoice_date"`
	AccountCode   string `json:"account_code"`
	AccountName   string `json:"account_name"`
	AirtimePeriod string `json:"airtime_period"`

	Currency string `json:"currency"`
	FXNote   string `json:"fx_note"`

	TotalInvoiceUSD float64 `json:"total_invoice_usd"`

	Supplier Supplier          `json:"supplier"`
	Products []ProductSummary  `json:"products,omitempty"`
	Usage    *UsageSummary     `json:"usage,omitempty"`
	RawHints map[string]string `json:"raw_hints,omitempty"`
}

type Supplier struct {
	Name  string `json:"name"`
	UEN   string `json:"uen_gst"`
	PAN   string `json:"pan"`
	Tel   string `json:"tel"`
	Email string `json:"email"`
}

type ProductCharge struct {
	ChargeType string  `json:"charge_type"`
	AmountUSD  float64 `json:"amount_usd"`
}

type DeviceCharge struct {
	DeviceID    string  `json:"device_id"`
	Description string  `json:"description"`
	AmountUSD   float64 `json:"amount_usd"`
}

type ProductSummary struct {
	Product string `json:"product"`

	// High-level summary (from "Summary per Product")
	Charges  []ProductCharge `json:"charges,omitempty"`
	TotalUSD float64         `json:"total_usd,omitempty"`

	// Per device/SIM charges (from "Charges per Device / SIM Card")
	Devices []DeviceCharge `json:"devices,omitempty"`
}

type UsageSummary struct {
	DeviceID      string      `json:"device_id"`
	TotalVolumeGB float64     `json:"total_volume_gb,omitempty"`
	AirtimeUSD    float64     `json:"airtime_usd,omitempty"`
	PricedDays    []PricedDay `json:"priced_days,omitempty"`
}

type PricedDay struct {
	Date     string  `json:"date"`
	VolumeGB float64 `json:"volume_gb"`
	USD      float64 `json:"usd"`
}

// Runner allows swapping out the PDF-to-text extraction method.
// Implementations must return UTF-8 (or best-effort) text.
//
// Default runner uses the `pdftotext` binary.
//
// Note: This library intentionally does not implement a pure-Go PDF parser.
// In production, Poppler's pdftotext is the most deterministic choice.
type Runner interface {
	PDFToText(pdfPath string) (string, error)
}

type PdftotextRunner struct{}

func (r PdftotextRunner) PDFToText(pdfPath string) (string, error) {
	cmd := exec.Command("pdftotext", pdfPath, "-")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// ParsePDF extracts text from the PDF using `pdftotext` and parses it.
func ParsePDF(pdfPath string) (*Invoice, error) {
	return ParsePDFWithRunner(pdfPath, PdftotextRunner{})
}

// ParsePDFWithRunner extracts text using the provided runner and parses it.
func ParsePDFWithRunner(pdfPath string, runner Runner) (*Invoice, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		// Only relevant for the default runner; custom runners can ignore this.
		// Still helpful as a hint.
		// Do not hard fail here if a custom runner is provided.
		if _, ok := runner.(PdftotextRunner); ok {
			return nil, fmt.Errorf("pdftotext not found; install poppler-utils")
		}
	}
	text, err := runner.PDFToText(pdfPath)
	if err != nil {
		return nil, err
	}
	return ParseText(text)
}

// ParseText parses text extracted from a Station Satcom invoice PDF.
func ParseText(text string) (*Invoice, error) {
	return parseInvoice(text)
}

func mustFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	return strconv.ParseFloat(s, 64)
}

func findFirst(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func findLast(re *regexp.Regexp, text string) string {
	all := re.FindAllStringSubmatch(text, -1)
	if len(all) == 0 {
		return ""
	}
	m := all[len(all)-1]
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func parseInvoice(text string) (*Invoice, error) {
	lines := strings.Split(text, "\n")
	all := text

	inv := &Invoice{
		Currency: "USD",
		RawHints: map[string]string{},
	}

	inv.InvoiceNumber = findFirst(regexp.MustCompile(`(?m)^Tax Invoice\s+(\S+)`), all)
	inv.InvoiceDate = findFirst(regexp.MustCompile(`(?m)\b(\d{1,2}\s+[A-Za-z]+,\s+\d{4})\b`), all)

	inv.AccountCode = findFirst(regexp.MustCompile(`(?m)^\s*(SMTSPL(?:-\d+)?)\s*$`), all)
	inv.AccountName = findAccountName(lines)
	inv.AirtimePeriod = findFirst(regexp.MustCompile(`(?m)^Airtime Period\s*\n\s*\n([^\n]+)`), all)

	inv.FXNote = findFirst(regexp.MustCompile(`(?m)^(1\s+USD\s*=\s*[\d.]+\s+SGD)\s*$`), all)

	totalStr := findFirst(regexp.MustCompile(`(?m)^Total Amount This Invoice\s*\n\s*\nUSD\s*\n\s*\n([\d.]+)\s*$`), all)
	if totalStr == "" {
		totalStr = findFirst(regexp.MustCompile(`(?m)^Total Amount This Invoice\s*\n\s*\nUSD\s*\n\s*([\d.]+)\s*$`), all)
	}
	if totalStr != "" {
		if v, err := mustFloat(totalStr); err == nil {
			inv.TotalInvoiceUSD = v
		}
	}

	inv.Supplier.Name = "STATION SATCOM PTE LTD"
	inv.Supplier.UEN = findFirst(regexp.MustCompile(`(?m)^UEN/GST Reg no:\s*([A-Z0-9]+)\s*$`), all)
	inv.Supplier.PAN = findFirst(regexp.MustCompile(`(?m)^PAN No\s*:\s*([A-Z0-9]+)\s*$`), all)
	inv.Supplier.Tel = findFirst(regexp.MustCompile(`(?m)^Tel No\.\s*:\s*(.+)\s*$`), all)
	inv.Supplier.Email = findFirst(regexp.MustCompile(`(?m)^Email id\s*:\s*(\S+)\s*$`), all)

	products := parseSummaryPerProduct(lines, inv.TotalInvoiceUSD)
	deviceCharges := parseChargesPerDeviceSIM(lines)
	inv.Products = mergeProductDeviceCharges(products, deviceCharges)

	deviceID := findFirst(regexp.MustCompile(`(?m)\b(KITP\d{8,})\b`), all)
	if deviceID != "" {
		usage := &UsageSummary{DeviceID: deviceID}
		inv.Usage = usage

		volStr := findFirst(regexp.MustCompile(`(?m)Total for\s+\S+\s*\([^\n]*\)\s+([\d.]+)\s+GB`), all)
		if volStr != "" {
			if v, err := mustFloat(volStr); err == nil {
				usage.TotalVolumeGB = v
			}
		}

		deviceTotalUSD := findFirst(regexp.MustCompile(`(?ms)Total for\s+[^\n]*\(`+regexp.QuoteMeta(deviceID)+`[^\n]*\)[^\n]*\n.*?\bUSD\b\s*\n\s*([\d.]+)\s*$`), all)
		if deviceTotalUSD == "" {
			deviceTotalUSD = findLast(regexp.MustCompile(`(?m)^USD\s*\n\s*([\d.]+)\s*$`), all)
		}
		if deviceTotalUSD != "" {
			if v, err := mustFloat(deviceTotalUSD); err == nil {
				usage.AirtimeUSD = v
			}
		}

		dayRe := regexp.MustCompile(`^\d{2}\s+[A-Za-z]{3}\s+\d{4}\s+00:00:00$`)
		for i := 0; i < len(lines); i++ {
			dateLine := strings.TrimSpace(lines[i])
			if !dayRe.MatchString(dateLine) {
				continue
			}
			svcIdx := nextNonEmpty(lines, i+1)
			if svcIdx == -1 {
				continue
			}
			service := strings.TrimSpace(lines[svcIdx])
			if service != "Background IP" {
				continue
			}

			gbIdx := findNextGBLine(lines, svcIdx+1)
			if gbIdx == -1 {
				continue
			}
			volLine := strings.TrimSpace(lines[gbIdx])
			volNum := strings.TrimSpace(strings.TrimSuffix(volLine, "GB"))
			vol, err1 := mustFloat(volNum)
			if err1 != nil {
				continue
			}

			usdIdx := findNextFloatLine(lines, gbIdx+1)
			if usdIdx == -1 {
				continue
			}
			usd, err2 := mustFloat(strings.TrimSpace(lines[usdIdx]))
			if err2 != nil || usd <= 0 {
				continue
			}

			usage.PricedDays = append(usage.PricedDays, PricedDay{
				Date:     strings.TrimSpace(strings.ReplaceAll(dateLine, " 00:00:00", "")),
				VolumeGB: vol,
				USD:      usd,
			})
		}
	}

	if inv.InvoiceNumber == "" {
		return nil, errors.New("could not parse invoice number")
	}
	if inv.TotalInvoiceUSD == 0 {
		inv.RawHints["warn_total_invoice_usd"] = "total_invoice_usd parsed as 0 (pattern may not match)"
	}

	return inv, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var reLongDate = regexp.MustCompile(`^\d{1,2}\s+[A-Za-z]+,\s+\d{4}$`)

func findAccountName(lines []string) string {
	idx := -1
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "Account" {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ""
	}

	for i := idx + 1; i < min(idx+25, len(lines)); i++ {
		s := strings.TrimSpace(lines[i])
		if s == "" {
			continue
		}
		if reLongDate.MatchString(s) || reShortDate.MatchString(s) {
			continue
		}
		if strings.HasPrefix(s, "SMTSPL") {
			continue
		}
		if strings.HasPrefix(s, "UEN/GST") || strings.HasPrefix(s, "STATION SATCOM") {
			continue
		}
		return s
	}
	return ""
}

func nextNonEmpty(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

func findNextSuffix(lines []string, start int, suffix string) int {
	for i := start; i < len(lines); i++ {
		v := strings.TrimSpace(lines[i])
		if v == "" {
			continue
		}
		if strings.HasSuffix(v, suffix) {
			return i
		}
	}
	return -1
}

func isSkippableSummaryLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if s == "Product" || s == "Charge Type" || s == "Amount (USD)" || s == "Summary per Product" {
		return true
	}
	if s == "Page" {
		return true
	}
	if strings.HasPrefix(s, "SMTSPL") {
		return true
	}
	if rePageCount.MatchString(s) || reShortDate.MatchString(s) {
		return true
	}
	return false
}

func parseSummaryPerProduct(lines []string, invoiceTotal float64) []ProductSummary {
	start := -1
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "Summary per Product" {
			start = i
			break
		}
	}
	if start == -1 {
		return nil
	}

	byProduct := map[string]*ProductSummary{}

	state := "product" // product -> charge -> amount
	product := ""
	charge := ""
	pending := [][2]string{} // queued (product,charge) when amount appears later

	for i := start + 1; i < min(start+400, len(lines)); i++ {
		s := strings.TrimSpace(lines[i])
		if s == "" {
			continue
		}
		if strings.Contains(s, "Total charges (excl tax)") {
			if state == "amount" && product != "" && charge != "" {
				floats := collectNextFloatLines(lines, i+1, 8)
				if len(floats) > 0 {
					sumSoFar := 0.0
					for _, ps := range byProduct {
						sumSoFar += ps.TotalUSD
					}

					amt := floats[0]
					if len(floats) > 1 {
						amt = pickBestAmount(floats, invoiceTotal, sumSoFar)
					}

					ps := byProduct[product]
					if ps == nil {
						ps = &ProductSummary{Product: product}
						byProduct[product] = ps
					}
					ps.Charges = append(ps.Charges, ProductCharge{ChargeType: charge, AmountUSD: amt})
					ps.TotalUSD += amt
				}
			}
			break
		}
		if strings.HasPrefix(s, "Total for ") {
			continue
		}
		if isSkippableSummaryLine(s) {
			continue
		}

		switch state {
		case "product":
			product = s
			state = "charge"
		case "charge":
			charge = s
			state = "amount"
		case "amount":
			amt, err := mustFloat(s)
			if err != nil {
				if product != "" && charge != "" {
					pending = append(pending, [2]string{product, charge})
					product = s
					charge = ""
					state = "charge"
				}
				continue
			}

			if len(pending) > 0 {
				pc := pending[0]
				pending = pending[1:]
				ps := byProduct[pc[0]]
				if ps == nil {
					ps = &ProductSummary{Product: pc[0]}
					byProduct[pc[0]] = ps
				}
				ps.Charges = append(ps.Charges, ProductCharge{ChargeType: pc[1], AmountUSD: amt})
				ps.TotalUSD += amt
				continue
			}

			ps := byProduct[product]
			if ps == nil {
				ps = &ProductSummary{Product: product}
				byProduct[product] = ps
			}
			ps.Charges = append(ps.Charges, ProductCharge{ChargeType: charge, AmountUSD: amt})
			ps.TotalUSD += amt
			product = ""
			charge = ""
			state = "product"
		}
	}

	out := make([]ProductSummary, 0, len(byProduct))
	for _, ps := range byProduct {
		out = append(out, *ps)
	}

	if len(out) <= 1 {
		return out
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Product == "Starlink" {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

var floatLineRe = regexp.MustCompile(`^\s*\d+(?:\.\d+)?\s*$`)
var gbValueLineRe = regexp.MustCompile(`^\s*\d+(?:\.\d+)?\s+GB\s*$`)
var rePageCount = regexp.MustCompile(`^\d+\/\d+$`)
var reShortDate = regexp.MustCompile(`^\d{1,2}\s+[A-Za-z]{3}\s+\d{4}$`)

func findNextFloatLine(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		v := strings.TrimSpace(lines[i])
		if v == "" {
			continue
		}
		if floatLineRe.MatchString(v) {
			return i
		}
	}
	return -1
}

func collectNextFloatLines(lines []string, start int, maxCount int) []float64 {
	out := []float64{}
	for i := start; i < len(lines) && len(out) < maxCount; i++ {
		v := strings.TrimSpace(lines[i])
		if v == "" {
			continue
		}
		if floatLineRe.MatchString(v) {
			f, err := mustFloat(v)
			if err == nil {
				out = append(out, f)
			}
		}
		if v == "Page" || strings.HasPrefix(v, "SMTSPL-") {
			break
		}
	}
	return out
}

func approxEq(a, b float64) bool {
	if a == 0 && b == 0 {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.005
}

func pickBestAmount(candidates []float64, invoiceTotal float64, sumSoFar float64) float64 {
	filtered := []float64{}
	for _, c := range candidates {
		if approxEq(c, invoiceTotal) {
			continue
		}
		if approxEq(c, sumSoFar) {
			continue
		}
		filtered = append(filtered, c)
	}
	if len(filtered) == 0 {
		best := candidates[0]
		for _, c := range candidates[1:] {
			if c < best {
				best = c
			}
		}
		return best
	}
	best := filtered[0]
	for _, c := range filtered[1:] {
		if c >= 0 && c < best {
			best = c
		}
	}
	return best
}

func findNextGBLine(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		v := strings.TrimSpace(lines[i])
		if v == "" {
			continue
		}
		if gbValueLineRe.MatchString(v) {
			return i
		}
	}
	return -1
}

var reKitp = regexp.MustCompile(`^KITP\d{6,}$`)
var reNumericID = regexp.MustCompile(`^\d{6,}$`)

func isDeviceIDLine(s string) bool {
	s = strings.TrimSpace(s)
	return reKitp.MatchString(s) || reNumericID.MatchString(s)
}

func parseChargesPerDeviceSIM(lines []string) map[string][]DeviceCharge {
	// Map product -> device charges.
	out := map[string][]DeviceCharge{}

	// Find section starts; there can be multiple (different products) in one invoice.
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "Charges per Device / SIM Card" {
			continue
		}

		// Scan forward for blocks like "Charges for: <PRODUCT>"
		for j := i + 1; j < min(i+800, len(lines)); j++ {
			s := strings.TrimSpace(lines[j])
			if s == "" {
				continue
			}
			if strings.HasPrefix(s, "Summary per Product") {
				break
			}
			if strings.HasPrefix(s, "Call Details") {
				break
			}
			if strings.HasPrefix(s, "Charges for:") {
				product := strings.TrimSpace(strings.TrimPrefix(s, "Charges for:"))
				if product == "" {
					continue
				}

				// Advance until we hit the table rows. Usually after headers "Device / SIM", "Description", maybe "Amount (USD)".
				k := j + 1
				for ; k < min(j+40, len(lines)); k++ {
					h := strings.TrimSpace(lines[k])
					if h == "" {
						continue
					}
					if isDeviceIDLine(h) {
						break
					}
				}

				// Parse rows: deviceId, description (possibly multi-line), amount.
				for ; k < len(lines); k++ {
					row := strings.TrimSpace(lines[k])
					if row == "" {
						continue
					}
					if strings.HasPrefix(row, "Charges for:") || strings.HasPrefix(row, "Total charges for ") || strings.HasPrefix(row, "Total charges for this period") || strings.HasPrefix(row, "Total Invoice Amount") || strings.HasPrefix(row, "Call Details") {
						break
					}
					if !isDeviceIDLine(row) {
						continue
					}

					deviceID := row
					descParts := []string{}

					// Collect description until we find a float amount.
					m := k + 1
					amount := 0.0
					foundAmount := false
					for ; m < min(k+50, len(lines)); m++ {
						v := strings.TrimSpace(lines[m])
						if v == "" {
							continue
						}
						if strings.HasPrefix(v, "Total charges for ") || strings.HasPrefix(v, "Charges for:") || strings.HasPrefix(v, "Total charges for this period") || strings.HasPrefix(v, "Total Invoice Amount") || strings.HasPrefix(v, "Call Details") {
							break
						}
						if floatLineRe.MatchString(v) {
							f, err := mustFloat(v)
							if err == nil {
								amount = f
								foundAmount = true
								m++
								break
							}
						}
						// stop if another device id begins unexpectedly
						if isDeviceIDLine(v) {
							break
						}
						// skip common header tokens
						if v == "Device / SIM" || v == "Description" || v == "Amount (USD)" {
							continue
						}
						descParts = append(descParts, v)
					}

					if foundAmount {
						out[product] = append(out[product], DeviceCharge{
							DeviceID:    deviceID,
							Description: strings.Join(descParts, " "),
							AmountUSD:   amount,
						})
					}

					k = m - 1
				}
			}
		}
	}

	return out
}

func mergeProductDeviceCharges(products []ProductSummary, deviceCharges map[string][]DeviceCharge) []ProductSummary {
	byName := map[string]*ProductSummary{}
	for i := range products {
		p := &products[i]
		byName[p.Product] = p
	}

	for product, devices := range deviceCharges {
		p := byName[product]
		if p == nil {
			// Product wasn't present in summary table; still return it with devices.
			np := ProductSummary{Product: product}
			products = append(products, np)
			p = &products[len(products)-1]
			byName[product] = p
		}
		p.Devices = devices
	}

	return products
}
