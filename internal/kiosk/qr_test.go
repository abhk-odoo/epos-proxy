package kiosk

import (
	"fmt"
	"strings"
	"testing"

	"epos-proxy/internal/testutil"
)

func TestMobileKioskURL(t *testing.T) {
	testutil.ExpectedEqual(t, MobileKioskURL("192.168.1.50", 4550), "http://192.168.1.50:4550/kiosk")
}

func TestGenerateQRDataURI(t *testing.T) {
	uri, err := GenerateQRDataURI("http://192.168.1.50:4550/kiosk")
	testutil.ExpectedNoError(t, err)
	prefixLen := min(40, len(uri))
	testutil.ExpectedTrue(t, strings.HasPrefix(uri, "data:image/png;base64,"), fmt.Sprintf("expected a PNG data URI, got prefix: %q", uri[:prefixLen]))
	testutil.ExpectedTrue(t, len(uri) > len("data:image/png;base64,"), "expected non-empty payload")
}

func TestLocalIPv4Addresses_ExcludesLoopback(t *testing.T) {
	ips, err := LocalIPv4Addresses()
	testutil.ExpectedNoError(t, err)
	for _, ip := range ips {
		testutil.ExpectedFalse(t, ip == "127.0.0.1", "loopback should never be included")
	}
}
