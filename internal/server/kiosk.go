package server

import (
	"errors"
	"io/fs"

	"epos-proxy/internal/config"
	"epos-proxy/internal/logger"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
)

var (
	errInvalidPIN       = errors.New("invalid PIN")
	errKioskUnavailable = errors.New("kiosk control is not available")
)

// kioskStateResponse mirrors the desktop's WebViewConfig shape. The PIN
// itself is never included.
type kioskStateResponse struct {
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	KioskActive bool   `json:"kioskActive"`
	HasPIN      bool   `json:"hasPIN"`
}

func kioskState(cfg *config.Manager) kioskStateResponse {
	url := cfg.GetWebViewURL()
	enabled := cfg.GetWebViewEnabled()
	return kioskStateResponse{
		URL:     url,
		Enabled: enabled,
		// The desktop app enters fullscreen kiosk mode whenever it is
		// enabled and a URL is configured; there is no separate tracked
		// "active" state on the Go side, so mirror that same rule here.
		KioskActive: enabled && url != "",
		HasPIN:      cfg.HasWebViewPIN(),
	}
}

func kioskErrorJSON(ctx fiber.Ctx, status int, err error) error {
	return ctx.Status(status).JSON(fiber.Map{"error": err.Error()})
}

// registerKioskRoutes wires GET /api/kiosk/state and the PIN-gated mutating
// routes. Every handler is a thin wrapper: parse body, check PIN, call the
// existing config.Manager / KioskCallbacks methods, respond.
func registerKioskRoutes(app *fiber.App, cfg *config.Manager, server *Server) {
	api := app.Group("/api/kiosk", sameLANOnly())

	api.Get("/state", func(ctx fiber.Ctx) error {
		return ctx.JSON(kioskState(cfg))
	})

	api.Post("/url", func(ctx fiber.Ctx) error {
		var req struct {
			PIN string `json:"pin"`
			URL string `json:"url"`
		}
		if err := ctx.Bind().JSON(&req); err != nil {
			return kioskErrorJSON(ctx, fiber.StatusBadRequest, err)
		}
		if !cfg.CheckWebViewPIN(req.PIN) {
			return kioskErrorJSON(ctx, fiber.StatusUnauthorized, errInvalidPIN)
		}
		if err := cfg.SetWebViewURL(req.URL); err != nil {
			return kioskErrorJSON(ctx, fiber.StatusBadRequest, err)
		}
		logger.Infof("Kiosk URL updated via mobile UI")
		return ctx.JSON(kioskState(cfg))
	})

	api.Post("/pin", func(ctx fiber.Ctx) error {
		var req struct {
			PIN    string `json:"pin"`
			NewPIN string `json:"newPin"`
		}
		if err := ctx.Bind().JSON(&req); err != nil {
			return kioskErrorJSON(ctx, fiber.StatusBadRequest, err)
		}
		// A PIN must already exist and match, unless none has been set
		// yet — in that case this call establishes the first PIN.
		if cfg.HasWebViewPIN() && !cfg.CheckWebViewPIN(req.PIN) {
			return kioskErrorJSON(ctx, fiber.StatusUnauthorized, errInvalidPIN)
		}
		if err := cfg.SetWebViewPIN(req.NewPIN); err != nil {
			return kioskErrorJSON(ctx, fiber.StatusBadRequest, err)
		}
		logger.Infof("Kiosk PIN updated via mobile UI")
		return ctx.JSON(kioskState(cfg))
	})

	api.Post("/open", func(ctx fiber.Ctx) error {
		return withPIN(ctx, cfg, func() error {
			cb := server.callbacks()
			if cb == nil || cb.Open == nil {
				return errKioskUnavailable
			}
			return cb.Open()
		})
	})

	api.Post("/close", func(ctx fiber.Ctx) error {
		return withPIN(ctx, cfg, func() error {
			cb := server.callbacks()
			if cb == nil || cb.Close == nil {
				return errKioskUnavailable
			}
			return cb.Close()
		})
	})

	api.Post("/reload", func(ctx fiber.Ctx) error {
		return withPIN(ctx, cfg, func() error {
			cb := server.callbacks()
			if cb == nil || cb.Reload == nil {
				return errKioskUnavailable
			}
			return cb.Reload()
		})
	})
}

// withPIN parses the {"pin": "..."} body, validates it, runs action, and
// responds with the refreshed kiosk state. Shared by the open/close/reload
// handlers, which take no other input.
func withPIN(ctx fiber.Ctx, cfg *config.Manager, action func() error) error {
	var req struct {
		PIN string `json:"pin"`
	}
	if err := ctx.Bind().JSON(&req); err != nil {
		return kioskErrorJSON(ctx, fiber.StatusBadRequest, err)
	}
	if !cfg.CheckWebViewPIN(req.PIN) {
		return kioskErrorJSON(ctx, fiber.StatusUnauthorized, errInvalidPIN)
	}
	if err := action(); err != nil {
		return kioskErrorJSON(ctx, fiber.StatusInternalServerError, err)
	}
	return ctx.JSON(kioskState(cfg))
}

// registerKioskMobileUI serves the mobile kiosk-management SPA at /kiosk,
// reusing the same built frontend assets embedded for the desktop Wails
// window. assets is expected to be rooted at "frontend/dist" (the same
// directory Vite writes both the desktop and mobile bundles into). Access
// is restricted to the local network, same as the /api/kiosk/* routes.
func registerKioskMobileUI(app *fiber.App, assets fs.FS) {
	if assets == nil {
		logger.Warn("Kiosk mobile UI assets not provided; /kiosk will be unavailable")
		return
	}

	app.Get("/kiosk", sameLANOnly(), static.New("", static.Config{
		FS:         assets,
		IndexNames: []string{"kiosk.html"},
	}))

	// Vite emits hashed JS/CSS chunks under /assets for every entry point
	// (desktop and mobile share one dist/ directory), so the mobile page
	// needs that same prefix served alongside /kiosk.
	app.Get("/assets/*", static.New("assets", static.Config{FS: assets}))
}
