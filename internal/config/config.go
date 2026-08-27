package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

var ErrNoAvailablePort = errors.New("no available port in range")

const AppName = "EposProxy"

const (
	PortRangeStart = 4545
	PortRangeEnd   = 4555
)

// MaxWebViewURLLength caps the kiosk URL length to something sane.
const MaxWebViewURLLength = 2048

type AppConfig struct {
	Port        int      `json:"port"`
	LANPrinters []string `json:"lan_printers,omitempty"`
	WebViewURL  string   `json:"webview_url,omitempty"`
	// WebViewPIN is a legacy plaintext field. Only ever read during
	// migration in Load(); new PINs are always written to WebViewPINHash.
	WebViewPIN     string `json:"webview_pin,omitempty"`
	WebViewPINHash string `json:"webview_pin_hash,omitempty"`
	WebViewEnabled bool   `json:"webview_enabled"`
}

func defaults() AppConfig {
	return AppConfig{
		Port: 0,
	}
}

// ValidateWebViewURL enforces that url is a well-formed http(s) URL with a
// non-empty host and a sane length. Shared by the Wails-bound desktop path
// and the HTTP kiosk API so both entry points apply identical rules.
func ValidateWebViewURL(raw string) error {
	if raw == "" {
		return errors.New("URL cannot be empty")
	}
	if len(raw) > MaxWebViewURLLength {
		return fmt.Errorf("URL is too long (max %d characters)", MaxWebViewURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("URL must use http or https")
	}
	if u.Host == "" {
		return errors.New("URL must include a host")
	}
	return nil
}

type Manager struct {
	mu   sync.RWMutex
	path string
	Data AppConfig
}

func NewManager() (*Manager, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("cannot locate user config dir: %w", err)
	}

	dir := filepath.Join(base, AppName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create config dir: %w", err)
	}

	return &Manager{
		path: filepath.Join(dir, "config.json"),
		Data: defaults(),
	}, nil
}

func (cm *Manager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config read error: %w", err)
	}

	if err := json.Unmarshal(data, &cm.Data); err != nil {
		return fmt.Errorf("config parse error: %w", err)
	}

	// One-time migration: legacy installs stored the PIN in plaintext.
	// Hash it, drop the plaintext, and persist the migration.
	if cm.Data.WebViewPIN != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(cm.Data.WebViewPIN), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("PIN migration failed: %w", err)
		}
		cm.Data.WebViewPINHash = string(hash)
		cm.Data.WebViewPIN = ""
		if err := cm.saveLocked(); err != nil {
			return fmt.Errorf("PIN migration save failed: %w", err)
		}
	}

	return nil
}

func (cm *Manager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.saveLocked()
}

func (cm *Manager) saveLocked() error {
	data, err := json.MarshalIndent(cm.Data, "", "  ")
	if err != nil {
		return fmt.Errorf("config marshal error: %w", err)
	}
	if err := os.WriteFile(cm.path, data, 0644); err != nil {
		return fmt.Errorf("config write error: %w", err)
	}
	return nil
}

func (cm *Manager) Path() string { return cm.path }

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func findAvailablePort(start, end int) (int, error) {
	for p := start; p <= end; p++ {
		if isPortAvailable(p) {
			return p, nil
		}
	}

	return 0, fmt.Errorf("no available port found in range %d-%d: %w", start, end, ErrNoAvailablePort)
}

func (cm *Manager) ResolvePort() (int, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.Data.Port > 0 && isPortAvailable(cm.Data.Port) {
		return cm.Data.Port, nil
	}

	port, err := findAvailablePort(PortRangeStart, PortRangeEnd)
	if err != nil {
		return 0, err
	}

	cm.Data.Port = port
	if err := cm.saveLocked(); err != nil {
		log.Printf("[config] warning: could not save: %v\n", err)
	}
	return port, nil
}

func (cm *Manager) AddLanEposPrinter(ip string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, existing := range cm.Data.LANPrinters {
		if existing == ip {
			return nil // Already exists
		}
	}
	cm.Data.LANPrinters = append(cm.Data.LANPrinters, ip)
	return cm.saveLocked()
}

func (cm *Manager) RemoveLANPrinter(ip string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for i, existing := range cm.Data.LANPrinters {
		if existing == ip {
			cm.Data.LANPrinters = append(cm.Data.LANPrinters[:i], cm.Data.LANPrinters[i+1:]...)
			return cm.saveLocked()
		}
	}
	return nil // Not found, nothing to remove
}

// GetWebViewURL returns the configured kiosk URL.
func (cm *Manager) GetWebViewURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.Data.WebViewURL
}

// GetWebViewEnabled returns whether kiosk mode is enabled.
func (cm *Manager) GetWebViewEnabled() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.Data.WebViewEnabled
}

// HasWebViewPIN reports whether a PIN has been configured.
func (cm *Manager) HasWebViewPIN() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.Data.WebViewPINHash != ""
}

// SetWebViewURL validates and persists the kiosk URL.
func (cm *Manager) SetWebViewURL(rawURL string) error {
	if err := ValidateWebViewURL(rawURL); err != nil {
		return err
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.Data.WebViewURL = rawURL
	return cm.saveLocked()
}

// SetWebViewPIN validates (exactly 4 digits) and persists the PIN as a
// bcrypt hash. The plaintext value is never stored.
func (cm *Manager) SetWebViewPIN(pin string) error {
	if err := validatePINFormat(pin); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash PIN: %w", err)
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.Data.WebViewPINHash = string(hash)
	return cm.saveLocked()
}

// validatePINFormat enforces the 4-digit numeric PIN rule.
func validatePINFormat(pin string) error {
	if len(pin) != 4 {
		return errors.New("PIN must be exactly 4 digits")
	}
	for _, ch := range pin {
		if ch < '0' || ch > '9' {
			return errors.New("PIN must contain digits only")
		}
	}
	return nil
}

// CheckWebViewPIN returns true when raw matches the stored PIN hash.
// Returns false (never an error) when no PIN has been configured yet.
func (cm *Manager) CheckWebViewPIN(raw string) bool {
	cm.mu.RLock()
	hash := cm.Data.WebViewPINHash
	cm.mu.RUnlock()

	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw)) == nil
}

// SetWebViewEnabled persists the enabled flag.
func (cm *Manager) SetWebViewEnabled(v bool) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.Data.WebViewEnabled = v
	return cm.saveLocked()
}

func (cm *Manager) GetLANPrinters() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.Data.LANPrinters == nil {
		return []string{}
	}
	// Return a copy to avoid races if caller modifies the slice
	result := make([]string, len(cm.Data.LANPrinters))
	copy(result, cm.Data.LANPrinters)
	return result
}
