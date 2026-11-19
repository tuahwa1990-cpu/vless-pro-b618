package main

import (
	"os"
	"testing"
)

func TestDevices(t *testing.T) {
	d := t.TempDir()
	home = d
	_ = os.MkdirAll(d+"/etc", 0700)
	arpPath = d + "/arp"
	leaseFiles = []string{d + "/leases"}
	dev := lanDev()
	_ = os.WriteFile(arpPath, []byte("IP address       HW type     Flags       HW address            Mask     Device\n"+
		"192.168.8.20     0x1         0x2         AA:BB:CC:00:00:02     *        "+dev+"\n"+
		"192.168.8.3      0x1         0x2         aa:bb:cc:00:00:01     *        "+dev+"\n"+
		"192.168.8.9      0x1         0x0         aa:bb:cc:00:00:09     *        "+dev+"\n"+ // tidak lengkap
		"10.0.0.1         0x1         0x2         aa:bb:cc:00:00:0a     *        wwan0\n"+ // antara muka lain
		"192.168.8.3      0x1         0x2         aa:bb:cc:00:00:01     *        "+dev+"\n"), 0600) // pendua
	_ = os.WriteFile(d+"/leases", []byte("1795176000 aa:bb:cc:00:00:01 192.168.8.3 telefon-ali 01:aa\n1795176000 aa:bb:cc:00:00:02 192.168.8.20 * *\n"), 0600)

	ds := listDevices()
	if len(ds) != 2 || ds[0].IP != "192.168.8.3" || ds[1].IP != "192.168.8.20" {
		t.Fatalf("senarai: %+v", ds)
	}
	if ds[0].Host != "telefon-ali" || ds[1].Host != "" || ds[1].MAC != "aa:bb:cc:00:00:02" {
		t.Fatalf("hos/mac: %+v", ds)
	}
	if m, ok := setDevName("AA:BB:CC:00:00:01", "  Telefon\x07 Ali  "); !ok {
		t.Fatal(m)
	}
	if ds = listDevices(); ds[0].Name != "Telefon Ali" {
		t.Fatalf("nama: %+v", ds[0])
	}
	if _, ok := setDevName("bukan-mac", "x"); ok {
		t.Error("mac tidak sah diterima")
	}
	if _, ok := setDevName("aa:bb:cc:00:00:01", ""); !ok {
		t.Fatal("padam nama")
	}
	if ds = listDevices(); ds[0].Name != "" {
		t.Fatalf("nama tidak dipadam: %+v", ds[0])
	}
	if len(cleanName("123456789012345678901234567890", 24)) != 24 {
		t.Error("had panjang nama")
	}
}
