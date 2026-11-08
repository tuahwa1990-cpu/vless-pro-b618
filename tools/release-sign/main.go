// release-sign: kunci dan tandatangan pakej keluaran VLESS Pro (penjual sahaja).
//
//	go run . -gen ~/keys/vpro-release.seed        # jana kunci baharu (sekali sahaja)
//	go run . -seed ~/keys/vpro-release.seed -in SHA256SUMS -out SHA256SUMS.sig
//
// Kunci persendirian (seed) tidak disimpan dalam repo. Kunci awam dicetak untuk
// dimasukkan sebagai RELEASE_PUB dalam make-release.sh.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
)

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "GAGAL: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	gen := flag.String("gen", "", "jana seed baharu ke fail ini (tidak akan menimpa)")
	seedPath := flag.String("seed", "", "fail seed sedia ada")
	in := flag.String("in", "", "fail untuk ditandatangani (SHA256SUMS)")
	out := flag.String("out", "", "fail tandatangan base64 untuk ditulis")
	flag.Parse()

	if *gen != "" {
		if _, err := os.Stat(*gen); err == nil {
			die("fail sudah wujud: %s (tidak ditimpa)", *gen)
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			die("%v", err)
		}
		if err := os.WriteFile(*gen, []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0600); err != nil {
			die("%v", err)
		}
		fmt.Println("RELEASE_PUB=" + base64.StdEncoding.EncodeToString(pub))
		fmt.Fprintln(os.Stderr, "Seed disimpan di:", *gen, "(simpan selamat, jangan commit)")
		return
	}

	if *seedPath == "" || *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	raw, err := os.ReadFile(*seedPath)
	if err != nil {
		die("tidak dapat baca seed: %v", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		die("fail seed tidak sah")
	}
	msg, err := os.ReadFile(*in)
	if err != nil {
		die("tidak dapat baca %s: %v", *in, err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, msg)
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(sig)), 0644); err != nil {
		die("%v", err)
	}
	fmt.Println("Ditandatangani:", *out)
}
