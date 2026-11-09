package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SQM ringkas: HTB + fq_codel pada egress, dan ingress melalui peranti IFB.
// (Cake tiada dalam kernel AW1000, jadi fq_codel digunakan.)
type SQM struct {
	On   bool    `json:"on"`
	Down float64 `json:"down"` // Mbit/s, 0 = tiada had
	Up   float64 `json:"up"`
}

const ifbName = "ifbvp"

var sqmMu sync.Mutex

func findBin(n string) string {
	if p, err := exec.LookPath(n); err == nil {
		return p
	}
	for _, d := range []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"} {
		if st, err := os.Stat(d + "/" + n); err == nil && !st.IsDir() {
			return d + "/" + n
		}
	}
	return ""
}

func run2(name string, args ...string) (string, error) {
	b := findBin(name)
	if b == "" {
		return "", fmt.Errorf("%s tiada pada peranti ini", name)
	}
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	defer c()
	out, err := exec.CommandContext(ctx, b, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func loadSQM() SQM {
	var s SQM
	if b, err := os.ReadFile(home + "/etc/sqm.json"); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveSQM(s SQM) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(home+"/etc/sqm.json", b, 0600)
}

func kbit(m float64) string { return fmt.Sprintf("%dkbit", int(m*1000)) }

func sqmClean(dev string) {
	if dev != "" {
		_, _ = run2("tc", "qdisc", "del", "dev", dev, "root")
		_, _ = run2("tc", "qdisc", "del", "dev", dev, "ingress")
	}
	_, _ = run2("ip", "link", "del", ifbName)
}

// sqmApply memasang tetapan; mengembalikan ralat dengan keluaran tc jika gagal.
func sqmApply(s SQM) error {
	dev := wanIface()
	if dev == "" {
		return fmt.Errorf("antara muka WAN tidak dijumpai")
	}
	s.Down = 0 // muat turun memintas kernel (pecutan MediaTek), tc tidak boleh mengehadkannya
	sqmClean(dev)
	if !s.On {
		return nil
	}
	if s.Up <= 0 {
		return fmt.Errorf("tetapkan had muat naik (Mbit/s)")
	}
	htb := [][]string{
		{"qdisc", "add", "dev", dev, "root", "handle", "1:", "htb", "default", "11"},
		{"class", "add", "dev", dev, "parent", "1:", "classid", "1:11", "htb", "rate", kbit(s.Up), "ceil", kbit(s.Up)},
		{"qdisc", "add", "dev", dev, "parent", "1:11", "handle", "110:", "fq_codel"},
	}
	burst := int(s.Up * 1250)
	if burst < 6000 {
		burst = 6000
	}
	tbf := [][]string{
		{"qdisc", "add", "dev", dev, "root", "handle", "1:", "tbf", "rate", kbit(s.Up), "burst", fmt.Sprint(burst), "latency", "50ms"},
		{"qdisc", "add", "dev", dev, "parent", "1:1", "handle", "110:", "fq_codel"},
	}
	var first error
	for _, set := range [][][]string{htb, tbf} {
		var err error
		for _, a := range set {
			if out, e := run2("tc", a...); e != nil {
				err = fmt.Errorf("tc %s: %s", strings.Join(a[:min(3, len(a))], " "), out)
				break
			}
		}
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
		sqmClean(dev)
	}
	return fmt.Errorf("kernel modem tiada htb/tbf, SQM tidak boleh dipasang (%v)", first)
}

func sqmActive() bool {
	dev := wanIface()
	if dev == "" {
		return false
	}
	out, _ := run2("tc", "qdisc", "show", "dev", dev)
	return strings.Contains(out, "htb") || strings.Contains(out, "tbf")
}

// Pemantau: pasang semula jika WAN dicipta semula (contoh sambungan data bertukar).
func sqmWatch() {
	time.Sleep(20 * time.Second)
	for {
		s := loadSQM()
		if s.On {
			sqmMu.Lock()
			ok := sqmActive()
			if !ok {
				if err := sqmApply(s); err != nil {
					fmt.Fprintln(os.Stderr, "sqm:", err)
				}
			}
			sqmMu.Unlock()
		}
		time.Sleep(30 * time.Second)
	}
}

func regSQM(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/sqm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			var s SQM
			if json.Unmarshal(body(r, 1024), &s) != nil || s.Down < 0 || s.Up < 0 || s.Down > 5000 || s.Up > 5000 {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			if s.On && s.Up > 0 && s.Up < 0.5 {
				jr(w, map[string]any{"ok": false, "msg": "Had minimum 0.5 Mbit/s"})
				return
			}
			sqmMu.Lock()
			err := sqmApply(s)
			sqmMu.Unlock()
			if err != nil {
				s.On = false
				_ = saveSQM(s)
				jr(w, map[string]any{"ok": false, "msg": "Gagal: " + err.Error()})
				return
			}
			_ = saveSQM(s)
			if s.On {
				jr(w, map[string]any{"ok": true, "msg": "SQM dihidupkan"})
			} else {
				jr(w, map[string]any{"ok": true, "msg": "SQM dimatikan"})
			}
			return
		}
		dev := wanIface()
		st, _ := run2("tc", "-s", "qdisc", "show", "dev", dev)
		jr(w, map[string]any{"cfg": loadSQM(), "iface": dev, "active": sqmActive(),
			"tc": findBin("tc") != "", "ip": findBin("ip") != "", "stat": st})
	})
}
