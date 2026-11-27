package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Sandaran untuk peranti yang kernelnya tiada modul iptables REDIRECT/DNAT
// (contohnya sesetengah firmware AW1000 bersama NSS/hardware offload — modul
// xt_REDIRECT tiada terus). Dalam kes itu pengalihan port 53 langsung gagal
// senyap, jadi "Sekat Iklan" kelihatan aktif tetapi tiada apa yang disekat.
//
// Sebagai gantian, kita suap senarai terus kepada dnsmasq (DNS sebenar semua
// peranti LAN) melalui pilihan UCI "addnhosts" (fail hosts tambahan) +
// "addnmount" (benarkan fail itu dibaca dari dalam jail dnsmasq). Ini
// berfungsi berasingan sepenuhnya daripada mekanisme REDIRECT/local-server
// sedia ada, dan selamat dijalankan serentak dengannya.

func dnsmasqHostsFile() string { return home + "/etc/ads/dnsmasq-hosts.txt" }

// dnsmasqUciSupported: hanya pada peranti berasaskan OpenWrt/UCI dengan
// bahagian dnsmasq sedia ada (bukan B618, yang firmwarenya bukan UCI).
func dnsmasqUciSupported() bool {
	if isB618() {
		return false
	}
	if findBin("uci") == "" {
		return false
	}
	_, err := run2("uci", "get", "dhcp.@dnsmasq[0]")
	return err == nil
}

// dnsmasqEnsureUci: pastikan fail kita disenaraikan dalam addnhosts (dibaca
// dnsmasq) dan addnmount (dibenarkan jail baca). Pulangkan true jika baru
// ditambah (perlukan restart penuh, bukan sekadar tukar kandungan fail).
func dnsmasqEnsureUci(path string) bool {
	out, _ := run2("uci", "show", "dhcp.@dnsmasq[0]")
	quoted := "'" + path + "'"
	hasOpt := func(opt string) bool {
		for _, ln := range strings.Split(out, "\n") {
			if strings.HasPrefix(ln, "dhcp.@dnsmasq[0]."+opt+"=") && strings.Contains(ln, quoted) {
				return true
			}
		}
		return false
	}
	added := false
	if !hasOpt("addnhosts") {
		_, _ = run2("uci", "add_list", "dhcp.@dnsmasq[0].addnhosts="+path)
		added = true
	}
	if !hasOpt("addnmount") {
		_, _ = run2("uci", "add_list", "dhcp.@dnsmasq[0].addnmount="+path)
		added = true
	}
	if added {
		_, _ = run2("uci", "commit", "dhcp")
	}
	return added
}

func dnsmasqRestart() {
	ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
	defer c()
	_ = exec.CommandContext(ctx, "/etc/init.d/dnsmasq", "restart").Run()
}

// dnsmasqSyncHosts: tulis semula fail hosts gabungan (daripada senarai aktif +
// senarai hitam, kurang senarai putih) dan reload dnsmasq jika kandungan
// berubah atau UCI baru disediakan.
func dnsmasqSyncHosts(c AdCfg) {
	if !dnsmasqUciSupported() {
		return
	}
	path := dnsmasqHostsFile()
	_ = os.MkdirAll(home+"/etc/ads", 0700)
	tmp := path + ".tmp"
	wf, err := os.Create(tmp)
	if err != nil {
		return
	}
	bw := bufio.NewWriterSize(wf, 1<<20)
	white := make([]string, 0, len(c.White))
	for _, w := range c.White {
		if w != "" {
			white = append(white, w)
		}
	}
	isWhite := func(d string) bool {
		for _, w := range white {
			if d == w || strings.HasSuffix(d, "."+w) {
				return true
			}
		}
		return false
	}
	write := func(d string) {
		if d == "" || isWhite(d) {
			return
		}
		bw.WriteString("0.0.0.0 ")
		bw.WriteString(d)
		bw.WriteString("\n:: ")
		bw.WriteString(d)
		bw.WriteString("\n")
	}
	for _, id := range c.Lists {
		f, err := os.Open(adFile(id))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			write(parseAdLine(sc.Text()))
		}
		f.Close()
	}
	for _, d := range c.Black {
		write(d)
	}
	_ = bw.Flush()
	wf.Close()

	old, _ := os.ReadFile(path)
	newB, _ := os.ReadFile(tmp)
	changed := len(old) != len(newB) || string(old) != string(newB)
	if changed {
		_ = os.Rename(tmp, path)
	} else {
		_ = os.Remove(tmp)
	}
	firstSetup := dnsmasqEnsureUci(path)
	if changed || firstSetup {
		dnsmasqRestart()
	}
}

// dnsmasqClearHosts: bila Sekat Iklan dimatikan, kosongkan fail supaya tiada
// domain disekat melalui laluan dnsmasq ini lagi.
func dnsmasqClearHosts() {
	if !dnsmasqUciSupported() {
		return
	}
	path := dnsmasqHostsFile()
	old, err := os.ReadFile(path)
	if err != nil || len(old) == 0 {
		return
	}
	if os.WriteFile(path, []byte{}, 0644) == nil {
		dnsmasqRestart()
	}
}
