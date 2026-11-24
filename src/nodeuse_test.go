package main

import (
	"os"
	"testing"
)

func TestNodeUseAdd(t *testing.T) {
	old := home
	home = t.TempDir()
	defer func() { home = old; nuMap = map[string]nuVal{} }()
	_ = os.MkdirAll(home+"/etc", 0700)
	nuMap = map[string]nuVal{}
	// mod xlite: tiada pengiraan
	_ = os.WriteFile(home+"/etc/mode", []byte("xlite"), 0600)
	nodeUseAdd(100, 50)
	if len(nuMap) != 0 {
		t.Fatalf("xlite tidak patut dikira: %v", nuMap)
	}
	// mod vless dengan satu profil dipilih
	_ = os.WriteFile(home+"/etc/mode", []byte("vless"), 0600)
	_ = saveNodes([]Node{{Name: "a", Link: "vless://u@h:443?type=tcp#a"}})
	_ = setSel(0)
	nodeUseAdd(100, 50)
	nodeUseAdd(10, 5)
	v := nuMap[nid("vless://u@h:443?type=tcp#a")]
	if v.Rx != 110 || v.Tx != 55 {
		t.Fatalf("salah: %+v", v)
	}
	nodeUseSave()
	nuMap = map[string]nuVal{}
	nodeUseLoad()
	if nuMap[nid("vless://u@h:443?type=tcp#a")].Rx != 110 {
		t.Fatal("tidak dimuat semula")
	}
}
