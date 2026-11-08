package main

// Kemas kini tanpa internet: pengguna muat naik pakej .tgz dari telefon.
// Pakej mesti ditandatangani dengan kunci keluaran (relpub) yang ditanam semasa binaan.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// relpub ialah kunci awam Ed25519 (base64, 32 byte) untuk pakej keluaran.
// Diisi semasa binaan: -ldflags "-X main.relpub=BASE64".
var relpub = ""

const (
	maxPkg   = 32 << 20
	maxEntry = 16 << 20
)

// verifyPackage sahkan tandatangan SHA256SUMS, kemudian sahkan new/vp dan new/vpro.
// Hanya dua fail itu dikembalikan; fail lain dalam pakej diabaikan.
func verifyPackage(data []byte) (ver string, vp, vpro []byte, err error) {
	if relpub == "" {
		return "", nil, nil, errors.New("binari ini tiada kunci keluaran, kemas kini dari fail ditolak")
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(relpub))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "", nil, nil, errors.New("kunci keluaran dalam binari tidak sah")
	}
	files, err := readTgz(data)
	if err != nil {
		return "", nil, nil, err
	}
	sums, ok1 := files["SHA256SUMS"]
	sigB64, ok2 := files["SHA256SUMS.sig"]
	if !ok1 || !ok2 {
		return "", nil, nil, errors.New("pakej tiada SHA256SUMS atau SHA256SUMS.sig")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", nil, nil, errors.New("tandatangan pakej tidak sah")
	}
	if !ed25519.Verify(pub, sums, sig) {
		return "", nil, nil, errors.New("tandatangan pakej tidak sepadan, kemas kini ditolak")
	}
	m := sums2(sums)
	for _, n := range []string{"new/vp", "new/vpro"} {
		b, ok := files[n]
		if !ok {
			return "", nil, nil, fmt.Errorf("pakej tiada %s", n)
		}
		h := sha256.Sum256(b)
		if m[n] == "" || hex.EncodeToString(h[:]) != m[n] {
			return "", nil, nil, fmt.Errorf("semakan %s gagal", n)
		}
	}
	if v, ok := files["VERSION"]; ok {
		ver = strings.TrimSpace(string(v))
	}
	if ver == "" {
		return "", nil, nil, errors.New("pakej tiada VERSION")
	}
	return ver, files["new/vp"], files["new/vpro"], nil
}

// sums2 ialah versi lain sums() yang sedia ada, tetapi terima laluan tanpa "./".
func sums2(b []byte) map[string]string {
	m := map[string]string{}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 {
			m[strings.TrimPrefix(f[1], "./")] = strings.ToLower(f[0])
		}
	}
	return m
}

// readTgz baca semua fail biasa dalam tgz ke memori, dengan had saiz dan laluan selamat.
func readTgz(data []byte) (map[string][]byte, error) {
	if len(data) > maxPkg {
		return nil, errors.New("pakej terlalu besar")
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("bukan fail .tgz yang sah")
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("fail .tgz rosak")
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg || name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("laluan tidak dibenarkan dalam pakej: %s", h.Name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxEntry+1))
		if err != nil {
			return nil, errors.New("fail .tgz rosak")
		}
		if len(b) > maxEntry {
			return nil, errors.New("fail dalam pakej terlalu besar")
		}
		out[name] = b
	}
	return out, nil
}

// stageFiles letak new/vp dan new/vpro, kemudian lancar upgrade.sh yang ditanam dalam binari.
// Logik ini dikongsi oleh kemas kini GitHub dan muat naik fail.
func stageFiles(vp, vpro []byte) error {
	_ = os.MkdirAll(home+"/new", 0755)
	for n, b := range map[string][]byte{"vp": vp, "vpro": vpro} {
		if err := os.WriteFile(home+"/new/"+n+".tmp", b, 0755); err != nil {
			return err
		}
	}
	for _, n := range []string{"vp", "vpro"} {
		if err := os.Rename(home+"/new/"+n+".tmp", home+"/new/"+n); err != nil {
			return err
		}
	}
	return os.WriteFile(home+"/upgrade.sh", upgradeSh, 0755)
}

// launchUpgrade menjalankan upgrade.sh dalam sesi baharu, supaya ia boleh menghentikan UI ini.
func launchUpgrade() error {
	lf, _ := os.OpenFile(home+"/run/vp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	cmd := exec.Command("/bin/sh", home+"/upgrade.sh")
	if lf != nil {
		cmd.Stdout, cmd.Stderr = lf, lf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// doUploadUpdate ialah kerja latar belakang untuk pakej yang dimuat naik.
func doUploadUpdate(ver string, vp, vpro []byte) {
	defer func() { upMu.Lock(); upRun = false; upMu.Unlock() }()
	upSet("run", "Menyediakan versi "+ver+"...")
	if err := stageFiles(vp, vpro); err != nil {
		upSet("err", "Tidak dapat simpan fail: "+shortErr(err))
		return
	}
	upSet("run", "Memasang versi "+ver+"...")
	if err := launchUpgrade(); err != nil {
		upSet("err", "Gagal memulakan pemasangan")
		return
	}
	upSet("done", "Dipasang, memulakan semula UI...")
	time.Sleep(60 * time.Second)
}
