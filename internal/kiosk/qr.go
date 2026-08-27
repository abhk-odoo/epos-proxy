// Package kiosk provides small, self-contained helpers for the remote
// kiosk-management feature that don't belong in internal/config (URL/PIN
// storage) or internal/server (HTTP routing): enumerating this machine's
// LAN IPv4 addresses and generating a QR code for the mobile kiosk URL.
package kiosk

import (
	"encoding/base64"
	"fmt"
	"net"

	qrcode "github.com/skip2/go-qrcode"
)

// QRSize is the pixel width/height of the generated QR PNG.
const QRSize = 256

// LocalIPv4Addresses returns the non-loopback IPv4 addresses configured on
// this machine's network interfaces, e.g. ["192.168.1.50"]. Order is not
// guaranteed; callers that need a stable default should sort or pick the
// first deterministically if that matters to them.
func LocalIPv4Addresses() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	var ips []string
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			ips = append(ips, ip4.String())
		}
	}

	return ips, nil
}

// MobileKioskURL builds the URL the QR code should encode: the kiosk
// machine's mobile-management page for the given LAN IP and port.
func MobileKioskURL(ip string, port int) string {
	return fmt.Sprintf("http://%s:%d/kiosk", ip, port)
}

// GenerateQRDataURI returns a "data:image/png;base64,..." string encoding a
// QR code for content, suitable for direct use as an <img src>.
func GenerateQRDataURI(content string) (string, error) {
	png, err := qrcode.Encode(content, qrcode.Medium, QRSize)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
