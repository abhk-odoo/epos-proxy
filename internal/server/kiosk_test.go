package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"testing"

	"epos-proxy/internal/printer"
	"epos-proxy/internal/testutil"

	"github.com/gofiber/fiber/v3"
)

// allowLoopbackInTests makes the LAN-only middleware treat every request as
// coming from loopback, so kiosk-route tests can focus on PIN/state logic
// rather than fasthttp's fixed 0.0.0.0 test-transport IP.
func allowLoopbackInTests(t *testing.T) {
	t.Helper()
	original := clientIPFromCtx
	clientIPFromCtx = func(_ fiber.Ctx) net.IP {
		return net.ParseIP("127.0.0.1")
	}
	t.Cleanup(func() { clientIPFromCtx = original })
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	raw, err := json.Marshal(v)
	testutil.ExpectedNoError(t, err)
	return bytes.NewReader(raw)
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	testutil.ExpectedNoError(t, json.Unmarshal(body, &v))
	return v
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	data, err := io.ReadAll(r)
	testutil.ExpectedNoError(t, err)
	return data
}

func TestKioskState_NoAuthRequired(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	req := httptest.NewRequest("GET", "/api/kiosk/state", nil)
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 200)

	body := readAll(t, resp.Body)
	state := decodeJSON[kioskStateResponse](t, body)
	testutil.ExpectedEqual(t, state.URL, "")
	testutil.ExpectedEqual(t, state.Enabled, false)
	testutil.ExpectedEqual(t, state.HasPIN, false)
	testutil.ExpectedFalse(t, bytes.Contains(body, []byte("pin")), "state response must never include the PIN")
}

func TestKioskState_RejectsNonLAN(t *testing.T) {
	original := clientIPFromCtx
	clientIPFromCtx = func(_ fiber.Ctx) net.IP {
		return net.ParseIP("203.0.113.9") // public, unrelated address
	}
	t.Cleanup(func() { clientIPFromCtx = original })

	cfg := newTestConfig(t)
	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	req := httptest.NewRequest("GET", "/api/kiosk/state", nil)
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 403)
}

func TestKioskSetURL_RequiresValidPIN(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedNoError(t, cfg.SetWebViewPIN("1234"))

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	// Wrong PIN is rejected.
	req := httptest.NewRequest("POST", "/api/kiosk/url", jsonBody(t, map[string]string{
		"pin": "0000",
		"url": "https://example.com/pos",
	}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 401)
	testutil.ExpectedEqual(t, cfg.GetWebViewURL(), "")

	// Correct PIN persists the URL.
	req = httptest.NewRequest("POST", "/api/kiosk/url", jsonBody(t, map[string]string{
		"pin": "1234",
		"url": "https://example.com/pos",
	}))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 200)
	testutil.ExpectedEqual(t, cfg.GetWebViewURL(), "https://example.com/pos")
}

func TestKioskSetURL_RejectsInvalidURL(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedNoError(t, cfg.SetWebViewPIN("1234"))

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	req := httptest.NewRequest("POST", "/api/kiosk/url", jsonBody(t, map[string]string{
		"pin": "1234",
		"url": "javascript:alert(1)",
	}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 400)
	testutil.ExpectedEqual(t, cfg.GetWebViewURL(), "")
}

func TestKioskSetPIN_FirstTimeNoCurrentPINRequired(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedFalse(t, cfg.HasWebViewPIN())

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	req := httptest.NewRequest("POST", "/api/kiosk/pin", jsonBody(t, map[string]string{
		"pin":    "", // no current PIN to provide yet
		"newPin": "4321",
	}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 200)
	testutil.ExpectedTrue(t, cfg.HasWebViewPIN())
	testutil.ExpectedTrue(t, cfg.CheckWebViewPIN("4321"))
}

func TestKioskSetPIN_ChangeRequiresCurrentPIN(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedNoError(t, cfg.SetWebViewPIN("1111"))

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	// Wrong current PIN.
	req := httptest.NewRequest("POST", "/api/kiosk/pin", jsonBody(t, map[string]string{
		"pin":    "9999",
		"newPin": "2222",
	}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 401)
	testutil.ExpectedTrue(t, cfg.CheckWebViewPIN("1111"))

	// Correct current PIN.
	req = httptest.NewRequest("POST", "/api/kiosk/pin", jsonBody(t, map[string]string{
		"pin":    "1111",
		"newPin": "2222",
	}))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 200)
	testutil.ExpectedTrue(t, cfg.CheckWebViewPIN("2222"))
}

func TestKioskOpenClose_CallCallbacksAndPersistState(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedNoError(t, cfg.SetWebViewPIN("1234"))
	testutil.ExpectedNoError(t, cfg.SetWebViewURL("https://example.com/pos"))

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	var opened, closed, reloaded int
	s.SetKioskCallbacks(&KioskCallbacks{
		Open:   func() error { opened++; return nil },
		Close:  func() error { closed++; return nil },
		Reload: func() error { reloaded++; return nil },
	})

	for _, route := range []string{"open", "close", "reload"} {
		req := httptest.NewRequest("POST", "/api/kiosk/"+route, jsonBody(t, map[string]string{"pin": "1234"}))
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.app.Test(req)
		testutil.ExpectedNoError(t, err)
		testutil.ExpectedEqual(t, resp.StatusCode, 200)
	}

	testutil.ExpectedEqual(t, opened, 1)
	testutil.ExpectedEqual(t, closed, 1)
	testutil.ExpectedEqual(t, reloaded, 1)
}

func TestKioskOpen_WrongPINDoesNotCallCallback(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedNoError(t, cfg.SetWebViewPIN("1234"))

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	called := false
	s.SetKioskCallbacks(&KioskCallbacks{
		Open: func() error { called = true; return nil },
	})

	req := httptest.NewRequest("POST", "/api/kiosk/open", jsonBody(t, map[string]string{"pin": "0000"}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 401)
	testutil.ExpectedFalse(t, called)
}

func TestKioskOpen_NoCallbacksRegistered(t *testing.T) {
	allowLoopbackInTests(t)

	cfg := newTestConfig(t)
	testutil.ExpectedNoError(t, cfg.SetWebViewPIN("1234"))

	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, nil)
	defer s.Stop()

	req := httptest.NewRequest("POST", "/api/kiosk/open", jsonBody(t, map[string]string{"pin": "1234"}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 500)
}
