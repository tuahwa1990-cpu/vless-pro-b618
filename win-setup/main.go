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
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed payload
var payload embed.FS

const xrayURL = "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-arm32-v7a.zip"

// Mesti sepadan dengan vp/vpro di dalam modem; backupCmd hanya dijalankan dalam mod Pasang bersih.
const backupCmd = `mount -o remount,rw /online 2>/dev/null; p=; [ -f /online/vlesspro/run/ui.pid ] && read p < /online/vlesspro/run/ui.pid; [ -n "$p" ] && kill $p 2>/dev/null; sleep 1; [ -n "$p" ] && kill -9 $p 2>/dev/null; rm -f /online/vlesspro/run/ui.pid; ts=$(date +%Y%m%d%H%M%S); mkdir -p /online/vlesspro/backup && mv /online/vlesspro/etc /online/vlesspro/backup/etc-$ts && mkdir -p /online/vlesspro/etc && for f in license public.key; do [ -f /online/vlesspro/backup/etc-$ts/$f ] && cp /online/vlesspro/backup/etc-$ts/$f /online/vlesspro/etc/$f; done; chmod 600 /online/vlesspro/etc/license 2>/dev/null; echo "Tetapan lama dialih ke /online/vlesspro/backup/etc-$ts; lesen dikekalkan"`

var ip = "192.168.8.1"
var opts = []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=8"}

var stdin = bufio.NewReader(os.Stdin)

type device struct{ name, ip, kind string }

// Peranti yang disokong: B618 (ARMv7, /online) dan AW1000 Ultra (arm64, OpenWrt, /data).
var devices = []device{
	{"Huawei B618", "192.168.8.1", "b618"},
	{"Arcadyan AW1000 Ultra", "192.168.1.1", "aw1000"},
}

// Kata laluan ditanya sekali sahaja, kemudian diberi kepada ssh/scp melalui SSH_ASKPASS
// (fail .exe ini sendiri bertindak sebagai pembantu). Jika tidak berfungsi, ssh bertanya seperti biasa.
var sshEnv []string

var k32 = syscall.NewLazyDLL("kernel32.dll")

func readSecret(prompt string) string {
	fmt.Print(prompt)
	h := os.Stdin.Fd()
	var mode uint32
	gm, sm := k32.NewProc("GetConsoleMode"), k32.NewProc("SetConsoleMode")
	ok, _, _ := gm.Call(h, uintptr(unsafe.Pointer(&mode)))
	if ok != 0 {
		sm.Call(h, uintptr(mode&^0x0004)) // matikan ENABLE_ECHO_INPUT
	}
	s, _ := stdin.ReadString('\n')
	if ok != 0 {
		sm.Call(h, uintptr(mode))
	}
	fmt.Println()
	return strings.TrimRight(s, "\r\n")
}

// shortPath mengelak masalah ruang (cth. "My Drive") pada laluan pembantu.
func shortPath(p string) string {
	b, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 520)
	n, _, _ := k32.NewProc("GetShortPathNameW").Call(uintptr(unsafe.Pointer(b)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) >= len(buf) {
		return p
	}
	return syscall.UTF16ToString(buf[:n])
}

func setupAskpass() {
	pw := readSecret("Kata laluan root modem (Enter jika tiada kata laluan): ")
	if pw == "" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	helper := filepath.Join(os.TempDir(), "vpro-askpass.exe")
	in, err := os.ReadFile(self)
	if err != nil || os.WriteFile(helper, in, 0700) != nil {
		return
	}
	sshEnv = append(os.Environ(), "SSH_ASKPASS="+shortPath(helper), "SSH_ASKPASS_REQUIRE=force", "VP_HELPER=1", "VP_PW="+pw, "DISPLAY=vpro")
}

func cleanupAskpass() { os.Remove(filepath.Join(os.TempDir(), "vpro-askpass.exe")) }

func ask(prompt string) string {
	fmt.Print(prompt)
	s, _ := stdin.ReadString('\n')
	return strings.TrimSpace(s)
}

func yes(q string) bool {
	c := strings.ToLower(ask(q + " [Y/n]: "))
	return c == "" || c == "y" || c == "ya"
}

func pause() {
	ask("\nTekan Enter untuk tutup...")
}

func die(msg string) {
	cleanupAskpass()
	fmt.Println("\nGAGAL:", msg)
	pause()
	os.Exit(1)
}

func cancelled() {
	cleanupAskpass()
	fmt.Println("\nDibatalkan. Tiada apa diubah pada modem.")
	pause()
	os.Exit(0)
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = sshEnv
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// sshLive menunjukkan output modem secara langsung (bukan selepas siap).
func sshLive(cmd string) error {
	c := exec.Command("ssh", append(append([]string{}, opts...), "root@"+ip, cmd)...)
	c.Env = sshEnv
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
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

// pickDevice membenarkan pengguna pilih jenis modem atau masukkan IP sendiri.
func pickDevice() device {
	fmt.Println("Pilih modem anda:")
	for i, d := range devices {
		fmt.Printf("  %d. %s  (%s)\n", i+1, d.name, d.ip)
	}
	lain := len(devices) + 1
	fmt.Printf("  %d. Lain-lain (masukkan IP)\n", lain)
	for {
		c := ask("Pilihan [1]: ")
		if c == "" {
			c = "1"
		}
		n, err := strconv.Atoi(c)
		if err == nil && n >= 1 && n <= len(devices) {
			return devices[n-1]
		}
		if err == nil && n == lain {
			for {
				fmt.Println("AMARAN: pemasang ini hanya diuji pada Huawei B618 dan Arcadyan AW1000 Ultra. Modem lain mungkin tidak berfungsi.")
				s := ask("Masukkan IP modem: ")
				if net.ParseIP(s) != nil {
					k := "b618"
					fmt.Println("Jenis modem pada IP ini:")
					fmt.Println("  1. Huawei B618")
					fmt.Println("  2. Arcadyan AW1000 Ultra")
					if ask("Pilihan [1]: ") == "2" {
						return device{"Arcadyan AW1000 Ultra", s, "aw1000"}
					}
					return device{"Lain-lain", s, k}
				}
				fmt.Println("IP tidak sah.")
			}
		}
		fmt.Println("Pilihan tidak sah.")
	}
}

type modemState struct {
	arch  string
	xlite bool // XLITE dijumpai
	vx    bool // Xray VLESS Pro sedia ada
	lama  bool // pemasangan VLESS Pro sedia ada (folder etc)
}

func checkModem() modemState {
	out, _ := ssh("echo A=$(awk -F: '/^CPU architecture/{print $2+0;exit}' /proc/cpuinfo); [ -f /online/xlite/bin/xray ] && echo xl; [ -f /online/vlesspro/bin/xray ] && echo vx; [ -d /online/vlesspro/etc ] && echo lama; true")
	f := strings.Fields(out)
	var s modemState
	tok := map[string]bool{}
	for _, t := range f {
		if strings.HasPrefix(t, "A=") {
			switch strings.TrimPrefix(t, "A=") {
			case "7":
				s.arch = "armv7"
			case "8":
				s.arch = "arm64"
			case "":
				s.arch = "tidak diketahui"
			default:
				s.arch = "arm" + strings.TrimPrefix(t, "A=")
			}
			continue
		}
		tok[t] = true
	}
	s.xlite = tok["xl"]
	s.vx = tok["vx"]
	s.lama = tok["lama"]
	return s
}

func main() {
	if os.Getenv("VP_HELPER") == "1" {
		fmt.Println(os.Getenv("VP_PW"))
		return
	}
	defer cleanupAskpass()
	dev := device{"Huawei B618", ip, "b618"}
	if len(os.Args) > 1 && os.Args[1] != "" {
		ip = os.Args[1]
		dev = device{"Lain-lain", ip, "b618"}
		if len(os.Args) > 2 && os.Args[2] == "aw1000" {
			dev = device{"Arcadyan AW1000 Ultra", ip, "aw1000"}
		}
		for _, d := range devices {
			if d.ip == ip {
				dev = d
			}
		}
	} else {
		dev = pickDevice()
		ip = dev.ip
	}
	fmt.Println("\nPemasang VLESS Pro (" + dev.name + ")")
	fmt.Println("Modem:", ip)

	if _, err := exec.LookPath("ssh"); err != nil {
		die("ssh tiada. Pasang 'OpenSSH Client' dalam Settings > Apps > Optional features.")
	}
	if _, err := exec.LookPath("scp"); err != nil {
		die("scp tiada. Pasang 'OpenSSH Client' dalam Settings > Apps > Optional features.")
	}

	setupAskpass()
	fmt.Println("\n[1/4] Menyemak modem ...")
	if _, err := ssh("echo ok"); err != nil {
		die("tidak dapat sambung SSH ke " + ip + ". Pastikan PC disambung ke WiFi modem dan SSH modem dihidupkan.")
	}
	if dev.kind == "aw1000" {
		installAW1000()
		return
	}
	st := checkModem()
	if !strings.HasPrefix(st.arch, "armv7") {
		fmt.Printf("AMARAN: modem melaporkan seni bina '%s'. Pemasang ini untuk ARMv7 sahaja.\n", st.arch)
		if !yes("Teruskan juga?") {
			cancelled()
		}
	}

	mode := "baru"
	if st.lama {
		fmt.Println("\nPemasangan VLESS Pro sedia ada dijumpai.")
		fmt.Println("  1. Baiki / naik taraf (disyorkan): kemas kini fail, kekalkan tetapan, lesen dan nod.")
		fmt.Println("  2. Pasang bersih: alih tetapan lama ke folder backup, pasang semula dengan kata laluan awal admin.")
		if ask("Pilihan [1]: ") == "2" {
			mode = "bersih"
		} else {
			mode = "baiki"
		}
	}
	if mode == "bersih" {
		fmt.Println("\nAMARAN: tetapan lama akan dialih ke /online/vlesspro/backup/ dan kata laluan UI akan bertukar.")
		fmt.Println("Lesen akan dikekalkan secara automatik. Nod dan tetapan lain akan dialih ke backup.")
		if ask("Taip PADAM untuk teruskan: ") != "PADAM" {
			cancelled()
		}
	}

	noX := !st.xlite && !st.vx
	fmt.Println("\nRingkasan sebelum pasang:")
	fmt.Println("  Modem        :", ip, "("+st.arch+")")
	if st.xlite {
		fmt.Println("  XLITE        : dijumpai (tidak diubah)")
	} else {
		fmt.Println("  XLITE        : tidak dijumpai")
	}
	switch mode {
	case "baiki":
		fmt.Println("  Tindakan     : naik taraf VLESS Pro (tetapan dikekalkan)")
	case "bersih":
		fmt.Println("  Tindakan     : pasang bersih (tetapan lama dalam backup)")
	default:
		fmt.Println("  Tindakan     : pemasangan baharu")
	}
	if noX {
		fmt.Println("  Xray         : akan dimuat turun dan dipasang bersama VLESS Pro")
	}
	if !yes("\nTeruskan pemasangan?") {
		cancelled()
	}

	tmp, err := os.MkdirTemp("", "vpro")
	if err != nil {
		die(err.Error())
	}
	defer os.RemoveAll(tmp)
	if _, err := extract(tmp); err != nil {
		die("gagal menyediakan fail: " + err.Error())
	}

	fmt.Println("\n[2/4] Menghantar fail ...")
	if mode == "bersih" {
		out, err := ssh(backupCmd)
		fmt.Print(out)
		if err != nil {
			die("gagal mengalih tetapan lama (lihat mesej di atas). Tiada apa dipasang.")
		}
	}
	if mode == "baiki" {
		if err := scp(filepath.Join(tmp, "new"), "/online/vlesspro/", true); err != nil {
			die("gagal menghantar fail")
		}
		if err := scp(filepath.Join(tmp, "upgrade.sh"), "/online/vlesspro/", false); err != nil {
			die("gagal menghantar fail")
		}
	} else {
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

	fmt.Println("\n[3/4] Memasang ...")
	script := "sh /online/vlesspro/install.sh"
	if mode == "baiki" {
		script = "sh /online/vlesspro/upgrade.sh"
	}
	if err := sshLive(script); err != nil {
		die("pemasangan di modem gagal (lihat mesej di atas)")
	}

	fmt.Println("\n[4/4] Siap.")
	fmt.Println("Buka di pelayar: http://" + ip + ":8990")
	if mode != "baiki" {
		fmt.Println("Log masuk dengan kata laluan awal: admin. Boleh ditukar di Tetapan.")
	}
	fmt.Println("Lesen: jika belum ada, hantar ID Peranti (di atas) kepada penjual.")
	fmt.Println("Simpan kunci lesen anda. Jika modem direset, pasang semula dan tampal kunci yang sama.")
	if st.xlite {
		fmt.Println("XLITE tidak diubah dan boleh ditukar semula bila-bila masa.")
	}
	pause()
}

// installAW1000 memasang/menaik taraf VLESS Pro pada Arcadyan AW1000 Ultra (arm64, OpenWrt).
// Sama seperti aw1000/pasang.sh: tetapan, lesen dan nod sedia ada tidak diubah, XLITE tidak disentuh.
func installAW1000() {
	out, _ := ssh("echo A=$(uname -m); [ -d /data/vlesspro/etc ] && echo lama; [ -x /data/vlesspro/bin/xray ] && echo vx; [ -x /opt/xlite/bin/xray ] && echo xl; [ -x /usr/bin/xray ] && echo ux; true")
	tok := map[string]bool{}
	arch := ""
	for _, t := range strings.Fields(out) {
		if strings.HasPrefix(t, "A=") {
			arch = strings.TrimPrefix(t, "A=")
			continue
		}
		tok[t] = true
	}
	if arch != "aarch64" {
		fmt.Printf("AMARAN: modem melaporkan seni bina '%s'. Pemasang AW1000 ini untuk arm64 (aarch64) sahaja.\n", arch)
		if !yes("Teruskan juga?") {
			cancelled()
		}
	}
	hasX := tok["vx"] || tok["xl"] || tok["ux"]
	fmt.Println("\nRingkasan sebelum pasang:")
	fmt.Println("  Modem        :", ip, "(AW1000 Ultra, "+arch+")")
	if tok["lama"] {
		fmt.Println("  Tindakan     : naik taraf VLESS Pro (tetapan, lesen dan nod dikekalkan)")
	} else {
		fmt.Println("  Tindakan     : pemasangan baharu")
	}
	if tok["xl"] {
		fmt.Println("  XLITE        : dijumpai (tidak diubah)")
	} else {
		fmt.Println("  XLITE        : tidak dijumpai")
	}
	if !hasX {
		fmt.Println("  AMARAN       : Xray tidak dijumpai pada modem. UI akan dipasang tetapi sambungan VLESS tidak boleh digunakan sehingga Xray ada.")
	}
	if !yes("\nTeruskan pemasangan?") {
		cancelled()
	}
	tmp, err := os.MkdirTemp("", "vpro")
	if err != nil {
		die(err.Error())
	}
	defer os.RemoveAll(tmp)
	if _, err := extract(tmp); err != nil {
		die("gagal menyediakan fail: " + err.Error())
	}
	f, err := os.Open(filepath.Join(tmp, "aw1000.tgz"))
	if err != nil {
		die("fail pemasang AW1000 tiada dalam program ini")
	}
	defer f.Close()
	fmt.Println("\n[2/4] Menghantar fail ...")
	c := exec.Command("ssh", append(append([]string{}, opts...), "root@"+ip,
		"p=; [ -f /data/vlesspro/run/ui.pid ] && read p < /data/vlesspro/run/ui.pid; [ -n \"$p\" ] && kill $p 2>/dev/null; rm -f /data/vlesspro/bin/vpro; mkdir -p /data && cd /data && tar xzf -")...)
	c.Env = sshEnv
	c.Stdin = f
	if o, err := c.CombinedOutput(); err != nil {
		fmt.Print(string(o))
		die("gagal menghantar fail ke modem")
	}
	fmt.Println("\n[3/4] Memasang ...")
	if err := sshLive("sh /data/vlesspro/install.sh"); err != nil {
		die("pemasangan di modem gagal (lihat mesej di atas)")
	}
	fmt.Println("\n[4/4] Siap.")
	fmt.Println("Buka di pelayar: http://" + ip + ":8990")
	fmt.Println("Log masuk dengan kata laluan awal: admin. Boleh ditukar di Tetapan.")
	fmt.Println("Lesen: jika belum ada, hantar ID Peranti (di atas) kepada penjual.")
	fmt.Println("Simpan kunci lesen anda. Jika modem direset, pasang semula dan tampal kunci yang sama.")
	pause()
}
