package main

import (
	"net"
	"net/http"
	"strings"
)

// Rentang IP Cloudflare (https://www.cloudflare.com/ips/), dipakai untuk memutuskan
// kapan header CF-Connecting-IP boleh dipercaya.
var cloudflareCIDRs = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:eb50::/29",
}

type IPResolver struct {
	trustedProxies []*net.IPNet
	cloudflare     []*net.IPNet
}

func parseCIDRs(list []string) []*net.IPNet {
	var out []*net.IPNet
	for _, c := range list {
		if _, n, err := net.ParseCIDR(strings.TrimSpace(c)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func NewIPResolver(trusted []string) *IPResolver {
	return &IPResolver{trustedProxies: parseCIDRs(trusted), cloudflare: parseCIDRs(cloudflareCIDRs)}
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP menentukan IP klien.
//
// Header proxy HANYA dipercaya karena backend berada di belakang Nginx Proxy Manager
// (peer = alamat privat jaringan Docker):
//   - X-Real-IP / X-Forwarded-For (ditimpa NPM dengan $remote_addr) dipakai bila peer
//     termasuk TRUSTED_PROXY_CIDRS;
//   - CF-Connecting-IP hanya dipakai bila alamat tersebut berasal dari Cloudflare,
//     sehingga klien yang melewati Cloudflare tidak bisa memalsukannya.
//
// Nilai ini hanya untuk batas laju, Turnstile, dan catatan sesi; tidak pernah dikirim balik.
func (res *IPResolver) ClientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return nil
	}
	if !inNets(peer, res.trustedProxies) {
		return peer
	}

	ip := peer
	if v := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); v != nil {
		ip = v
	} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if v := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); v != nil {
			ip = v
		}
	}
	if inNets(ip, res.cloudflare) {
		if v := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); v != nil {
			ip = v
		}
	}
	return ip
}

// rateKey: IPv4 apa adanya, IPv6 dikelompokkan per /64.
func rateKey(ip net.IP) string {
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// ipBytes: 4 byte untuk IPv4, 16 byte untuk IPv6 (kolom VARBINARY(16)).
func ipBytes(ip net.IP) []byte {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return []byte(v4)
	}
	return []byte(ip.To16())
}
