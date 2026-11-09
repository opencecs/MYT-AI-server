//go:build windows

package main

import _ "embed"

//go:embed ffdecode.dll
var ffdecodeDLL []byte

//go:embed d3d11render.dll
var d3d11renderDLL []byte
