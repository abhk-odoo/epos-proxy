package printer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"epos-proxy/logger"

	"github.com/google/gousb"
)

type PrinterType int

const (
	PrinterTypeUSB PrinterType = iota
	PrinterTypeLAN
)

const (
	QueueSize    = 100
	WriteTimeout = 5 * time.Second
)

var ErrNotFound = errors.New("printer not found")
var ErrQueueFull = errors.New("printer queue is full")

type JobResult struct {
	OK  bool
	Err error
}

type JobFunc func(p *Printer) JobResult

type job struct {
	run   JobFunc
	reply chan JobResult
}

type Printer struct {
	printerType PrinterType
	id          *PrinterID
	lanIP       string
	mu          sync.Mutex
	// USB fields
	usbCtx      *gousb.Context
	device      *gousb.Device
	config      *gousb.Config
	iFace       *gousb.Interface
	outEndpoint *gousb.OutEndpoint
	// LAN fields
	tcpConn net.Conn
	jobs    chan job
	// Backend for OS-claimed printers (CUPS/WinSpooler)
	backend PrintBackend
}

func newPrinter(id string) *Printer {
	// Check if this is a LAN printer
	if lanIP, ok := DecodeLANPrinterID(id); ok {
		p := &Printer{
			printerType: PrinterTypeLAN,
			lanIP:       lanIP,
			jobs:        make(chan job, QueueSize),
		}
		go p.loop()
		return p
	}

	// Check if this is an OS-managed printer (CUPS/WinSpooler)
	if backend, info, ok := getOSBackendForID(id); ok {
		p := &Printer{
			printerType: PrinterTypeUSB, // Keep USB type for compatibility
			id:          nil,
			backend:     backend,
			jobs:        make(chan job, QueueSize),
		}
		logger.Debugf("Created OS-managed printer instance for %s (backend: %s)", 
			info.Name, info.BackendType)
		go p.loop()
		return p
	}

	// USB printer (raw/direct)
	var printerID *PrinterID = nil
	if id != "" {
		printerID, _ = decodePrinterID(id)
	}

	p := &Printer{
		printerType: PrinterTypeUSB,
		id:          printerID,
		jobs:        make(chan job, QueueSize),
	}

	logger.Debugf("Created new USB printer instance for ID: %s", p.idToString())
	go p.loop()
	return p
}

// getOSBackendForID checks if a printer ID corresponds to an OS-managed printer
// and returns the appropriate backend
func getOSBackendForID(id string) (PrintBackend, UnifiedPrinterInfo, bool) {
	// Check for OS printer ID prefix
	if !strings.HasPrefix(id, "os:") {
		return nil, UnifiedPrinterInfo{}, false
	}

	// Parse OS printer ID: os:<backend>:<name>
	parts := strings.SplitN(id, ":", 3)
	if len(parts) != 3 {
		return nil, UnifiedPrinterInfo{}, false
	}

	backendType := parts[1]
	printerName := parts[2]

	switch backendType {
	case "cups":
		info := UnifiedPrinterInfo{
			ID:          id,
			Name:        printerName,
			BackendType: BackendCUPS,
			IsOSClaimed: true,
			Online:      true,
		}
		return newCUPSBackend(printerName, info), info, true

	case "winspool":
		info := UnifiedPrinterInfo{
			ID:          id,
			Name:        printerName,
			BackendType: BackendWinSpooler,
			IsOSClaimed: true,
			Online:      true,
		}
		return newWinSpoolBackend(printerName, info), info, true
	}

	return nil, UnifiedPrinterInfo{}, false
}

func (p *Printer) Enqueue(fn JobFunc, reply chan JobResult) error {
	j := job{run: fn, reply: reply}
	select {
	case p.jobs <- j:
		logger.Debugf("Enqueued print job for printer %s", p.idToString())
		return nil
	default:
		logger.Warnf("Printer queue full for printer %s", p.idToString())
		return ErrQueueFull
	}
}

func (p *Printer) Write(data []byte) error {
	// Use backend if available (OS-claimed printers)
	if p.backend != nil {
		logger.Debugf("Using OS backend to write %d bytes to printer %s", len(data), p.idToString())
		if err := p.backend.Print(data); err != nil {
			return fmt.Errorf("backend print failed for %s: %w", p.idToString(), err)
		}
		return nil
	}

	// Otherwise use direct USB/LAN
	if err := p.ensureOpen(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	logger.Debugf("Writing %d bytes to printer %s", len(data), p.idToString())

	if p.printerType == PrinterTypeLAN {
		if err := p.tcpConn.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil {
			p.closeDeviceLocked()
			return fmt.Errorf("failed to set write deadline for LAN printer %s: %w", p.idToString(), err)
		}
		if _, err := p.tcpConn.Write(data); err != nil {
			p.closeDeviceLocked()
			return fmt.Errorf("failed to write to LAN printer %s: %w", p.idToString(), err)
		}
		logger.Debugf("Successfully wrote to LAN printer %s", p.idToString())
		return nil
	}

	// USB write
	ctx, cancel := context.WithTimeout(context.Background(), WriteTimeout)
	defer cancel()
	logger.Debugf("Writing to USB printer %s with timeout %v", p.idToString(), WriteTimeout)

	if _, err := p.outEndpoint.WriteContext(ctx, data); err != nil {
		p.closeDeviceLocked()
		return fmt.Errorf("failed to write to USB printer %s: %w", p.idToString(), err)
	}
	return nil
}

func (p *Printer) loop() {
	logger.Debugf("Printer loop started for %s with %d jobs", p.idToString(), len(p.jobs))
	for j := range p.jobs {
		result := j.run(p)
		if j.reply != nil {
			j.reply <- result
			close(j.reply)
		}
		if len(p.jobs) == 0 {
			p.close()
		}
	}
}
func (p *Printer) ensureOpen() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.printerType == PrinterTypeLAN {
		return p.ensureOpenLANLocked()
	}
	return p.ensureOpenUSBLocked()
}

func (p *Printer) ensureOpenLANLocked() error {
	if p.tcpConn != nil {
		logger.Debugf("LAN printer %s already connected", p.idToString())
		return nil // already connected
	}

	addr := fmt.Sprintf("%s:%d", p.lanIP, LANPort)
	logger.Debugf("Attempting to connect to LAN printer %s at %s", p.idToString(), addr)
	conn, err := net.DialTimeout("tcp", addr, LANConnectTimeout)
	if err != nil {
		logger.Errorf("Failed to connect to LAN printer %s at %s: %v", p.idToString(), addr, err)
		return fmt.Errorf("failed to connect to LAN printer at %s: %w", addr, err)
	}

	p.tcpConn = conn
	return nil
}

func (p *Printer) ensureOpenUSBLocked() error {
	if p.device != nil {
		logger.Debugf("USB printer %s already connected", p.idToString())
		return nil // already connected
	}

	ctx := gousb.NewContext()

	var (
		eps     []EndpointInfo
		findAny = p.id == nil
	)

	devices, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		if findAny && len(eps) > 0 {
			return false
		}
		ep, ok := findPrinterEndpoint(desc)
		if ok {
			eps = append(eps, ep)
			return true
		}
		return false
	})
	if err != nil {
		_ = ctx.Close()
		return fmt.Errorf("failed to open USB device for printer %s: %w", p.idToString(), err)
	}
	if len(devices) == 0 {
		_ = ctx.Close()
		logger.Warnf("USB printer %s not found", p.idToString())
		return ErrNotFound
	}

	var (
		target   *gousb.Device
		targetEP *EndpointInfo
	)
	for i, d := range devices {
		serial, _ := d.SerialNumber()

		match := false
		if findAny {
			match = true
		} else if p.id.Serial != "" {
			match = serial == p.id.Serial
		} else if p.id.ProductID != 0 {
			match = d.Desc.Vendor == p.id.VendorID && d.Desc.Product == p.id.ProductID
		}

		if match && target == nil {
			target = d
			ep := eps[i]
			targetEP = &ep
		} else {
			_ = d.Close()
		}
	}
	if target == nil || targetEP == nil {
		_ = ctx.Close()
		return ErrNotFound
	}

	_ = target.SetAutoDetach(true)

	cfg, err := target.Config(targetEP.config)
	if err != nil {
		// Retry without auto-detach.
		_ = target.SetAutoDetach(false)
		cfg, err = target.Config(targetEP.config)
	}
	logger.Debugf("Configuring USB device %s", p.idToString())
	if err != nil {
		_ = target.Close()
		_ = ctx.Close()
		return err
	}

	iFace, err := cfg.Interface(targetEP.iFace, targetEP.alternateSetting)
	if err != nil {
		logger.Errorf("Failed to claim USB interface for printer %s: Error: %v", p.idToString(), err)
		_ = cfg.Close()
		_ = target.Close()
		_ = ctx.Close()
		return err
	}

	ep, err := iFace.OutEndpoint(targetEP.outEndpoint)
	if err != nil {
		logger.Errorf("Failed to get USB out endpoint for printer %s: Error: %v", p.idToString(), err)
		iFace.Close()
		_ = cfg.Close()
		_ = target.Close()
		_ = ctx.Close()
		return err
	}

	p.usbCtx = ctx
	p.device = target
	p.config = cfg
	p.iFace = iFace
	p.outEndpoint = ep
	return nil
}

func (p *Printer) close() {
	p.mu.Lock()
	logger.Debugf("Closing printer %s", p.idToString())
	defer p.mu.Unlock()
	p.closeDeviceLocked()
}

func (p *Printer) closeDeviceLocked() {
	// Close backend if using OS-managed printer
	if p.backend != nil {
		_ = p.backend.Close()
		p.backend = nil
		logger.Debugf("OS-managed printer %s backend closed", p.idToString())
		return
	}

	if p.printerType == PrinterTypeLAN {
		if p.tcpConn != nil {
			_ = p.tcpConn.Close()
			p.tcpConn = nil
			logger.Debugf("LAN printer %s connection closed", p.idToString())
		}
		return
	}

	// USB close
	if p.device == nil {
		return
	}
	p.iFace.Close()
	_ = p.config.Close()
	_ = p.device.Close()
	_ = p.usbCtx.Close()
	p.device = nil
	p.config = nil
	p.iFace = nil
	p.outEndpoint = nil
	p.usbCtx = nil
	logger.Debugf("USB printer %s device closed", p.idToString())
}

func (p *Printer) idToString() string {
	if p.printerType == PrinterTypeLAN {
		return fmt.Sprintf("LAN:%s", p.lanIP)
	}
	if p.id != nil {
		return fmt.Sprintf("USB:%s", p.id.Serial)
	}
	return "USB:unknown"
}
