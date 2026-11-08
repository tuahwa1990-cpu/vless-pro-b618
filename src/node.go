package main

import (
	"crypto/sha1"
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
	if !strings.HasPrefix(link, "vless://") {
		return nil, errors.New("hanya pautan vless:// disokong")
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
