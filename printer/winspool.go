//go:build windows
// +build windows

package printer

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"epos-proxy/logger"
	"golang.org/x/sys/windows"
)

var (
	winspool                        = windows.NewLazySystemDLL("winspool.drv")
	procOpenPrinter                 = winspool.NewProc("OpenPrinterW")
	procClosePrinter                = winspool.NewProc("ClosePrinter")
	procStartDocPrinter             = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter               = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter            = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter              = winspool.NewProc("EndPagePrinter")
	procWritePrinter                = winspool.NewProc("WritePrinter")
	procEnumPrinters                = winspool.NewProc("EnumPrintersW")
	procGetPrinter                  = winspool.NewProc("GetPrinterW")
)

// WinPrinter represents a Windows spooler printer
type WinPrinter struct {
	Name        string
	PortName    string
	DriverName  string
	DeviceID    string // Hardware ID or port info
	IsUSB       bool
	USBSerial   string
}

// winSpoolBackend implements PrintBackend for Windows spooler
type winSpoolBackend struct {
	printerName string
	hPrinter    windows.Handle
	info        UnifiedPrinterInfo
}

// newWinSpoolBackend creates a Windows spooler backend
func newWinSpoolBackend(printerName string, info UnifiedPrinterInfo) PrintBackend {
	return &winSpoolBackend{
		printerName: printerName,
		info:        info,
	}
}

func (w *winSpoolBackend) Print(data []byte) error {
	// Open printer if not already open
	if w.hPrinter == 0 {
		hPrinter, err := openPrinter(w.printerName)
		if err != nil {
			return fmt.Errorf("failed to open printer %s: %w", w.printerName, err)
		}
		w.hPrinter = hPrinter
	}

	// Create DOC_INFO structure
	docName := windows.StringToUTF16Ptr("ePOS Print Job")
	outputFile := (*uint16)(unsafe.Pointer(uintptr(0))) // null
	datatype := windows.StringToUTF16Ptr("RAW")

	docInfo := DOC_INFO_1{
		pDocName:    docName,
		pOutputFile: outputFile,
		pDatatype:   datatype,
	}

	// Start document
	docID, err := startDocPrinter(w.hPrinter, 1, (*byte)(unsafe.Pointer(&docInfo)))
	if err != nil {
		return fmt.Errorf("failed to start doc: %w", err)
	}

	// Start page
	if err := startPagePrinter(w.hPrinter); err != nil {
		_ = endDocPrinter(w.hPrinter)
		return fmt.Errorf("failed to start page: %w", err)
	}

	// Write data
	written, err := writePrinter(w.hPrinter, data)
	if err != nil {
		_ = endPagePrinter(w.hPrinter)
		_ = endDocPrinter(w.hPrinter)
		return fmt.Errorf("failed to write: %w", err)
	}

	logger.Debugf("Windows spooler: wrote %d bytes", written)

	// End page and document
	if err := endPagePrinter(w.hPrinter); err != nil {
		logger.Warnf("Failed to end page: %v", err)
	}

	if err := endDocPrinter(w.hPrinter); err != nil {
		logger.Warnf("Failed to end doc: %v", err)
	}

	return nil
}

func (w *winSpoolBackend) GetInfo() UnifiedPrinterInfo {
	return w.info
}

func (w *winSpoolBackend) Close() error {
	if w.hPrinter != 0 {
		err := closePrinter(w.hPrinter)
		w.hPrinter = 0
		return err
	}
	return nil
}

// ListWinPrinters enumerates Windows printers
func ListWinPrinters() ([]WinPrinter, error) {
	logger.Debug("Querying Windows spooler for printers")

	const PRINTER_ENUM_LOCAL = 2
	var needed, returned uint32

	// First call to get buffer size
	_, _, _ = procEnumPrinters.Call(
		uintptr(PRINTER_ENUM_LOCAL),
		uintptr(0),
		uintptr(2), // PRINTER_INFO_2
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if needed == 0 {
		return nil, nil
	}

	// Allocate buffer
	buf := make([]byte, needed)

	ret, _, err := procEnumPrinters.Call(
		uintptr(PRINTER_ENUM_LOCAL),
		uintptr(0),
		uintptr(2),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if ret == 0 {
		return nil, fmt.Errorf("EnumPrinters failed: %v", err)
	}

	var printers []WinPrinter
	const printerInfo2Size = 84 // Size of PRINTER_INFO_2W structure (varies by architecture)

	for i := uint32(0); i < returned; i++ {
		info := (*PRINTER_INFO_2)(unsafe.Pointer(&buf[i*printerInfo2Size]))

		printer := WinPrinter{
			Name:     windows.UTF16PtrToString(info.pPrinterName),
			PortName: windows.UTF16PtrToString(info.pPortName),
			DriverName: windows.UTF16PtrToString(info.pDriverName),
		}

		// Check if USB port
		if strings.Contains(strings.ToUpper(printer.PortName), "USB") {
			printer.IsUSB = true
			// Try to extract serial from port info
			printer.USBSerial = extractSerialFromPort(printer.PortName)
		}

		printers = append(printers, printer)
		logger.Debugf("Found Windows printer: %s (Port: %s)", printer.Name, printer.PortName)
	}

	return printers, nil
}

// Windows API structures
type PRINTER_INFO_2 struct {
	pServerName     *uint16
	pPrinterName    *uint16
	pShareName      *uint16
	pPortName       *uint16
	pDriverName     *uint16
	pComment        *uint16
	pLocation       *uint16
	pDevMode        uintptr
	pSepFile        *uint16
	pPrintProcessor *uint16
	pDatatype       *uint16
	pParameters     *uint16
	pSecurityDescriptor uintptr
	Attributes      uint32
	Priority        uint32
	DefaultPriority uint32
	StartTime       uint32
	UntilTime       uint32
	Status          uint32
	cJobs           uint32
	AveragePPM      uint32
}

type DOC_INFO_1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

func openPrinter(name string) (windows.Handle, error) {
	pName := windows.StringToUTF16Ptr(name)
	var hPrinter windows.Handle

	ret, _, err := procOpenPrinter.Call(
		uintptr(unsafe.Pointer(pName)),
		uintptr(unsafe.Pointer(&hPrinter)),
		uintptr(0), // defaults
	)

	if ret == 0 {
		return 0, fmt.Errorf("OpenPrinter failed: %v", err)
	}
	return hPrinter, nil
}

func closePrinter(hPrinter windows.Handle) error {
	ret, _, err := procClosePrinter.Call(uintptr(hPrinter))
	if ret == 0 {
		return fmt.Errorf("ClosePrinter failed: %v", err)
	}
	return nil
}

func startDocPrinter(hPrinter windows.Handle, level uint32, pDocInfo *byte) (uint32, error) {
	var docID uint32
	ret, _, err := procStartDocPrinter.Call(
		uintptr(hPrinter),
		uintptr(level),
		uintptr(unsafe.Pointer(pDocInfo)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("StartDocPrinter failed: %v", err)
	}
	return uint32(ret), nil
}

func endDocPrinter(hPrinter windows.Handle) error {
	ret, _, err := procEndDocPrinter.Call(uintptr(hPrinter))
	if ret == 0 {
		return fmt.Errorf("EndDocPrinter failed: %v", err)
	}
	return nil
}

func startPagePrinter(hPrinter windows.Handle) error {
	ret, _, err := procStartPagePrinter.Call(uintptr(hPrinter))
	if ret == 0 {
		return fmt.Errorf("StartPagePrinter failed: %v", err)
	}
	return nil
}

func endPagePrinter(hPrinter windows.Handle) error {
	ret, _, err := procEndPagePrinter.Call(uintptr(hPrinter))
	if ret == 0 {
		return fmt.Errorf("EndPagePrinter failed: %v", err)
	}
	return nil
}

func writePrinter(hPrinter windows.Handle, data []byte) (uint32, error) {
	var written uint32
	ret, _, err := procWritePrinter.Call(
		uintptr(hPrinter),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("WritePrinter failed: %v", err)
	}
	return written, nil
}

func extractSerialFromPort(portName string) string {
	// USB ports often contain serial info like "USB001" or more detailed
	// This is a simplified extraction - real implementation may need WMI queries
	return ""
}
