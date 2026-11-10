package main

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// Pelayan proksi palsu pada 127.0.0.1:10855: jawapan 503 (nod rosak) mesti dikira gagal, 204 lulus.
func TestProxyOK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:10855")
	if err != nil {
		t.Skip("port 10855 digunakan")
	}
	code := 503
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })}
	go srv.Serve(ln)
	defer srv.Close()
	if proxyOK("http://example.invalid/generate_204", 3*time.Second) {
		t.Fatal("503 sepatutnya gagal")
	}
	code = 204
	if !proxyOK("http://example.invalid/generate_204", 3*time.Second) {
		t.Fatal("204 sepatutnya lulus")
	}
}
