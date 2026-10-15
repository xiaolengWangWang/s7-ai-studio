package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

//go:embed help.md
var helpMarkdown string

func helpPanel() fyne.CanvasObject {
	return container.NewScroll(widget.NewRichTextFromMarkdown(helpMarkdown))
}
