package printer

import (
	"fmt"
	"sync"

	"epos-proxy/logger"
)

// BackendType identifies the printing backend implementation
type BackendType int

const (
	BackendLibUSB BackendType = iota
	BackendCUPS
	BackendWinSpooler
	BackendTCP
)

func (bt BackendType) String() string {
	switch bt {
	case BackendLibUSB:
		return "libusb"
	case BackendCUPS:
		return "cups"
	case BackendWinSpooler:
		return "winspool"
	case BackendTCP:
		return "tcp"
	default:
		return "unknown"
	}
}

// PrintBackend is the common interface for all printer backends
type PrintBackend interface {
	// Print sends data to the printer. The backend forwards data appropriately:
	// - OS backends: forward to OS driver
	// - Raw backends: send bytes directly
	Print(data []byte) error

	// GetInfo returns printer identification and routing info
	GetInfo() UnifiedPrinterInfo

	// Close releases any resources held by the backend
	Close() error
}

// UnifiedPrinterInfo contains unified printer information for UI display
type UnifiedPrinterInfo struct {
	ID          string      // Unique identifier
	Name        string      // Display name
	BackendType BackendType // Which backend handles this printer
	IsOSClaimed bool        // true = routed via OS spooler
	IsLAN       bool        // true = network printer
	LANIP       string      // IP address for LAN printers (if IsLAN)
	Online      bool        // Availability status
}

// backendRegistry manages active backends
// For OS-claimed printers, each printer has its own backend instance
// For USB/LAN printers, backends may be shared or per-printer
type backendRegistry struct {
	mu       sync.Mutex
	backends map[string]PrintBackend // key: printer ID
}

var globalRegistry = &backendRegistry{
	backends: make(map[string]PrintBackend),
}

// GetOrCreateBackend returns an existing backend or creates a new one
// This is used by the detector to ensure consistent backend instances
func GetOrCreateBackend(info UnifiedPrinterInfo, factory func() (PrintBackend, error)) (PrintBackend, error) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	if backend, ok := globalRegistry.backends[info.ID]; ok {
		logger.Debugf("Reusing existing backend for printer %s", info.ID)
		return backend, nil
	}

	backend, err := factory()
	if err != nil {
		return nil, fmt.Errorf("failed to create backend for %s: %w", info.ID, err)
	}

	globalRegistry.backends[info.ID] = backend
	logger.Debugf("Created new backend for printer %s (type: %s)", info.ID, info.BackendType)
	return backend, nil
}

// RemoveBackend removes a backend from the registry
func RemoveBackend(id string) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	if backend, ok := globalRegistry.backends[id]; ok {
		_ = backend.Close()
		delete(globalRegistry.backends, id)
		logger.Debugf("Removed backend for printer %s", id)
	}
}

// CloseAllBackends closes all registered backends
func CloseAllBackends() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	for id, backend := range globalRegistry.backends {
		_ = backend.Close()
		delete(globalRegistry.backends, id)
		logger.Debugf("Closed backend for printer %s", id)
	}
}
