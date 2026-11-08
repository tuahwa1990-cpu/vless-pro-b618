// Pemasang VLESS Pro untuk Windows 10/11 (64-bit).
// Menggunakan ssh.exe dan scp.exe yang sudah ada dalam Windows (OpenSSH Client).
package main

import (
	"archive/zip"
	"bufio"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed payload
var payload embed.FS

const xrayURL = "https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-arm32-v7a.zip"

var ip = "192.168.8.1"
var opts = []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=8"}

func pause() {
	fmt.Print("\nTekan Enter untuk tutup...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func die(msg string) {
	fmt.Println("\nGAGAL:", msg)
	pause()
	os.Exit(1)
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func ssh(cmd string) (string, error) {
	return run("ssh", append(append([]string{}, opts...), "root@"+ip, cmd)...)
}

func scp(src, dst string, recursive bool) error {
	a := append([]string{"-O"}, opts...)
	if recursive {
		a = append(a, "-r")
	}
	a = append(a, src, "root@"+ip+":"+dst)
	_, err := run("scp", a...)
	return err
}

// Salin fail dari embed ke folder sementara supaya scp boleh menghantarnya.
func extract(dir string) (string, error) {
	err := fs.WalkDir(payload, "payload", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "payload/")
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		b, err := payload.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0755)
	})
	return dir, err
}

func getXray(tmp string) error {
	fmt.Println("Memuat turun Xray (sekitar 15 MB) ...")
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Get(xrayURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	zpath := filepath.Join(tmp, "x.zip")
	f, err := os.Create(zpath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()
	zr, err := zip.OpenReader(zpath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != "xray" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(tmp, "xray"))
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		return err
	}
	return fmt.Errorf("fail xray tiada dalam zip")
}

func main() {
	if len(os.Args) > 1 && os.Args[1] != "" {
		ip = os.Args[1]
	}
	fmt.Println("Pemasang VLESS Pro (B618 / Gteniq ARMv7 / ARMv7 OpenWrt)")
	fmt.Println("Modem:", ip)

	if _, err := exec.LookPath("ssh"); err != nil {
		die("ssh tiada. Pasang 'OpenSSH Client' dalam Settings > Apps > Optional features.")
	}
	if _, err := exec.LookPath("scp"); err != nil {
		die("scp tiada. Pasang 'OpenSSH Client' dalam Settings > Apps > Optional features.")
	}

	fmt.Println("[1/4] Menyemak modem ...")
	if _, err := ssh("echo ok"); err != nil {
		die("tidak dapat sambung SSH ke " + ip + ". Pastikan PC disambung ke WiFi modem dan SSH modem dihidupkan.")
	}

	st, _ := ssh("[ -f /online/xlite/bin/xray ] && echo xl; [ -f /online/vlesspro/bin/xray ] && echo vx; [ -d /online/vlesspro/etc ] && echo lama; true")
	noX := !strings.Contains(st, "xl") && !strings.Contains(st, "vx")
	upgrade := strings.Contains(st, "lama")
	if noX {
		fmt.Println("XLITE tidak dijumpai: Xray akan dimuat turun dan dipasang bersama VLESS Pro.")
	}

	tmp, err := os.MkdirTemp("", "vpro")
	if err != nil {
		die(err.Error())
	}
	defer os.RemoveAll(tmp)
	if _, err := extract(tmp); err != nil {
		die("gagal menyediakan fail: " + err.Error())
	}

	if upgrade {
		fmt.Println("[2/4] Pemasangan sedia ada dijumpai: naik taraf (tetapan dan lesen dikekalkan).")
		if err := scp(filepath.Join(tmp, "new"), "/online/vlesspro/", true); err != nil {
			die("gagal menghantar fail")
		}
		if err := scp(filepath.Join(tmp, "upgrade.sh"), "/online/vlesspro/", false); err != nil {
			die("gagal menghantar fail")
		}
	} else {
		fmt.Println("[2/4] Menghantar fail ...")
		if err := scp(filepath.Join(tmp, "vlesspro"), "/online/", true); err != nil {
			die("gagal menghantar fail")
		}
	}

	if noX {
		if err := getXray(tmp); err != nil {
			die("gagal memuat turun Xray: " + err.Error())
		}
		ssh("mkdir -p /online/vlesspro/bin")
		if err := scp(filepath.Join(tmp, "xray"), "/online/vlesspro/bin/xray", false); err != nil {
			die("gagal menghantar Xray (ruang storan modem mencukupi?)")
		}
	}

	fmt.Println("[3/4] Memasang ...")
	script := "sh /online/vlesspro/install.sh"
	if upgrade {
		script = "sh /online/vlesspro/upgrade.sh"
	}
	out, err := ssh(script)
	fmt.Print(out)
	if err != nil {
		die("pemasangan di modem gagal (lihat mesej di atas)")
	}

	fmt.Println("[4/4] Siap.")
	fmt.Println("Buka di pelayar: http://" + ip + ":8990")
	fmt.Println("Hantar ID Peranti (di atas) kepada penjual untuk mendapatkan kunci lesen.")
	if !noX {
		fmt.Println("XLITE tidak diubah dan boleh ditukar semula bila-bila masa.")
	}
	pause()
}
