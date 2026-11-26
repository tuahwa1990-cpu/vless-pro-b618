package main

import (
	"net/http"
	"os"
	"strconv"
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

// AW1000 Ultra (JunWRT) ada ciri TTL terbina dalam firmware sendiri: pakej UCI "3ginfo_ttl"
// (medan "enable" 0/1 dan "ttl" 1-255) + skrip /etc/init.d/3ginfo_ttl yang memasang peraturan
// iptables mangle (--ttl-set) pada peranti WAN selular aktif. Mekanisme ini SAMA SEKALI berbeza
// daripada modipv4.sh (tiada konsep mod NSS/VPN di sini — hanya nilai TTL IPv4 + suis hidup/mati).
// Kami panggil terus skrip init vendor (reload) supaya peraturan firewall dipasang/dibuang dengan
// cara yang sama seperti bila ditetapkan melalui LuCI, tiada risiko teka logik iptables sendiri.

const junwrtTtlInit = "/etc/init.d/3ginfo_ttl"

func ttlSupportedJunwrt() bool {
	st, err := os.Stat(junwrtTtlInit)
	return err == nil && !st.IsDir()
}

func ttlJunwrtCurrent() (enabled bool, ttl int) {
	e, _ := run2("uci", "-q", "get", "3ginfo_ttl.main.enable")
	t, _ := run2("uci", "-q", "get", "3ginfo_ttl.main.ttl")
	enabled = strings.TrimSpace(e) == "1"
	ttl, err := strconv.Atoi(strings.TrimSpace(t))
	if err != nil || ttl < 1 || ttl > 255 {
		ttl = 64
	}
	return
}

func ttlJunwrtSet(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "off" {
		if _, err := run2("uci", "set", "3ginfo_ttl.main.enable=0"); err != nil {
			return "Gagal matikan TTL: " + err.Error(), false
		}
		_, _ = run2("uci", "commit", "3ginfo_ttl")
		go func() { _, _ = run2(junwrtTtlInit, "reload") }()
		return "TTL dimatikan (kembali kepada lalai).", true
	}
	ttl, err := strconv.Atoi(arg)
	if err != nil || ttl < 1 || ttl > 255 {
		return "Nilai TTL tidak sah (1-255)", false
	}
	if _, err := run2("uci", "set", "3ginfo_ttl.main.enable=1"); err != nil {
		return "Gagal tetapkan TTL: " + err.Error(), false
	}
	if _, err := run2("uci", "set", "3ginfo_ttl.main.ttl="+strconv.Itoa(ttl)); err != nil {
		return "Gagal tetapkan TTL: " + err.Error(), false
	}
	_, _ = run2("uci", "commit", "3ginfo_ttl")
	go func() { _, _ = run2(junwrtTtlInit, "reload") }()
	return "TTL ditetapkan kepada " + strconv.Itoa(ttl) + " dan diaktifkan.", true
}

func regTTL(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/ttl", func(w http.ResponseWriter, r *http.Request) {
		if ttlSupported() {
			jr(w, map[string]any{"supported": true, "kind": "mode", "mode": ttlCurrentMode()})
			return
		}
		if ttlSupportedJunwrt() {
			enabled, ttl := ttlJunwrtCurrent()
			jr(w, map[string]any{"supported": true, "kind": "value", "enabled": enabled, "ttl": ttl})
			return
		}
		jr(w, map[string]any{"supported": false})
	})
	act("/api/ttl/set", func(r *http.Request) (string, bool) {
		if ttlSupported() {
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
		}
		if ttlSupportedJunwrt() {
			return ttlJunwrtSet(string(body(r, 16)))
		}
		return "Ciri ini belum disokong pada modem ini", false
	})
}
