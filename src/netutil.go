package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"time"
)

// dialIPv4 paksa semua sambungan HTTP KELUAR guna IPv4 sahaja ("tcp4"), walaupun
// DNS memulangkan alamat IPv6 (AAAA) yang sah. Banyak modem mobile broadband
// (cth. Huawei B618) beri alamat IPv6 yang sah melalui DNS tetapi sebenarnya
// tiada laluan WAN IPv6 langsung — percubaan sambung terus gagal serta-merta
// ("Network is unreachable"), dan "jatuh balik" automatik ke IPv4 tidak
// berlaku dengan baik pada firmware/kernel sesetengah peranti ini.
func dialIPv4(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second}
	return d.DialContext(ctx, "tcp4", addr)
}

// netTLSPool: kolam root CA untuk sambungan HTTPS keluar. Sesetengah firmware
// peranti ini (cth. B618) tiada bundle root CA sistem yang lengkap/terkini,
// menyebabkan setiap sambungan HTTPS gagal dengan "x509: certificate signed by
// unknown authority" walaupun internet (IPv4) berfungsi sepenuhnya. Gabungkan
// kolam sistem (jika ada) dengan bundle terbina-dalam (cacert.pem, lihat
// upd.go) supaya sentiasa ada root CA yang mencukupi untuk sahkan pelayan awam
// (GitHub, dll) tanpa bergantung pada bundle sistem peranti.
var netTLSPool = func() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(caPEM)
	return pool
}()

// netTransport: http.Transport kongsi untuk semua sambungan HTTP KELUAR (muat
// turun senarai iklan, geoip/geosite, semakan kemas kini sendiri, langganan,
// notifikasi Telegram, semakan IP WAN awam) — paksa IPv4 dan sahkan TLS
// menggunakan netTLSPool di atas.
func netTransport() *http.Transport {
	return &http.Transport{
		DialContext:     dialIPv4,
		TLSClientConfig: &tls.Config{RootCAs: netTLSPool},
	}
}

// netClient: http.Client kongsi, sedia pakai, untuk permintaan HTTP(S) keluar.
func netClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: netTransport()}
}
