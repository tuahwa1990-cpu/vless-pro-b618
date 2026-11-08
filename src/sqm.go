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
	sqmClean(dev)
	if !s.On {
		return nil
	}
	if s.Up <= 0 && s.Down <= 0 {
		return fmt.Errorf("tetapkan sekurang-kurangnya satu had (Mbit/s)")
	}
	step := func(name string, a ...string) error {
		if out, err := run2(name, a...); err != nil {
			sqmClean(dev)
			return fmt.Errorf("%s %s: %s", name, strings.Join(a[:min(3, len(a))], " "), out)
		}
		return nil
	}
	if s.Up > 0 {
		for _, a := range [][]string{
			{"qdisc", "add", "dev", dev, "root", "handle", "1:", "htb", "default", "11"},
			{"class", "add", "dev", dev, "parent", "1:", "classid", "1:11", "htb", "rate", kbit(s.Up), "ceil", kbit(s.Up)},
			{"qdisc", "add", "dev", dev, "parent", "1:11", "handle", "110:", "fq_codel"},
		} {
			if err := step("tc", a...); err != nil {
				return err
			}
		}
	}
	if s.Down > 0 {
		_, _ = run2("modprobe", "ifb")
		for _, a := range [][]string{
			{"ip", "link", "add", "name", ifbName, "type", "ifb"},
			{"ip", "link", "set", "dev", ifbName, "up"},
			{"tc", "qdisc", "add", "dev", ifbName, "root", "handle", "1:", "htb", "default", "11"},
			{"tc", "class", "add", "dev", ifbName, "parent", "1:", "classid", "1:11", "htb", "rate", kbit(s.Down), "ceil", kbit(s.Down)},
			{"tc", "qdisc", "add", "dev", ifbName, "parent", "1:11", "handle", "110:", "fq_codel"},
			{"tc", "qdisc", "add", "dev", dev, "handle", "ffff:", "ingress"},
			{"tc", "filter", "add", "dev", dev, "parent", "ffff:", "protocol", "all", "u32", "match", "u32", "0", "0", "action", "mirred", "egress", "redirect", "dev", ifbName},
		} {
			if err := step(a[0], a[1:]...); err != nil {
				return err
			}
		}
	}
	return nil
}

func sqmActive() bool {
	dev := wanIface()
	if dev == "" {
		return false
	}
	out, _ := run2("tc", "qdisc", "show", "dev", dev)
	return strings.Contains(out, "htb")
}

// Pemantau: pasang semula jika WAN dicipta semula (contoh sambungan data bertukar).
func sqmWatch() {
	time.Sleep(20 * time.Second)
	for {
		s := loadSQM()
		if s.On {
			sqmMu.Lock()
			ok := sqmActive()
			if ok && s.Down > 0 {
				if out, _ := run2("tc", "qdisc", "show", "dev", wanIface(), "ingress"); !strings.Contains(out, "ingress") {
					ok = false
				}
			}
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
			if s.On && s.Down > 0 && s.Down < 0.5 || s.On && s.Up > 0 && s.Up < 0.5 {
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
