package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func k32() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func TestParseWG(t *testing.T) {
	priv, pub := k32(), k32()
	link := "wireguard://" + url.PathEscape(priv) + "@1.2.3.4:51820?publickey=" + url.QueryEscape(pub) + "&address=10.0.0.2/32,fd00::2&mtu=1280&reserved=1,2,3#Ujian%20WG"
	p, err := parseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if p.Proto != "wg" || p.Host != "1.2.3.4" || p.Port != 51820 || p.Name != "Ujian WG" || p.WG.MTU != 1280 || len(p.WG.Reserved) != 3 {
		t.Fatalf("salah: %+v %+v", p, p.WG)
	}
	if p.WG.Addr[1] != "fd00::2/128" {
		t.Fatalf("alamat v6: %v", p.WG.Addr)
	}
	o := proxyOut(p, "proxy")
	st := o["settings"].(map[string]any)
	if o["protocol"] != "wireguard" || st["secretKey"] != priv || st["domainStrategy"] != nil {
		t.Fatalf("outbound: %v", o)
	}
	if _, err := parseLink("wireguard://bukan@1.2.3.4:1?publickey=" + url.QueryEscape(pub) + "&address=10.0.0.2"); err == nil {
		t.Fatal("kunci rosak diterima")
	}
	if _, err := parseLink("wireguard://" + url.PathEscape(priv) + "@1.2.3.4:1?publickey=" + url.QueryEscape(pub)); err == nil {
		t.Fatal("tanpa address diterima")
	}
}

func TestConfToWG(t *testing.T) {
	priv, pub, psk := k32(), k32(), k32()
	conf := "[Interface]\nPrivateKey = " + priv + "\nAddress = 10.8.0.2/24\nDNS = 1.1.1.1\nMTU = 1380\n\n[Peer]\nPublicKey = " + pub + "\nPresharedKey = " + psk + "\nEndpoint = vpn.contoh.my:51820\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n"
	ln, err := confToWG(conf)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseLink(ln)
	if err != nil {
		t.Fatal(err)
	}
	if p.WG.Key != priv || p.WG.Pub != pub || p.WG.PSK != psk || p.WG.MTU != 1380 || p.WG.Keep != 25 || p.Host != "vpn.contoh.my" {
		t.Fatalf("salah: %+v", p.WG)
	}
	o := wgOut(p, "x")
	if o["settings"].(map[string]any)["domainStrategy"] != "ForceIPv4" {
		t.Fatal("alamat v4 sahaja mesti ForceIPv4")
	}
}

func TestParseSSH(t *testing.T) {
	pl := "GET / HTTP/1.1[crlf]Host: bug.contoh.my[crlf]Upgrade: websocket[crlf][crlf]"
	p, err := parseLink("ssh://ali:r%40hsia@sg1.contoh.my:80?mode=ws&payload=" + url.QueryEscape(pl) + "&proxy=104.16.1.1:80#SSH%20SG")
	if err != nil {
		t.Fatal(err)
	}
	if p.Proto != "ssh" || p.SSH.User != "ali" || p.SSH.Pass != "r@hsia" || p.SSH.Payload != pl || p.Name != "SSH SG" {
		t.Fatalf("%+v %+v", p, p.SSH)
	}
	if h, pt, ok := pingTarget(p); !ok || h != "104.16.1.1" || pt != 80 {
		t.Fatal("ping mesti ke proksi")
	}
	for _, bad := range []string{"ssh://@h:22", "ssh://a:b@h:22?mode=udp", "ssh://a:b@h:22?proxy=tiada"} {
		if _, err := parseLink(bad); err == nil {
			t.Errorf("%s diterima", bad)
		}
	}
	parts := expandPayload("A[split]B [host]:[port][crlf]", "h", 8)
	if len(parts) != 2 || parts[1] != "B h:8\r\n" {
		t.Fatalf("%q", parts)
	}
	ps := []*Parsed{p}
	c := buildConfig(ps, nil)
	if c["outbounds"].([]any)[0].(map[string]any)["protocol"] != "socks" {
		t.Fatal("nod SSH mesti outbound socks")
	}
}

// Pelayan SSH ujian di belakang hadapan WebSocket (balas 101) atau TLS.
func sshTestServer(t *testing.T, mode string) (net.Listener, int) {
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(hk)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "ali" && string(p) == "rahsia" {
			return nil, nil
		}
		return nil, io.EOF
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "tls" || mode == "tlsws" {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{selfCert(t)}})
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				var nc net.Conn = c
				if mode == "ws" || mode == "tlsws" {
					br := bufio.NewReader(c)
					for {
						l, err := br.ReadString('\n')
						if err != nil {
							return
						}
						if strings.TrimSpace(l) == "" {
							break
						}
					}
					io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\n\r\n")
					nc = &bufConn{Conn: c, r: br}
				}
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					if nch.ChannelType() != "direct-tcpip" {
						nch.Reject(ssh.UnknownChannelType, "")
						continue
					}
					var d struct {
						Host string
						Port uint32
						OH   string
						OP   uint32
					}
					ssh.Unmarshal(nch.ExtraData(), &d)
					tc, err := net.Dial("tcp", net.JoinHostPort(d.Host, strconv.Itoa(int(d.Port))))
					if err != nil {
						nch.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, rq, _ := nch.Accept()
					go ssh.DiscardRequests(rq)
					go func() { io.Copy(ch, tc); ch.Close() }()
					go func() { io.Copy(tc, ch); tc.Close() }()
				}
			}(c)
		}
	}()
	return ln, ln.Addr().(*net.TCPAddr).Port
}

func selfCert(t *testing.T) tls.Certificate {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"bug.contoh.my"}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
}

func socksGet(t *testing.T, port int, target string) string {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	c.Write([]byte{5, 1, 0})
	io.ReadFull(c, make([]byte, 2))
	h, ps, _ := net.SplitHostPort(target)
	pn, _ := strconv.Atoi(ps)
	req := []byte{5, 1, 0, 3, byte(len(h))}
	req = append(req, h...)
	req = binary.BigEndian.AppendUint16(req, uint16(pn))
	c.Write(req)
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0 {
		t.Fatalf("SOCKS gagal: %v %v", rep, err)
	}
	io.WriteString(c, "GET / HTTP/1.0\r\nHost: x\r\n\r\n")
	b, _ := io.ReadAll(c)
	return string(b)
}

func TestSSHTunnel(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "salam dari pelayan") }))
	defer web.Close()
	for i, mode := range []string{"ws", "tls", "tlsws"} {
		ln, port := sshTestServer(t, mode)
		link := "ssh://ali:rahsia@127.0.0.1:" + strconv.Itoa(port) + "?mode=" + mode + "&sni=bug.contoh.my#t"
		p, err := parseLink(link)
		if err != nil {
			t.Fatal(err)
		}
		lp := 20900 + i
		tun, err := startTun(p, tunKey(p), lp)
		if err != nil {
			t.Fatal(err)
		}
		if got := socksGet(t, lp, web.Listener.Addr().String()); !strings.Contains(got, "salam dari pelayan") {
			t.Fatalf("%s: jawapan salah: %q", mode, got)
		}
		tun.close()
		ln.Close()
	}
	// kata laluan salah
	ln, port := sshTestServer(t, "ws")
	defer ln.Close()
	p, _ := parseLink("ssh://ali:salah@127.0.0.1:" + strconv.Itoa(port) + "?mode=ws#t")
	if _, err := sshConnect(p); err == nil || !strings.Contains(err.Error(), "salah") {
		t.Fatalf("mesti gagal pengesahan: %v", err)
	}
}
