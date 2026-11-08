package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func mk(t *testing.T) (string, ed25519.PrivateKey, []byte) {
	pk, sk, _ := ed25519.GenerateKey(rand.Reader)
	kn := []byte("12345678")
	pb := append(append([]byte("Ed"), kn...), pk...)
	return base64.StdEncoding.EncodeToString(pb), sk, kn
}
func sign(sk ed25519.PrivateKey, kn []byte, msg string) string {
	s := ed25519.Sign(sk, []byte(msg))
	return base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), kn...), s...))
}
func TestLic(t *testing.T) {
	os.Setenv("VP_MAC", "AA:BB:CC:DD:EE:FF")
	pub, sk, kn := mk(t)
	pubkey = pub
	id := devID20()
	cases := map[string]string{
		id + ":Ali:20261108:0":                "active",
		id + ":Ali:20261108:20200101":         "expired",
		"AAAAAAAAAAAAAAAAAAAA:Ali:20261108:0": "wrongdevice",
	}
	for pay, want := range cases {
		for _, nl := range []string{"", "\n"} {
			k := pay + "." + sign(sk, kn, pay+nl)
			if got := checkKey(k).State; got != want {
				t.Errorf("%s nl=%q got %s want %s", pay, nl, got, want)
			}
		}
	}
	good := id + ":Ali:20261108:0." + sign(sk, kn, id+":Ali:20261108:0")
	if checkKey(strings.Replace(good, "Ali", "Bob", 1)).State != "invalid" {
		t.Error("tamper not invalid")
	}
	if checkKey("").State != "none" || checkKey("abc").State != "invalid" {
		t.Error("none/invalid")
	}
	_, sk2, kn2 := mk(t)
	if checkKey(id+":Ali:20261108:0."+sign(sk2, kn2, id+":Ali:20261108:0")).State != "invalid" {
		t.Error("wrong signer accepted")
	}
}
func TestParse(t *testing.T) {
	p, err := parseLink("vless://11111111-2222-3333-4444-555555555555@example.com:443?type=ws&security=tls&sni=example.com&host=example.com&path=%2Fws&fp=chrome#Nod%20Satu")
	if err != nil || p.Name != "Nod Satu" || p.Net != "ws" || p.Path != "/ws" || p.Port != 443 {
		t.Fatalf("%v %+v", err, p)
	}
	if _, err := parseLink("vmess://x"); err == nil {
		t.Error("vmess accepted")
	}
	r, err := parseLink("vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?security=reality&pbk=abc&sid=01&sni=x.com&flow=xtls-rprx-vision&type=tcp#R")
	if err != nil || r.Flow == "" {
		t.Fatal(err)
	}
	cfg := buildConfig([]*Parsed{r}, nil)
	if cfg["inbounds"] == nil {
		t.Error("cfg")
	}
}
