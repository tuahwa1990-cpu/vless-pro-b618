package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	old := relpub
	relpub = base64.StdEncoding.EncodeToString(pub)
	defer func() { relpub = old }()
	sums := []byte("abc  vpro\ndef  vp\n")
	good := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.sig":
			w.Write([]byte(good + "\n"))
		case "/bad.sig":
			w.Write([]byte(base64.StdEncoding.EncodeToString(make([]byte, 64))))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	if err := verifySums(srv.URL+"/ok.sig", sums); err != nil {
		t.Fatalf("tandatangan sah ditolak: %v", err)
	}
	if verifySums(srv.URL+"/bad.sig", sums) == nil {
		t.Fatal("tandatangan salah diterima")
	}
	if verifySums(srv.URL+"/tiada.sig", sums) == nil {
		t.Fatal("tandatangan tiada diterima")
	}
	if verifySums(srv.URL+"/ok.sig", []byte("ubah")) == nil {
		t.Fatal("senarai diubah diterima")
	}
}

func TestValidUpdURL(t *testing.T) {
	for s, want := range map[string]bool{
		"https://raw.githubusercontent.com/a/b/main/aw1000-latest.txt": true,
		"http://x.com/aw1000-latest.txt":                               false,
		"https://x.com/aw1000.txt":                                     false,
		"https://x.com/a b-latest.txt":                                 false,
		"https:///-latest.txt":                                         false,
	} {
		if validUpdURL(s) != want {
			t.Errorf("%s: mahu %v", s, want)
		}
	}
}
