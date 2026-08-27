package server

import (
	"net"

	"github.com/gofiber/fiber/v3"
)

// localSubnets returns the IPv4 networks (CIDR blocks) attached to this
// machine's non-loopback interfaces, e.g. 192.168.1.0/24. Used to decide
// whether an incoming request to the kiosk routes originates from the same
// LAN as this machine.
func localSubnets() []*net.IPNet {
	var nets []*net.IPNet

	ifaces, err := net.Interfaces()
	if err != nil {
		return nets
	}

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
			nets = append(nets, ipNet)
		}
	}

	return nets
}

// isLocalNetworkAddr reports whether ip is either loopback or falls inside
// one of subnets. Loopback is always allowed so the feature is testable
// from the desktop machine's own browser.
func isLocalNetworkAddr(ip net.IP, subnets []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, subnet := range subnets {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIPFromCtx extracts the client IP from a fiber context. Extracted as
// a variable (not a hard call) purely so tests can substitute a fixed IP —
// the fasthttp test transport always reports 0.0.0.0 for c.IP(), which would
// make the middleware untestable at the HTTP layer otherwise.
var clientIPFromCtx = func(c fiber.Ctx) net.IP {
	return net.ParseIP(c.IP())
}

// sameLANOnly rejects any request whose client address is not on the same
// local network as this machine (or loopback). This is the entire access
// boundary for the kiosk routes — deliberately simple, no auth layer.
func sameLANOnly() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !isLocalNetworkAddr(clientIPFromCtx(c), localSubnets()) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "This feature is only accessible from the same local network as the kiosk machine.",
			})
		}
		return c.Next()
	}
}
