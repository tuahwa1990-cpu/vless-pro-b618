// issue-licence: jana kunci lesen VLESS Pro untuk satu ID Peranti (penjual sahaja).
//
// Kunci persendirian TIDAK disimpan dalam repo. Fail seed (32-byte Ed25519, base64)
// dibaca daripada laluan yang anda beri melalui -seed.
//
// Contoh:
//
//	go run . -seed ~/keys/vpro.seed -pub "RWQEaRIHBlats0krniS/twQsiXX+YrMtCgz4NYkS7Ia+Bwm4YotZYHwk" \
//	  -id 1A2B-3C4D-5E6F-7A8B-9C0D -name "Pelanggan Ali" -exp 20271231
//
// -exp 0 bermaksud tiada tarikh tamat. Output ialah satu baris kunci untuk ditampal di UI.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,24}$`)
var expRe = regexp.MustCompile(`^([0-9]{8}|0)$`)

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "GAGAL: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	seedPath := flag.String("seed", "", "laluan fail seed Ed25519 (base64, 32-byte)")
	pubB64 := flag.String("pub", "", "kunci awam base64 (42-byte, sama seperti dalam build)")
	id := flag.String("id", "", "ID Peranti dari UI (contoh 1A2B-3C4D-5E6F-7A8B-9C0D)")
	name := flag.String("name", "", "nama pelanggan (huruf, nombor, - atau _, maks 24)")
	exp := flag.String("exp", "0", "tarikh tamat YYYYMMDD, atau 0 untuk tiada tamat")
	issued := flag.String("issued", "", "tarikh keluar YYYYMMDD (lalai hari ini)")
	gen := flag.String("gen", "", "jana pasangan kunci lesen baharu; tulis seed ke fail ini (tidak menimpa)")
	flag.Parse()

	if *gen != "" {
		if _, err := os.Stat(*gen); err == nil {
			die("%s sudah wujud; tidak ditimpa", *gen)
		}
		seed := make([]byte, ed25519.SeedSize)
		kid := make([]byte, 8)
		if _, err := rand.Read(seed); err != nil {
			die("rawak gagal: %v", err)
		}
		rand.Read(kid)
		pk := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		if err := os.WriteFile(*gen, []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0600); err != nil {
			die("tidak dapat tulis seed: %v", err)
		}
		blob := append([]byte{'E', 'd'}, kid...)
		blob = append(blob, pk...)
		fmt.Println("LICENCE_PUB=" + base64.StdEncoding.EncodeToString(blob))
		return
	}

	if *seedPath == "" || *pubB64 == "" || *id == "" || *name == "" {
		flag.Usage()
		os.Exit(2)
	}
	if !nameRe.MatchString(*name) {
		die("nama tidak sah (huruf, nombor, - atau _, maks 24 aksara, tiada ruang)")
	}
	if !expRe.MatchString(*exp) {
		die("-exp mesti YYYYMMDD atau 0")
	}

	// ID Peranti dipaparkan dengan sengkang; payload perlu 20 aksara HEX huruf besar.
	devID := strings.ToUpper(strings.ReplaceAll(*id, "-", ""))
	if len(devID) != 20 || strings.Trim(devID, "0123456789ABCDEF") != "" {
		die("ID Peranti mesti 20 aksara HEX (contoh 1A2B-3C4D-5E6F-7A8B-9C0D)")
	}

	iss := *issued
	if iss == "" {
		iss = time.Now().Format("20060102")
	}
	if !expRe.MatchString(iss) || iss == "0" {
		die("-issued mesti YYYYMMDD")
	}

	// Kunci awam: "Ed" + keyid(8) + pubkey(32), sama seperti pubBytes() dalam vpro.
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(*pubB64))
	if err != nil || len(pub) != 42 || pub[0] != 'E' || pub[1] != 'd' {
		die("-pub tidak sah (perlu 42-byte base64 bermula 'Ed')")
	}
	keyID := pub[2:10]

	raw, err := os.ReadFile(*seedPath)
	if err != nil {
		die("tidak dapat baca fail seed: %v", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		die("fail seed mesti base64 bagi 32 byte")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	// Pastikan seed sepadan dengan kunci awam yang dibenarkan.
	if string(priv.Public().(ed25519.PublicKey)) != string(pub[10:]) {
		die("fail seed tidak sepadan dengan -pub (salah kunci?)")
	}

	payload := fmt.Sprintf("%s:%s:%s:%s", devID, *name, iss, *exp)
	sig := ed25519.Sign(priv, []byte(payload))
	blob := append([]byte{'E', 'd'}, keyID...)
	blob = append(blob, sig...)

	fmt.Println(payload + "." + base64.StdEncoding.EncodeToString(blob))
}
