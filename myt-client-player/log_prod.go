//go:build production

package main

func logInit(id, dir string) {}

func logf(format string, args ...any) {}

func logln(args ...any) {}
