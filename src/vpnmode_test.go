package main

import (
	"os"
	"testing"
)

func TestVPNModeNorm(t *testing.T) {
	if defOpts().VPNMode != "redirect" {
		t.Fatal("lalai mesti redirect")
	}
	for in, want := range map[string]string{"": "redirect", "tproxy": "tproxy", "tun": "tun", "xx": "redirect"} {
		if got := normOpts(Opts{VPNMode: in}).VPNMode; got != want {
			t.Errorf("%q -> %q, mahu %q", in, got, want)
		}
	}
}

func TestParseCaps(t *testing.T) {
	tp, tun := parseCaps("tproxy=1\ntun=0\n")
	if !tp || tun {
		t.Fatal(tp, tun)
	}
	tp, tun = parseCaps("tproxy=0\r\ntun=1")
	if tp || !tun {
		t.Fatal(tp, tun)
	}
}

func TestVPNModeInbounds(t *testing.T) {
	home = t.TempDir()
	_ = os.MkdirAll(home+"/etc", 0700)
	p, err := parseLink("vless://11111111-2222-3333-4444-555555555555@example.com:443?type=tcp&security=tls&sni=example.com#t")
	if err != nil {
		t.Fatal(err)
	}
	tags := func(m string) map[string]bool {
		t.Setenv("VP_VPNMODE", m)
		r := map[string]bool{}
		for _, ib := range buildConfig([]*Parsed{p}, nil)["inbounds"].([]any) {
			r[ib.(map[string]any)["tag"].(string)] = true
		}
		return r
	}
	if g := tags("redirect"); !g["redir"] || g["tproxy"] || g["tun"] {
		t.Fatal("redirect", g)
	}
	if g := tags("tproxy"); !g["redir"] || !g["tproxy"] || g["tun"] {
		t.Fatal("tproxy", g)
	}
	if g := tags("tun"); !g["redir"] || g["tproxy"] || !g["tun"] {
		t.Fatal("tun", g)
	}
}
