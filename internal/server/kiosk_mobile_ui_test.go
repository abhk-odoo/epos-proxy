package server

import (
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"epos-proxy/internal/printer"
	"epos-proxy/internal/testutil"

	"github.com/gofiber/fiber/v3"
)

// distDir locates the built frontend/dist directory relative to this test
// file, so the test exercises the real Vite output (both entry points) the
// same way main.go's go:embed does in production.
func distDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "frontend", "dist")
	if _, err := os.Stat(filepath.Join(dir, "kiosk.html")); err != nil {
		t.Skipf("frontend/dist/kiosk.html not built, skipping mobile UI integration test (%v)", err)
	}
	return dir
}

func TestKioskMobileUI_ServesIndexAndAssets(t *testing.T) {
	allowLoopbackInTests(t)

	assets := os.DirFS(distDir(t))
	cfg := newTestConfig(t)
	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, assets)
	defer s.Stop()

	// GET /kiosk should serve kiosk.html (via IndexNames), not index.html.
	req := httptest.NewRequest("GET", "/kiosk", nil)
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 200)

	body := readAll(t, resp.Body)
	testutil.ExpectedContains(t, string(body), "Kiosk Management")
	testutil.ExpectedContains(t, string(body), `src="/assets/`)

	// The desktop entry must remain unaffected — /kiosk must not leak
	// into serving the desktop index.html content.
	testutil.ExpectedFalse(t, strings.Contains(string(body), "./src/main.tsx"), "must not serve the desktop entry point")
}

func TestKioskMobileUI_ServesAssets(t *testing.T) {
	allowLoopbackInTests(t)

	assets := os.DirFS(distDir(t))

	// Find the hashed CSS/JS filenames actually produced by the build so
	// the test doesn't hardcode a content hash.
	entries, err := os.ReadDir(filepath.Join(distDir(t), "assets"))
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedTrue(t, len(entries) > 0, "expected at least one built asset")

	cfg := newTestConfig(t)
	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, assets)
	defer s.Stop()

	req := httptest.NewRequest("GET", "/assets/"+entries[0].Name(), nil)
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 200)
}

func TestKioskMobileUI_RejectsNonLAN(t *testing.T) {
	original := clientIPFromCtx
	clientIPFromCtx = func(_ fiber.Ctx) net.IP { return net.ParseIP("203.0.113.9") }
	t.Cleanup(func() { clientIPFromCtx = original })

	assets := os.DirFS(distDir(t))
	cfg := newTestConfig(t)
	port := testutil.GetFreePort(t)
	s := New(port, printer.NewManager(), cfg, assets)
	defer s.Stop()

	req := httptest.NewRequest("GET", "/kiosk", nil)
	resp, err := s.app.Test(req)
	testutil.ExpectedNoError(t, err)
	testutil.ExpectedEqual(t, resp.StatusCode, 403)
}
