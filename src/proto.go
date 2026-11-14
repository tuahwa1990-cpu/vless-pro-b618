package main

// Jenis nod tambahan selain VLESS: WireGuard (outbound wireguard Xray) dan SSH
// (terowong SSH dalam vpro, Xray menyambung melaluinya sebagai SOCKS tempatan).

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type wgCfg struct {
	Key, Pub, PSK string
	Addr          []string
	MTU, Keep     int
	Reserved      []int
}

type sshCfg struct {
	User, Pass string
	Mode       string // ws, tls atau tlsws
	Payload    string
	Proxy      string // host:port pilihan (sambung ke sini dahulu)
}

func wgKeyOK(k string) bool {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k))
	return err == nil && len(b) == 32
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// splitHostPort menerima host:port, [v6]:port.
func splitHP(hp string) (string, int, bool) {
	h, ps, err := net.SplitHostPort(strings.TrimSpace(hp))
	if err != nil || h == "" {
		return "", 0, false
	}
	p, err := strconv.Atoi(ps)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, false
	}
	return h, p, true
}

// parseWG: wireguard://KUNCI_PERIBADI@hos:port?publickey=..&address=..&presharedkey=..&mtu=..&reserved=1,2,3&keepalive=25#nama
// Kunci base64 mungkin mengandungi "/" atau "+", jadi pautan dipecah sendiri, bukan dengan url.Parse.
func parseWG(link string) (*Parsed, error) {
	rest := link[strings.Index(link, "://")+3:]
	name := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		name, _ = url.PathUnescape(rest[i+1:])
		rest = rest[:i]
	}
	qs := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		qs, rest = rest[i+1:], rest[:i]
	}
	rest = strings.TrimSuffix(rest, "/")
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return nil, errors.New("kunci peribadi WireGuard tiada")
	}
	key, err := url.PathUnescape(rest[:at])
	if err != nil {
		key = rest[:at]
	}
	host, port, ok := splitHP(rest[at+1:])
	if !ok {
		return nil, errors.New("endpoint WireGuard tidak sah (perlu hos:port)")
	}
	q, _ := url.ParseQuery(qs)
	get := func(ks ...string) string {
		for _, k := range ks {
			if v := q.Get(k); v != "" {
				return v
			}
		}
		return ""
	}
	w := &wgCfg{Key: strings.TrimSpace(key), Pub: get("publickey", "publicKey", "peer_public_key"), PSK: get("presharedkey", "preSharedKey", "psk")}
	w.Addr = splitList(get("address", "ip", "local_address"))
	w.MTU, _ = strconv.Atoi(get("mtu"))
	w.Keep, _ = strconv.Atoi(get("keepalive", "persistentkeepalive"))
	for _, r := range splitList(get("reserved")) {
		n, err := strconv.Atoi(r)
		if err != nil || n < 0 || n > 255 {
			return nil, errors.New("reserved WireGuard tidak sah")
		}
		w.Reserved = append(w.Reserved, n)
	}
	if !wgKeyOK(w.Key) {
		return nil, errors.New("kunci peribadi WireGuard tidak sah")
	}
	if !wgKeyOK(w.Pub) {
		return nil, errors.New("publickey WireGuard tidak sah")
	}
	if w.PSK != "" && !wgKeyOK(w.PSK) {
		return nil, errors.New("presharedkey WireGuard tidak sah")
	}
	if len(w.Addr) == 0 {
		return nil, errors.New("alamat (address) WireGuard tiada")
	}
	for i, a := range w.Addr {
		if !strings.Contains(a, "/") {
			if strings.Contains(a, ":") {
				a += "/128"
			} else {
				a += "/32"
			}
			w.Addr[i] = a
		}
		if _, _, err := net.ParseCIDR(w.Addr[i]); err != nil {
			return nil, errors.New("alamat WireGuard tidak sah: " + a)
		}
	}
	if w.MTU <= 0 || w.MTU > 1500 {
		w.MTU = 1420
	}
	if name == "" {
		name = "WireGuard " + host
	}
	return &Parsed{Proto: "wg", Name: name, Host: host, Port: port, Net: "wg", Sec: "udp", WG: w}, nil
}

// confToWG menukar teks konfigurasi WireGuard ([Interface] + [Peer]) kepada pautan wireguard://.
func confToWG(conf string) (string, error) {
	sec := ""
	kv := map[string]string{}
	for _, ln := range strings.Split(conf, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, ";") {
			continue
		}
		if strings.HasPrefix(ln, "[") {
			sec = strings.ToLower(strings.Trim(ln, "[] "))
			continue
		}
		i := strings.Index(ln, "=")
		if i < 0 {
			continue
		}
		k := sec + "." + strings.ToLower(strings.TrimSpace(ln[:i]))
		v := strings.TrimSpace(ln[i+1:])
		if old, ok := kv[k]; ok {
			if strings.HasSuffix(k, "address") || strings.HasSuffix(k, "allowedips") {
				kv[k] = old + "," + v
			}
			continue
		}
		kv[k] = v
	}
	ep := kv["peer.endpoint"]
	host, _, ok := splitHP(ep)
	if !ok {
		return "", errors.New("Endpoint dalam [Peer] tidak sah")
	}
	q := url.Values{}
	q.Set("publickey", kv["peer.publickey"])
	q.Set("address", strings.ReplaceAll(kv["interface.address"], " ", ""))
	if v := kv["peer.presharedkey"]; v != "" {
		q.Set("presharedkey", v)
	}
	if v := kv["interface.mtu"]; v != "" {
		q.Set("mtu", v)
	}
	if v := kv["peer.persistentkeepalive"]; v != "" {
		q.Set("keepalive", v)
	}
	link := "wireguard://" + url.PathEscape(kv["interface.privatekey"]) + "@" + ep + "?" + q.Encode() + "#" + url.PathEscape("WireGuard "+host)
	if _, err := parseWG(link); err != nil {
		return "", err
	}
	return link, nil
}

// parseSSH: ssh://pengguna:kata@hos:port?mode=ws|tls|tlsws&sni=..&payload=..&proxy=hos:port#nama
func parseSSH(link string) (*Parsed, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return nil, errors.New("pautan SSH tidak sah")
	}
	p := &Parsed{Proto: "ssh", Name: u.Fragment, Host: u.Hostname(), Net: "ssh"}
	p.Port, _ = strconv.Atoi(u.Port())
	if p.Port == 0 {
		p.Port = 22
	}
	pass, _ := u.User.Password()
	q := u.Query()
	c := &sshCfg{User: u.User.Username(), Pass: pass, Mode: q.Get("mode"), Payload: q.Get("payload"), Proxy: q.Get("proxy")}
	p.SNI = q.Get("sni")
	if p.Host == "" || p.Port > 65535 || c.User == "" {
		return nil, errors.New("hos, port atau nama pengguna SSH tiada")
	}
	switch c.Mode {
	case "":
		c.Mode = "ws"
	case "ws", "tls", "tlsws":
	default:
		return nil, errors.New("mod SSH tidak disokong: " + c.Mode)
	}
	if c.Proxy != "" {
		if _, _, ok := splitHP(c.Proxy); !ok {
			return nil, errors.New("proksi SSH mesti hos:port")
		}
	}
	p.Sec = map[string]string{"ws": "ws", "tls": "ssl", "tlsws": "ssl+ws"}[c.Mode]
	p.SSH = c
	if p.Name == "" {
		p.Name = "SSH " + p.Host
	}
	return p, nil
}

// pingTarget: hos dan port untuk ujian TCP. WireGuard (UDP) tidak boleh diuji dengan TCP.
func pingTarget(p *Parsed) (string, int, bool) {
	switch p.Proto {
	case "wg":
		return "", 0, false
	case "ssh":
		if p.SSH.Proxy != "" {
			h, pt, _ := splitHP(p.SSH.Proxy)
			return h, pt, true
		}
	}
	return p.Host, p.Port, true
}

func wgOut(p *Parsed, tag string) map[string]any {
	w := p.WG
	peer := map[string]any{"publicKey": w.Pub, "endpoint": net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		"allowedIPs": []any{"0.0.0.0/0", "::/0"}}
	if w.PSK != "" {
		peer["preSharedKey"] = w.PSK
	}
	if w.Keep > 0 {
		peer["keepAlive"] = w.Keep
	}
	addr := []any{}
	v6 := false
	for _, a := range w.Addr {
		addr = append(addr, a)
		if strings.Contains(a, ":") {
			v6 = true
		}
	}
	st := map[string]any{"secretKey": w.Key, "address": addr, "peers": []any{peer}, "mtu": w.MTU}
	if !v6 {
		st["domainStrategy"] = "ForceIPv4"
	}
	if len(w.Reserved) > 0 {
		r := []any{}
		for _, x := range w.Reserved {
			r = append(r, x)
		}
		st["reserved"] = r
	}
	return map[string]any{"tag": tag, "protocol": "wireguard", "settings": st,
		"streamSettings": map[string]any{"sockopt": map[string]any{"mark": 254}}}
}

// sshPort: port SOCKS tempatan bagi nod SSH ke-i dalam pilihan aktif.
func sshPort(i int) int { return 10810 + i }

func sshOut(i int, tag string) map[string]any {
	return map[string]any{"tag": tag, "protocol": "socks",
		"settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": sshPort(i)}}}}
}
