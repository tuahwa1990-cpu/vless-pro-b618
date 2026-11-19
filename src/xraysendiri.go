package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"syscall"
	"time"
)

// Xray sendiri: VLESS Pro memakai ${home}/bin/xray (binaan rasmi Xray-core, sama seperti yang
// dibawa VPN asal). Jika belum ada, ia dimuat turun sekali dan disahkan SHA-256 sebelum dipakai.
// Kegagalan tidak mengganggu: xray VPN asal (jika ada) kekal sebagai pilihan terakhir.
const xrayVer = "v26.3.27"

var xrayBase = "https://github.com/XTLS/Xray-core/releases/download/" + xrayVer + "/"

var xrayZip = map[string]struct{ nama, sha string }{
	"arm":   {"Xray-linux-arm32-v7a.zip", "c7265ae13c63ca0241a037df4ef960ad37938c8a67d984cc08834b2cfdf5654b"},
	"arm64": {"Xray-linux-arm64-v8a.zip", "4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c"},
}

func xraySendiriAda() bool {
	st, err := os.Stat(home + "/bin/xray")
	return err == nil && !st.IsDir() && st.Size() > 10<<20
}

func ruangBebas(dir string) uint64 {
	var s syscall.Statfs_t
	if syscall.Statfs(dir, &s) != nil {
		return 0
	}
	return uint64(s.Bavail) * uint64(s.Bsize)
}

func pasangXraySendiri() error {
	if xraySendiriAda() {
		return nil
	}
	z, ok := xrayZip[runtime.GOARCH]
	if !ok {
		return errors.New("senibina tidak disokong")
	}
	if ruangBebas(home) < 60<<20 {
		return errors.New("ruang storan tidak cukup")
	}
	b, err := fetch(xrayBase+z.nama, 40<<20, 10*time.Minute)
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != z.sha {
		return errors.New("semakan SHA-256 xray tidak sepadan")
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.Name != "xray" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		x, err := io.ReadAll(io.LimitReader(rc, 80<<20))
		rc.Close()
		if err != nil || len(x) < 10<<20 {
			return errors.New("fail xray tidak lengkap")
		}
		_ = os.MkdirAll(home+"/bin", 0755)
		tmp := home + "/bin/xray.tmp"
		if err := os.WriteFile(tmp, x, 0755); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, home+"/bin/xray")
	}
	return errors.New("xray tiada dalam zip")
}

// xraySendiriLoop cuba memasang xray sendiri di latar belakang (sekali berjaya, berhenti).
func xraySendiriLoop() {
	time.Sleep(3 * time.Minute)
	for {
		if xraySendiriAda() {
			return
		}
		if err := pasangXraySendiri(); err == nil {
			return
		}
		time.Sleep(6 * time.Hour)
	}
}
