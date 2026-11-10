package main

import "testing"

func TestParseNodeDown(t *testing.T) {
	cases := []struct {
		in string
		v  uint64
		ok bool
	}{
		{`{"stat":[{"name":"outbound>>>proxy>>>traffic>>>downlink"},{"name":"outbound>>>direct>>>traffic>>>downlink","value":"999"}]}`, 0, true},
		{`{"stat":[{"name":"outbound>>>proxy>>>traffic>>>downlink","value":"1234"},{"name":"outbound>>>direct>>>traffic>>>downlink","value":"999"}]}`, 1234, true},
		{`{"stat":[{"name":"outbound>>>px-a>>>traffic>>>downlink","value":5},{"name":"outbound>>>px-b>>>traffic>>>downlink","value":"7"}]}`, 12, true},
		{`{}`, 0, true},
		{`gagal sambung`, 0, false},
	}
	for i, c := range cases {
		v, ok := parseNodeDown(c.in)
		if v != c.v || ok != c.ok {
			t.Errorf("kes %d: dapat %d,%v mahu %d,%v", i, v, ok, c.v, c.ok)
		}
	}
}
