package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"vcom/internal/config"
	vfs "vcom/internal/fs"
	"vcom/internal/theme"
)

type PaneID string

const (
	PaneLeft  PaneID = "left"
	PaneRight PaneID = "right"
)

type BrowserPane struct {
	ID      PaneID
	Path    string
	Entries []vfs.Entry
	Cursor  int
	Offset  int
}

func (p *BrowserPane) Selected() (vfs.Entry, bool) {
	if len(p.Entries) == 0 || p.Cursor < 0 || p.Cursor >= len(p.Entries) {
		return vfs.Entry{}, false
	}
	return p.Entries[p.Cursor], true
}

func (p *BrowserPane) SetEntries(entries []vfs.Entry, preserveKey string) {
	p.Entries = entries
	if len(entries) == 0 {
		p.Cursor = 0
		p.Offset = 0
		return
	}
	if preserveKey != "" {
		p.Cursor = vfs.FindSelected(entries, preserveKey)
	}
	if p.Cursor >= len(entries) {
		p.Cursor = len(entries) - 1
	}
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Offset > p.Cursor {
		p.Offset = p.Cursor
	}
}

func (p *BrowserPane) Move(delta int, pageSize int) {
	if len(p.Entries) == 0 {
		p.Cursor = 0
		return
	}
	p.Cursor += delta
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor >= len(p.Entries) {
		p.Cursor = len(p.Entries) - 1
	}

	if p.Cursor < p.Offset {
		p.Offset = p.Cursor
	}
	if pageSize > 0 && p.Cursor >= p.Offset+pageSize {
		p.Offset = p.Cursor - pageSize + 1
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
}

func (p *BrowserPane) EnsureVisible(pageSize int) {
	if pageSize <= 0 {
		return
	}
	if p.Cursor < p.Offset {
		p.Offset = p.Cursor
	}
	if p.Cursor >= p.Offset+pageSize {
		p.Offset = p.Cursor - pageSize + 1
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
}

func renderPane(
	pane BrowserPane,
	cfg config.Config,
	palette theme.Palette,
	width int,
	height int,
	active bool,
	hoverIndex int,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	borderColor := palette.Border
	headerBg := palette.PanelInactive
	if active {
		borderColor = palette.BorderActive
		headerBg = palette.Selection
	}

	innerWidth := max(width-2, 1)

	box := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(palette.PanelInactive).
		Foreground(palette.Text).
		BorderStyle(borderStyle(cfg.UI.Border)).
		BorderForeground(borderColor)

	header := lipgloss.NewStyle().
		Render(renderPaneHeader(pane, cfg, palette, innerWidth, active, headerBg))

	rowsHeight := max(height-4, 1)
	headerRow := renderColumnsHeader(cfg, innerWidth, palette)
	rows := renderPaneRows(pane, cfg, palette, innerWidth, rowsHeight, active, hoverIndex)
	content := lipgloss.JoinVertical(lipgloss.Left, header, headerRow, rows)
	return box.Render(content)
}

func renderPaneHeader(pane BrowserPane, cfg config.Config, palette theme.Palette, width int, active bool, headerBg lipgloss.Color) string {
	label := strings.ToUpper(string(pane.ID))
	labelStyle := lipgloss.NewStyle().
		Foreground(palette.Background).
		Background(palette.FooterKey).
		Bold(true).
		Padding(0, 1)
	if active {
		labelStyle = labelStyle.Background(palette.Accent)
	}

	labelView := labelStyle.Render(label)
	pathWidth := max(width-lipgloss.Width(labelView)-1, 4)
	pathStyle := lipgloss.NewStyle().
		Width(pathWidth).
		Background(headerBg).
		Foreground(palette.Text).
		Bold(active)

	return lipgloss.NewStyle().
		Width(width).
		Background(headerBg).
		Render(labelView + " " + pathStyle.Render(truncateMiddle(compactPath(pane.Path, cfg.UI.PathDisplay), pathWidth)))
}

func renderColumnsHeader(cfg config.Config, width int, palette theme.Palette) string {
	columns := buildColumns(cfg, width)
	parts := make([]string, 0, len(columns))
	for idx, column := range columns {
		style := lipgloss.NewStyle().
			Width(column.Width).
			Foreground(palette.Muted).
			Bold(true)
		if column.AlignRight {
			style = style.Align(lipgloss.Right)
		}
		parts = append(parts, style.Render(truncateRight(column.Title, column.Width)))
		if idx < len(columns)-1 {
			parts = append(parts, columnSeparator(column.Key, palette, lipgloss.Color("")))
		}
	}
	return lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Render(strings.Join(parts, ""))
}

func renderPaneRows(pane BrowserPane, cfg config.Config, palette theme.Palette, width int, height int, active bool, hoverIndex int) string {
	if len(pane.Entries) == 0 {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Padding(1, 1).
			Foreground(palette.Muted).
			Render("Empty directory")
	}

	visibleHeight := max(height, 1)
	pane.EnsureVisible(visibleHeight)
	end := min(len(pane.Entries), pane.Offset+visibleHeight)

	lines := make([]string, 0, visibleHeight)
	for idx := pane.Offset; idx < end; idx++ {
		entry := pane.Entries[idx]
		row := renderEntryRow(entry, cfg, width, idx == pane.Cursor, idx == hoverIndex, active, palette)
		lines = append(lines, row)
	}
	for len(lines) < visibleHeight {
		lines = append(lines, lipgloss.NewStyle().Width(width).Render(""))
	}

	return lipgloss.NewStyle().
		Width(width).
		Render(strings.Join(lines, "\n"))
}

func renderEntryRow(entry vfs.Entry, cfg config.Config, width int, selected bool, hovered bool, active bool, palette theme.Palette) string {
	columns := buildColumns(cfg, width)
	rowBackground := lipgloss.Color("")
	switch {
	case selected:
		rowBackground = palette.Selection
	case hovered:
		rowBackground = palette.PanelElevated
	}

	parts := make([]string, 0, len(columns))
	for idx, column := range columns {
		value := column.Value(entry, cfg.Browser.HumanReadableSize)
		style := lipgloss.NewStyle().
			Width(column.Width).
			Foreground(entryColor(entry, palette))
		if rowBackground != lipgloss.Color("") {
			style = style.Background(rowBackground)
		}

		if entry.IsHidden {
			style = style.Foreground(palette.Muted)
		}
		if column.AlignRight {
			style = style.Align(lipgloss.Right)
		}
		parts = append(parts, style.Render(truncateForColumn(value, column.Width, column.AlignRight)))
		if idx < len(columns)-1 {
			parts = append(parts, columnSeparator(column.Key, palette, rowBackground))
		}
	}

	rowStyle := lipgloss.NewStyle().Width(width)
	if rowBackground != lipgloss.Color("") {
		rowStyle = rowStyle.Background(rowBackground)
	}
	if selected && active {
		rowStyle = rowStyle.Bold(true)
	}

	return rowStyle.Render(strings.Join(parts, ""))
}

type columnSpec struct {
	Key        string
	Title      string
	Width      int
	MinWidth   int
	AlignRight bool
	Value      func(entry vfs.Entry, human bool) string
}

func buildColumns(cfg config.Config, totalWidth int) []columnSpec {
	fixed := []columnSpec{}

	if cfg.Browser.Columns.Permissions {
		fixed = append(fixed, columnSpec{
			Key:      "permissions",
			Title:    "Perms",
			Width:    10,
			MinWidth: 9,
			Value: func(entry vfs.Entry, _ bool) string {
				return vfs.Permissions(entry.Mode)
			},
		})
	}
	if cfg.Browser.Columns.Extension {
		fixed = append(fixed, columnSpec{
			Key:      "extension",
			Title:    "Ext",
			Width:    6,
			MinWidth: 4,
			Value: func(entry vfs.Entry, _ bool) string {
				if entry.IsDir {
					return ""
				}
				return entry.Extension
			},
		})
	}
	if cfg.Browser.Columns.Size {
		fixed = append(fixed, columnSpec{
			Key:        "size",
			Title:      "Size",
			Width:      10,
			MinWidth:   7,
			AlignRight: true,
			Value: func(entry vfs.Entry, human bool) string {
				if entry.IsDir {
					if !entry.DirSizeKnown {
						return ""
					}
					if human {
						return vfs.HumanSize(entry.Size)
					}
					return fmt.Sprintf("%d", entry.Size)
				}
				if human {
					return vfs.HumanSize(entry.Size)
				}
				return fmt.Sprintf("%d", entry.Size)
			},
		})
	}
	if cfg.Browser.Columns.Created {
		fixed = append(fixed, columnSpec{
			Key:      "created",
			Title:    "Created",
			Width:    11,
			MinWidth: 8,
			Value: func(entry vfs.Entry, _ bool) string {
				if !entry.CreatedKnown {
					return "n/a"
				}
				return vfs.CompactTime(entry.CreatedAt)
			},
		})
	}
	if cfg.Browser.Columns.Modified {
		fixed = append(fixed, columnSpec{
			Key:      "modified",
			Title:    "Modified",
			Width:    11,
			MinWidth: 8,
			Value: func(entry vfs.Entry, _ bool) string {
				return vfs.CompactTime(entry.ModifiedAt)
			},
		})
	}

	minNameWidth := 4
	gaps := 0
	for _, column := range fixed {
		gaps += separatorWidth(column.Key)
	}
	availableForColumns := totalWidth - gaps
	if availableForColumns < minNameWidth {
		availableForColumns = minNameWidth
	}

	fixedWidth := 0
	for _, column := range fixed {
		fixedWidth += column.Width
	}
	for fixedWidth+minNameWidth > availableForColumns {
		changed := false
		for idx := len(fixed) - 1; idx >= 0 && fixedWidth+minNameWidth > availableForColumns; idx-- {
			if fixed[idx].Width > fixed[idx].MinWidth {
				fixed[idx].Width--
				fixedWidth--
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	nameWidth := max(availableForColumns-fixedWidth, minNameWidth)
	name := columnSpec{
		Key:      "name",
		Title:    "Name",
		Width:    nameWidth,
		MinWidth: minNameWidth,
		Value: func(entry vfs.Entry, _ bool) string {
			return entryIcon(entry) + " " + entry.DisplayName()
		},
	}

	return append([]columnSpec{name}, fixed...)
}

func columnSeparator(columnKey string, palette theme.Palette, background lipgloss.Color) string {
	width := separatorWidth(columnKey)
	style := lipgloss.NewStyle().
		Width(width).
		Foreground(palette.Border)
	if background != lipgloss.Color("") {
		style = style.Background(background)
	}
	return style.Render(strings.Repeat(" ", width))
}

func separatorWidth(columnKey string) int {
	if columnKey == "size" {
		return 3
	}
	return 1
}

func borderStyle(value string) lipgloss.Border {
	switch strings.ToLower(value) {
	case "double":
		return lipgloss.DoubleBorder()
	case "thick":
		return lipgloss.ThickBorder()
	default:
		return lipgloss.RoundedBorder()
	}
}

func compactPath(path string, mode string) string {
	switch strings.ToLower(mode) {
	case "full":
		return path
	case "smart":
		return smartPath(path, 42)
	default:
		return vfs.SafeBase(path)
	}
}

func smartPath(path string, maxWidth int) string {
	if lipgloss.Width(path) <= maxWidth {
		return path
	}
	return truncateMiddle(path, maxWidth)
}

func truncateMiddle(value string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(value) <= maxWidth {
		return value
	}
	if maxWidth <= 3 {
		return value[:maxWidth]
	}
	left := maxWidth/2 - 1
	right := maxWidth - left - 1
	if left < 1 {
		left = 1
	}
	if right < 1 {
		right = 1
	}
	return value[:left] + "…" + value[len(value)-right:]
}

func truncateRight(value string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(value) <= maxWidth {
		return value
	}
	if maxWidth == 1 {
		return value[:1]
	}
	return value[:maxWidth-1] + "…"
}

func truncateForColumn(value string, maxWidth int, alignRight bool) string {
	if lipgloss.Width(value) <= maxWidth {
		return value
	}
	if alignRight {
		if maxWidth <= 1 {
			return value[len(value)-1:]
		}
		if len(value) <= maxWidth {
			return value
		}
		return "…" + value[len(value)-maxWidth+1:]
	}
	return truncateRight(value, maxWidth)
}

func entryIcon(entry vfs.Entry) string {
	switch entry.Category() {
	case "parent":
		return "↩"
	case "directory":
		return ""
	case "config":
		return ""
	case "text":
		return "󰈙"
	case "image":
		return "󰋩"
	case "executable":
		return "󰆍"
	case "archive":
		return ""
	default:
		return "󰈔"
	}
}

func entryColor(entry vfs.Entry, palette theme.Palette) lipgloss.Color {
	switch entry.Category() {
	case "directory", "parent":
		return palette.Folder
	case "config":
		return palette.ConfigFile
	case "text":
		return palette.TextFile
	case "image":
		return palette.ImageFile
	case "executable":
		return palette.ExecFile
	default:
		return palette.BinaryFile
	}
}
