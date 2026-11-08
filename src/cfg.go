package main

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

type Group struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Strat   string   `json:"strat,omitempty"`
}

// kaedah kumpulan aktif: leastPing (lalai), random, roundRobin
func curStrat() string {
	if gi := selGroup(); gi >= 0 {
		gs := loadGroups()
		if gi < len(gs) {
			switch gs[gi].Strat {
			case "random", "roundRobin":
				return gs[gi].Strat
			}
		}
	}
	return "leastPing"
}

func groupsFile() string { return home + "/etc/groups.json" }

func loadGroups() []Group {
	var g []Group
	if b, err := os.ReadFile(groupsFile()); err == nil {
		_ = json.Unmarshal(b, &g)
	}
	return g
}

func saveGroups(g []Group) error {
	b, _ := json.Marshal(g)
	return os.WriteFile(groupsFile(), b, 0600)
}

func selGroup() int {
	b, err := os.ReadFile(home + "/etc/selgroup")
	if err != nil {
		return -1
	}
	i, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return -1
	}
	return i
}

func setSelGroup(i int) {
	if i < 0 {
		_ = os.Remove(home + "/etc/selgroup")
		return
	}
	_ = os.WriteFile(home+"/etc/selgroup", []byte(strconv.Itoa(i)), 0600)
}

func directList() []string {
	b, err := os.ReadFile(home + "/etc/direct.txt")
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Fields(strings.ReplaceAll(string(b), ",", " ")) {
		out = append(out, l)
	}
	return out
}

func proxyOut(p *Parsed, tag string) map[string]any {
	user := map[string]any{"id": p.UUID, "encryption": "none"}
	if p.Flow != "" {
		user["flow"] = p.Flow
	}
	ss := map[string]any{"network": p.Net, "sockopt": map[string]any{"mark": 254}}
	sec := p.Sec
	if sec == "" {
		sec = "none"
	}
	ss["security"] = sec
	fp := p.FP
	if fp == "" {
		fp = "chrome"
	}
	sni := p.SNI
	switch sec {
	case "tls":
		t := map[string]any{"fingerprint": fp}
		if sni == "" {
			sni = p.HHost
		}
		if sni != "" {
			t["serverName"] = sni
		}
		if p.ALPN != "" {
			t["alpn"] = strings.Split(p.ALPN, ",")
		}
		if p.Insecure {
			t["allowInsecure"] = true
		}
		ss["tlsSettings"] = t
	case "reality":
		r := map[string]any{"fingerprint": fp, "publicKey": p.PBK, "shortId": p.SID}
		if sni != "" {
			r["serverName"] = sni
		}
		if p.SPX != "" {
			r["spiderX"] = p.SPX
		}
		ss["realitySettings"] = r
	}
	switch p.Net {
	case "ws":
		w := map[string]any{"path": orStr(p.Path, "/")}
		if p.HHost != "" {
			w["headers"] = map[string]any{"Host": p.HHost}
		}
		ss["wsSettings"] = w
	case "grpc":
		ss["grpcSettings"] = map[string]any{"serviceName": p.Service, "multiMode": p.Mode == "multi"}
	case "xhttp":
		x := map[string]any{"path": orStr(p.Path, "/")}
		if p.HHost != "" {
			x["host"] = p.HHost
		}
		if p.Mode != "" {
			x["mode"] = p.Mode
		}
		ss["xhttpSettings"] = x
	case "httpupgrade":
		h := map[string]any{"path": orStr(p.Path, "/")}
		if p.HHost != "" {
			h["host"] = p.HHost
		}
		ss["httpupgradeSettings"] = h
	}
	return map[string]any{
		"tag": tag, "protocol": "vless",
		"settings": map[string]any{"vnext": []any{map[string]any{
			"address": p.Host, "port": p.Port, "users": []any{user}}}},
		"streamSettings": ss,
	}
}

func buildConfig(ps []*Parsed, direct []string) map[string]any {
	outs := []any{}
	multi := len(ps) > 1
	for i, p := range ps {
		tag := "proxy"
		if multi {
			tag = "px-" + strconv.Itoa(i)
		}
		outs = append(outs, proxyOut(p, tag))
	}
	outs = append(outs,
		map[string]any{"tag": "direct", "protocol": "freedom",
			"streamSettings": map[string]any{"sockopt": map[string]any{"mark": 254}}},
		map[string]any{"tag": "block", "protocol": "blackhole"})
	rule := func(m map[string]any) map[string]any {
		m["type"] = "field"
		if multi {
			m["balancerTag"] = "bal"
		} else {
			m["outboundTag"] = "proxy"
		}
		return m
	}
	rules := []any{rule(map[string]any{"inboundTag": []any{"dns-in", "probe"}})}
	var doms, ips []any
	for _, d := range direct {
		if strings.ContainsAny(d, "abcdefghijklmnopqrstuvwxyz") && !strings.Contains(d, ":") {
			doms = append(doms, "domain:"+strings.TrimPrefix(d, "*."))
		} else {
			ips = append(ips, d)
		}
	}
	if len(doms) > 0 {
		rules = append(rules, map[string]any{"type": "field", "domain": doms, "outboundTag": "direct"})
	}
	if len(ips) > 0 {
		rules = append(rules, map[string]any{"type": "field", "ip": ips, "outboundTag": "direct"})
	}
	rules = append(rules, rule(map[string]any{"network": "tcp,udp"}))
	routing := map[string]any{"domainStrategy": "AsIs", "rules": rules}
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{"tag": "redir", "port": 12346, "listen": "0.0.0.0", "protocol": "dokodemo-door",
				"settings":       map[string]any{"network": "tcp", "followRedirect": true},
				"sniffing":       map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}},
				"streamSettings": map[string]any{"sockopt": map[string]any{"tproxy": "redirect"}}},
			map[string]any{"tag": "dns-in", "port": 10854, "listen": "0.0.0.0", "protocol": "dokodemo-door",
				"settings": map[string]any{"address": "8.8.8.8", "port": 53, "network": "tcp,udp"}},
			map[string]any{"tag": "probe", "port": 10855, "listen": "127.0.0.1", "protocol": "http",
				"settings": map[string]any{}},
		},
		"outbounds": outs,
		"routing":   routing,
	}
	if multi {
		st := curStrat()
		routing["balancers"] = []any{map[string]any{"tag": "bal", "selector": []any{"px-"},
			"strategy": map[string]any{"type": st}}}
		if st == "leastPing" {
			cfg["observatory"] = map[string]any{"subjectSelector": []any{"px-"},
				"probeUrl": "http://www.gstatic.com/generate_204", "probeInterval": "60s"}
		}
	}
	return cfg
}

// active selection: group if selgroup valid, else single node
func activeParsed() ([]*Parsed, string, error) {
	ns := loadNodes()
	if gi := selGroup(); gi >= 0 {
		gs := loadGroups()
		if gi < len(gs) {
			byID := map[string]Node{}
			for _, n := range ns {
				byID[nid(n.Link)] = n
			}
			var ps []*Parsed
			for _, m := range gs[gi].Members {
				if n, ok := byID[m]; ok {
					if p, err := parseLink(n.Link); err == nil {
						ps = append(ps, p)
					}
				}
			}
			if len(ps) > 0 {
				return ps, "Kumpulan: " + gs[gi].Name, nil
			}
		}
	}
	i := selIdx()
	if i < 0 || i >= len(ns) {
		return nil, "", errors.New("tiada nod dipilih")
	}
	p, err := parseLink(ns[i].Link)
	if err != nil {
		return nil, "", err
	}
	return []*Parsed{p}, p.Name, nil
}

func genConfig() error {
	ps, _, err := activeParsed()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(buildConfig(ps, directList()), "", " ")
	_ = os.MkdirAll(home+"/run", 0700)
	return os.WriteFile(home+"/run/xray.json", b, 0600)
}
