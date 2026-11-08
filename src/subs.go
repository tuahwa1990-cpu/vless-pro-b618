package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Sub struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Updated int64  `json:"updated"`
	Count   int    `json:"count"`
}

func subsFile() string { return home + "/etc/subs.json" }

func loadSubs() []Sub {
	var s []Sub
	if b, err := os.ReadFile(subsFile()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveSubs(s []Sub) { b, _ := json.Marshal(s); _ = os.WriteFile(subsFile(), b, 0600) }

func decodeSub(body string) []string {
	body = strings.TrimSpace(body)
	if !strings.Contains(body, "://") {
		cl := strings.Join(strings.Fields(body), "")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if d, err := enc.DecodeString(cl); err == nil {
				body = string(d)
				break
			}
		}
	}
	var out []string
	for _, l := range strings.Fields(body) {
		if strings.HasPrefix(l, "vless://") {
			out = append(out, l)
		}
	}
	return out
}

func refreshSub(s *Sub) error {
	cl := &http.Client{Timeout: 25 * time.Second}
	r, err := cl.Get(s.URL)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return errors.New("HTTP " + r.Status)
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	links := decodeSub(string(b))
	var fresh []Node
	for _, l := range links {
		if p, err := parseLink(l); err == nil {
			fresh = append(fresh, Node{Name: p.Name, Link: l, Src: s.ID})
		}
	}
	if len(fresh) == 0 {
		return errors.New("tiada nod vless sah dalam langganan")
	}
	ns := loadNodes()
	cur := ""
	if i := selIdx(); i >= 0 && i < len(ns) {
		cur = ns[i].Link
	}
	var keep []Node
	for _, n := range ns {
		if n.Src != s.ID {
			keep = append(keep, n)
		}
	}
	keep = append(keep, fresh...)
	_ = saveNodes(keep)
	for i, n := range keep {
		if n.Link == cur {
			_ = setSel(i)
		}
	}
	s.Updated, s.Count = time.Now().Unix(), len(fresh)
	return nil
}
