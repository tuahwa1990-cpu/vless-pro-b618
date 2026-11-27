package main

import (
	"net/http"
	"os"
	"regexp"
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

// Nilai TTL tersendiri (unit AW1000 biasa, pakej vendor luci-app-ttl64): pakej ni pasang
// SATU fail nftables tetap (/etc/nftables.d/ttl64.nft, disumber terus oleh fw4 semasa
// /etc/init.d/firewall restart) yang tetapkan TTL IPv4 (dan hop-limit IPv6 yang sepadan)
// kepada 64 secara global untuk SEMUA trafik — bukan konsep mod NSS/VPN langsung (itu
// cuma togol pdp_type ipv4v6/ip + offload perkakasan, berasingan sepenuhnya). Kami hanya
// tukar nombor "64" tetap itu kepada nilai pilihan pengguna dalam fail SAMA yang vendor
// dah sedia pasang, kemudian guna "/etc/init.d/firewall restart" (operasi sama yang
// modipv4.sh sendiri guna) untuk muat semula — tiada peraturan nftables baru direka.
const ttl64NftPath = "/etc/nftables.d/ttl64.nft"

var ttl64SetRe = regexp.MustCompile(`set \d{1,3}\b`)

func ttl64CustomSupported() bool {
	st, err := os.Stat(ttl64NftPath)
	return err == nil && !st.IsDir()
}

func ttl64CustomCurrent() int {
	b, err := os.ReadFile(ttl64NftPath)
	if err != nil {
		return 64
	}
	m := ttl64SetRe.FindStringSubmatch(string(b))
	if len(m) < 1 {
		return 64
	}
	n, err := strconv.Atoi(strings.TrimPrefix(m[0], "set "))
	if err != nil || n < 1 || n > 255 {
		return 64
	}
	return n
}

func ttl64CustomSet(arg string) (string, bool) {
	val, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || val < 1 || val > 255 {
		return "Nilai TTL tidak sah (1-255)", false
	}
	b, err := os.ReadFile(ttl64NftPath)
	if err != nil {
		return "Gagal baca fail ttl64.nft: " + err.Error(), false
	}
	newContent := ttl64SetRe.ReplaceAllString(string(b), "set "+strconv.Itoa(val))
	if err := os.WriteFile(ttl64NftPath, []byte(newContent), 0644); err != nil {
		return "Gagal tulis fail ttl64.nft: " + err.Error(), false
	}
	go func() { _, _ = run2("/etc/init.d/firewall", "restart") }()
	return "Nilai TTL ditetapkan kepada " + strconv.Itoa(val) + ". Rangkaian mungkin terputus seketika (~5 saat).", true
}

func regTTL(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/ttl", func(w http.ResponseWriter, r *http.Request) {
		if ttlSupported() {
			resp := map[string]any{"supported": true, "kind": "mode", "mode": ttlCurrentMode()}
			if ttl64CustomSupported() {
				resp["customTtl"] = map[string]any{"supported": true, "value": ttl64CustomCurrent()}
			}
			jr(w, resp)
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
	act("/api/ttl/custom/set", func(r *http.Request) (string, bool) {
		if !ttl64CustomSupported() {
			return "Ciri nilai TTL tersendiri belum disokong pada modem ini", false
		}
		return ttl64CustomSet(string(body(r, 16)))
	})
}
