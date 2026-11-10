package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tetapan tambahan (etc/opts.json). Semua pilihan ada nilai lalai selamat.
type Opts struct {
	AutoRecon bool   `json:"autorecon"`
	DNS       string `json:"dns"`     // IP pelayan DNS, kosong = lalai
	QS        string `json:"qs"`      // UseIP | UseIPv4 | UseIPv6
	PingURL   string `json:"pingurl"` // URL ujian sambungan
	LogLevel  string `json:"loglevel"`
	Reboot    string `json:"reboot"` // HH:MM atau kosong
}

var optMu sync.Mutex

func defOpts() Opts {
	return Opts{AutoRecon: true, QS: "UseIP", PingURL: "http://www.youtube.com/generate_204", LogLevel: "warning"}
}

func loadOpts() Opts {
	o := defOpts()
	if b, err := os.ReadFile(home + "/etc/opts.json"); err == nil {
		_ = json.Unmarshal(b, &o)
	}
	return normOpts(o)
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func normOpts(o Opts) Opts {
	d := defOpts()
	o.DNS = strings.TrimSpace(o.DNS)
	if o.DNS != "" && net.ParseIP(o.DNS) == nil {
		o.DNS = ""
	}
	switch o.QS {
	case "UseIP", "UseIPv4", "UseIPv6":
	default:
		o.QS = d.QS
	}
	o.PingURL = strings.TrimSpace(o.PingURL)
	if !strings.HasPrefix(o.PingURL, "http://") && !strings.HasPrefix(o.PingURL, "https://") {
		o.PingURL = d.PingURL
	}
	switch o.LogLevel {
	case "debug", "info", "warning", "error", "none":
	default:
		o.LogLevel = d.LogLevel
	}
	if !hhmm.MatchString(o.Reboot) {
		o.Reboot = ""
	}
	return o
}

func saveOpts(o Opts) error {
	optMu.Lock()
	defer optMu.Unlock()
	b, _ := json.Marshal(normOpts(o))
	return os.WriteFile(home+"/etc/opts.json", b, 0600)
}

// proxyOK benar jika sambungan melalui nod benar-benar berfungsi. Jawapan 5xx (contohnya 503 daripada
// Xray bila nod rosak) dikira gagal, bukan sihat.
func proxyOK(u string, t time.Duration) bool {
	t0 := time.Now()
	r, err := proxyClient(t).Get(u)
	if err != nil {
		probeLog(u, "ralat: "+shortErr(err), t0)
		return false
	}
	r.Body.Close()
	ok := r.StatusCode < 500
	if !ok {
		probeLog(u, "status "+strconv.Itoa(r.StatusCode), t0)
	}
	return ok
}

// probeLog merekod ujian yang gagal (hanya yang gagal) supaya punca boleh disemak: run/probe.log (had ~16 KB).
func probeLog(u, why string, t0 time.Time) {
	f := home + "/run/probe.log"
	if st, err := os.Stat(f); err == nil && st.Size() > 16384 {
		_ = os.Remove(f)
	}
	lf, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer lf.Close()
	fmt.Fprintf(lf, "%s %s %s (%dms)\n", time.Now().Format("15:04:05"), u, why, time.Since(t0).Milliseconds())
}

// Pemantau: sambung semula automatik bila xray mati atau ujian gagal 3 kali berturut.
func watchdog() {
	fails := 0
	var last time.Time
	cool := 5 * time.Minute
	var lastRx uint64
	for {
		time.Sleep(30 * time.Second)
		if rx, _, ok := netBytes(wanIface()); ok {
			if lastRx > 0 && rx >= lastRx {
				wanDelta.Store(rx - lastRx)
			} else {
				wanDelta.Store(0)
			}
			lastRx = rx
		}
		o := loadOpts()
		if !o.AutoRecon || readStr(home+"/etc/mode", "xlite") != "vless" || locked(readLic()) {
			fails = 0
			foFails.Store(0)
			continue
		}
		if time.Since(last) < cool {
			continue
		}
		bad := false
		if !vlessUp() {
			bad = true
			fails = 3
		} else {
			if aliveVia() {
				fails = 0
			} else {
				fails++
			}
		}
		foFails.Store(int32(fails))
		if fails >= 3 {
			_ = bad
			fails = 0
			last = time.Now()
			fmt.Fprintln(os.Stderr, "pemantau: sambung semula automatik")
			foFails.Store(0) // jangan paparkan "gagal 3/3" lapuk selepas penukaran
			if recoverVP() {
				cool = 90 * time.Second // profil baharu: semak cepat, tukar lagi jika masih gagal
			} else {
				cool = 5 * time.Minute
			}
		}
	}
}

// Reboot berjadual harian pada masa HH:MM waktu modem.
func rebootSched() {
	day := ""
	for {
		time.Sleep(20 * time.Second)
		o := loadOpts()
		now := time.Now()
		if o.Reboot == "" || now.Year() < 2024 || now.Format("15:04") != o.Reboot || day == now.Format("20060102") {
			continue
		}
		day = now.Format("20060102")
		fmt.Fprintln(os.Stderr, "reboot berjadual")
		doReboot()
	}
}

func doReboot() {
	_ = exec.Command("sync").Run()
	if exec.Command("reboot").Start() != nil {
		_ = exec.Command("/sbin/reboot").Start()
	}
}

func sh(name string, args ...string) string {
	ctx, c := context.WithTimeout(context.Background(), 6*time.Second)
	defer c()
	out, _ := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out)
}

func diagText() string {
	var b strings.Builder
	sec := func(t, s string) { fmt.Fprintf(&b, "\n===== %s =====\n%s\n", t, strings.TrimSpace(s)) }
	sec("VLESS Pro", fmt.Sprintf("versi %s\narch %s\nID %s\nMAC %s\nmod %s\nxray %v\nxlite %v\nmasa %s",
		version, runtime.GOARCH, hyph(devID20()), macAddr(), readStr(home+"/etc/mode", "xlite"), vlessUp(), xliteRunning(), time.Now().Format(time.RFC3339)))
	o, _ := json.Marshal(loadOpts())
	sec("Tetapan", string(o))
	sec("uname", sh("uname", "-a"))
	sec("uptime", readStr("/proc/uptime", "")+" "+readStr("/proc/loadavg", ""))
	sec("memori", sh("head", "-n", "6", "/proc/meminfo"))
	sec("df", sh("df", "-h"))
	sec("ip addr", sh("ip", "addr"))
	sec("ip route", sh("ip", "route"))
	sec("iptables nat", sh("iptables", "-t", "nat", "-S"))
	sec("tc WAN", sh("tc", "-s", "qdisc", "show", "dev", wanIface()))
	sec("tc ifb", sh("tc", "-s", "qdisc", "show", "dev", ifbName))
	sec("vp status", sh("/bin/sh", home+"/vp", "status"))
	sec("vp.log", tail(home+"/run/vp.log", 6000))
	sec("xray.out", tail(home+"/run/xray.out", 6000))
	return b.String()
}

// nama fail etc yang tidak dipulihkan daripada sandaran
var noRestore = map[string]bool{"pass": true, "pass_initial": true, "listen": true, "mode": true}

func backupTgz() []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	ents, _ := os.ReadDir(home + "/etc")
	for _, e := range ents {
		if !e.Type().IsRegular() || noRestore[e.Name()] {
			continue
		}
		b, err := os.ReadFile(home + "/etc/" + e.Name())
		if err != nil {
			continue
		}
		_ = tw.WriteHeader(&tar.Header{Name: "etc/" + e.Name(), Mode: 0600, Size: int64(len(b)), ModTime: time.Now()})
		_, _ = tw.Write(b)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func restoreTgz(data []byte) (int, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("bukan fail sandaran")
	}
	tr := tar.NewReader(io.LimitReader(gz, 4<<20))
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("fail rosak")
		}
		if h.Typeflag != tar.TypeReg || !strings.HasPrefix(h.Name, "etc/") {
			continue
		}
		n := strings.TrimPrefix(h.Name, "etc/")
		if n == "" || n != filepath.Base(n) || strings.HasPrefix(n, ".") || noRestore[n] || h.Size > 1<<20 {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(tr, 1<<20))
		files[n] = b
	}
	if len(files) == 0 {
		return 0, fmt.Errorf("tiada data sah dalam fail")
	}
	for n, b := range files {
		if err := os.WriteFile(home+"/etc/"+n+".tmp", b, 0600); err != nil {
			return 0, err
		}
		if err := os.Rename(home+"/etc/"+n+".tmp", home+"/etc/"+n); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

func regOpts(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/opts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var o Opts
			if json.Unmarshal(body(r, 4096), &o) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			n := normOpts(o)
			if o.DNS != "" && n.DNS == "" {
				jr(w, map[string]any{"ok": false, "msg": "Alamat DNS mesti IP, contoh 1.1.1.1"})
				return
			}
			if o.Reboot != "" && n.Reboot == "" {
				jr(w, map[string]any{"ok": false, "msg": "Masa reboot mesti HH:MM"})
				return
			}
			if saveOpts(n) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Gagal simpan"})
				return
			}
			restartIfActive()
			jr(w, map[string]any{"ok": true, "msg": "Disimpan"})
			return
		}
		jr(w, loadOpts())
	})
	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.URL.Query().Get("dl") == "1" {
			w.Header().Set("Content-Disposition", `attachment; filename="vlesspro-log.txt"`)
		}
		_, _ = io.WriteString(w, tail(home+"/run/vp.log", 8000)+"\n--- xray ---\n"+tail(home+"/run/xray.out", 8000))
	})
	act("/api/log/clear", func(r *http.Request) (string, bool) {
		_ = os.Truncate(home+"/run/vp.log", 0)
		_ = os.Truncate(home+"/run/xray.out", 0)
		return "Log dikosongkan", true
	})
	mux.HandleFunc("/api/diag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="vlesspro-diag.txt"`)
		_, _ = io.WriteString(w, diagText())
	})
	mux.HandleFunc("/api/backup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="vlesspro-backup.tgz"`)
		_, _ = w.Write(backupTgz())
	})
	act("/api/restore", func(r *http.Request) (string, bool) {
		n, err := restoreTgz(body(r, 5<<20))
		if err != nil {
			return "Gagal: " + err.Error(), false
		}
		restartIfActive()
		return fmt.Sprintf("Dipulihkan (%d fail)", n), true
	})
	act("/api/sys/restart", func(r *http.Request) (string, bool) {
		// Mula semula VLESS Pro: xray (jika mod VLESS) dan UI ini. UI baharu menjana sesi baharu,
		// jadi kata laluan diperlukan semula.
		go func() { time.Sleep(time.Second); runVP(true, "svc-restart") }()
		return "VLESS Pro dimulakan semula. Log masuk semula sebentar lagi.", true
	})
	act("/api/sys/reboot", func(r *http.Request) (string, bool) {
		go func() { time.Sleep(2 * time.Second); doReboot() }()
		return "Modem akan reboot sebentar lagi", true
	})
}
