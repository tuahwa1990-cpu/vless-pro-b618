package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Peranti tanpa iptables (contoh G630V/AlphaWRT: kernel nftables sahaja) guna jadual nft sendiri.
func useNft() bool { return findBin("iptables") == "" && findBin("nft") != "" }

// nftF: jalankan skrip nft melalui stdin.
func nftF(script string) error {
	b := findBin("nft")
	if b == "" {
		return fmt.Errorf("nft tiada pada peranti ini")
	}
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	defer c()
	cmd := exec.CommandContext(ctx, b, "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func nftHas(fam, tbl string) bool {
	_, err := run2("nft", "list", "table", fam, tbl)
	return err == nil
}

func nftDel(fam, tbl string) {
	if nftHas(fam, tbl) {
		_, _ = run2("nft", "delete", "table", fam, tbl)
	}
}

// adNft: alih DNS LAN (port 53) ke penapis iklan. Keutamaan -101 supaya mendahului dnsredir xray (dstnat -100).
func adNft(want, v6 bool) {
	fam, tbl := "ip", "vpad4"
	if v6 {
		fam, tbl = "ip6", "vpad6"
	}
	has := nftHas(fam, tbl)
	switch {
	case want && !has:
		p := adPortV()
		_ = nftF(fmt.Sprintf(`table %s %s {
  chain pre {
    type nat hook prerouting priority -101; policy accept;
    iifname "%s" udp dport 53 redirect to :%d
    iifname "%s" tcp dport 53 redirect to :%d
  }
}
`, fam, tbl, lanDev(), p, lanDev(), p))
	case !want && has:
		nftDel(fam, tbl)
	}
}

// quotaNft: putuskan trafik keluar melalui antara muka WAN apabila kuota penuh.
func quotaNft(on bool, dev string) {
	if !on {
		nftDel("inet", "vpquota")
		return
	}
	nftDel("inet", "vpquota")
	_ = nftF(fmt.Sprintf(`table inet vpquota {
  chain fwd {
    type filter hook forward priority -10; policy accept;
    oifname "%s" drop
  }
  chain out {
    type filter hook output priority -10; policy accept;
    oifname "%s" drop
  }
}
`, dev, dev))
}
