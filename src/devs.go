package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Ciri "Peranti Disambung": senarai peranti pada rangkaian LAN/WiFi modem (jadual ARP),
// dengan nama daripada DHCP dan nama pilihan pengguna (etc/devnames.json). Baca sahaja kecuali nama.

type Dev struct {
	IP   string `json:"ip"`
	MAC  string `json:"mac"`
	Host string `json:"host"` // nama daripada DHCP (jika ada)
	Name string `json:"name"` // nama pilihan pengguna
}

var (
	arpPath    = "/proc/net/arp"
	leaseFiles = []string{"/tmp/dhcp.leases", "/var/dhcp.leases", "/tmp/dnsmasq.leases", "/var/lib/misc/dnsmasq.leases",
		"/var/lib/dnsmasq/dnsmasq.leases", "/data/misc/dhcp/dnsmasq.leases", "/data/dnsmasq.leases"}
	macRe = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)
)

const maxDevNames = 100

// leaseHosts: peta mac -> nama hos daripada fail sewa dnsmasq (format: tamat mac ip nama id).
func leaseHosts() map[string]string {
	m := map[string]string{}
	for _, f := range leaseFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			p := strings.Fields(l)
			if len(p) >= 4 && p[3] != "*" {
				m[strings.ToLower(p[1])] = cleanName(p[3], 40)
			}
		}
	}
	return m
}

// cleanName membuang aksara kawalan, memangkas ruang dan memendekkan kepada n aksara.
func cleanName(s string, n int) string {
	var r []rune
	for _, c := range strings.TrimSpace(s) {
		if unicode.IsControl(c) {
			continue
		}
		r = append(r, c)
		if len(r) >= n {
			break
		}
	}
	return strings.TrimSpace(string(r))
}

func loadDevNames() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(home + "/etc/devnames.json"); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func listDevices() []Dev {
	out := []Dev{}
	b, err := os.ReadFile(arpPath)
	if err != nil {
		return out
	}
	dev, hosts, names := lanDev(), leaseHosts(), loadDevNames()
	seen := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 6 || f[5] != dev || f[2] == "0x0" {
			continue
		}
		mac := strings.ToLower(f[3])
		if !macRe.MatchString(mac) || mac == "00:00:00:00:00:00" || seen[mac] {
			continue
		}
		seen[mac] = true
		out = append(out, Dev{IP: f[0], MAC: mac, Host: hosts[mac], Name: names[mac]})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := net.ParseIP(out[i].IP).To4(), net.ParseIP(out[j].IP).To4()
		if a != nil && b != nil {
			return bytes.Compare(a, b) < 0
		}
		return out[i].IP < out[j].IP
	})
	return out
}

func setDevName(mac, name string) (string, bool) {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if !macRe.MatchString(mac) {
		return "Data tidak sah", false
	}
	name = cleanName(name, 24)
	m := loadDevNames()
	if name == "" {
		delete(m, mac)
	} else {
		if _, ok := m[mac]; !ok && len(m) >= maxDevNames {
			return "Terlalu banyak nama disimpan", false
		}
		m[mac] = name
	}
	b, _ := json.Marshal(m)
	_ = os.MkdirAll(home+"/etc", 0700)
	if os.WriteFile(home+"/etc/devnames.json", b, 0600) != nil {
		return "Gagal simpan", false
	}
	return "Disimpan", true
}

func regDev(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) {
		jr(w, map[string]any{"devs": listDevices()})
	})
	act("/api/devices/name", func(r *http.Request) (string, bool) {
		var q struct {
			MAC  string `json:"mac"`
			Name string `json:"name"`
		}
		if json.Unmarshal(body(r, 1024), &q) != nil {
			return "Data tidak sah", false
		}
		return setDevName(q.MAC, q.Name)
	})
}
