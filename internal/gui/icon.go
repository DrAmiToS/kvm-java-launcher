package gui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed app_icon.png
var appIconBytes []byte

var appIcon = fyne.NewStaticResource("app_icon.png", appIconBytes)
