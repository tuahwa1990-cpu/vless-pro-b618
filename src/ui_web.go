//go:build !aplsahaja

package main

import "embed"

//go:embed ui.html
var uiFS embed.FS

const aplSahaja = false

// updSub: subfolder GitHub untuk kemas kini (edisi apl sahaja guna aplsahaja/ supaya tidak bertukar kepada edisi dengan web).
const updSub = ""

func uiBytes() []byte { b, _ := uiFS.ReadFile("ui.html"); return b }
