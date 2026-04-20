package server

import (
	"encoding/xml"
	"errors"
	"fmt"
	"sync/atomic"

	"epos-proxy/escpos"
	"epos-proxy/logger"
	"epos-proxy/printer"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
)

type EPOSResponse struct {
	XMLName xml.Name `xml:"response"`
	Success bool     `xml:"success,attr"`
	Code    string   `xml:"code,attr"`
	Status  string   `xml:"status,attr"`
}

type KioskRequest struct {
	Command string `json:"command"` // "open" or "close"
	URL     string `json:"url"`     // required for "open" command
}

type KioskCallbacks struct {
	OpenKiosk  func(url string) error
	CloseKiosk func() error
}

type Server struct {
	app            *fiber.App
	Port           int
	running        atomic.Bool
	kioskCallbacks *KioskCallbacks
}

func New(port int, mgr *printer.Manager) *Server {
	app := fiber.New(fiber.Config{
		AppName: "ePOS proxy",
	})
	app.Use(cors.New(cors.Config{
		AllowOrigins:        []string{"*"},
		AllowPrivateNetwork: true,
	}))

	app.Post("/p/:printerId/cgi-bin/epos/service.cgi", func(ctx fiber.Ctx) error {
		printerId := ctx.Params("printerId")
		logger.Debugf("Print request received for printer: %s", printerId)
		return printData(mgr, ctx, printerId)
	})

	app.Post("/cgi-bin/epos/service.cgi", func(ctx fiber.Ctx) error {
		logger.Debugf("Print request received (auto printer selection)")
		return printData(mgr, ctx, "")
	})

	server := &Server{app: app, Port: port}

	app.Post("/kiosk", func(ctx fiber.Ctx) error {
		println("Kiosk endpoint - handles both open and close commands")
		return server.handleKiosk(ctx)
	})
	server.running.Store(true)
	go func() {
		logger.Infof("HTTP server listening on 0.0.0.0:%d", port)
		err := app.Listen(fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil {
			logger.Error("EPOS Server Error: ", err)
		}
		server.running.Store(false)
		logger.Warn("HTTP server stopped")
	}()
	return server
}

func printData(mgr *printer.Manager, ctx fiber.Ctx, printerID string) error {
	logger.Debugf("Processing print job for printer: %s", printerID)
	jobData, err := escpos.ParseXML(ctx.Body())
	if err != nil {
		logger.Errorf("XML parsing error: %v", err)
		return ctx.XML(EPOSResponse{Success: false, Code: "SchemaError", Status: ""})
	}
	logger.Debug("XML parsed successfully")

	reply, err := mgr.WriteAsync(printerID, jobData)
	if err == nil {
		logger.Debug("Print job queued")
		result := <-reply
		if !result.OK {
			err = result.Err
		}
	}
	if err != nil {
		retCode := ""
		if errors.Is(err, printer.ErrQueueFull) {
			retCode = "TooManyRequests"
			logger.Warn("Printer queue full")
		} else {
			retCode = "EX_BADPORT"
		}
		logger.Errorf("Print error [%s]: %v, Printer ID: %s", retCode, err, printerID)
		return ctx.XML(EPOSResponse{Success: false, Code: retCode, Status: ""})
	}
	logger.Debugf("Print job completed successfully for printer: %s", printerID)
	return ctx.XML(EPOSResponse{Success: true, Code: "", Status: ""})
}

func (s *Server) Stop() error {
	logger.Infof("Stopping HTTP server")
	return s.app.Shutdown()
}

func (s *Server) Running() bool {
	return s.running.Load()
}

func (s *Server) SetKioskCallbacks(callbacks *KioskCallbacks) {
	s.kioskCallbacks = callbacks
}

func (s *Server) handleKiosk(ctx fiber.Ctx) error {
	var req KioskRequest
	if err := ctx.Bind().JSON(&req); err != nil {
		logger.Warnf("Invalid kiosk request: %v", err)
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	switch req.Command {
	case "open":
		if req.URL == "" {
			logger.Warn("Kiosk open request missing URL")
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"error":   "URL is required for open command",
			})
		}
		logger.Infof("Opening kiosk with URL: %s", req.URL)

		if s.kioskCallbacks != nil && s.kioskCallbacks.OpenKiosk != nil {
			if err := s.kioskCallbacks.OpenKiosk(req.URL); err != nil {
				logger.Errorf("Failed to open kiosk: %v", err)
				return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"success": false,
					"error":   err.Error(),
				})
			}
		}

		logger.Infof("Kiosk opened successfully")
		return ctx.JSON(fiber.Map{
			"success": true,
			"message": "Kiosk opened",
		})

	case "close":
		logger.Infof("Closing kiosk")

		if s.kioskCallbacks != nil && s.kioskCallbacks.CloseKiosk != nil {
			if err := s.kioskCallbacks.CloseKiosk(); err != nil {
				logger.Errorf("Failed to close kiosk: %v", err)
				return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"success": false,
					"error":   err.Error(),
				})
			}
		}

		logger.Infof("Kiosk closed successfully")
		return ctx.JSON(fiber.Map{
			"success": true,
			"message": "Kiosk closed",
		})

	default:
		logger.Warnf("Invalid kiosk command: %s", req.Command)
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid command. Use 'open' or 'close'",
		})
	}
}
