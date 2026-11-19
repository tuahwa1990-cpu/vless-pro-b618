//go:build !aplsahaja

package main

import "embed"

//go:embed ui.html
var uiFS embed.FS

const aplSahaja = false

func uiBytes() []byte { b, _ := uiFS.ReadFile("ui.html"); return b }
