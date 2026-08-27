package server

import (
	"net"
	"net/http/httptest"
	"testing"

	"epos-proxy/internal/testutil"

	"github.com/gofiber/fiber/v3"
)

func mustParseCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(cidr)
	testutil.ExpectedNoError(t, err)
	return ipNet
}

func TestIsLocalNetworkAddr(t *testing.T) {
	subnets := []*net.IPNet{
		mustParseCIDR(t, "192.168.1.0/24"),
		mustParseCIDR(t, "10.0.0.0/8"),
	}

	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"loopback v4 always allowed", "127.0.0.1", true},
		{"loopback v6 always allowed", "::1", true},
		{"address inside first subnet", "192.168.1.42", true},
		{"address inside second subnet", "10.5.6.7", true},
		{"address outside all subnets", "8.8.8.8", false},
		{"address just outside first subnet", "192.168.2.1", false},
		{"nil ip is rejected", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ip net.IP
			if tc.ip != "" {
				ip = net.ParseIP(tc.ip)
			}
			testutil.ExpectedEqual(t, isLocalNetworkAddr(ip, subnets), tc.expected)
		})
	}
}

func TestIsLocalNetworkAddr_NoSubnets(t *testing.T) {
	// With no known local subnets (e.g. interface enumeration failed),
	// only loopback should be allowed.
	testutil.ExpectedTrue(t, isLocalNetworkAddr(net.ParseIP("127.0.0.1"), nil))
	testutil.ExpectedFalse(t, isLocalNetworkAddr(net.ParseIP("192.168.1.1"), nil))
}

func TestLocalSubnets_ExcludesLoopback(t *testing.T) {
	subnets := localSubnets()
	for _, subnet := range subnets {
		testutil.ExpectedFalse(t, subnet.IP.IsLoopback(), "loopback interface should not appear in localSubnets()")
	}
}

// TestSameLANOnly_Middleware exercises the actual Fiber middleware, with
// clientIPFromCtx swapped out so the test can drive both allowed and
// rejected client addresses without depending on the test transport's
// (always 0.0.0.0) reported IP.
func TestSameLANOnly_Middleware(t *testing.T) {
	original := clientIPFromCtx
	t.Cleanup(func() { clientIPFromCtx = original })

	tests := []struct {
		name       string
		ip         string
		wantStatus int
	}{
		{"loopback allowed", "127.0.0.1", 200},
		{"remote address rejected", "203.0.113.5", 403},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clientIPFromCtx = func(_ fiber.Ctx) net.IP {
				return net.ParseIP(tc.ip)
			}

			app := fiber.New()
			app.Get("/protected", sameLANOnly(), func(c fiber.Ctx) error {
				return c.SendString("ok")
			})

			req := httptest.NewRequest("GET", "/protected", nil)
			resp, err := app.Test(req)
			testutil.ExpectedNoError(t, err)
			testutil.ExpectedEqual(t, resp.StatusCode, tc.wantStatus)
		})
	}
}
