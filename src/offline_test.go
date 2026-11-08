package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

type ent struct {
	name string
	data []byte
}

func mkTgz(t *testing.T, ents []ent) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range ents {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0644, Size: int64(len(e.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// pakejSah bina pakej yang ditandatangani dengan priv, seperti make-release.sh.
func pakejSah(t *testing.T, priv ed25519.PrivateKey, vp, vpro []byte) []byte {
	t.Helper()
	h := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	sums := []byte(fmt.Sprintf("%s  new/vp\n%s  new/vpro\n", h(vp), h(vpro)))
	sig := ed25519.Sign(priv, sums)
	return mkTgz(t, []ent{
		{"VERSION", []byte("1.2.0\n")},
		{"new/vp", vp},
		{"new/vpro", vpro},
		{"SHA256SUMS", sums},
		{"SHA256SUMS.sig", []byte(base64.StdEncoding.EncodeToString(sig))},
	})
}

func setKey(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	relpub = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { relpub = "" })
}

func TestPakejSahDiterima(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	setKey(t, pub)
	vp, vpro := []byte("vp-script"), []byte("vpro-binary")
	ver, gvp, gvpro, err := verifyPackage(pakejSah(t, priv, vp, vpro))
	if err != nil {
		t.Fatalf("pakej sah ditolak: %v", err)
	}
	if ver != "1.2.0" || !bytes.Equal(gvp, vp) || !bytes.Equal(gvpro, vpro) {
		t.Fatalf("kandungan tidak sepadan: ver=%q", ver)
	}
}

func TestPakejDitolak(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	vp, vpro := []byte("vp-script"), []byte("vpro-binary")

	cases := map[string]func() ([]byte, string){
		"kunci keluaran kosong": func() ([]byte, string) {
			relpub = ""
			return pakejSah(t, priv, vp, vpro), "tiada kunci keluaran"
		},
		"ditandatangani kunci lain": func() ([]byte, string) {
			setKeyNoCleanup(pub)
			return pakejSah(t, otherPriv, vp, vpro), "tandatangan"
		},
		"vpro diubah selepas ditandatangani": func() ([]byte, string) {
			setKeyNoCleanup(pub)
			data := pakejSah(t, priv, vp, vpro)
			data = tamperEntry(t, data, "new/vpro", []byte("vpro-DIUBAH"))
			return data, "semakan new/vpro"
		},
		"SHA256SUMS diubah": func() ([]byte, string) {
			setKeyNoCleanup(pub)
			data := pakejSah(t, priv, vp, vpro)
			data = tamperEntry(t, data, "SHA256SUMS", []byte("0000  new/vp\n"))
			return data, "tandatangan"
		},
		"tiada tandatangan": func() ([]byte, string) {
			setKeyNoCleanup(pub)
			return mkTgz(t, []ent{{"VERSION", []byte("1.2.0")}, {"new/vp", vp}, {"new/vpro", vpro}}), "tiada SHA256SUMS"
		},
		"laluan keluar dari folder": func() ([]byte, string) {
			setKeyNoCleanup(pub)
			return mkTgz(t, []ent{{"../etc/passwd", []byte("x")}}), "laluan tidak dibenarkan"
		},
		"bukan tgz": func() ([]byte, string) {
			setKeyNoCleanup(pub)
			return []byte("bukan gzip"), "bukan fail .tgz"
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() { relpub = "" }()
			data, want := mk()
			_, _, _, err := verifyPackage(data)
			if err == nil {
				t.Fatalf("pakej seharusnya ditolak")
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("mesej %q tidak mengandungi %q", err.Error(), want)
			}
		})
	}
}

func setKeyNoCleanup(pub ed25519.PublicKey) {
	relpub = base64.StdEncoding.EncodeToString(pub)
}

// tamperEntry gantikan kandungan satu fail dalam tgz, kekalkan fail lain.
func tamperEntry(t *testing.T, data []byte, name string, nb []byte) []byte {
	t.Helper()
	files, err := readTgz(data)
	if err != nil {
		t.Fatal(err)
	}
	files[name] = nb
	var ents []ent
	for _, n := range []string{"VERSION", "new/vp", "new/vpro", "SHA256SUMS", "SHA256SUMS.sig"} {
		if b, ok := files[n]; ok {
			ents = append(ents, ent{n, b})
		}
	}
	return mkTgz(t, ents)
}

func TestVerCmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"1.1.7", "1.1.8", -1}, {"1.1.8", "1.1.8", 0}, {"1.1.10", "1.1.9", 1}, {"1.2", "1.1.9", 1}, {"1.1", "1.1.0", 0}}
	for _, c := range cases {
		if got := verCmp(c.a, c.b); got != c.want {
			t.Errorf("verCmp(%s,%s)=%d, mahu %d", c.a, c.b, got, c.want)
		}
	}
}
