package main

import "testing"

func TestBandlockArgRe(t *testing.T) {
	ok := []string{
		"5,1/3/41",
		"5,1/3/41;4,3/7",
		"5,1;4,3;3,1",
		"4,40",
	}
	for _, a := range ok {
		if !bandlockArgRe.MatchString(a) {
			t.Errorf("sepatutnya sah: %q", a)
		}
	}
	bad := []string{
		"",
		"5,",
		"5",
		"5,1/",
		"5,1;",
		"5,1/3; rm -rf",
		"5,1/3/41;",
		"x,1/3",
		"5,1/3/41;4",
	}
	for _, a := range bad {
		if bandlockArgRe.MatchString(a) {
			t.Errorf("sepatutnya tidak sah: %q", a)
		}
	}
}

func TestBandlockSupportedFalseWithoutBinary(t *testing.T) {
	// Dalam persekitaran ujian (bukan AW1000 sebenar), /bin/modem_status tiada.
	if bandlockSupported() {
		t.Skip("ada /bin/modem_status pada mesin ujian ini - langkau")
	}
}
