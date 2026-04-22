package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	View         key.Binding
	Edit         key.Binding
	Info         key.Binding
	ToggleHidden key.Binding
	CycleTheme   key.Binding
	CycleSort    key.Binding
	Up           key.Binding
	Down         key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	Open         key.Binding
	Back         key.Binding
	Switch       key.Binding
	Refresh      key.Binding
	DirSize      key.Binding
	Copy         key.Binding
	Move         key.Binding
	Mkdir        key.Binding
	Delete       key.Binding
	Confirm      key.Binding
	Cancel       key.Binding
	Quit         key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		View:         key.NewBinding(key.WithKeys("f3", "v"), key.WithHelp("F3/v", "view")),
		Edit:         key.NewBinding(key.WithKeys("f4", "e"), key.WithHelp("F4/e", "edit")),
		Info:         key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "info")),
		ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
		CycleTheme:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
		CycleSort:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("PgUp/b", "page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "f"), key.WithHelp("PgDn/f", "page down")),
		Open:         key.NewBinding(key.WithKeys("enter", "right", "l"), key.WithHelp("Enter", "open")),
		Back:         key.NewBinding(key.WithKeys("backspace", "left", "h"), key.WithHelp("←", "parent")),
		Switch:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch pane")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		DirSize:      key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "dir size")),
		Copy:         key.NewBinding(key.WithKeys("f5", "c"), key.WithHelp("F5/c", "copy")),
		Move:         key.NewBinding(key.WithKeys("f6", "m"), key.WithHelp("F6/m", "move")),
		Mkdir:        key.NewBinding(key.WithKeys("f7", "n"), key.WithHelp("F7/n", "mkdir")),
		Delete:       key.NewBinding(key.WithKeys("f8", "delete", "x"), key.WithHelp("F8/x", "delete")),
		Confirm:      key.NewBinding(key.WithKeys("enter", "y"), key.WithHelp("Enter/y", "confirm")),
		Cancel:       key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("Esc/n", "cancel")),
		Quit:         key.NewBinding(key.WithKeys("f10", "q", "ctrl+c"), key.WithHelp("F10/q", "quit")),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Info, k.Copy, k.Move, k.Mkdir, k.Delete, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Open, k.Back, k.Switch, k.Info},
		{k.View, k.Edit, k.Copy, k.Move, k.Mkdir, k.Delete},
		{k.DirSize, k.Refresh, k.ToggleHidden, k.CycleSort, k.CycleTheme, k.Quit},
	}
}
