package main

import _ "embed"

//go:embed ui/side.css
var sideCSS string

//go:embed ui/side_body.html
var sideBody string

//go:embed ui/side_langs.js
var sideLangsJS string

//go:embed ui/side.js
var sideJS string

//go:embed ui/side_api.js
var sideAPIJS string

//go:embed ui/side_keymap.js
var sideKeymapJS string

//go:embed ui/side_keymap_presets.js
var sideKeymapPresetsJS string

func composeSidePanelHTML() string {
	return "<!DOCTYPE html><html lang=\"zh\"><head><meta charset=\"UTF-8\">" +
		"<style>" + sideCSS + "</style></head><body>" +
		sideBody +
		"<script>" + sideLangsJS + "</script>" +
		"<script>" + sideJS + "</script>" +
		"<script>" + sideAPIJS + "</script>" +
		"<script>" + sideKeymapJS + "</script>" +
		"<script>" + sideKeymapPresetsJS + "</script>" +
		"</body></html>"
}
