package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed cacert.pem
var caPEM []byte

//go:embed upgrade.sh
var upgradeSh []byte

// Setiap sasaran ada fail sendiri di GitHub supaya binari seni bina lain tidak tersalah pasang.
// ARM 32-bit (B618): b618-latest.txt, b618-sha256.txt, new/. ARM64 (AW1000): aw1000-latest.txt, aw1000-sha256.txt, aw1000/.
func target() (tag, dir string) {
	if runtime.GOARCH == "arm64" {
		return "aw1000", "aw1000/"
	}
	return "b618", "new/"
}

var defUpd = func() string {
	t, _ := target()
	return "https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main/" + t + "-latest.txt"
}()

func updURL() string { return readStr(home+"/etc/update_url", defUpd) }

func updBase() string {
	u := updURL()
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[:i+1]
	}
	return u
}

func httpc(t time.Duration) *http.Client {
	pool, _ := x509.SystemCertPool()
	if pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(caPEM)
	return &http.Client{Timeout: t, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
}

func fetch(u string, max int64, t time.Duration) ([]byte, error) {
	resp, err := httpc(t).Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("fail terlalu besar")
	}
	return b, nil
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s) > 80 {
		s = s[i+2:]
	}
	if len(s) > 90 {
		s = s[:90]
	}
	return s
}

func latestVer() (string, error) {
	b, err := fetch(updURL(), 256, 15*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

var (
	upMu  sync.Mutex
	upSt  = map[string]any{"state": "idle", "msg": ""}
	upRun bool
)

func upSet(state, msg string) {
	upMu.Lock()
	upSt = map[string]any{"state": state, "msg": msg}
	upMu.Unlock()
}

func upGet() map[string]any {
	upMu.Lock()
	defer upMu.Unlock()
	m := map[string]any{}
	for k, v := range upSt {
		m[k] = v
	}
	return m
}

func sums(b []byte) map[string]string {
	m := map[string]string{}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 {
			m[f[1]] = strings.ToLower(f[0])
		}
	}
	return m
}

// G630V: binari dalam RAM; kemas kini = "vp update" (muat turun + semak SHA256 + ganti, tanpa reboot).
func isG630V() bool {
	_, err := os.Stat("/etc/vlesspro-boot.sh")
	return err == nil
}

func doUpdateG630V() {
	upSet("run", "Memuat turun dari GitHub...")
	var out bytes.Buffer
	cmd := exec.Command("/bin/sh", home+"/vp", "update")
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err != nil {
		ls := strings.Split(strings.TrimSpace(out.String()), "\n")
		upSet("err", ls[len(ls)-1])
		return
	}
	upSet("done", "Dikemas kini, memulakan semula UI...")
}

func doUpdate(force bool) {
	defer func() { upMu.Lock(); upRun = false; upMu.Unlock() }()
	if isG630V() {
		doUpdateG630V()
		return
	}
	lv, err := latestVer()
	if err != nil {
		upSet("err", "Tidak dapat semak: "+shortErr(err))
		return
	}
	if lv == "" || (verCmp(lv, version) <= 0 && !force) {
		upSet("ok", "Sudah versi terkini")
		return
	}
	if verCmp(lv, version) < 0 {
		upSet("err", "Versi di GitHub ("+lv+") lebih lama daripada yang dipasang, dibatalkan")
		return
	}
	base := updBase()
	tag, tdir := target()
	upSet("run", "Memuat turun senarai semak...")
	sb, err := fetch(base+tag+"-sha256.txt", 4096, 20*time.Second)
	if err != nil {
		upSet("err", "Senarai semak gagal: "+shortErr(err))
		return
	}
	sm := sums(sb)
	files := []struct {
		n   string
		max int64
		min int
	}{{"vp", 262144, 1000}, {"vpro", 16 << 20, 1 << 20}}
	_ = os.MkdirAll(home+"/new", 0755)
	for _, f := range files {
		upSet("run", "Memuat turun "+f.n+"...")
		var b []byte
		var err error
		if f.n == "vpro" {
			// versi mampat (.gz, kira-kira separuh saiz) dahulu; jika tiada atau rosak, guna fail penuh
			if gb, e := fetch(base+tdir+"vpro.gz", 8<<20, 5*time.Minute); e == nil {
				if zr, e := gzip.NewReader(bytes.NewReader(gb)); e == nil {
					if ub, e := io.ReadAll(io.LimitReader(zr, f.max+1)); e == nil && int64(len(ub)) <= f.max {
						b = ub
					}
				}
			}
		}
		if b == nil {
			b, err = fetch(base+tdir+f.n, f.max, 5*time.Minute)
			if err != nil {
				upSet("err", "Muat turun "+f.n+" gagal: "+shortErr(err))
				return
			}
		}
		h := sha256.Sum256(b)
		if len(b) < f.min || hex.EncodeToString(h[:]) != sm[f.n] {
			upSet("err", "Semakan "+f.n+" gagal, kemas kini dibatalkan")
			return
		}
		if err := os.WriteFile(home+"/new/"+f.n+".tmp", b, 0755); err != nil {
			upSet("err", "Tidak dapat simpan "+f.n)
			return
		}
	}
	for _, f := range files {
		_ = os.Rename(home+"/new/"+f.n+".tmp", home+"/new/"+f.n)
	}
	_ = os.WriteFile(home+"/upgrade.sh", upgradeSh, 0755)
	upSet("run", "Memasang versi "+lv+"...")
	lf, _ := os.OpenFile(home+"/run/vp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	cmd := exec.Command("/bin/sh", home+"/upgrade.sh")
	if lf != nil {
		cmd.Stdout, cmd.Stderr = lf, lf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		upSet("err", "Gagal memulakan pemasangan")
		return
	}
	upSet("done", "Dipasang, memulakan semula UI...")
	time.Sleep(60 * time.Second)
}
