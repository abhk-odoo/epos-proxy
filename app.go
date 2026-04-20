package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"epos-proxy/config"
	"epos-proxy/logger"
	"epos-proxy/printer"
	"epos-proxy/server"
	"epos-proxy/util"

	autostart "github.com/emersion/go-autostart"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	webserver      *server.Server
	config         *config.Manager
	printerManager *printer.Manager
	autoStart      *autostart.App
}

func NewApp() *App {
	a := &App{}

	a.autoStart = &autostart.App{
		Name:        "epos-proxy",
		DisplayName: "ePOS Proxy",
		Exec:        []string{os.Args[0]},
	}

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
	a.printerManager = printer.NewManager()

	port, err := cfg.ResolvePort()
	if err != nil {
		logger.Warn("Unable to resolve port, using default")
	}

	a.webserver = server.New(port, a.printerManager)

	go ensureFirewallPort(port, cfg)

	// Register kiosk callbacks
	a.webserver.SetKioskCallbacks(&server.KioskCallbacks{
		OpenKiosk:  a.OpenKiosk,
		CloseKiosk: a.CloseKiosk,
	})
}

func (a *App) shutdown(ctx context.Context) {
	logger.Infof("Stopping proxy server")

	if err := a.webserver.Stop(); err != nil {
		logger.Errorf("Server stop error: %v", err)
	}
}

func ensureFirewallPort(port int, cfg *config.Manager) {
	if cfg.IsFirewallPortConfigured(port) {
		logger.Infof("Firewall port %d already configured (skipping permission prompt)", port)
		return
	}

	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("pkexec", "ufw", "status").Output()
		if err != nil {
			logger.Warnf("ufw not available: %v", err)
			return
		}
		if !strings.Contains(string(out), "Status: active") {
			logger.Info("ufw inactive, skipping firewall rule")
			return
		}
		rule := fmt.Sprintf("%d/tcp", port)
		if strings.Contains(string(out), rule) {
			logger.Infof("Firewall port %d already open", port)
			cfg.AddFirewallPort(port)
			return
		}
		if err := exec.Command("pkexec", "ufw", "allow", rule).Run(); err != nil {
			logger.Warnf("Failed to open ufw port %d: %v", port, err)
			return
		}
		logger.Infof("ufw: port %d opened", port)
		cfg.AddFirewallPort(port)

	case "windows":
		ruleName := "ePOS Proxy"
		rule := fmt.Sprintf("%d/tcp", port)

		out, _ := exec.Command("netsh", "advfirewall", "firewall", "show", "rule",
			fmt.Sprintf("name=%s", ruleName),
		).Output()
		if strings.Contains(string(out), ruleName) {
			logger.Infof("Windows firewall port %d already open", port)
			cfg.AddFirewallPort(port)
			return
		}

		psCmd := fmt.Sprintf(
			`Start-Process netsh -ArgumentList 'advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%s' -Verb RunAs -Wait`,
			ruleName, rule,
		)
		if err := exec.Command("powershell", "-Command", psCmd).Run(); err != nil {
			logger.Warnf("Failed to open Windows firewall port %d: %v", port, err)
			return
		}
		logger.Infof("Windows firewall: port %d opened", port)
		cfg.AddFirewallPort(port)
	}
}

type Printer struct {
	Name   string `json:"name"`
	Serial string `json:"serial"`
	Ip     string `json:"ip"`
	Id     string `json:"id"`
	IsLAN  bool   `json:"isLAN"`
	LANIp  string `json:"lanIp,omitempty"`
	Online bool   `json:"online"`
}

type UnavailablePrinter struct {
	Name     string `json:"name"`
	ErrorMsg string `json:"errorMsg"`
	IsLAN    bool   `json:"isLAN"`
	LANIp    string `json:"lanIp,omitempty"`
}

type Status struct {
	ServerRunning       bool                 `json:"serverRunning"`
	DefaultIp           string               `json:"defaultIp"`
	NetworkIp           string               `json:"networkIp"`
	ErrorMsg            string               `json:"errorMsg"`
	Printers            []Printer            `json:"printers"`
	UnavailablePrinters []UnavailablePrinter `json:"unavailablePrinters"`
	Os                  string               `json:"os"`
}

func (a *App) GetPrinterIp(id string) string {
	ip := fmt.Sprintf("127.0.0.1:%d/p/%s", a.webserver.Port, id)
	logger.Debugf("Generated printer endpoint: %s", ip)
	return ip
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func (a *App) Status() Status {

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
				Name:   info.VendorName + " " + info.ProductName,
				Serial: info.Serial,
				Ip:     a.GetPrinterIp(info.Id),
				Online: true,
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
		})
	}

	return Status{
		ServerRunning:       a.webserver.Running(),
		DefaultIp:           fmt.Sprintf("127.0.0.1:%d", a.webserver.Port),
		NetworkIp:           fmt.Sprintf("%s:%d", getLocalIP(), a.webserver.Port),
		Printers:            printers,
		UnavailablePrinters: unavailablePrinters,
		ErrorMsg:            errorMsg,
		Os:                  runtime.GOOS,
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

	if err := a.config.AddLANPrinter(ip); err != nil {
		return fmt.Errorf("failed to save LAN printer: %s, error: %v", ip, err)
	}

	logger.Debugf("LAN printer added successfully: %s", ip)

	return nil
}

func (a *App) ConfirmRemoveLANPrinter(ip string) (bool, error) {

	logger.Debugf("Remove LAN printer requested: %s", ip)

	result, err := wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
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
		err := a.config.RemoveLANPrinter(ip)
		return true, fmt.Errorf("Error removing LAN printer: %s, error: %v", ip, err)
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
	savePath, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save Archive",
		DefaultFilename: zipName,
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Zip Archives (*.zip)",
				Pattern:     "*.zip",
			},
		},
	})
	err = util.ZipLogs(logDir, savePath)
	if err != nil {
		logger.Errorf("Log export failed: %v", err)
		wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
			Type:    wailsruntime.ErrorDialog,
			Title:   "Download Logs Failed",
			Message: err.Error(),
		})
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

func (a *App) OpenKiosk(url string) error {
	logger.Infof("Opening kiosk with URL: %s", url)

	wailsruntime.MenuSetApplicationMenu(a.ctx, nil)

	// Navigate to the URL via event to frontend
	wailsruntime.EventsEmit(a.ctx, "kiosk_open", url)

	logger.Infof("Kiosk mode activated for URL: %s", url)
	return nil
}

func (a *App) CloseKiosk() error {
	logger.Infof("Closing kiosk mode")

	wailsruntime.EventsEmit(a.ctx, "kiosk_close")

	wailsruntime.MenuSetApplicationMenu(a.ctx, createMenu(a))

	logger.Infof("Kiosk mode closed, returned to normal mode")
	return nil
}
