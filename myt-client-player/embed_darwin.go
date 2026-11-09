//go:build darwin

package main

import _ "embed"

//go:embed ffdecode.dylib
var ffdecodeDLL []byte

//go:embed metalrender.dylib
var metalrenderDLL []byte
