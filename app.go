package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"time"

	"epos-proxy/internal/config"
	"epos-proxy/internal/kiosk"
	"epos-proxy/internal/logger"
	"epos-proxy/internal/printer"
	"epos-proxy/internal/server"
	"epos-proxy/internal/util"

	autostart "github.com/emersion/go-autostart"
	"github.com/wailsapp/wails/v2/pkg/menu"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// dialoger abstracts the Wails runtime dialog calls. Production code uses
// runtimeDialogs; tests substitute a fake so the dialog-driven code paths can
// be exercised without a live Wails context.
type dialoger interface {
	Message(ctx context.Context, opts wailsruntime.MessageDialogOptions) (string, error)
	SaveFile(ctx context.Context, opts wailsruntime.SaveDialogOptions) (string, error)
}

// runtimeDialogs forwards to the real Wails runtime.
type runtimeDialogs struct{}

func (runtimeDialogs) Message(ctx context.Context, opts wailsruntime.MessageDialogOptions) (string, error) {
	return wailsruntime.MessageDialog(ctx, opts)
}

func (runtimeDialogs) SaveFile(ctx context.Context, opts wailsruntime.SaveDialogOptions) (string, error) {
	return wailsruntime.SaveFileDialog(ctx, opts)
}

// App struct
type App struct {
	ctx            context.Context
	webserver      *server.Server
	config         *config.Manager
	printerManager *printer.Manager
	autoStart      *autostart.App
	dialogs        dialoger
	appMenu        *menu.Menu // stored so kiosk mode can hide/restore the menu bar
	// mobileAssets serves the mobile kiosk-management UI (/kiosk) over
	// HTTP. Same embed.FS as the desktop Wails asset server (main.go),
	// rooted at frontend/dist.
	mobileAssets fs.FS
}

// dlg returns the dialog backend, defaulting to the Wails runtime so an App
// built as a bare struct literal still behaves correctly.
func (a *App) dlg() dialoger {
	if a.dialogs == nil {
		return runtimeDialogs{}
	}
	return a.dialogs
}

// showError surfaces an error to the user and logs any failure to do so.
func (a *App) showError(title, message string) {
	if _, err := a.dlg().Message(a.ctx, wailsruntime.MessageDialogOptions{
		Type:    wailsruntime.ErrorDialog,
		Title:   title,
		Message: message,
	}); err != nil {
		logger.Errorf("Failed to show error dialog %q: %v", title, err)
	}
}

type Printer struct {
	Name   string `json:"name"`
	Ip     string `json:"ip"`
	Id     string `json:"id"`
	IsLAN  bool   `json:"isLAN"`
	LANIp  string `json:"lanIp,omitempty"`
	Online bool   `json:"online"`
	Type   string `json:"type"`
}

type UnavailablePrinter struct {
	Name     string `json:"name"`
	ErrorMsg string `json:"errorMsg"`
	IsLAN    bool   `json:"isLAN"`
	LANIp    string `json:"lanIp,omitempty"`
}

type AppVariable struct {
	ServerRunning bool   `json:"serverRunning"`
	DefaultIp     string `json:"defaultIp"`
	Os            string `json:"os"`
}

// WebViewConfig is the public view of kiosk settings (PIN is never exposed).
type WebViewConfig struct {
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
	HasPIN  bool   `json:"hasPIN"`
}

type Printers struct {
	ErrorMsg            string               `json:"errorMsg"`
	Printers            []Printer            `json:"printers"`
	UnavailablePrinters []UnavailablePrinter `json:"unavailablePrinters"`
}

func NewApp() *App {
	a := &App{}

	a.autoStart = &autostart.App{
		Name:        "epos-proxy",
		DisplayName: "ePOS Proxy",
		Exec:        []string{os.Args[0]},
	}
	a.printerManager = printer.NewManager()
	a.dialogs = runtimeDialogs{}

	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	logger.Debugf("Application startup")

	cfg, err := config.NewManager()
	if err != nil {
		logger.Fatalf("Config initialization failed: %v", err)
	}

	if err := cfg.Load(); err != nil {
		logger.Warnf("Config load warning: %v", err)
	}

	logger.Debugf("Config loaded from %s", cfg.Path())

	a.config = cfg

	port, err := cfg.ResolvePort()
	if err != nil {
		logger.Warn("Unable to resolve port, using default")
	}

	mobileAssets := a.mobileAssets
	if mobileAssets != nil {
		sub, err := fs.Sub(mobileAssets, "frontend/dist")
		if err != nil {
			logger.Errorf("Failed to prepare mobile UI assets: %v", err)
			mobileAssets = nil
		} else {
			mobileAssets = sub
		}
	}

	a.webserver = server.New(port, a.printerManager, a.config, mobileAssets)
	a.webserver.SetKioskCallbacks(&server.KioskCallbacks{
		Open:   a.kioskOpen,
		Close:  a.kioskClose,
		Reload: a.kioskReload,
	})
}

func (a *App) shutdown(ctx context.Context) {
	logger.Infof("Stopping proxy server")

	if err := a.webserver.Stop(); err != nil {
		logger.Errorf("Server stop error: %v", err)
	}
}

// kioskOpen enables kiosk mode and enters fullscreen. Used both by the
// desktop toggle and the /api/kiosk/open route driven from the mobile UI.
func (a *App) kioskOpen() error {
	if err := a.config.SetWebViewEnabled(true); err != nil {
		return err
	}
	a.SetWindowFullscreen(true)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "kiosk:state-changed")
	}
	return nil
}

// kioskClose disables kiosk mode and exits fullscreen. Used both by the
// desktop toggle and the /api/kiosk/close route driven from the mobile UI.
func (a *App) kioskClose() error {
	if err := a.config.SetWebViewEnabled(false); err != nil {
		return err
	}
	a.SetWindowFullscreen(false)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "kiosk:state-changed")
	}
	return nil
}

// kioskReload asks the desktop kiosk overlay (if active) to reload its
// iframe. Used by the /api/kiosk/reload route driven from the mobile UI.
func (a *App) kioskReload() error {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "kiosk:reload")
	}
	return nil
}

func (a *App) AppVariable() AppVariable {
	return AppVariable{
		Os:            runtime.GOOS,
		ServerRunning: a.webserver.Running(),
		DefaultIp:     fmt.Sprintf("127.0.0.1:%d", a.webserver.Port),
	}
}

func (a *App) GetPrinterIp(id string) string {
	ip := fmt.Sprintf("127.0.0.1:%d/p/%s", a.webserver.Port, id)
	logger.Debugf("Generated printer endpoint: %s", ip)
	return ip
}

func (a *App) Printers() Printers {

	logger.Debug("Collecting printer status")

	printers := make([]Printer, 0)
	unavailablePrinters := make([]UnavailablePrinter, 0)

	printerInfos, err := printer.ListUSBPrinters()
	errorMsg := ""

	if err == nil {

		logger.Debugf("Detected %d available USB printers", len(printerInfos.Available))

		for _, info := range printerInfos.Available {
			printers = append(printers, Printer{
				Id:     info.Id,
				Name:   info.Name,
				Ip:     a.GetPrinterIp(info.Id),
				Online: true,
				Type:   string(info.Type),
			})
		}

		for _, info := range printerInfos.Unavailable {
			unavailablePrinters = append(unavailablePrinters, UnavailablePrinter{
				Name:     info.Name,
				ErrorMsg: info.Error,
			})

			logger.Warnf("USB printer unavailable: %s (%s)", info.Name, info.Error)
		}
	} else {
		errorMsg = err.Error()
		logger.Errorf("USB printer detection failed: %v", err)
	}

	lanPrinters := printer.ListLANPrinters(a.config)

	for _, info := range lanPrinters {
		printers = append(printers, Printer{
			Id:    info.Id,
			Name:  fmt.Sprintf("Network - %s", info.IP),
			Ip:    a.GetPrinterIp(info.Id),
			IsLAN: true,
			LANIp: info.IP,
			Type:  string(printer.TypeReceipt),
		})
	}

	return Printers{
		Printers:            printers,
		UnavailablePrinters: unavailablePrinters,
		ErrorMsg:            errorMsg,
	}
}

func (a *App) AddLANPrinter(ip string) error {

	logger.Debugf("Adding LAN printer: %s", ip)

	ip, err := printer.ValidateIPAddress(ip)
	if err != nil {
		return fmt.Errorf("invalid IP address: %s, error: %v", ip, err)
	}

	if err := printer.CheckLANPrinter(ip); err != nil {
		return fmt.Errorf("LAN printer unreachable: %s, error: %v", ip, err)
	}

	if err := a.config.AddLanEposPrinter(ip); err != nil {
		return fmt.Errorf("failed to save LAN printer: %s, error: %v", ip, err)
	}

	logger.Debugf("LAN printer added successfully: %s", ip)

	return nil
}

// ─── WebView / Kiosk ──────────────────────────────────────────────────────────

// GetWebViewConfig returns the public kiosk configuration (URL, enabled flag,
// and whether a PIN has been set). The PIN itself is never returned.
func (a *App) GetWebViewConfig() WebViewConfig {
	return WebViewConfig{
		URL:     a.config.GetWebViewURL(),
		Enabled: a.config.GetWebViewEnabled(),
		HasPIN:  a.config.HasWebViewPIN(),
	}
}

// SetWebViewURL persists the kiosk URL.
func (a *App) SetWebViewURL(url string) error {
	logger.Debugf("Setting WebView URL")
	return a.config.SetWebViewURL(url)
}

// SetWebViewPIN validates and persists the 4-digit kiosk PIN.
func (a *App) SetWebViewPIN(pin string) error {
	logger.Debug("Setting WebView PIN")
	return a.config.SetWebViewPIN(pin)
}

// ValidateWebViewPIN returns true when pin matches the stored PIN.
// The incoming value is compared but never logged.
func (a *App) ValidateWebViewPIN(pin string) bool {
	return a.config.CheckWebViewPIN(pin)
}

// SetWebViewEnabled persists the kiosk-enabled flag.
func (a *App) SetWebViewEnabled(v bool) error {
	logger.Debugf("Setting WebView enabled: %v", v)
	return a.config.SetWebViewEnabled(v)
}

// RemoteKioskInfo is what the desktop "Remote Kiosk Management" section
// needs to render: the list of LAN IPs to choose from, the mobile URL for
// the selected one, and a QR code encoding that same URL.
type RemoteKioskInfo struct {
	LANIPs    []string `json:"lanIPs"`
	MobileURL string   `json:"mobileURL"`
	QRDataURI string   `json:"qrDataURI"`
}

// GetRemoteKioskInfo returns the mobile kiosk-management URL and a QR code
// for it, for the given LAN IP. If ip is empty, the first detected
// non-loopback IPv4 address is used. The QR always encodes exactly
// http://<ip>:<port>/kiosk — no pairing code, token, or PIN.
func (a *App) GetRemoteKioskInfo(ip string) (RemoteKioskInfo, error) {
	ips, err := kiosk.LocalIPv4Addresses()
	if err != nil {
		return RemoteKioskInfo{}, fmt.Errorf("failed to list LAN addresses: %w", err)
	}

	selected := ip
	if selected == "" && len(ips) > 0 {
		selected = ips[0]
	}

	info := RemoteKioskInfo{LANIPs: ips}
	if selected == "" {
		// No LAN interface available (e.g. offline machine) — nothing to
		// show, but not an error.
		return info, nil
	}

	info.MobileURL = kiosk.MobileKioskURL(selected, a.webserver.Port)
	qr, err := kiosk.GenerateQRDataURI(info.MobileURL)
	if err != nil {
		return RemoteKioskInfo{}, fmt.Errorf("failed to generate QR code: %w", err)
	}
	info.QRDataURI = qr

	return info, nil
}

// SetWindowFullscreen puts the main Wails window into or out of fullscreen
// and hides/restores the native menu bar accordingly.
func (a *App) SetWindowFullscreen(fullscreen bool) {
	if a.ctx == nil {
		return
	}
	if fullscreen {
		wailsruntime.WindowFullscreen(a.ctx)
		// Hide the native menu bar in kiosk mode
		setNativeMenubarVisible(false)
		wailsruntime.MenuSetApplicationMenu(a.ctx, menu.NewMenu())
		wailsruntime.MenuUpdateApplicationMenu(a.ctx)
	} else {
		wailsruntime.WindowUnfullscreen(a.ctx)
		// Restore the menu bar when leaving kiosk mode
		setNativeMenubarVisible(true)
		if a.appMenu == nil {
			a.appMenu = createMenu(a)
		}
		wailsruntime.MenuSetApplicationMenu(a.ctx, a.appMenu)
		wailsruntime.MenuUpdateApplicationMenu(a.ctx)
	}
}

func (a *App) ConfirmRemoveLANPrinter(ip string) (bool, error) {

	logger.Debugf("Remove LAN printer requested: %s", ip)

	result, err := a.dlg().Message(a.ctx, wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.QuestionDialog,
		Title:         "Remove Printer",
		Message:       fmt.Sprintf("Are you sure you want to remove the printer at %s?", ip),
		Buttons:       []string{"Cancel", "Confirm"},
		DefaultButton: "Cancel",
		CancelButton:  "Cancel",
	})
	if err != nil {
		return false, fmt.Errorf("failed to show confirmation dialog: %w", err)
	}
	if result == "Confirm" || result == "Yes" {
		if err := a.config.RemoveLANPrinter(ip); err != nil {
			return false, fmt.Errorf("failed to remove LAN printer: %w", err)
		}
		return true, nil
	}
	logger.Infof("Remove LAN printer cancelled, Remove printer dialog result: %s", result)
	return false, nil
}

func (a *App) CheckLANPrinterStatus(ip string) bool {
	logger.Debugf("Checking LAN printer status: %s", ip)
	return printer.CheckLANPrinter(ip) == nil
}

func (a *App) DownloadLogs() {
	logger.Debugf("Download logs requested")
	logDir := logger.LogDirectory()
	zipName := fmt.Sprintf("epos-proxy-logs-%s.zip",
		time.Now().Format("2006-01-02"))
	logger.Debugf("Creating logs archive: %s", zipName)
	savePath, err := a.dlg().SaveFile(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save Archive",
		DefaultFilename: zipName,
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Zip Archives (*.zip)",
				Pattern:     "*.zip",
			},
		},
	})
	if err != nil {
		logger.Errorf("Save dialog failed: %v", err)
		a.showError("Download Logs Failed", err.Error())
		return
	}

	// An empty path means the user dismissed the save dialog.
	if savePath == "" {
		logger.Infof("Download logs cancelled by user")
		return
	}

	if err := util.ZipLogs(logDir, savePath); err != nil {
		logger.Errorf("Log export failed: %v", err)
		a.showError("Download Logs Failed", err.Error())
		return
	}
	logger.Infof("Logs successfully exported to: %s", savePath)
}

func (a *App) IsAutostartEnabled() bool {
	return a.autoStart.IsEnabled()
}

func (a *App) EnableAutostart() error {
	logger.Info("Enabling autostart")

	if runtime.GOOS == "linux" {
		return util.EnableLinuxAutostart()
	}

	if !a.autoStart.IsEnabled() {
		return a.autoStart.Enable()
	}

	return nil
}

func (a *App) DisableAutostart() error {
	logger.Info("Disabling autostart")

	if a.autoStart.IsEnabled() {
		return a.autoStart.Disable()
	}

	return nil
}
