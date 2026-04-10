package printer

import (
	"fmt"
	"strings"

	"github.com/google/gousb"
)

// PrinterMatch represents a detected printer with deduplication info
type PrinterMatch struct {
	USBDevice    *gousb.DeviceDesc    // USB device info (if detected via libusb)
	CUPSPrinter  *CUPSPrinter         // CUPS printer info (if detected via CUPS)
	WinPrinter   *WinPrinter          // Windows printer info (if detected via spooler)
	LANPrinter   *LANPrinterInfo      // LAN printer info (if network printer)

	IsOSClaimed  bool
	BackendType  BackendType
}

// dedupMatcher handles USB/OS printer deduplication
type dedupMatcher struct {
	cupsPrinters []CUPSPrinter
	winPrinters  []WinPrinter
	matchedIDs   map[string]bool // Track matched printers
}

// newDedupMatcher creates a deduplication engine
func newDedupMatcher(cups []CUPSPrinter, windows []WinPrinter) *dedupMatcher {
	return &dedupMatcher{
		cupsPrinters: cups,
		winPrinters:  windows,
		matchedIDs:   make(map[string]bool),
	}
}

// MatchUSB attempts to match a USB device with OS printers
// Returns: isOSClaimed, cupsPrinter (if matched), backendType
func (d *dedupMatcher) MatchUSB(desc *gousb.DeviceDesc, serial string) (bool, *CUPSPrinter, *WinPrinter, BackendType) {
	deviceKey := fmt.Sprintf("usb:%04X:%04X:%s", desc.Vendor, desc.Product, serial)
	
	if d.matchedIDs[deviceKey] {
		// Already matched, skip
		return false, nil, nil, BackendLibUSB
	}

	// Try CUPS matching first (Linux/macOS)
	// Note: dedup matcher uses device desc, so vendor/product names are empty
	// The actual name-based matching happens in detector.go with full info
	for i := range d.cupsPrinters {
		cup := &d.cupsPrinters[i]
		if cup.IsCUPSPrinterUSB(
			fmt.Sprintf("%04X", uint16(desc.Vendor)),
			fmt.Sprintf("%04X", uint16(desc.Product)),
			serial,
			"", // vendorName not available from DeviceDesc
			"", // productName not available from DeviceDesc
		) {
			d.matchedIDs[deviceKey] = true
			return true, cup, nil, BackendCUPS
		}
	}

	// Try Windows spooler matching
	for i := range d.winPrinters {
		win := &d.winPrinters[i]
		if win.IsUSB && d.matchWindowsUSB(win, desc, serial) {
			d.matchedIDs[deviceKey] = true
			return true, nil, win, BackendWinSpooler
		}
	}

	// Not claimed by OS - use direct USB
	return false, nil, nil, BackendLibUSB
}

// matchWindowsUSB checks if Windows printer matches USB device
func (d *dedupMatcher) matchWindowsUSB(win *WinPrinter, desc *gousb.DeviceDesc, serial string) bool {
	// Match by serial if available
	if serial != "" && win.USBSerial == serial {
		return true
	}

	// Match by port name containing USB identifiers
	upperPort := strings.ToUpper(win.PortName)
	if strings.Contains(upperPort, "USB") {
		// Windows USB ports are generic, but we can cross-reference
		// In a full implementation, use WMI to query device ID
		return true
	}

	return false
}

// GetUnmatchedOS returns OS printers that weren't matched to USB
func (d *dedupMatcher) GetUnmatchedOS() ([]CUPSPrinter, []WinPrinter) {
	var unmatchedCUPS []CUPSPrinter
	var unmatchedWin []WinPrinter

	// Find CUPS printers not matched to USB
	for _, cup := range d.cupsPrinters {
		found := false
		for key := range d.matchedIDs {
			if strings.Contains(key, cup.Serial) {
				found = true
				break
			}
		}
		if !found {
			unmatchedCUPS = append(unmatchedCUPS, cup)
		}
	}

	// Find Windows printers not matched
	for _, win := range d.winPrinters {
		// Mark unmatched Windows printers (network, LPT, etc.)
		if !win.IsUSB {
			unmatchedWin = append(unmatchedWin, win)
		}
	}

	return unmatchedCUPS, unmatchedWin
}

// DedupEngine performs full deduplication across all printer sources
type DedupEngine struct {
	cupsPrinters []CUPSPrinter
	winPrinters  []WinPrinter
	lanPrinters  []LANPrinterInfo
}

// NewDedupEngine creates a new deduplication engine
func NewDedupEngine() *DedupEngine {
	return &DedupEngine{}
}

// LoadOSPrinters loads OS printer lists (call on init)
func (d *DedupEngine) LoadOSPrinters() error {
	var err error

	// Load CUPS printers (Linux/macOS)
	d.cupsPrinters, err = ListCUPSPrinters()
	if err != nil {
		// CUPS not available is OK
		d.cupsPrinters = nil
	}

	// Load Windows printers (Windows only - build tagged)
	d.winPrinters, _ = ListWinPrinters()

	return nil
}

// DeduplicateUSB takes a list of USB devices and returns unified printer info
// with routing determined by OS claim status
func (d *DedupEngine) DeduplicateUSB(usbInfos []Info, ctx *gousb.Context) []UnifiedPrinterInfo {
	matcher := newDedupMatcher(d.cupsPrinters, d.winPrinters)
	var unified []UnifiedPrinterInfo

	for _, usbInfo := range usbInfos {
		// Get device descriptor for matching
		desc := findDeviceBySerial(ctx, usbInfo.Serial)
		if desc == nil {
			// No device match, treat as raw USB
			unified = append(unified, UnifiedPrinterInfo{
				ID:          usbInfo.Id,
				Name:        usbInfo.VendorName + " " + usbInfo.ProductName,
				BackendType: BackendLibUSB,
				IsOSClaimed: false,
				IsLAN:       false,
				Online:      true,
			})
			continue
		}

		// Check if OS-claimed
		isClaimed, cupPrinter, winPrinter, backend := matcher.MatchUSB(desc, usbInfo.Serial)

		info := UnifiedPrinterInfo{
			ID:          usbInfo.Id,
			Name:        usbInfo.VendorName + " " + usbInfo.ProductName,
			BackendType: backend,
			IsOSClaimed: isClaimed,
			IsLAN:       false,
			Online:      true,
		}

		// Adjust name to indicate OS route
		if isClaimed {
			if cupPrinter != nil {
				info.Name = fmt.Sprintf("%s (via CUPS)", cupPrinter.Name)
			} else if winPrinter != nil {
				info.Name = fmt.Sprintf("%s (via Spooler)", winPrinter.Name)
			}
		}

		unified = append(unified, info)
	}

	return unified
}

// findDeviceBySerial locates a USB device by serial number
func findDeviceBySerial(ctx *gousb.Context, serial string) *gousb.DeviceDesc {
	var found *gousb.DeviceDesc
	_, _ = ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		if found != nil {
			return false
		}
		// We need to open device to check serial
		return true // Keep enumerating
	})
	// Note: In practice, we'd need to track device references from earlier enumeration
	// This is a simplified version - the actual implementation should pass device refs
	return nil
}
