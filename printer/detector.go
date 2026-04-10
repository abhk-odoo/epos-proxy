package printer

import (
	"epos-proxy/logger"
	"fmt"
	"runtime"
	"sync"
)

// Detector handles unified printer discovery across all backends
type Detector struct {
	mu           sync.RWMutex
	lastResults  []UnifiedPrinterInfo
	dedupEngine  *DedupEngine
}

// NewDetector creates a new unified printer detector
func NewDetector() *Detector {
	return &Detector{
		dedupEngine: NewDedupEngine(),
	}
}

// Detect performs a full printer detection across all sources
// Returns unified printer list with routing info
func (d *Detector) Detect() ([]UnifiedPrinterInfo, []UnavailableInfo, error) {
	logger.Debug("Starting unified printer detection")

	// Load OS printers for deduplication
	if err := d.dedupEngine.LoadOSPrinters(); err != nil {
		logger.Warnf("Failed to load OS printers: %v", err)
	}

	var unified []UnifiedPrinterInfo
	var unavailable []UnavailableInfo

	// 1. Detect USB printers with deduplication
	usbPrinters, usbUnavailable, err := d.detectUSBWithDeduplication()
	if err != nil {
		logger.Errorf("USB detection failed: %v", err)
		// Continue with other sources
	} else {
		unified = append(unified, usbPrinters...)
		unavailable = append(unavailable, usbUnavailable...)
	}

	// 2. Detect LAN printers (no dedup needed)
	// Note: LAN printers are handled separately and appended

	// 3. Add standalone OS printers not matched to USB
	osOnlyPrinters := d.detectStandaloneOSPrinters()
	unified = append(unified, osOnlyPrinters...)

	d.mu.Lock()
	d.lastResults = unified
	d.mu.Unlock()

	logger.Infof("Unified detection complete: %d printers available", len(unified))
	return unified, unavailable, nil
}

// detectUSBWithDeduplication detects USB printers and determines routing
func (d *Detector) detectUSBWithDeduplication() ([]UnifiedPrinterInfo, []UnavailableInfo, error) {
	logger.Debug("Detecting USB printers with OS deduplication")

	// Get USB printer list (existing method)
	usbList, err := ListUSBPrinters()
	if err != nil {
		return nil, nil, fmt.Errorf("USB enumeration failed: %w", err)
	}

	var unified []UnifiedPrinterInfo
	var unavailable []UnavailableInfo

	// Process available USB printers
	for _, usbInfo := range usbList.Available {
		// Determine if this USB printer is OS-claimed
		isClaimed, backendType, backendName := d.matchUSBToOS(usbInfo)

		info := UnifiedPrinterInfo{
			ID:          usbInfo.Id,
			Name:        fmt.Sprintf("%s %s", usbInfo.VendorName, usbInfo.ProductName),
			BackendType: backendType,
			IsOSClaimed: isClaimed,
			IsLAN:       false,
			Online:      true,
		}

		// Update name to show OS routing
		if isClaimed && backendName != "" {
			info.Name = fmt.Sprintf("%s (via %s)", info.Name, backendName)
		}

		unified = append(unified, info)
		logger.Debugf("USB printer %s: backend=%s, osClaimed=%v", 
			usbInfo.Id, backendType, isClaimed)
	}

	// Process unavailable USB printers
	for _, unavail := range usbList.Unavailable {
		unavailable = append(unavailable, UnavailableInfo{
			Name:  unavail.Name,
			Error: unavail.Error,
		})
	}

	return unified, unavailable, nil
}

// matchUSBToOS checks if a USB printer is claimed by the OS
// Returns: isClaimed, backendType, backendName
func (d *Detector) matchUSBToOS(usbInfo Info) (bool, BackendType, string) {
	vid, pid, serial := extractIDsFromInfo(usbInfo)

	// Check CUPS printers (Linux/macOS)
	for _, cup := range d.dedupEngine.cupsPrinters {
		if cup.IsCUPSPrinterUSB(vid, pid, serial, usbInfo.VendorName, usbInfo.ProductName) {
			logger.Debugf("USB printer %s matched to CUPS printer %s", usbInfo.Id, cup.Name)
			return true, BackendCUPS, cup.Name
		}
	}

	// Check Windows printers
	for _, win := range d.dedupEngine.winPrinters {
		if win.IsUSB && serial != "" && win.USBSerial == serial {
			logger.Debugf("USB printer %s matched to Windows printer %s", usbInfo.Id, win.Name)
			return true, BackendWinSpooler, win.Name
		}
	}

	// Not claimed by OS - use direct USB
	return false, BackendLibUSB, ""
}

// detectStandaloneOSPrinters returns OS printers not matched to USB devices
// These are typically network printers or printers on other interfaces
func (d *Detector) detectStandaloneOSPrinters() []UnifiedPrinterInfo {
	var standalone []UnifiedPrinterInfo

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// Add CUPS printers not matched to USB
		for _, cup := range d.dedupEngine.cupsPrinters {
			if !cup.IsUSBPrinter() { // Non-USB CUPS printers
				info := UnifiedPrinterInfo{
					ID:          encodeOSPrinterID("cups", cup.Name),
					Name:        cup.Name,
					BackendType: BackendCUPS,
					IsOSClaimed: true,
					IsLAN:       false, // Could be network, but managed by CUPS
					Online:      true,
				}
				standalone = append(standalone, info)
			}
		}
	}

	return standalone
}

// extractIDsFromInfo extracts VID/PID/Serial from USB printer info
func extractIDsFromInfo(info Info) (vid, pid, serial string) {
	serial = info.Serial
	// Decode ID to get VID/PID
	printerID, err := decodePrinterID(info.Id)
	if err == nil && printerID != nil {
		vid = fmt.Sprintf("%04X", uint16(printerID.VendorID))
		pid = fmt.Sprintf("%04X", uint16(printerID.ProductID))
	}
	return
}


// IsUSBPrinter checks if CUPS printer is a USB device
func (cp *CUPSPrinter) IsUSBPrinter() bool {
	return cp.VendorID != "" || cp.ProductID != "" || 
		   (cp.DeviceURI != "" && len(cp.DeviceURI) > 6 && cp.DeviceURI[:6] == "usb://")
}

// encodeOSPrinterID creates a unique ID for OS-managed printers
func encodeOSPrinterID(backend, name string) string {
	return fmt.Sprintf("os:%s:%s", backend, name)
}

// GetLastResults returns cached detection results
func (d *Detector) GetLastResults() []UnifiedPrinterInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastResults
}
