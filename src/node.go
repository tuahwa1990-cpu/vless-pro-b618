package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Node struct {
	Name string `json:"name"`
	Link string `json:"link"`
	Src  string `json:"src,omitempty"`
}

type Parsed struct {
	Name, Host, UUID, Net, Sec, SNI, HHost, Path, FP, Flow, PBK, SID, SPX, Service, Mode, ALPN string
	Port                                                                                       int
	Insecure                                                                                   bool
	// Proto: "" (vless), "vmess", "wg" (WireGuard) atau "ssh"
	Proto string
	WG    *wgCfg
	SSH   *sshCfg
	// Khas vmess: cara penyulitan (scy, cth "auto") dan alterId (lazim 0 pada pelayan moden).
	Scy     string
	AlterId int
}

func nodesFile() string { return home + "/etc/nodes.json" }

func loadNodes() []Node {
	var n []Node
	b, err := os.ReadFile(nodesFile())
	if err == nil {
		_ = json.Unmarshal(b, &n)
	}
	return n
}

func saveNodes(n []Node) error {
	b, _ := json.Marshal(n)
	tmp := nodesFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, nodesFile())
}

func selIdx() int {
	b, err := os.ReadFile(home + "/etc/selected")
	if err != nil {
		return 0
	}
	i, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return i
}

func setSel(i int) error {
	return os.WriteFile(home+"/etc/selected", []byte(strconv.Itoa(i)), 0600)
}

func parseLink(link string) (*Parsed, error) {
	link = strings.TrimSpace(link)
	switch {
	case strings.HasPrefix(link, "wireguard://"), strings.HasPrefix(link, "wg://"):
		return parseWG(link)
	case strings.HasPrefix(link, "ssh://"):
		return parseSSH(link)
	case strings.HasPrefix(link, "vmess://"):
		return parseVmess(link)
	}
	if !strings.HasPrefix(link, "vless://") {
		return nil, errors.New("hanya pautan vless://, vmess://, wireguard:// atau ssh:// disokong")
	}
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return nil, errors.New("pautan tidak sah")
	}
	q := u.Query()
	p := &Parsed{Name: u.Fragment, Host: u.Hostname(), UUID: u.User.Username()}
	p.Port, _ = strconv.Atoi(u.Port())
	if p.Host == "" || p.Port <= 0 || p.Port > 65535 || len(p.UUID) < 8 {
		return nil, errors.New("pelayan, port atau UUID tiada")
	}
	if enc := q.Get("encryption"); enc != "" && enc != "none" {
		return nil, errors.New("encryption bukan none tidak disokong")
	}
	p.Net = q.Get("type")
	if p.Net == "" {
		p.Net = "tcp"
	}
	switch p.Net {
	case "tcp", "ws", "grpc", "xhttp", "httpupgrade":
	default:
		return nil, errors.New("jenis rangkaian tidak disokong: " + p.Net)
	}
	p.Sec = q.Get("security")
	switch p.Sec {
	case "", "none", "tls", "reality":
	default:
		return nil, errors.New("jenis keselamatan tidak disokong: " + p.Sec)
	}
	p.SNI, p.HHost, p.Path, p.FP = q.Get("sni"), q.Get("host"), q.Get("path"), q.Get("fp")
	p.Flow, p.PBK, p.SID, p.SPX = q.Get("flow"), q.Get("pbk"), q.Get("sid"), q.Get("spx")
	p.Service, p.Mode, p.ALPN = q.Get("serviceName"), q.Get("mode"), q.Get("alpn")
	v := q.Get("allowInsecure")
	p.Insecure = v == "1" || v == "true"
	if p.Sec == "reality" && p.PBK == "" {
		return nil, errors.New("reality tanpa pbk")
	}
	if p.Name == "" {
		p.Name = p.Host
	}
	return p, nil
}

// parseVmess huraikan pautan vmess:// (JSON base64, format vmessping/v2rayN piawai).
func parseVmess(link string) (*Parsed, error) {
	b64 := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(strings.TrimPrefix(link, "vmess://")))
	for len(b64)%4 != 0 {
		b64 += "="
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("pautan vmess tidak sah (base64)")
	}
	var j map[string]any
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, errors.New("pautan vmess tidak sah (json)")
	}
	str := func(k string) string {
		v, _ := j[k].(string)
		return v
	}
	num := func(k string) string {
		switch v := j[k].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		default:
			return ""
		}
	}
	host := str("add")
	port, _ := strconv.Atoi(num("port"))
	uuid := str("id")
	if host == "" || port <= 0 || port > 65535 || len(uuid) < 8 {
		return nil, errors.New("pelayan, port atau id tiada")
	}
	p := &Parsed{Proto: "vmess", Name: str("ps"), Host: host, Port: port, UUID: uuid}
	p.Net = orStr(str("net"), "tcp")
	switch p.Net {
	case "tcp", "ws", "grpc", "xhttp", "httpupgrade":
	default:
		return nil, errors.New("jenis rangkaian tidak disokong: " + p.Net)
	}
	if str("tls") == "tls" {
		p.Sec = "tls"
	}
	p.SNI, p.HHost, p.Path, p.FP, p.ALPN = str("sni"), str("host"), str("path"), str("fp"), str("alpn")
	p.Service, p.Mode = str("path"), str("mode")
	p.Scy = orStr(str("scy"), "auto")
	aid, _ := strconv.Atoi(num("aid"))
	p.AlterId = aid
	if p.Name == "" {
		p.Name = p.Host
	}
	return p, nil
}

func mask(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:2] + "***" + s[len(s)-4:]
}

func orStr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func nid(link string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(link)))
	return hex.EncodeToString(h[:4])
}
