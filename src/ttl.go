package main

import (
	"net/http"
	"os"
	"strings"
)

// Mod TTL/NSS: sesetengah unit AW1000 (dengan modipv4.sh daripada "ttl64" vendor) ada 2 mod rangkaian:
//   - "nss": ipv4v6 + pecutan perkakasan (NAT offload) dihidupkan — lalai, laju.
//   - "vpn": IPv4 sahaja, bagi laluan trafik melalui laluan perisian supaya peraturan TTL
//            (elak pengesanan perkongsian/hotspot oleh telco) dapat dikenakan.
// Kami TIDAK menulis semula logik git/firewall sendiri — kami panggil terus skrip vendor sedia
// ada (/usr/bin/modipv4.sh nss|vpn), sama seperti apl LuCI "MOD/Band" bawaan peranti, supaya
// tiada risiko teka arahan firewall/rangkaian yang salah.

const modipv4Bin = "/usr/bin/modipv4.sh"

func ttlSupported() bool {
	st, err := os.Stat(modipv4Bin)
	return err == nil && !st.IsDir()
}

func ttlCurrentMode() string {
	pdp, _ := run2("uci", "-q", "get", "qmodem.4_1.pdp_type")
	flow, _ := run2("uci", "-q", "get", "firewall.@defaults[0].flow_offloading")
	pdp = strings.TrimSpace(pdp)
	flow = strings.TrimSpace(flow)
	switch {
	case pdp == "ipv4v6" && flow == "1":
		return "nss"
	case pdp == "ip" && flow == "1":
		return "vpn"
	default:
		return "unknown"
	}
}

func regTTL(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/ttl", func(w http.ResponseWriter, r *http.Request) {
		if !ttlSupported() {
			jr(w, map[string]any{"supported": false})
			return
		}
		jr(w, map[string]any{"supported": true, "mode": ttlCurrentMode()})
	})
	act("/api/ttl/set", func(r *http.Request) (string, bool) {
		if !ttlSupported() {
			return "Ciri ini belum disokong pada modem ini", false
		}
		mode := strings.TrimSpace(string(body(r, 16)))
		if mode != "nss" && mode != "vpn" {
			return "Mod tidak sah", false
		}
		// Skrip vendor memulangkan serta-merta dan restart perkhidmatan di latar belakang
		// (ada sleep ~10s di dalamnya), jadi kita jalankan tanpa tunggu supaya panel tak beku.
		go func() { _, _ = run2("sh", modipv4Bin, mode) }()
		if mode == "nss" {
			return "Mod NSS (lalai, pantas) sedang diguna pakai. Rangkaian mungkin terputus seketika (~10 saat).", true
		}
		return "Mod VPN/TTL sedang diguna pakai. Rangkaian mungkin terputus seketika (~10 saat).", true
	})
}
