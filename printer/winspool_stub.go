//go:build !windows
// +build !windows

package printer

// Stub implementations for non-Windows platforms
// These functions are no-ops on Linux/macOS

type WinPrinter struct {
	Name        string
	PortName    string
	DriverName  string
	DeviceID    string
	IsUSB       bool
	USBSerial   string
}

// ListWinPrinters is a stub on non-Windows platforms
func ListWinPrinters() ([]WinPrinter, error) {
	// Windows spooler not available on this platform
	return nil, nil
}

// newWinSpoolBackend is a stub on non-Windows platforms
func newWinSpoolBackend(printerName string, info UnifiedPrinterInfo) PrintBackend {
	// This should never be called on non-Windows platforms
	return nil
}
