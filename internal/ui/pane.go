package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"vcom/internal/config"
	vfs "vcom/internal/fs"
	"vcom/internal/fs/remote"
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
	Marked  map[string]struct{}
	Archive []ArchiveMount
	Remote  []RemoteMount

	dirHistory []string
	dirFuture  []string
}

type ArchiveMount struct {
	SourcePath string
	ParentPath string
	RootPath   string
	TempDir    string
}

// RemoteMount represents an active SSH/SFTP remote filesystem connection.
type RemoteMount struct {
	Host       remote.SSHHost
	RemotePath string
	Client     *remote.SSHClient
	Connected  bool
}

func (p *BrowserPane) Selected() (vfs.Entry, bool) {
	if len(p.Entries) == 0 || p.Cursor < 0 || p.Cursor >= len(p.Entries) {
		return vfs.Entry{}, false
	}
	return p.Entries[p.Cursor], true
}

func (p *BrowserPane) SetEntries(entries []vfs.Entry, preserveKey string) {
	p.Entries = entries
	p.PruneMarks()
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

func (p *BrowserPane) EnsureMarked(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if p.Marked == nil {
		p.Marked = map[string]struct{}{}
	}
	p.Marked[path] = struct{}{}
}

func (p *BrowserPane) ToggleMarked(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if p.Marked == nil {
		p.Marked = map[string]struct{}{}
	}
	if _, ok := p.Marked[path]; ok {
		delete(p.Marked, path)
		if len(p.Marked) == 0 {
			p.Marked = nil
		}
		return
	}
	p.Marked[path] = struct{}{}
}

func (p *BrowserPane) IsMarked(path string) bool {
	if p.Marked == nil {
		return false
	}
	_, ok := p.Marked[path]
	return ok
}

func (p *BrowserPane) ClearMarks() {
	p.Marked = nil
}

func (p *BrowserPane) PruneMarks() {
	if len(p.Marked) == 0 {
		return
	}
	valid := map[string]struct{}{}
	for _, entry := range p.Entries {
		if entry.IsParent {
			continue
		}
		valid[entry.Path] = struct{}{}
	}
	for path := range p.Marked {
		if _, ok := valid[path]; !ok {
			delete(p.Marked, path)
		}
	}
	if len(p.Marked) == 0 {
		p.Marked = nil
	}
}

func (p *BrowserPane) MarkedEntries() []vfs.Entry {
	if len(p.Marked) == 0 {
		return nil
	}
	result := make([]vfs.Entry, 0, len(p.Marked))
	for _, entry := range p.Entries {
		if entry.IsParent {
			continue
		}
		if p.IsMarked(entry.Path) {
			result = append(result, entry)
		}
	}
	return result
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

func (p *BrowserPane) InArchive() bool {
	return len(p.Archive) > 0
}

// PushHistory saves the current path to the back-stack and clears the forward-stack.
func (p *BrowserPane) PushHistory(path string) {
	p.dirHistory = append(p.dirHistory, path)
	p.dirFuture = nil
}

// PopHistory returns the most recent path from the back-stack.
func (p *BrowserPane) PopHistory() (string, bool) {
	if len(p.dirHistory) == 0 {
		return "", false
	}
	path := p.dirHistory[len(p.dirHistory)-1]
	p.dirHistory = p.dirHistory[:len(p.dirHistory)-1]
	return path, true
}

// PushFuture saves the current path to the forward-stack.
func (p *BrowserPane) PushFuture(path string) {
	p.dirFuture = append(p.dirFuture, path)
}

// PopFuture returns the most recent path from the forward-stack.
func (p *BrowserPane) PopFuture() (string, bool) {
	if len(p.dirFuture) == 0 {
		return "", false
	}
	path := p.dirFuture[len(p.dirFuture)-1]
	p.dirFuture = p.dirFuture[:len(p.dirFuture)-1]
	return path, true
}

// HasHistory returns true if there are entries in the back-stack.
func (p *BrowserPane) HasHistory() bool {
	return len(p.dirHistory) > 0
}

// HasFuture returns true if there are entries in the forward-stack.
func (p *BrowserPane) HasFuture() bool {
	return len(p.dirFuture) > 0
}

// HistoryDepth returns the number of entries in the back-stack.
func (p *BrowserPane) HistoryDepth() int {
	return len(p.dirHistory)
}

// FutureDepth returns the number of entries in the forward-stack.
func (p *BrowserPane) FutureDepth() int {
	return len(p.dirFuture)
}

func (p *BrowserPane) PushArchive(mount ArchiveMount) {
	p.Archive = append(p.Archive, mount)
}

func (p *BrowserPane) PopArchive() (ArchiveMount, bool) {
	if len(p.Archive) == 0 {
		return ArchiveMount{}, false
	}
	last := p.Archive[len(p.Archive)-1]
	p.Archive = p.Archive[:len(p.Archive)-1]
	return last, true
}

func (p *BrowserPane) CurrentArchive() (ArchiveMount, bool) {
	if len(p.Archive) == 0 {
		return ArchiveMount{}, false
	}
	return p.Archive[len(p.Archive)-1], true
}

func (p *BrowserPane) ClearArchives() []ArchiveMount {
	if len(p.Archive) == 0 {
		return nil
	}
	out := make([]ArchiveMount, len(p.Archive))
	copy(out, p.Archive)
	p.Archive = nil
	return out
}

func (p *BrowserPane) PushRemote(mount RemoteMount) {
	p.Remote = append(p.Remote, mount)
}

func (p *BrowserPane) PopRemote() (RemoteMount, bool) {
	if len(p.Remote) == 0 {
		return RemoteMount{}, false
	}
	last := p.Remote[len(p.Remote)-1]
	p.Remote = p.Remote[:len(p.Remote)-1]
	return last, true
}

func (p *BrowserPane) CurrentRemote() (RemoteMount, bool) {
	if len(p.Remote) == 0 {
		return RemoteMount{}, false
	}
	return p.Remote[len(p.Remote)-1], true
}

func (p *BrowserPane) InRemote() bool {
	return len(p.Remote) > 0
}

func (p *BrowserPane) ClearRemotes() []RemoteMount {
	if len(p.Remote) == 0 {
		return nil
	}
	out := make([]RemoteMount, len(p.Remote))
	copy(out, p.Remote)
	p.Remote = nil
	return out
}

func (p *BrowserPane) DisplayPath() string {
	if len(p.Remote) > 0 {
		top := p.Remote[len(p.Remote)-1]
		statusIcon := "󰖩"
		if !top.Connected {
			statusIcon = "󰤭"
		}
		if top.RemotePath == "/" || top.RemotePath == "" {
			return fmt.Sprintf("%s %s:", statusIcon, top.Host.Name)
		}
		return fmt.Sprintf("%s %s:%s", statusIcon, top.Host.Name, top.RemotePath)
	}
	if len(p.Archive) == 0 {
		return p.Path
	}
	top := p.Archive[len(p.Archive)-1]
	rel, err := filepath.Rel(top.RootPath, p.Path)
	if err != nil {
		return p.Path
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		rel = ""
	}
	if rel == "" {
		return top.SourcePath + "::"
	}
	return top.SourcePath + "::/" + rel
}

func renderPane(
	pane BrowserPane,
	cfg config.Config,
	palette theme.Palette,
	width int,
	height int,
	active bool,
	hoverIndex int,
	useNerdIcons bool,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	borderColor := palette.Border
	headerBg := palette.PanelInactive
	bodyBg := palette.Panel
	if active {
		borderColor = palette.BorderActive
		headerBg = palette.Selection
	}

	innerWidth := max(width-2, 1)
	innerHeight := max(height-2, 1)

	box := lipgloss.NewStyle().
		Width(innerWidth).
		Height(innerHeight).
		Background(bodyBg).
		Foreground(palette.Text).
		BorderStyle(borderStyle(cfg.UI.Border)).
		BorderForeground(borderColor).
		BorderBackground(bodyBg)

	header := lipgloss.NewStyle().
		Render(renderPaneHeader(pane, cfg, palette, innerWidth, active, headerBg))

	isSSHHostList := pane.Path == "ssh://"
	rowsHeight := max(innerHeight-2, 1)
	headerRow := renderColumnsHeader(cfg, innerWidth, palette, bodyBg, useNerdIcons, isSSHHostList)
	rows := renderPaneRows(pane, cfg, palette, innerWidth, rowsHeight, active, hoverIndex, bodyBg, useNerdIcons, isSSHHostList)
	content := lipgloss.JoinVertical(lipgloss.Left, header, headerRow, rows)
	return box.Render(content)
}

func renderPaneHeader(pane BrowserPane, cfg config.Config, palette theme.Palette, width int, active bool, headerBg lipgloss.Color) string {
	pathWidth := max(width, 4)
	pathStyle := lipgloss.NewStyle().
		Width(pathWidth).
		Background(headerBg).
		Foreground(palette.Text).
		Bold(active)
	if active {
		pathStyle = pathStyle.Foreground(palette.ActivePath)
	}

	return lipgloss.NewStyle().
		Width(width).
		Background(headerBg).
		Render(pathStyle.Render(truncateMiddle(compactPath(pane.DisplayPath(), cfg.UI.PathDisplay), pathWidth)))
}

func renderColumnsHeader(cfg config.Config, width int, palette theme.Palette, background lipgloss.Color, useNerdIcons bool, hideExtraCols bool) string {
	columns := buildColumns(cfg, width, useNerdIcons, hideExtraCols)
	parts := make([]string, 0, len(columns))
	for idx, column := range columns {
		style := lipgloss.NewStyle().
			Width(column.Width).
			Foreground(palette.Muted).
			Background(background).
			Bold(true)
		if column.AlignRight {
			style = style.Align(lipgloss.Right)
		}
		parts = append(parts, style.Render(truncateRight(column.Title, column.Width)))
		if idx < len(columns)-1 {
			parts = append(parts, columnSeparator(column.Key, palette, background))
		}
	}
	return lipgloss.NewStyle().
		Width(width).
		Background(background).
		Render(strings.Join(parts, ""))
}

func renderPaneRows(pane BrowserPane, cfg config.Config, palette theme.Palette, width int, height int, active bool, hoverIndex int, background lipgloss.Color, useNerdIcons bool, hideExtraCols bool) string {
	if len(pane.Entries) == 0 {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Padding(1, 1).
			Background(background).
			Foreground(palette.Muted).
			Render("Empty directory")
	}

	visibleHeight := max(height, 1)
	pane.EnsureVisible(visibleHeight)
	end := min(len(pane.Entries), pane.Offset+visibleHeight)

	lines := make([]string, 0, visibleHeight)
	for idx := pane.Offset; idx < end; idx++ {
		entry := pane.Entries[idx]
		isSelected := idx == pane.Cursor && active
		marked := !entry.IsParent && pane.IsMarked(entry.Path)
		row := renderEntryRow(entry, cfg, width, isSelected, marked, idx == hoverIndex, active, palette, background, useNerdIcons, hideExtraCols)
		lines = append(lines, row)
	}
	for len(lines) < visibleHeight {
		lines = append(lines, lipgloss.NewStyle().Width(width).Background(background).Render(""))
	}

	return lipgloss.NewStyle().
		Width(width).
		Background(background).
		Render(strings.Join(lines, "\n"))
}

func renderEntryRow(entry vfs.Entry, cfg config.Config, width int, selected bool, marked bool, hovered bool, active bool, palette theme.Palette, baseBackground lipgloss.Color, useNerdIcons bool, hideExtraCols bool) string {
	columns := buildColumns(cfg, width, useNerdIcons, hideExtraCols)
	rowBackground := baseBackground
	switch {
	case marked:
		rowBackground = palette.Marked
	case selected:
		rowBackground = palette.Selection
	case hovered:
		rowBackground = palette.Hover
	}

	parts := make([]string, 0, len(columns))
	for idx, column := range columns {
		value := column.Value(entry, cfg.Browser.HumanReadableSize)
		foreground := entryColor(entry, palette)
		if marked {
			foreground = palette.Background
		}
		style := lipgloss.NewStyle().
			Width(column.Width).
			Foreground(foreground).
			Background(rowBackground)

		if entry.IsHidden && !marked {
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

	rowStyle := lipgloss.NewStyle().Width(width).Background(rowBackground)
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

func buildColumns(cfg config.Config, totalWidth int, useNerdIcons bool, hideExtraCols bool) []columnSpec {
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
	if cfg.Browser.Columns.Size && !hideExtraCols {
		fixed = append(fixed, columnSpec{
			Key:        "size",
			Title:      "Size",
			Width:      9,
			MinWidth:   6,
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
	if cfg.Browser.Columns.Modified && !hideExtraCols {
		fixed = append(fixed, columnSpec{
			Key:      "modified",
			Title:    "Modified",
			Width:    11,
			MinWidth: 8,
			Value: func(entry vfs.Entry, _ bool) string {
				if entry.IsParent {
					return ""
				}
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
			return entryIcon(entry, useNerdIcons) + " " + entry.DisplayName()
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
		return 2
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
		return trimToWidthRight(value, maxWidth)
	}
	left := maxWidth/2 - 1
	right := maxWidth - left - 1
	if left < 1 {
		left = 1
	}
	if right < 1 {
		right = 1
	}
	return trimToWidthRight(value, left) + "…" + trimToWidthLeft(value, right)
}

func truncateRight(value string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(value) <= maxWidth {
		return value
	}
	if maxWidth == 1 {
		return trimToWidthRight(value, 1)
	}
	return trimToWidthRight(value, maxWidth-1) + "…"
}

func truncateForColumn(value string, maxWidth int, alignRight bool) string {
	if lipgloss.Width(value) <= maxWidth {
		return value
	}
	if alignRight {
		if maxWidth <= 1 {
			return trimToWidthLeft(value, 1)
		}
		return "…" + trimToWidthLeft(value, maxWidth-1)
	}
	return truncateRight(value, maxWidth)
}

func trimToWidthRight(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	width := 0
	var builder strings.Builder
	for _, r := range value {
		rw := lipgloss.Width(string(r))
		if width+rw > maxWidth {
			break
		}
		builder.WriteRune(r)
		width += rw
	}
	return builder.String()
}

func trimToWidthLeft(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	runes := []rune(value)
	width := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := lipgloss.Width(string(runes[i]))
		if width+rw > maxWidth {
			break
		}
		width += rw
		start = i
	}
	return string(runes[start:])
}

func entryIcon(entry vfs.Entry, useNerdIcons bool) string {
	if !useNerdIcons {
		switch entry.Category() {
		case "parent":
			return "<-"
		case "remote":
			return "[SV]"
		case "directory":
			return "[D]"
		case "config":
			return "[C]"
		case "text":
			return "[T]"
		case "image":
			return "[I]"
		case "executable":
			return "[X]"
		case "archive":
			return "[A]"
		default:
			return "[F]"
		}
	}
	switch entry.Category() {
	case "parent":
		return "↩"
	case "remote":
		return "󰒋"
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
	case "remote":
		return palette.ExecFile
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
