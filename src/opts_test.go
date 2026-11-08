package main

import (
	"os"
	"testing"
)

func TestOptsNorm(t *testing.T) {
	o := normOpts(Opts{DNS: "bukanip", QS: "x", PingURL: "ftp://a", LogLevel: "zzz", Reboot: "25:99"})
	if o.DNS != "" || o.QS != "UseIP" || o.LogLevel != "warning" || o.Reboot != "" || o.PingURL == "ftp://a" {
		t.Fatalf("%+v", o)
	}
	o = normOpts(Opts{DNS: "1.1.1.1", QS: "UseIPv4", PingURL: "https://x.y/z", LogLevel: "debug", Reboot: "04:30"})
	if o.DNS != "1.1.1.1" || o.QS != "UseIPv4" || o.Reboot != "04:30" {
		t.Fatalf("%+v", o)
	}
}

func TestBackupRestore(t *testing.T) {
	d := t.TempDir()
	home = d
	_ = os.MkdirAll(d+"/etc", 0700)
	_ = os.WriteFile(d+"/etc/direct.txt", []byte("a.com\n"), 0600)
	_ = os.WriteFile(d+"/etc/pass", []byte("rahsia\n"), 0600)
	b := backupTgz()
	_ = os.WriteFile(d+"/etc/direct.txt", []byte("lain\n"), 0600)
	_ = os.WriteFile(d+"/etc/pass", []byte("baru\n"), 0600)
	n, err := restoreTgz(b)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if x, _ := os.ReadFile(d + "/etc/direct.txt"); string(x) != "a.com\n" {
		t.Fatal(string(x))
	}
	if x, _ := os.ReadFile(d + "/etc/pass"); string(x) != "baru\n" {
		t.Fatal("pass tidak patut dipulihkan")
	}
	if _, err := restoreTgz([]byte("sampah")); err == nil {
		t.Fatal("patut ditolak")
	}
}

func TestConfigOpts(t *testing.T) {
	d := t.TempDir()
	home = d
	_ = os.MkdirAll(d+"/etc", 0700)
	_ = os.WriteFile(d+"/etc/opts.json", []byte(`{"dns":"9.9.9.9","qs":"UseIPv4","loglevel":"error"}`), 0600)
	p, _ := parseLink("vless://11111111-1111-1111-1111-111111111111@h.example:443?security=tls&type=ws&path=/&host=h.example#n")
	c := buildConfig([]*Parsed{p}, nil)
	if c["log"].(map[string]any)["loglevel"] != "error" || c["dns"] == nil {
		t.Fatalf("%v", c)
	}
}
