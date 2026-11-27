package main

import (
	"context"
	"net"
	"time"
)

// dialIPv4 paksa semua sambungan HTTP KELUAR (muat turun senarai iklan, semakan
// kemas kini apl/firmware, semakan IP WAN awam, dll.) guna IPv4 sahaja ("tcp4"),
// walaupun DNS memulangkan alamat IPv6 (AAAA) yang sah.
//
// Banyak modem mobile broadband (cth. Huawei B618) beri alamat IPv6 yang sah
// melalui DNS tetapi sebenarnya tiada laluan WAN IPv6 langsung — percubaan
// sambung terus gagal serta-merta ("Network is unreachable"). Pada firmware/
// kernel sesetengah peranti ini, "jatuh balik" automatik Go ke IPv4 (happy
// eyeballs) tidak berlaku dengan baik, menyebabkan keseluruhan permintaan HTTP
// gagal walaupun IPv4 sepenuhnya berfungsi. Paksa "tcp4" terus di sini
// mengelakkan masalah itu tanpa bergantung pada tingkah laku jatuh balik.
func dialIPv4(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second}
	return d.DialContext(ctx, "tcp4", addr)
}
