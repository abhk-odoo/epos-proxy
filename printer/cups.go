//go:build linux || darwin
// +build linux darwin

package printer

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"epos-proxy/logger"
)

// CUPSPrinter represents a CUPS-managed printer
type CUPSPrinter struct {
	Name        string // CUPS printer name (e.g., "Epson-TM-T88V")
	DeviceURI   string // Device URI (e.g., "usb://EPSON/TM-T88V?serial=12345")
	Serial      string // Extracted serial number
	VendorID    string // Extracted VID (if USB)
	ProductID   string // Extracted PID (if USB)
	IsRaw       bool   // true if using raw backend
	Description string // Human-readable description
}

// cupsBackend implements PrintBackend for CUPS
type cupsBackend struct {
	printerName string
	info        UnifiedPrinterInfo
}

// newCUPSBackend creates a CUPS backend for the given printer
func newCUPSBackend(printerName string, info UnifiedPrinterInfo) PrintBackend {
	return &cupsBackend{
		printerName: printerName,
		info:        info,
	}
}

func (c *cupsBackend) Print(data []byte) error {
	// Use lp command to send data to CUPS
	cmd := exec.Command("lp", "-d", c.printerName, "-o", "raw")
	cmd.Stdin = bytes.NewReader(data)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cups print failed: %w (output: %s)", err, string(output))
	}

	logger.Debugf("CUPS print job sent to %s: %s", c.printerName, strings.TrimSpace(string(output)))
	return nil
}

func (c *cupsBackend) GetInfo() UnifiedPrinterInfo {
	return c.info
}

func (c *cupsBackend) Close() error {
	// CUPS backend doesn't hold persistent resources
	return nil
}

// ListCUPSPrinters enumerates all CUPS printers and returns those that are USB-connected
func ListCUPSPrinters() ([]CUPSPrinter, error) {
	logger.Debug("Querying CUPS for installed printers")

	// Get list of printers with device URIs
	cmd := exec.Command("lpstat", "-v")
	output, err := cmd.Output()
	if err != nil {
		// CUPS may not be installed or running
		logger.Debugf("CUPS not available: %v", err)
		return nil, nil
	}

	var printers []CUPSPrinter
	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Parse: "device for <name>: <uri>"
	devicePattern := regexp.MustCompile(`device for (.+?): (.+)`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := devicePattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			continue
		}

		name := strings.TrimSpace(matches[1])
		uri := strings.TrimSpace(matches[2])

		printer := CUPSPrinter{
			Name:      name,
			DeviceURI: uri,
		}

		// Parse USB URI format: usb://<vendor>/<product>?serial=<serial>&..."
		if strings.HasPrefix(uri, "usb://") {
			extractUSBInfo(&printer, uri)
		}

		printers = append(printers, printer)
		logger.Debugf("Found CUPS printer: %s (URI: %s)", name, uri)
	}

	return printers, nil
}

// extractUSBInfo parses USB URI to extract VID/PID/Serial
// URI format: usb://<vendor>/<product>?serial=<serial>
func extractUSBInfo(printer *CUPSPrinter, uri string) {
	// Remove "usb://" prefix
	uri = strings.TrimPrefix(uri, "usb://")

	// Parse vendor and product (URL-encoded)
	parts := strings.SplitN(uri, "?", 2)
	pathPart := parts[0]

	// Split vendor/product
	pathParts := strings.Split(pathPart, "/")
	if len(pathParts) >= 2 {
		printer.Description = fmt.Sprintf("%s %s", pathParts[0], pathParts[1])
	}

	// Parse query parameters for serial and IDs
	if len(parts) == 2 {
		query := parts[1]
		params := parseQueryParams(query)

		if serial, ok := params["serial"]; ok {
			printer.Serial = serial
		}
		if vid, ok := params["vid"]; ok {
			printer.VendorID = vid
		}
		if pid, ok := params["pid"]; ok {
			printer.ProductID = pid
		}
	}

	// Check if backend is raw
	printer.IsRaw = strings.Contains(uri, "?backend=raw") || strings.Contains(uri, "&backend=raw")
}

// parseQueryParams parses URL query parameters
func parseQueryParams(query string) map[string]string {
	params := make(map[string]string)
	pairs := strings.Split(query, "&")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}
	return params
}

// IsCUPSPrinterUSB checks if a CUPS printer matches the given USB device
// Tries multiple strategies: serial → VID/PID → product name
func (cp *CUPSPrinter) IsCUPSPrinterUSB(vid, pid, serial, vendorName, productName string) bool {
	// Strategy 1: Match by serial (most reliable)
	if serial != "" && cp.Serial == serial {
		logger.Debugf("CUPS match by serial: %s", serial)
		return true
	}

	// Strategy 2: Match by VID/PID (hex comparison)
	if vid != "" && pid != "" && cp.VendorID != "" && cp.ProductID != "" {
		cupVID := normalizeHex(cp.VendorID)
		cupPID := normalizeHex(cp.ProductID)
		devVID := normalizeHex(vid)
		devPID := normalizeHex(pid)
		
		if cupVID == devVID && cupPID == devPID {
			logger.Debugf("CUPS match by VID/PID: %s:%s", vid, pid)
			return true
		}
	}

	// Strategy 3: Match by product name from URI (when serial/VID/PID unavailable)
	// CUPS URIs like "usb://EPSON/TM-T88V" contain manufacturer/model
	// Note: Description may be URL-encoded (e.g., %20 for spaces)
	if cp.Description != "" && (vendorName != "" || productName != "") {
		// URL-decode the description first, then normalize
		decodedDesc, _ := url.QueryUnescape(cp.Description)
		cupDesc := strings.ToUpper(strings.ReplaceAll(decodedDesc, " ", ""))
		usbVendor := strings.ToUpper(strings.ReplaceAll(vendorName, " ", ""))
		usbProduct := strings.ToUpper(strings.ReplaceAll(productName, " ", ""))

		// Check if CUPS description contains USB vendor and/or product
		if usbVendor != "" && usbProduct != "" {
			if strings.Contains(cupDesc, usbVendor) && strings.Contains(cupDesc, usbProduct) {
				logger.Debugf("CUPS match by name: %s ~ %s %s", decodedDesc, vendorName, productName)
				return true
			}
		} else if usbVendor != "" && strings.Contains(cupDesc, usbVendor) {
			logger.Debugf("CUPS match by vendor: %s ~ %s", decodedDesc, vendorName)
			return true
		} else if usbProduct != "" && strings.Contains(cupDesc, usbProduct) {
			logger.Debugf("CUPS match by product: %s ~ %s", decodedDesc, productName)
			return true
		}
	}

	return false
}

// normalizeHex ensures hex string is lowercase 4-char without 0x prefix
func normalizeHex(s string) string {
	s = strings.ToLower(strings.TrimPrefix(s, "0x"))
	return s
}

// PrintToCUPS sends raw data to a CUPS printer by name
func PrintToCUPS(printerName string, data []byte) error {
	cmd := exec.Command("lp", "-d", printerName, "-o", "raw")
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}
