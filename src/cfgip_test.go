package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIPNodeAlias(t *testing.T) {
	p := &Parsed{Host: "104.17.147.22", Port: 80, Net: "ws", UUID: "u"}
	c := buildConfig([]*Parsed{p}, nil)
	b, _ := json.Marshal(c)
	s := string(b)
	if strings.Contains(s, `"address":"104.17.147.22"`) || !strings.Contains(s, `"hosts":{"n104-17-147-22.vp":"104.17.147.22"}`) {
		t.Fatal(s)
	}
}
