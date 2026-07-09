package printer

import (
	"fmt"
	"strings"

	"github.com/google/gousb"
)

// Some thermal printers do not expose the standard USB printer class (0x07)
// and instead use vendor-specific interfaces. These known VID:PID pairs are
// treated as printers even when printer-class detection fails.
var printerRegistry = map[string]PrinterType{
	// Receipt printers
	"2aaf:6015": PrinterTypeReceipt, // Essae thermal
	"04b8:0e32": PrinterTypeReceipt, // Epson thermal
	"04b8:0202": PrinterTypeReceipt, // Epson thermal
	"04b8:0203": PrinterTypeReceipt, // Epson thermal
	"2d84:c7c8": PrinterTypeReceipt, // Zhuhai Poskey
	"4b43:3830": PrinterTypeReceipt, // Caysn
	"0483:5720": PrinterTypeReceipt, // STMicroelectronics

	// Label printers
	"0a5f:0187": PrinterTypeLabel, // Zebra ZD421
}

func isKnownPrinter(desc *gousb.DeviceDesc) bool {
	vidPid := strings.ToLower(
		fmt.Sprintf("%04x:%04x", uint16(desc.Vendor), uint16(desc.Product)),
	)
	_, ok := printerRegistry[vidPid]
	return ok
}

func getPrinterType(vidPid string) PrinterType {
	if printerType, ok := printerRegistry[strings.ToLower(vidPid)]; ok {
		return printerType
	}
	return PrinterTypeReceipt
}
