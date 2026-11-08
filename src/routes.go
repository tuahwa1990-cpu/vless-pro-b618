package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Peraturan penghalaan pengguna (etc/routes.json), dinilai mengikut turutan.
type Rule struct {
	T string `json:"t"` // domain | keyword | ip | geosite | geoip
	V string `json:"v"`
	A string `json:"a"` // direct | proxy | block
}

type Routes struct {
	DS    string `json:"ds"` // AsIs | IPIfNonMatch | IPOnDemand
	Rules []Rule `json:"rules"`
}

var tagRe = regexp.MustCompile(`^[a-z0-9@!_.-]+(:[a-z0-9@!_.-]+)*$`)
var domRe = regexp.MustCompile(`^[a-zA-Z0-9*._-]+$`)

func normRoutes(r Routes) (Routes, error) {
	switch r.DS {
	case "AsIs", "IPIfNonMatch", "IPOnDemand":
	default:
		r.DS = "AsIs"
	}
	if len(r.Rules) > 300 {
		return r, fmt.Errorf("terlalu banyak peraturan (maks 300)")
	}
	out := []Rule{}
	for _, x := range r.Rules {
		x.V = strings.TrimSpace(x.V)
		if x.V == "" {
			continue
		}
		if x.A != "direct" && x.A != "proxy" && x.A != "block" {
			return r, fmt.Errorf("tindakan tidak sah: %s", x.A)
		}
		switch x.T {
		case "domain", "keyword":
			x.V = strings.ToLower(strings.TrimPrefix(x.V, "*."))
			if !domRe.MatchString(x.V) {
				return r, fmt.Errorf("nilai tidak sah: %s", x.V)
			}
		case "ip":
			if _, _, err := net.ParseCIDR(x.V); err != nil && net.ParseIP(x.V) == nil {
				return r, fmt.Errorf("IP/CIDR tidak sah: %s", x.V)
			}
		case "geosite", "geoip":
			x.V = strings.ToLower(x.V)
			if !tagRe.MatchString(x.V) {
				return r, fmt.Errorf("tag geo tidak sah: %s", x.V)
			}
			if _, ok := geoFile(x.T); !ok {
				return r, fmt.Errorf("data %s belum ada, muat turun dahulu", x.T)
			}
		default:
			return r, fmt.Errorf("jenis tidak sah: %s", x.T)
		}
		out = append(out, x)
	}
	r.Rules = out
	return r, nil
}

func loadRoutes() Routes {
	r := Routes{DS: "AsIs", Rules: []Rule{}}
	if b, err := os.ReadFile(home + "/etc/routes.json"); err == nil {
		_ = json.Unmarshal(b, &r)
	}
	switch r.DS {
	case "AsIs", "IPIfNonMatch", "IPOnDemand":
	default:
		r.DS = "AsIs"
	}
	if r.Rules == nil {
		r.Rules = []Rule{}
	}
	return r
}

// Laluan fail geosite.dat / geoip.dat: ${home}/geo dahulu, kemudian lokasi lazim xray.
func geoDirs() []string {
	return []string{home + "/geo", xliteDir + "/bin", xliteDir, "/usr/share/xray", "/usr/share/v2ray", "/usr/local/share/xray"}
}

func geoFile(kind string) (string, bool) {
	n := kind + ".dat"
	for _, d := range geoDirs() {
		if st, err := os.Stat(d + "/" + n); err == nil && st.Size() > 100000 {
			return d + "/" + n, true
		}
	}
	return "", false
}

// Pastikan ${home}/geo mengandungi pautan ke fail geo yang ditemui supaya xray menjumpainya.
func geoLink() {
	_ = os.MkdirAll(home+"/geo", 0700)
	for _, k := range []string{"geosite", "geoip"} {
		p, ok := geoFile(k)
		if !ok {
			continue
		}
		dst := home + "/geo/" + k + ".dat"
		if p == dst {
			continue
		}
		if st, err := os.Lstat(dst); err == nil && st.Mode()&os.ModeSymlink == 0 {
			continue
		}
		_ = os.Remove(dst)
		_ = os.Symlink(p, dst)
	}
}

func routeRules(rt Routes, base func(map[string]any) map[string]any) []any {
	out := []any{}
	for _, x := range rt.Rules {
		m := map[string]any{"type": "field"}
		switch x.T {
		case "domain":
			m["domain"] = []any{"domain:" + x.V}
		case "keyword":
			m["domain"] = []any{"keyword:" + x.V}
		case "ip":
			m["ip"] = []any{x.V}
		case "geosite":
			m["domain"] = []any{"geosite:" + x.V}
		case "geoip":
			m["ip"] = []any{"geoip:" + x.V}
		}
		switch x.A {
		case "direct":
			m["outboundTag"] = "direct"
		case "block":
			m["outboundTag"] = "block"
		default:
			m = base(m)
		}
		out = append(out, m)
	}
	return out
}

type geoState struct {
	sync.Mutex
	run bool
	msg string
}

var geoSt geoState

var geoSrc = map[string]string{
	"geoip":   "https://github.com/v2fly/geoip/releases/latest/download/geoip.dat",
	"geosite": "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
}

func geoDownload() {
	set := func(m string) { geoSt.Lock(); geoSt.msg = m; geoSt.Unlock() }
	defer func() { geoSt.Lock(); geoSt.run = false; geoSt.Unlock() }()
	_ = os.MkdirAll(home+"/geo", 0700)
	cl := &http.Client{Timeout: 5 * time.Minute}
	for _, k := range []string{"geoip", "geosite"} {
		set("Memuat turun " + k + "...")
		resp, err := cl.Get(geoSrc[k])
		if err != nil {
			set("Gagal " + k + ": " + shortErr(err))
			return
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			set(fmt.Sprintf("Gagal %s: HTTP %d", k, resp.StatusCode))
			return
		}
		tmp := home + "/geo/" + k + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			resp.Body.Close()
			set("Gagal tulis: " + err.Error())
			return
		}
		n, err := io.Copy(f, io.LimitReader(resp.Body, 60<<20))
		f.Close()
		resp.Body.Close()
		if err != nil || n < 100000 {
			_ = os.Remove(tmp)
			set("Gagal " + k + ": fail tidak lengkap")
			return
		}
		dst := home + "/geo/" + k + ".dat"
		_ = os.Remove(dst)
		if os.Rename(tmp, dst) != nil {
			set("Gagal simpan " + k)
			return
		}
	}
	set("Siap")
	restartIfActive()
}

func regRoutes(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/routes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var rt Routes
			if json.Unmarshal(body(r, 1<<18), &rt) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			n, err := normRoutes(rt)
			if err != nil {
				jr(w, map[string]any{"ok": false, "msg": err.Error()})
				return
			}
			b, _ := json.Marshal(n)
			if os.WriteFile(home+"/etc/routes.json", b, 0600) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Gagal simpan"})
				return
			}
			restartIfActive()
			jr(w, map[string]any{"ok": true, "msg": "Disimpan"})
			return
		}
		jr(w, loadRoutes())
	})
	mux.HandleFunc("/api/geo", func(w http.ResponseWriter, r *http.Request) {
		sz := func(k string) int64 {
			if p, ok := geoFile(k); ok {
				st, _ := os.Stat(p)
				return st.Size()
			}
			return 0
		}
		geoSt.Lock()
		run, msg := geoSt.run, geoSt.msg
		geoSt.Unlock()
		jr(w, map[string]any{"geosite": sz("geosite"), "geoip": sz("geoip"), "run": run, "msg": msg})
	})
	act("/api/geo/get", func(r *http.Request) (string, bool) {
		geoSt.Lock()
		if geoSt.run {
			geoSt.Unlock()
			return "Sedang memuat turun", true
		}
		geoSt.run, geoSt.msg = true, "Bermula..."
		geoSt.Unlock()
		go geoDownload()
		return "Muat turun dimulakan", true
	})
	_ = filepath.Base
}
