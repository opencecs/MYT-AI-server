//go:build linux

package main

import _ "embed"

//go:embed libffdecode.so
var ffdecodeSO []byte
