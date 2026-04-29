package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Help           key.Binding
	Visual         key.Binding
	Caret          key.Binding
	View           key.Binding
	Rename         key.Binding
	Info           key.Binding
	Archive        key.Binding
	SelectText     key.Binding
	ToggleHidden   key.Binding
	CycleTheme     key.Binding
	CycleSort      key.Binding
	SSH            key.Binding
	Mirror         key.Binding
	Up             key.Binding
	Down           key.Binding
	SelectUp       key.Binding
	SelectDown     key.Binding
	PageUp         key.Binding
	PageDown       key.Binding
	Open           key.Binding
	Back           key.Binding
	Switch         key.Binding
	Filter         key.Binding
	Refresh        key.Binding
	DirSize        key.Binding
	Copy           key.Binding
	Move           key.Binding
	Mkdir          key.Binding
	Delete         key.Binding
	Unpack         key.Binding
	Confirm        key.Binding
	Background     key.Binding
	ProgressCancel key.Binding
	Cancel         key.Binding
	Quit           key.Binding
	HistoryBack    key.Binding
	HistoryForward key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Help:           key.NewBinding(key.WithKeys("f1", "?"), key.WithHelp("F1/?", "help")),
		Rename:         key.NewBinding(key.WithKeys("f2", "r"), key.WithHelp("F2/r", "rename")),
		View:           key.NewBinding(key.WithKeys("f3", "v"), key.WithHelp("F3/v", "view")),
		Visual:         key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "visual")),
		Caret:          key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "caret")),
		Archive:        key.NewBinding(key.WithKeys("f4", "a"), key.WithHelp("F4/a", "archive")),
		Info:           key.NewBinding(key.WithKeys("f9", "o"), key.WithHelp("F9/o", "info")),
		SelectText:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("C-t", "text select")),
		ToggleHidden:   key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
		CycleTheme:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
		CycleSort:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "sort")),
		SSH:            key.NewBinding(key.WithKeys("f12", "s"), key.WithHelp("F12/s", "ssh")),
		Mirror:         key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "mirror pane")),
		Up:             key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:           key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		SelectUp:       key.NewBinding(key.WithKeys("shift+up", "K"), key.WithHelp("S-↑/K", "select up")),
		SelectDown:     key.NewBinding(key.WithKeys("shift+down", "J"), key.WithHelp("S-↓/J", "select down")),
		PageUp:         key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		PageDown:       key.NewBinding(),
		Filter:         key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Open:           key.NewBinding(key.WithKeys("enter", "right"), key.WithHelp("Enter", "open")),
		Back:           key.NewBinding(key.WithKeys("backspace", "left"), key.WithHelp("←", "parent")),
		Switch:         key.NewBinding(key.WithKeys("tab", "h", "l"), key.WithHelp("Tab/h/l", "switch pane")),
		Refresh:        key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("C-r", "refresh")),
		DirSize:        key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "dir size")),
		Copy:           key.NewBinding(key.WithKeys("f5", "c"), key.WithHelp("F5/c", "copy")),
		Move:           key.NewBinding(key.WithKeys("f6", "m"), key.WithHelp("F6/m", "move")),
		Mkdir:          key.NewBinding(key.WithKeys("f7", "n"), key.WithHelp("F7/n", "mkdir")),
		Delete:         key.NewBinding(key.WithKeys("f8", "delete", "x"), key.WithHelp("F8/x", "delete")),
		Unpack:         key.NewBinding(key.WithKeys("f11", "e"), key.WithHelp("F11/e", "unpack")),
		Confirm:        key.NewBinding(key.WithKeys("enter", "y"), key.WithHelp("Enter/y", "confirm")),
		Background:     key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "background")),
		ProgressCancel: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel transfer")),
		HistoryBack:    key.NewBinding(key.WithKeys("alt+left"), key.WithHelp("A-←", "back")),
		HistoryForward: key.NewBinding(key.WithKeys("alt+right"), key.WithHelp("A-→", "forward")),
		Cancel:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
		Quit:           key.NewBinding(key.WithKeys("f10", "q", "ctrl+c"), key.WithHelp("F10/q", "quit")),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Rename, k.View, k.Archive, k.Copy, k.Move, k.Mkdir, k.Delete, k.Info, k.Quit, k.Unpack, k.SSH}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Help, k.Up, k.Down, k.SelectUp, k.SelectDown, k.Open, k.Back},
		{k.Rename, k.View, k.Caret, k.Archive, k.Copy, k.Move, k.Delete},
		{k.Unpack, k.SelectText, k.DirSize, k.Refresh, k.ToggleHidden, k.CycleSort, k.CycleTheme, k.Quit},
	}
}
