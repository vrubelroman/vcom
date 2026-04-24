package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"vcom/internal/config"
	vfs "vcom/internal/fs"
	"vcom/internal/theme"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalMkdir
	modalConfirm
	modalCopyProgress
	modalNotice
	modalHelp
)

type fileOpKind int

const (
	opCopy fileOpKind = iota
	opMove
	opDelete
	opMkdir
	opEdit
	opView
)

type pendingOperation struct {
	kind            fileOpKind
	sourcePaths     []string
	targetDir       string
	overwrite       bool
	existingTargets int
	stats           vfs.TransferStats
}

type modalState struct {
	kind    modalKind
	title   string
	body    string
	note    string
	input   textinput.Model
	pending *pendingOperation
}

type previewMsg struct {
	entryPath string
	preview   vfs.Preview
}

type dirSizeMsg struct {
	path string
	size int64
	err  error
}

type opMsg struct {
	kind       fileOpKind
	sourcePath string
	targetPath string
	err        error
}

type copyPlanMsg struct {
	kind            fileOpKind
	sourcePaths     []string
	targetDir       string
	overwrite       bool
	existingTargets int
	stats           vfs.TransferStats
	err             error
}

type copyProgressMsg struct {
	jobID    int
	progress vfs.CopyProgress
}

type deletePlanMsg struct {
	sourcePaths []string
	stats       vfs.TransferStats
	err         error
}

type copyDoneMsg struct {
	jobID       int
	kind        fileOpKind
	sourcePaths []string
	targetDir   string
	err         error
}

type dismissNoticeMsg struct{}

type copyJobState struct {
	id          int
	kind        fileOpKind
	sourcePaths []string
	targetDir   string
	progress    vfs.CopyProgress
	overwrite   bool
	background  bool
	cancel      context.CancelFunc
	startedAt   time.Time
}

type mouseClickState struct {
	pane  PaneID
	index int
	at    time.Time
}

type hoverState struct {
	pane  PaneID
	index int
	ok    bool
}

type Model struct {
	cfg        config.Config
	configPath string
	palette    theme.Palette
	keys       KeyMap

	width  int
	height int

	left         BrowserPane
	right        BrowserPane
	active       PaneID
	infoMode     bool
	selectMode   bool
	viewMode     bool
	viewPrevInfo bool

	previewModel viewport.Model
	previewData  vfs.Preview

	modal  modalState
	status string
	busy   bool

	lastClick mouseClickState
	hover     hoverState

	copyJob      *copyJobState
	nextCopyJob  int
	copyProgress chan tea.Msg
}

func NewModel(cfg config.Config, configPath string) (Model, error) {
	palette, err := theme.Resolve(cfg.UI.Theme)
	if err != nil {
		return Model{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Model{}, err
	}

	leftPath, err := resolveStartPath(cfg.Startup.LeftPath, cwd)
	if err != nil {
		return Model{}, err
	}
	rightPath, err := resolveStartPath(cfg.Startup.RightPath, cwd)
	if err != nil {
		return Model{}, err
	}

	model := Model{
		cfg:          cfg,
		configPath:   configPath,
		palette:      palette,
		keys:         DefaultKeyMap(),
		left:         BrowserPane{ID: PaneLeft, Path: leftPath},
		right:        BrowserPane{ID: PaneRight, Path: rightPath},
		active:       PaneLeft,
		status:       "Ready",
		copyProgress: make(chan tea.Msg, 256),
	}

	model.previewModel = viewport.New(0, 0)
	if err := model.reloadPane(PaneLeft, ""); err != nil {
		return Model{}, err
	}
	if err := model.reloadPane(PaneRight, ""); err != nil {
		return Model{}, err
	}
	return model, nil
}

func (m Model) Init() tea.Cmd {
	return m.loadPreviewCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePreview()
		m.syncPreviewContent()
		return m, nil

	case previewMsg:
		if selected, ok := m.activePane().Selected(); ok && selected.Path == msg.entryPath {
			m.applyPreview(msg.preview)
		}
		if m.selectMode && !m.viewMode && msg.preview.Kind != vfs.PreviewKindText {
			m.selectMode = false
			return m, enableMouseCmd()
		}
		return m, nil

	case dirSizeMsg:
		m.busy = false
		if msg.err != nil {
			m.status = fmt.Sprintf("Dir size failed: %v", msg.err)
			return m, nil
		}
		m.applyDirSize(msg.path, msg.size)
		m.status = fmt.Sprintf("Directory size calculated: %s", vfs.HumanSize(msg.size))
		return m, m.loadPreviewCmd()

	case opMsg:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}

		m.modal = modalState{}
		switch msg.kind {
		case opCopy:
			m.status = fmt.Sprintf("Copied to %s", msg.targetPath)
		case opMove:
			m.status = fmt.Sprintf("Moved to %s", msg.targetPath)
		case opDelete:
			m.status = "Deleted"
			m.activePane().ClearMarks()
		case opMkdir:
			m.status = fmt.Sprintf("Created %s", msg.targetPath)
		case opEdit:
			m.status = "Editor closed"
			return m, tea.Batch(m.loadPreviewCmd(), enableMouseCmd())
		case opView:
			m.status = "Viewer closed"
			return m, enableMouseCmd()
		}

		activeSelection := selectedName(m.activePane())
		_ = m.reloadPane(PaneLeft, activeSelection)
		_ = m.reloadPane(PaneRight, activeSelection)
		return m, m.loadPreviewCmd()

	case copyPlanMsg:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}

		verb := operationVerb(msg.kind)
		title := fmt.Sprintf("%s selected entry?", strings.Title(verb))
		body := strings.Join([]string{
			fmt.Sprintf("Items: %d", len(msg.sourcePaths)),
			fmt.Sprintf("Files: %d", msg.stats.FilesTotal),
			fmt.Sprintf("Size:  %s", formatSize(msg.stats.BytesTotal, true)),
		}, "\n")
		note := "confirm-actions"
		m.openConfirmModal(title, body, note, pendingOperation{
			kind:            msg.kind,
			sourcePaths:     append([]string(nil), msg.sourcePaths...),
			targetDir:       msg.targetDir,
			overwrite:       msg.overwrite,
			existingTargets: msg.existingTargets,
			stats:           msg.stats,
		})
		return m, nil

	case deletePlanMsg:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}

		title := "Delete selected entr" + pluralSuffix(len(msg.sourcePaths), "y", "ies") + "?"
		bodyLines := []string{
			fmt.Sprintf("Items: %d", len(msg.sourcePaths)),
			fmt.Sprintf("Files: %d", msg.stats.FilesTotal),
			fmt.Sprintf("Size:  %s", formatSize(msg.stats.BytesTotal, true)),
		}
		m.openConfirmModal(
			title,
			strings.Join(bodyLines, "\n"),
			"confirm-actions",
			pendingOperation{
				kind:        opDelete,
				sourcePaths: append([]string(nil), msg.sourcePaths...),
				stats:       msg.stats,
			},
		)
		return m, nil

	case copyProgressMsg:
		if m.copyJob == nil || msg.jobID != m.copyJob.id {
			return m, nil
		}
		m.copyJob.progress = msg.progress
		if m.copyJob.background {
			m.status = formatCopyStatus(m.copyJob.kind, msg.progress)
		}
		return m, waitCopyProgressCmd(m.copyProgress)

	case copyDoneMsg:
		if m.copyJob == nil || msg.jobID != m.copyJob.id {
			return m, nil
		}

		m.busy = false
		if msg.err != nil {
			activeSelection := selectedName(m.activePane())
			_ = m.reloadPane(PaneLeft, activeSelection)
			_ = m.reloadPane(PaneRight, activeSelection)
			if msg.err == context.Canceled {
				m.status = strings.Title(operationVerb(msg.kind)) + " cancelled"
			} else {
				m.status = fmt.Sprintf("%s failed: %v", strings.Title(operationVerb(msg.kind)), msg.err)
			}
			m.copyJob = nil
			if m.modal.kind == modalCopyProgress {
				m.modal = modalState{}
			}
			return m, m.loadPreviewCmd()
		}

		m.status = fmt.Sprintf("%s %d entr%s to %s", operationDoneLabel(msg.kind), len(msg.sourcePaths), pluralSuffix(len(msg.sourcePaths), "y", "ies"), msg.targetDir)
		activeSelection := selectedName(m.activePane())
		_ = m.reloadPane(PaneLeft, activeSelection)
		_ = m.reloadPane(PaneRight, activeSelection)
		background := m.copyJob.background
		kind := m.copyJob.kind
		sourceCount := len(m.copyJob.sourcePaths)
		m.copyJob = nil
		m.activePane().ClearMarks()

		cmd := m.loadPreviewCmd()
		if m.modal.kind == modalCopyProgress {
			m.modal = modalState{}
		}
		if background {
			doneWord := "copied"
			if kind == opMove {
				doneWord = "moved"
			}
			doneBody := fmt.Sprintf("%d entr%s %s successfully.", sourceCount, pluralSuffix(sourceCount, "y", "ies"), doneWord)
			if sourceCount == 1 && len(msg.sourcePaths) == 1 {
				doneBody = filepath.Base(msg.sourcePaths[0]) + " " + doneWord + " successfully."
			}
			m.modal = modalState{
				kind:  modalNotice,
				title: strings.Title(operationVerb(kind)) + " complete",
				body:  doneBody,
			}
			cmd = tea.Batch(cmd, dismissNoticeCmd(time.Second))
		}
		return m, cmd

	case dismissNoticeMsg:
		if m.modal.kind == modalNotice {
			m.modal = modalState{}
		}
		return m, nil

	case tea.KeyMsg:
		if m.modal.kind != modalNone {
			return m.handleModalKey(msg)
		}
		if m.viewMode {
			switch {
			case key.Matches(msg, m.keys.View), key.Matches(msg, m.keys.Cancel), msg.String() == "q":
				return m.exitViewMode()
			case key.Matches(msg, m.keys.Up):
				m.previewModel.LineUp(1)
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.previewModel.LineDown(1)
				return m, nil
			case key.Matches(msg, m.keys.PageUp):
				m.previewModel.LineUp(max(m.previewModel.Height-2, 1))
				return m, nil
			case key.Matches(msg, m.keys.PageDown):
				m.previewModel.LineDown(max(m.previewModel.Height-2, 1))
				return m, nil
			default:
				return m, nil
			}
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.openHelpModal()
			return m, nil
		case key.Matches(msg, m.keys.Cancel):
			if len(m.activePane().MarkedEntries()) > 0 {
				m.activePane().ClearMarks()
				m.status = "Selection cleared"
				return m, m.loadPreviewCmd()
			}
			return m, nil
		case key.Matches(msg, m.keys.View):
			return m.handleView()
		case key.Matches(msg, m.keys.Edit):
			return m.handleEdit()
		case key.Matches(msg, m.keys.Info):
			return m.toggleInfo()
		case key.Matches(msg, m.keys.SelectText):
			return m.toggleSelectMode()
		case key.Matches(msg, m.keys.ToggleHidden):
			return m.toggleHidden()
		case key.Matches(msg, m.keys.CycleTheme):
			return m.cycleTheme()
		case key.Matches(msg, m.keys.CycleSort):
			return m.cycleSort()
		case key.Matches(msg, m.keys.Switch):
			m.left.ClearMarks()
			m.right.ClearMarks()
			if m.active == PaneLeft {
				m.active = PaneRight
			} else {
				m.active = PaneLeft
			}
			m.status = fmt.Sprintf("Active pane: %s", strings.ToUpper(string(m.active)))
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.Up):
			m.moveCursor(-1)
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.Down):
			m.moveCursor(1)
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.SelectUp):
			m.selectMoveCursor(-1)
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.SelectDown):
			m.selectMoveCursor(1)
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.PageUp):
			m.moveCursor(-max(m.bodyHeight()-6, 5))
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.PageDown):
			m.moveCursor(max(m.bodyHeight()-6, 5))
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.Open):
			return m.handleOpenSelected()
		case key.Matches(msg, m.keys.Back):
			if err := m.goParent(); err != nil {
				m.status = err.Error()
			}
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.Refresh):
			return m.refreshAllPanes("Refreshed")
		case key.Matches(msg, m.keys.DirSize):
			return m.handleDirSize()
		case key.Matches(msg, m.keys.Copy):
			return m.handleTransfer(opCopy)
		case key.Matches(msg, m.keys.Move):
			return m.handleTransfer(opMove)
		case key.Matches(msg, m.keys.Mkdir):
			m.openMkdirModal()
			return m, nil
		case key.Matches(msg, m.keys.Delete):
			return m.handleDelete()
		}

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	return m, nil
}

func (m Model) View() string {
	if m.width < 72 || m.height < 18 {
		return lipgloss.NewStyle().
			Foreground(m.palette.Warning).
			Padding(1, 2).
			Render("Terminal is too small for vcom. Resize the window.")
	}

	leftWidth, previewWidth, rightWidth := m.layoutWidths()
	bodyHeight := m.bodyHeight()
	gap := lipgloss.NewStyle().
		Width(m.cfg.UI.PaneGap).
		Height(bodyHeight).
		Background(m.palette.Panel).
		Render("")

	var panels string
	if m.selectMode && m.infoMode {
		panels = renderSelectionPane(m.previewData, &m.previewModel, m.palette, m.width, bodyHeight)
	} else if m.infoMode {
		if m.active == PaneLeft {
			panels = lipgloss.JoinHorizontal(
				lipgloss.Top,
				renderPane(m.left, m.cfg, m.palette, leftWidth, bodyHeight, true, m.hoverIndexFor(PaneLeft)),
				gap,
				renderPreviewPane(m.previewData, &m.previewModel, m.cfg, m.palette, previewWidth, bodyHeight),
			)
		} else {
			panels = lipgloss.JoinHorizontal(
				lipgloss.Top,
				renderPreviewPane(m.previewData, &m.previewModel, m.cfg, m.palette, previewWidth, bodyHeight),
				gap,
				renderPane(m.right, m.cfg, m.palette, rightWidth, bodyHeight, true, m.hoverIndexFor(PaneRight)),
			)
		}
	} else {
		panels = lipgloss.JoinHorizontal(
			lipgloss.Top,
			renderPane(m.left, m.cfg, m.palette, leftWidth, bodyHeight, m.active == PaneLeft, m.hoverIndexFor(PaneLeft)),
			gap,
			renderPane(m.right, m.cfg, m.palette, rightWidth, bodyHeight, m.active == PaneRight, m.hoverIndexFor(PaneRight)),
		)
	}

	parts := make([]string, 0, 3)
	parts = append(parts, panels)
	if m.cfg.UI.ShowFooter && !m.viewMode {
		parts = append(parts, renderFooter(m))
	}

	view := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Background(m.palette.Background).
		Foreground(m.palette.Text).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	if m.modal.kind != modalNone {
		modalWidth := min(72, m.width-8)
		if m.modal.kind == modalHelp {
			modalWidth = min(96, m.width-8)
		}
		view = overlayCenter(view, renderModal(m, m.palette, modalWidth), m.width)
	}
	return view
}

func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal.kind {
	case modalMkdir:
		switch {
		case isModalCloseKey(msg, m.keys):
			m.modal = modalState{}
			m.status = "Cancelled"
			return m, nil
		case key.Matches(msg, m.keys.Confirm):
			value := strings.TrimSpace(m.modal.input.Value())
			if value == "" {
				m.status = "Directory name must not be empty"
				return m, nil
			}
			m.busy = true
			return m, mkdirCmd(m.activePane().Path, value)
		}

		var cmd tea.Cmd
		m.modal.input, cmd = m.modal.input.Update(msg)
		return m, cmd

	case modalConfirm:
		switch {
		case isModalCloseKey(msg, m.keys):
			m.modal = modalState{}
			m.status = "Cancelled"
			return m, nil
		case key.Matches(msg, m.keys.Confirm):
			if m.modal.pending == nil {
				m.modal = modalState{}
				m.status = "Nothing to confirm"
				return m, nil
			}
			pending := *m.modal.pending
			m.modal = modalState{}
			if pending.kind == opCopy || pending.kind == opMove {
				if m.copyJob != nil {
					m.status = "Transfer is already running"
					return m, nil
				}
				m.busy = true
				return m, m.startCopyJob(pending.kind, pending.sourcePaths, pending.targetDir, pending.overwrite, pending.stats)
			}
			m.busy = true
			return m, pending.cmd()
		}

	case modalCopyProgress:
		if key.Matches(msg, m.keys.Background) {
			if m.copyJob == nil {
				m.modal = modalState{}
				return m, nil
			}
			m.copyJob.background = true
			m.modal = modalState{}
			m.status = "Transfer continues in background"
			return m, nil
		}
		if key.Matches(msg, m.keys.ProgressCancel) {
			if m.copyJob == nil {
				m.modal = modalState{}
				return m, nil
			}
			if m.copyJob.cancel != nil {
				m.copyJob.cancel()
			}
			m.status = strings.Title(operationVerb(m.copyJob.kind)) + " cancelling..."
			return m, nil
		}
		return m, nil

	case modalNotice:
		if key.Matches(msg, m.keys.Confirm) || isModalCloseKey(msg, m.keys) {
			m.modal = modalState{}
		}
		return m, nil

	case modalHelp:
		if isModalCloseKey(msg, m.keys) || key.Matches(msg, m.keys.Confirm) || key.Matches(msg, m.keys.Help) {
			m.modal = modalState{}
			m.status = "Help closed"
		}
		return m, nil
	}

	return m, nil
}

func isModalCloseKey(msg tea.KeyMsg, keys KeyMap) bool {
	return key.Matches(msg, keys.Cancel) || msg.String() == "q"
}

func (m *Model) reloadPane(id PaneID, preserve string) error {
	pane := m.paneByID(id)
	entries, err := vfs.ListDir(pane.Path, vfs.ListOptions{
		ShowHidden:  m.cfg.Browser.ShowHidden,
		DirsFirst:   m.cfg.Browser.DirsFirst,
		SortBy:      m.cfg.Browser.Sort.By,
		SortReverse: m.cfg.Browser.Sort.Reverse,
	})
	if err != nil {
		return err
	}
	pane.SetEntries(entries, strings.ToLower(preserve))
	return nil
}

func (m *Model) refreshAllPanes(status string) (tea.Model, tea.Cmd) {
	leftSelected := selectedName(&m.left)
	rightSelected := selectedName(&m.right)
	if err := m.reloadPane(PaneLeft, leftSelected); err != nil {
		m.status = err.Error()
		return m, nil
	}
	if err := m.reloadPane(PaneRight, rightSelected); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = status
	return m, m.loadPreviewCmd()
}

func (m *Model) moveCursor(delta int) {
	pane := m.activePane()
	pane.Move(delta, max(m.bodyHeight()-4, 1))
	m.hover = hoverState{}
}

func (m *Model) selectMoveCursor(delta int) {
	pane := m.activePane()
	if selected, ok := pane.Selected(); ok && !selected.IsParent {
		pane.ToggleMarked(selected.Path)
	}
	pane.Move(delta, max(m.bodyHeight()-4, 1))
	m.hover = hoverState{}
}

func (m *Model) operationSources() []string {
	pane := m.activePane()
	marked := pane.MarkedEntries()
	if len(marked) > 0 {
		paths := make([]string, 0, len(marked))
		for _, entry := range marked {
			if entry.IsParent {
				continue
			}
			paths = append(paths, entry.Path)
		}
		return paths
	}
	selected, ok := pane.Selected()
	if !ok || selected.IsParent {
		return nil
	}
	return []string{selected.Path}
}

func (m *Model) enterSelected() error {
	m.hover = hoverState{}
	pane := m.activePane()
	selected, ok := pane.Selected()
	if !ok {
		return nil
	}
	if !selected.IsDir {
		m.status = "File is shown in the middle pane. Use F3 for pager or F4 for editor."
		return nil
	}
	currentName := selected.Name
	pane.Path = selected.Path
	if err := m.reloadPane(pane.ID, currentName); err != nil {
		return err
	}
	m.status = fmt.Sprintf("Entered %s", pane.Path)
	return nil
}

func (m *Model) handleOpenSelected() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok {
		return m, nil
	}

	if selected.IsDir {
		if err := m.enterSelected(); err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m, m.loadPreviewCmd()
	}

	if isEditableEntry(selected) {
		return m.handleEdit()
	}
	return m.handleOpenExternal()
}

func (m *Model) goParent() error {
	m.hover = hoverState{}
	pane := m.activePane()
	parent := filepath.Dir(pane.Path)
	if parent == pane.Path {
		return nil
	}
	currentName := filepath.Base(pane.Path)
	pane.Path = parent
	if err := m.reloadPane(pane.ID, currentName); err != nil {
		return err
	}
	m.status = fmt.Sprintf("Moved to %s", parent)
	return nil
}

func (m Model) loadPreviewCmd() tea.Cmd {
	selected, ok := m.activePane().Selected()
	if !ok {
		return func() tea.Msg {
			return previewMsg{
				entryPath: "",
				preview: vfs.Preview{
					Kind:  vfs.PreviewKindEmpty,
					Title: "Nothing selected",
					Body:  "No entry selected.",
				},
			}
		}
	}

	options := vfs.PreviewOptions{
		ShowHidden:            m.cfg.Browser.ShowHidden,
		DirsFirst:             m.cfg.Browser.DirsFirst,
		SortBy:                m.cfg.Browser.Sort.By,
		SortReverse:           m.cfg.Browser.Sort.Reverse,
		MaxPreviewBytes:       m.cfg.Preview.MaxPreviewBytes,
		DirectoryPreviewLimit: m.cfg.Preview.DirectoryPreviewLimit,
		HumanReadableSize:     m.cfg.Browser.HumanReadableSize,
		ThemeName:             m.cfg.UI.Theme,
	}

	return func() tea.Msg {
		return previewMsg{
			entryPath: selected.Path,
			preview:   vfs.BuildPreview(selected, options),
		}
	}
}

func (m *Model) handleDirSize() (tea.Model, tea.Cmd) {
	if !m.cfg.Behavior.CalculateDirSizeOnSpace {
		m.status = "Directory size on Space is disabled in config"
		return m, nil
	}
	selected, ok := m.activePane().Selected()
	if !ok || !selected.IsDir || selected.IsParent {
		m.status = "Select a directory first"
		return m, nil
	}
	if selected.DirSizeKnown {
		m.status = fmt.Sprintf("Directory size: %s", formatSize(selected.Size, m.cfg.Browser.HumanReadableSize))
		return m, nil
	}

	m.busy = true
	m.status = fmt.Sprintf("Calculating directory size for %s", selected.DisplayName())
	return m, dirSizeCmd(selected.Path)
}

func (m *Model) handleTransfer(kind fileOpKind) (tea.Model, tea.Cmd) {
	sources := m.operationSources()
	if len(sources) == 0 {
		m.status = fmt.Sprintf("Nothing to %s", operationVerb(kind))
		return m, nil
	}

	targetDir := m.passivePane().Path
	existingTargets := 0
	for _, sourcePath := range sources {
		targetPath := filepath.Join(targetDir, filepath.Base(sourcePath))
		exists, err := vfs.PathExists(targetPath)
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		if exists {
			existingTargets++
		}
	}

	if kind == opCopy || kind == opMove {
		if m.copyJob != nil {
			m.status = "Transfer is already running"
			return m, nil
		}

		overwrite := existingTargets > 0
		if existingTargets > 0 && !m.cfg.Behavior.ConfirmOverwrite {
			overwrite = true
		}
		m.busy = true
		m.status = fmt.Sprintf("Calculating %s size", operationVerb(kind))
		return m, copyPlanCmd(kind, sources, targetDir, overwrite, existingTargets)
	}
	return m, nil
}

func (m *Model) handleDelete() (tea.Model, tea.Cmd) {
	sources := m.operationSources()
	if len(sources) == 0 {
		m.status = "Nothing to delete"
		return m, nil
	}
	if !m.cfg.Behavior.ConfirmDelete {
		m.busy = true
		m.status = fmt.Sprintf("Deleting %d entr%s", len(sources), pluralSuffix(len(sources), "y", "ies"))
		return m, deletePathsCmd(sources)
	}

	m.busy = true
	m.status = "Calculating delete size"
	return m, deletePlanCmd(sources)
}

func (m *Model) handleView() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok || selected.IsParent || selected.IsDir {
		m.status = "Select a file to view"
		return m, nil
	}
	if m.viewMode {
		return m.exitViewMode()
	}

	m.viewPrevInfo = m.infoMode
	m.infoMode = true
	m.selectMode = true
	m.viewMode = true
	m.resizePreview()
	m.syncPreviewContent()
	m.status = "View mode: F3/Esc/q to close"
	return m, tea.Batch(m.loadPreviewCmd(), disableMouseCmd())
}

func (m *Model) exitViewMode() (tea.Model, tea.Cmd) {
	if !m.viewMode {
		return m, nil
	}
	m.viewMode = false
	m.selectMode = false
	m.infoMode = m.viewPrevInfo
	m.resizePreview()
	m.syncPreviewContent()
	m.status = "View mode: off"
	return m, tea.Batch(m.loadPreviewCmd(), enableMouseCmd())
}

func (m *Model) handleOpenExternal() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok || selected.IsParent || selected.IsDir {
		m.status = "Select a file to open"
		return m, nil
	}

	command, name, err := externalCommand("", []string{"xdg-open", "open"}, selected.Path)
	if err != nil {
		m.status = "No system opener found (tried xdg-open/open)"
		return m, nil
	}

	m.status = fmt.Sprintf("Opening %s with %s", selected.DisplayName(), name)
	return m, tea.ExecProcess(command, func(err error) tea.Msg {
		return opMsg{kind: opView, sourcePath: selected.Path, err: err}
	})
}

func (m *Model) handleEdit() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok || selected.IsParent || selected.IsDir {
		m.status = "Select a file to edit"
		return m, nil
	}

	command, name, err := externalCommandFromEnv([]string{"VISUAL", "EDITOR"}, []string{"nvim", "vim", "vi", "nano"}, selected.Path)
	if err != nil {
		m.status = "Set $VISUAL/$EDITOR or install nvim/vim/vi/nano to enable F4 editing"
		return m, nil
	}

	m.status = fmt.Sprintf("Opening %s with %s", selected.DisplayName(), name)
	return m, tea.ExecProcess(command, func(err error) tea.Msg {
		return opMsg{kind: opEdit, sourcePath: selected.Path, err: err}
	})
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.viewMode {
		switch {
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
			m.previewModel.LineUp(3)
			return m, nil
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
			m.previewModel.LineDown(3)
			return m, nil
		default:
			return m, nil
		}
	}

	switch {
	case msg.Action == tea.MouseActionMotion:
		paneID, index, ok := m.mouseTarget(msg.X, msg.Y)
		if ok {
			m.hover = hoverState{pane: paneID, index: index, ok: true}
		} else {
			m.hover = hoverState{}
		}
		return m, nil
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
		if m.infoMode && m.mouseOverPreview(msg.X, msg.Y) {
			m.previewModel.LineUp(3)
			return m, nil
		}
		m.moveCursor(-1)
		return m, m.loadPreviewCmd()
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
		if m.infoMode && m.mouseOverPreview(msg.X, msg.Y) {
			m.previewModel.LineDown(3)
			return m, nil
		}
		m.moveCursor(1)
		return m, m.loadPreviewCmd()
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		paneID, index, ok := m.mouseTarget(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		m.hover = hoverState{pane: paneID, index: index, ok: true}
		if paneID != m.active {
			m.left.ClearMarks()
			m.right.ClearMarks()
		}
		m.active = paneID
		pane := m.paneByID(paneID)
		if index >= 0 && index < len(pane.Entries) {
			pane.Cursor = index
			pane.EnsureVisible(max(m.bodyHeight()-4, 1))
		}

		now := time.Now()
		doubleClick := m.lastClick.pane == paneID && m.lastClick.index == index && now.Sub(m.lastClick.at) <= 450*time.Millisecond
		m.lastClick = mouseClickState{pane: paneID, index: index, at: now}
		if doubleClick {
			return m.handleOpenSelected()
		}
		m.status = fmt.Sprintf("Selected %s pane", strings.ToUpper(string(paneID)))
		return m, m.loadPreviewCmd()
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight:
		if m.infoMode && m.mouseOverPreview(msg.X, msg.Y) {
			m.infoMode = false
			m.resizePreview()
			m.syncPreviewContent()
			m.status = "Info mode: off"
			return m, nil
		}

		paneID, index, ok := m.mouseTarget(msg.X, msg.Y)
		if !ok {
			return m, nil
		}

		if m.infoMode && paneID == m.active && index == m.activePane().Cursor {
			m.infoMode = false
			m.hover = hoverState{pane: paneID, index: index, ok: true}
			m.status = "Info mode: off"
			return m, nil
		}

		m.hover = hoverState{pane: paneID, index: index, ok: true}
		if paneID != m.active {
			m.left.ClearMarks()
			m.right.ClearMarks()
		}
		m.active = paneID
		pane := m.paneByID(paneID)
		if index >= 0 && index < len(pane.Entries) {
			pane.Cursor = index
			pane.EnsureVisible(max(m.bodyHeight()-4, 1))
		}
		m.infoMode = true
		m.resizePreview()
		m.syncPreviewContent()
		m.status = fmt.Sprintf("Info mode: %s selection", strings.ToUpper(string(paneID)))
		return m, m.loadPreviewCmd()
	default:
		return m, nil
	}
}

func (m *Model) toggleInfo() (tea.Model, tea.Cmd) {
	m.infoMode = !m.infoMode
	m.resizePreview()
	m.syncPreviewContent()
	if m.infoMode {
		m.status = fmt.Sprintf("Info mode: %s selection", strings.ToUpper(string(m.active)))
		return m, m.loadPreviewCmd()
	}
	if m.selectMode {
		m.selectMode = false
		return m, enableMouseCmd()
	}
	m.status = "Info mode: off"
	return m, nil
}

func (m *Model) toggleSelectMode() (tea.Model, tea.Cmd) {
	if m.viewMode {
		m.status = "Close view mode first (F3/Esc/q)"
		return m, nil
	}
	if m.selectMode {
		m.selectMode = false
		m.status = "Text selection mode: off"
		return m, enableMouseCmd()
	}
	if !m.infoMode || m.previewData.Kind != vfs.PreviewKindText {
		m.status = "Text selection mode works only for text preview in info pane"
		return m, nil
	}
	m.selectMode = true
	m.status = "Text selection mode: on"
	return m, disableMouseCmd()
}

func (m *Model) toggleHidden() (tea.Model, tea.Cmd) {
	m.cfg.Browser.ShowHidden = !m.cfg.Browser.ShowHidden
	return m.refreshAllPanes(fmt.Sprintf("Show hidden: %t", m.cfg.Browser.ShowHidden))
}

func (m *Model) cycleTheme() (tea.Model, tea.Cmd) {
	next := theme.Next(m.cfg.UI.Theme)
	palette, err := theme.Resolve(next)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.cfg.UI.Theme = next
	m.palette = palette
	m.status = fmt.Sprintf("Theme: %s", next)
	return m, nil
}

func (m *Model) cycleSort() (tea.Model, tea.Cmd) {
	order := []string{"name", "modified", "size", "created", "extension"}
	current := strings.ToLower(strings.TrimSpace(m.cfg.Browser.Sort.By))
	next := order[0]
	for idx, value := range order {
		if value == current {
			next = order[(idx+1)%len(order)]
			break
		}
	}
	m.cfg.Browser.Sort.By = next
	return m.refreshAllPanes(fmt.Sprintf("Sort: %s", next))
}

func (m *Model) openMkdirModal() {
	input := textinput.New()
	input.Placeholder = "new-directory"
	input.Focus()
	input.CharLimit = 128
	input.Width = 42

	m.modal = modalState{
		kind:  modalMkdir,
		title: "Create directory",
		body:  fmt.Sprintf("Active pane: %s", m.activePane().Path),
		note:  "Enter to confirm, Esc to cancel",
		input: input,
	}
}

func (m *Model) openConfirmModal(title, body, note string, pending pendingOperation) {
	m.modal = modalState{
		kind:    modalConfirm,
		title:   title,
		body:    body,
		note:    note,
		pending: &pending,
	}
}

func (m *Model) openHelpModal() {
	sections := []string{
		"Navigation",
		"  j / Down       move down",
		"  k / Up         move up",
		"  Shift+Down/J   extend selection down",
		"  Shift+Up/K     extend selection up",
		"  PgDn / f       page down",
		"  PgUp / b       page up",
		"  Enter / Right  open selected entry",
		"  Backspace/Left  go to parent directory",
		"  Tab / h / l    switch active pane",
		"  r              refresh both panes",
		"",
		"View and Panels",
		"  F9 / i         toggle preview/info pane",
		"  F3 / v         open read-only view mode",
		"  F3 / Esc / q   close view mode",
		"  Ctrl+t         toggle text selection mode in text preview",
		"  Space          calculate selected directory size",
		"  s              cycle sort mode",
		"  .              toggle hidden files",
		"  t              cycle theme",
		"",
		"Dialogs and Transfers",
		"  Enter / y      confirm action",
		"  Esc / n        cancel action",
		"  b              run copy/move in background (progress dialog)",
		"  c              cancel active copy/move transfer",
		"  F5/F6/F8       apply to marked entries when selection exists",
		"",
		"Mouse",
		"  Left click     select entry and activate pane",
		"  Double click   open selected entry",
		"  Right click    toggle preview/info mode for clicked entry",
		"  Wheel          scroll list or preview area",
		"",
		"F-key actions are shown in the footer.",
	}

	m.modal = modalState{
		kind:  modalHelp,
		title: "Keyboard and Mouse Help",
		body:  strings.Join(sections, "\n"),
		note:  "F1/? or Esc to close",
	}
	m.status = "Help opened"
}

func (m *Model) applyDirSize(path string, size int64) {
	for _, pane := range []*BrowserPane{&m.left, &m.right} {
		for idx := range pane.Entries {
			if pane.Entries[idx].Path == path {
				pane.Entries[idx].Size = size
				pane.Entries[idx].DirSizeKnown = true
			}
		}
	}
}

func (m *Model) applyPreview(preview vfs.Preview) {
	m.previewData = preview
	m.syncPreviewContent()
}

func (m *Model) syncPreviewContent() {
	content := m.previewData.Body
	if m.cfg.Preview.WrapText && m.previewModel.Width > 0 {
		content = lipgloss.NewStyle().Width(m.previewModel.Width).Render(content)
	}
	m.previewModel.SetContent(content)
}

func (m *Model) activePane() *BrowserPane {
	if m.active == PaneLeft {
		return &m.left
	}
	return &m.right
}

func (m *Model) passivePane() *BrowserPane {
	if m.active == PaneLeft {
		return &m.right
	}
	return &m.left
}

func (m *Model) paneByID(id PaneID) *BrowserPane {
	if id == PaneLeft {
		return &m.left
	}
	return &m.right
}

func (m *Model) layoutWidths() (int, int, int) {
	total := m.width
	gaps := m.cfg.UI.PaneGap
	usable := max(total-gaps, 60)
	left := usable / 2
	right := usable - left

	if m.active == PaneLeft {
		if m.infoMode {
			return left, right, 0
		}
		return left, 0, right
	}
	if m.infoMode {
		return 0, left, right
	}
	return left, 0, right
}

func (m *Model) bodyHeight() int {
	height := m.height
	if m.cfg.UI.ShowFooter && !m.viewMode {
		height--
	}
	return max(height, 8)
}

func (m *Model) resizePreview() {
	_, previewWidth, _ := m.layoutWidths()
	metaHeight := 0
	if m.cfg.Preview.ShowMetadata {
		metaHeight = 7
	}
	innerWidth := max(previewWidth-2, 1)
	innerHeight := max(m.bodyHeight()-2, 1)
	m.previewModel.Width = max(innerWidth-2, 10)
	m.previewModel.Height = max(innerHeight-metaHeight-3, 3)
}

func renderPreviewPane(preview vfs.Preview, viewportModel *viewport.Model, cfg config.Config, palette theme.Palette, width int, height int) string {
	innerWidth := max(width-2, 1)
	innerHeight := max(height-2, 1)
	contentWidth := max(innerWidth-2, 1)

	box := lipgloss.NewStyle().
		Width(innerWidth).
		Height(innerHeight).
		Background(palette.Panel).
		Foreground(palette.Text).
		BorderStyle(borderStyle(cfg.UI.Border)).
		BorderForeground(palette.BorderActive).
		BorderBackground(palette.Panel)

	title := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(0, 1).
		Background(palette.Accent).
		Foreground(palette.Background).
		Bold(true).
		Render("PREVIEW " + previewIcon(preview) + " " + preview.Title)

	parts := []string{title}
	usedHeight := lipgloss.Height(title)
	if cfg.Preview.ShowMetadata {
		metaView := renderMetadata(preview.Metadata, palette, innerWidth)
		parts = append(parts, metaView)
		usedHeight += lipgloss.Height(metaView)
	}
	contentHeight := max(innerHeight-usedHeight, 3)
	viewportModel.Width = max(innerWidth-2, 10)
	viewportModel.Height = max(contentHeight-3, 1)
	parts = append(parts, renderPreviewContent(viewportModel, palette, innerWidth, contentHeight))

	content := lipgloss.NewStyle().
		Width(innerWidth).
		MaxHeight(innerHeight).
		Background(palette.Panel).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return box.Render(content)
}

func renderSelectionPane(preview vfs.Preview, viewportModel *viewport.Model, palette theme.Palette, width int, height int) string {
	content := preview.PlainBody
	if strings.TrimSpace(content) == "" {
		content = preview.Body
	}
	viewportModel.Width = max(width, 1)
	viewportModel.Height = max(height, 1)
	viewportModel.SetContent(content)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(palette.Panel).
		Foreground(palette.Text).
		Render(viewportModel.View())
}

func renderMetadata(meta vfs.Metadata, palette theme.Palette, width int) string {
	outerWidth := max(width-2, 1)
	innerWidth := max(outerWidth-2, 1)
	leftRows := []string{
		fmt.Sprintf("kind: %s", fallback(meta.Kind, "n/a")),
		fmt.Sprintf("size: %s", metaSize(meta)),
		fmt.Sprintf("created: %s", fallback(meta.CreatedAt, "n/a")),
	}
	rightRows := []string{
		fmt.Sprintf("modified: %s", fallback(meta.ModifiedAt, "n/a")),
		fmt.Sprintf("mode: %s", fallback(meta.Permissions, "n/a")),
	}
	if meta.ImageFormat != "" {
		rightRows = append(rightRows, fmt.Sprintf("image: %s %s", meta.ImageFormat, meta.ImageSize))
	}

	leftWidth := max(innerWidth/2, 18)
	if leftWidth > innerWidth {
		leftWidth = innerWidth
	}
	rightWidth := max(innerWidth-leftWidth, 0)
	left := lipgloss.NewStyle().
		Width(leftWidth).
		Background(palette.PanelElevated).
		Foreground(palette.Muted).
		Render(strings.Join(leftRows, "\n"))
	right := lipgloss.NewStyle().
		Width(rightWidth).
		Background(palette.PanelElevated).
		Foreground(palette.Text).
		Render(strings.Join(rightRows, "\n"))

	pathLine := lipgloss.NewStyle().
		Width(innerWidth).
		Background(palette.PanelElevated).
		Foreground(palette.Text).
		Render(fmt.Sprintf("path: %s", truncateMiddle(meta.Path, max(innerWidth-8, 16))))

	return lipgloss.NewStyle().
		Width(outerWidth).
		Padding(0, 1).
		Background(palette.PanelElevated).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(palette.Border).
		BorderBackground(palette.PanelElevated).
		Render(lipgloss.JoinVertical(
			lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Top, left, right),
			"",
			pathLine,
		))
}

func renderTitleBar(m Model) string {
	left := lipgloss.NewStyle().
		Foreground(m.palette.Background).
		Background(m.palette.Accent).
		Bold(true).
		Padding(0, 1).
		Render(strings.ToUpper(m.cfg.UI.AppTitle))

	centerParts := []string{
		fmt.Sprintf("theme:%s", m.cfg.UI.Theme),
		fmt.Sprintf("hidden:%t", m.cfg.Browser.ShowHidden),
		fmt.Sprintf("sort:%s", m.cfg.Browser.Sort.By),
		fmt.Sprintf("info:%t", m.infoMode),
	}
	center := lipgloss.NewStyle().
		Foreground(m.palette.Text).
		Background(m.palette.Panel).
		Padding(0, 1).
		Render(strings.Join(centerParts, "  "))

	configLabel := "cfg:default"
	if m.configPath != "" {
		configLabel = "cfg:" + filepath.Base(m.configPath)
	}
	right := lipgloss.NewStyle().
		Foreground(m.palette.Muted).
		Background(m.palette.Panel).
		Padding(0, 1).
		Render(configLabel)

	fillWidth := max(m.width-lipgloss.Width(left)-lipgloss.Width(center)-lipgloss.Width(right), 0)
	fill := lipgloss.NewStyle().
		Width(fillWidth).
		Background(m.palette.Panel).
		Render("")

	return left + center + fill + right
}

func renderStatus(m Model) string {
	active := m.activePane()
	selected, _ := active.Selected()
	summary := fmt.Sprintf(
		"%s | %s | items:%d | selected:%s",
		strings.ToUpper(string(m.active)),
		compactPath(active.Path, m.cfg.UI.PathDisplay),
		len(active.Entries),
		fallback(selected.DisplayName(), "n/a"),
	)

	return lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 1).
		Background(m.palette.StatusBar).
		Foreground(m.palette.Text).
		Render(summary + "  ::  " + m.status)
}

func renderFooter(m Model) string {
	parts := make([]string, 0, 8)
	for _, binding := range m.keys.ShortHelp() {
		help := binding.Help()
		if help.Key == "" || help.Desc == "" {
			continue
		}
		keyView := lipgloss.NewStyle().
			Background(m.palette.Footer).
			Foreground(m.palette.FooterKey).
			Bold(true).
			Render(help.Key)
		descView := lipgloss.NewStyle().
			Background(m.palette.Footer).
			Foreground(m.palette.Text).
			Render(" " + help.Desc)
		parts = append(parts, keyView+descView)
	}
	line := strings.Join(parts, "   ")
	if m.selectMode {
		modeLabel := lipgloss.NewStyle().
			Foreground(m.palette.Info).
			Bold(true).
			Render("SELECT TEXT MODE")
		if line != "" {
			line += "   "
		}
		line += modeLabel
	}
	line = " " + line
	return lipgloss.PlaceHorizontal(
		m.width,
		lipgloss.Left,
		line,
		lipgloss.WithWhitespaceBackground(m.palette.Footer),
	)
}

func renderModal(m Model, palette theme.Palette, width int) string {
	if m.modal.kind == modalCopyProgress && m.copyJob != nil {
		return renderCopyProgressModal(*m.copyJob, palette, width)
	}
	if m.modal.kind == modalHelp {
		return renderHelpModal(m.modal, palette, width)
	}

	modal := m.modal
	outerWidth := max(width, 8)
	contentWidth := max(outerWidth-6, 1)
	titleStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Bold(true).Foreground(palette.Accent)
	noteStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.Muted)
	spacer := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Render(" ")

	box := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(1, 2).
		Background(palette.Panel).
		Foreground(palette.Text).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(palette.BorderActive).
		BorderBackground(palette.Panel)

	lines := []string{titleStyle.Render(modal.title), spacer}

	for _, raw := range strings.Split(modal.body, "\n") {
		lines = append(lines, renderModalBodyLine(raw, contentWidth, palette))
	}

	if modal.kind == modalMkdir {
		lines = append(lines, spacer, lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Render(modal.input.View()))
	}
	if modal.note != "" {
		lines = append(lines, spacer)
		if modal.note == "confirm-actions" {
			lines = append(lines, renderConfirmActions(contentWidth, palette))
		} else {
			for _, raw := range strings.Split(modal.note, "\n") {
				lines = append(lines, renderModalNoteLine(raw, contentWidth, palette, noteStyle))
			}
		}
	}

	return box.Render(strings.Join(lines, "\n"))
}

func renderModalBodyLine(raw string, width int, palette theme.Palette) string {
	base := lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Foreground(palette.Text)
	if strings.TrimSpace(raw) == "" {
		return base.Render("")
	}

	if idx := strings.Index(raw, ":"); idx > 0 {
		keyText := strings.TrimSpace(raw[:idx+1])
		valueText := strings.TrimLeft(raw[idx+1:], " ")
		keyWidth := min(max(idx+2, 8), width)
		valueWidth := max(width-keyWidth, 0)

		keyStyle := lipgloss.NewStyle().
			Width(keyWidth).
			Background(palette.Panel).
			Foreground(palette.FooterKey).
			Bold(true)
		valueStyle := lipgloss.NewStyle().
			Width(valueWidth).
			Background(palette.Panel).
			Foreground(palette.Text)
		return base.Render(keyStyle.Render(keyText) + valueStyle.Render(valueText))
	}

	if strings.HasPrefix(strings.TrimSpace(raw), "Existing targets") {
		return lipgloss.NewStyle().
			Width(width).
			Background(palette.Panel).
			Foreground(palette.Warning).
			Render(strings.TrimSpace(raw))
	}

	return base.Render(raw)
}

func renderModalNoteLine(raw string, width int, palette theme.Palette, fallback lipgloss.Style) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback.Render("")
	}

	for _, sep := range []string{" to ", " (", ","} {
		if idx := strings.Index(raw, sep); idx > 0 {
			keyLabel := strings.TrimSpace(raw[:idx])
			action := strings.TrimLeft(raw[idx:], " ")
			keyWidth := min(max(lipgloss.Width(keyLabel)+2, 10), width)
			actionWidth := max(width-keyWidth, 0)
			keyStyle := lipgloss.NewStyle().
				Width(keyWidth).
				Background(palette.Panel).
				Foreground(palette.FooterKey).
				Bold(true)
			actionStyle := lipgloss.NewStyle().
				Width(actionWidth).
				Background(palette.Panel).
				Foreground(palette.Muted)
			return keyStyle.Render(keyLabel) + actionStyle.Render(action)
		}
	}

	return fallback.Render(raw)
}

func renderConfirmActions(width int, palette theme.Palette) string {
	const minButtonWidth = 10
	const maxButtonWidth = 14
	const gapWidth = 4
	labelWidth := max(lipgloss.Width("Enter / y"), lipgloss.Width("Esc / n"))
	buttonWidth := min(max(labelWidth+2, minButtonWidth), maxButtonWidth)
	buttonWidth = min(buttonWidth, max((width-gapWidth)/2, labelWidth))

	confirm := lipgloss.NewStyle().
		Width(buttonWidth).
		Align(lipgloss.Center).
		Background(palette.ConfirmButton).
		Foreground(palette.Background).
		Bold(true).
		Render("Enter / y")

	cancel := lipgloss.NewStyle().
		Width(buttonWidth).
		Align(lipgloss.Center).
		Background(palette.CancelButton).
		Foreground(palette.Background).
		Bold(true).
		Render("Esc / n")

	gap := lipgloss.NewStyle().
		Width(gapWidth).
		Background(palette.Panel).
		Render("")
	enterBias := lipgloss.NewStyle().
		Width(9).
		Background(palette.Panel).
		Render("")
	cancelTail := lipgloss.NewStyle().
		Width(5).
		Background(palette.Panel).
		Render("")
	group := lipgloss.JoinHorizontal(lipgloss.Top, confirm, enterBias, gap, cancel, cancelTail)
	row := lipgloss.PlaceHorizontal(
		width,
		lipgloss.Center,
		group,
		lipgloss.WithWhitespaceBackground(palette.Panel),
	)
	return lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Render(row)
}

func renderHelpModal(modal modalState, palette theme.Palette, width int) string {
	outerWidth := max(width, 8)
	contentWidth := max(outerWidth-6, 1)
	titleStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Bold(true).Foreground(palette.Accent)
	lineStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.Text)
	mutedLineStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.Muted)
	noteStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.FooterKey).Bold(true)
	spacer := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Render(" ")

	keyColStyle := lipgloss.NewStyle().Width(24).Background(palette.Panel).Foreground(palette.FooterKey).Bold(true)
	descColStyle := lipgloss.NewStyle().Background(palette.Panel).Foreground(palette.Text)

	box := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(1, 2).
		Background(palette.Panel).
		Foreground(palette.Text).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(palette.BorderActive).
		BorderBackground(palette.Panel)

	lines := []string{titleStyle.Render(modal.title), spacer}
	for _, raw := range strings.Split(modal.body, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			lines = append(lines, spacer)
			continue
		}

		if strings.HasPrefix(raw, "  ") {
			keyLabel, action := splitHelpItem(raw)
			if action == "" {
				lines = append(lines, lineStyle.Render(trimmed))
				continue
			}
			row := keyColStyle.Render(keyLabel) + descColStyle.Render(action)
			lines = append(lines, lineStyle.Render(row))
			continue
		}

		if strings.HasSuffix(trimmed, ".") {
			lines = append(lines, mutedLineStyle.Render(trimmed))
			continue
		}

		sectionColor := sectionColorForHeader(trimmed, palette)
		header := lipgloss.NewStyle().
			Width(contentWidth).
			Background(palette.Panel).
			Foreground(sectionColor).
			Bold(true).
			Render(trimmed)
		lines = append(lines, header)
	}

	if modal.note != "" {
		lines = append(lines, spacer, noteStyle.Render(modal.note))
	}

	return box.Render(strings.Join(lines, "\n"))
}

func splitHelpItem(raw string) (string, string) {
	value := strings.TrimSpace(raw)
	for idx := 0; idx < len(value)-1; idx++ {
		if value[idx] == ' ' && value[idx+1] == ' ' {
			keyLabel := strings.TrimSpace(value[:idx])
			action := strings.TrimSpace(value[idx:])
			if keyLabel != "" && action != "" {
				return keyLabel, action
			}
		}
	}
	return value, ""
}

func sectionColorForHeader(header string, palette theme.Palette) lipgloss.Color {
	switch header {
	case "Navigation":
		return palette.HelpNav
	case "View and Panels":
		return palette.HelpPanels
	case "Dialogs and Transfers":
		return palette.HelpDialogs
	case "Mouse":
		return palette.HelpMouse
	default:
		return palette.Accent
	}
}

func renderCopyProgressModal(job copyJobState, palette theme.Palette, width int) string {
	outerWidth := max(width, 8)
	contentWidth := max(outerWidth-6, 1)
	titleStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Bold(true).Foreground(palette.Accent)
	mutedStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.Muted)
	spacer := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Render(" ")

	box := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(1, 2).
		Background(palette.Panel).
		Foreground(palette.Text).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(palette.BorderActive).
		BorderBackground(palette.Panel)

	progress := job.progress
	ratio := 0.0
	if progress.BytesTotal > 0 {
		ratio = float64(progress.BytesDone) / float64(progress.BytesTotal)
	}

	lines := []string{
		titleStyle.Render(progressTitle(job.kind)),
		spacer,
		renderProgressBarLine(ratio, contentWidth, palette),
		spacer,
		renderProgressPercentLine(ratio, contentWidth, palette),
		renderProgressStatLine("Stage:", progressStageLabel(progress, job.kind), contentWidth, palette),
		spacer,
		renderProgressStatLine("Files:", fmt.Sprintf("%d / %d", progress.FilesDone, progress.FilesTotal), contentWidth, palette),
		renderProgressStatLine("Size:", fmt.Sprintf("%s / %s", formatSize(progress.BytesDone, true), formatSize(progress.BytesTotal, true)), contentWidth, palette),
		renderProgressStatLine("Speed:", transferSpeed(progress.BytesDone, job.startedAt), contentWidth, palette),
		spacer,
		renderProgressActions(contentWidth, palette),
	}
	if job.background {
		lines = append(lines, mutedStyle.Render("Transfer continues in background"))
	}

	return box.Render(strings.Join(lines, "\n"))
}

func renderProgressBar(ratio float64, width int, palette theme.Palette) string {
	if width < 10 {
		width = 10
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(float64(width) * ratio)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	bar := lipgloss.NewStyle().Foreground(palette.ProgressFill).Render(strings.Repeat("█", filled))
	rest := lipgloss.NewStyle().Foreground(palette.ProgressEmpty).Render(strings.Repeat("░", width-filled))
	return bar + rest
}

func renderProgressBarLine(ratio float64, width int, palette theme.Palette) string {
	sidePad := max(width/8, 6)
	barWidth := max(width-(sidePad*2), 10)
	rightPad := max(width-sidePad-barWidth, 0)
	left := lipgloss.NewStyle().
		Width(sidePad).
		Background(palette.Panel).
		Render("")
	bar := lipgloss.NewStyle().
		Width(barWidth).
		Background(palette.Panel).
		Render(renderProgressBar(ratio, barWidth, palette))
	right := lipgloss.NewStyle().
		Width(rightPad).
		Background(palette.Panel).
		Render("")
	return lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Render(left + bar + right)
}

func renderProgressStatLine(label string, value string, width int, palette theme.Palette) string {
	keyWidth := min(max(lipgloss.Width(label)+1, 8), width)
	valueWidth := max(width-keyWidth, 0)
	keyStyle := lipgloss.NewStyle().
		Width(keyWidth).
		Background(palette.Panel).
		Foreground(palette.FooterKey).
		Bold(true)
	valueStyle := lipgloss.NewStyle().
		Width(valueWidth).
		Background(palette.Panel).
		Foreground(palette.Text)
	return keyStyle.Render(label) + valueStyle.Render(value)
}

func renderProgressActions(width int, palette theme.Palette) string {
	const minButtonWidth = 10
	const maxButtonWidth = 14
	const gapWidth = 4
	labelWidth := max(lipgloss.Width("Background / b"), lipgloss.Width("Cancel / c"))
	buttonWidth := min(max(labelWidth+2, minButtonWidth), maxButtonWidth)
	buttonWidth = min(buttonWidth, max((width-gapWidth)/2, labelWidth))

	backgroundBtn := lipgloss.NewStyle().
		Width(buttonWidth).
		Align(lipgloss.Center).
		Background(palette.Info).
		Foreground(palette.Background).
		Bold(true).
		Render("Background / b")

	cancelBtn := lipgloss.NewStyle().
		Width(buttonWidth).
		Align(lipgloss.Center).
		Background(palette.CancelButton).
		Foreground(palette.Background).
		Bold(true).
		Render("Cancel / c")

	gap := lipgloss.NewStyle().
		Width(gapWidth).
		Background(palette.Panel).
		Render("")
	enterBias := lipgloss.NewStyle().
		Width(9).
		Background(palette.Panel).
		Render("")
	cancelTail := lipgloss.NewStyle().
		Width(5).
		Background(palette.Panel).
		Render("")
	group := lipgloss.JoinHorizontal(lipgloss.Top, backgroundBtn, enterBias, gap, cancelBtn, cancelTail)
	row := lipgloss.PlaceHorizontal(
		width,
		lipgloss.Center,
		group,
		lipgloss.WithWhitespaceBackground(palette.Panel),
	)
	return lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Render(row)
}

func renderProgressPercentLine(ratio float64, width int, palette theme.Palette) string {
	percent := lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Foreground(palette.Info).
		Bold(true).
		Render(fmt.Sprintf("%.0f%%", ratio*100))
	return percent
}

func transferSpeed(bytesDone int64, startedAt time.Time) string {
	if startedAt.IsZero() || bytesDone <= 0 {
		return "calculating..."
	}
	elapsed := time.Since(startedAt)
	if elapsed <= 0 {
		return "calculating..."
	}
	perSecond := int64(float64(bytesDone) / elapsed.Seconds())
	if perSecond <= 0 {
		return "calculating..."
	}
	return fmt.Sprintf("%s/s", vfs.HumanSize(perSecond))
}

func progressStageLabel(progress vfs.CopyProgress, kind fileOpKind) string {
	if strings.TrimSpace(progress.Stage) != "" {
		if progress.Stage == "Transferring data" && progress.BytesTotal > 0 && progress.BytesDone >= progress.BytesTotal {
			if progress.FilesDone < progress.FilesTotal {
				return "Finalizing file"
			}
			if kind == opMove {
				return "Preparing move finalization"
			}
			return "Finalizing transfer"
		}
		return progress.Stage
	}
	if kind == opMove {
		return "Preparing move"
	}
	return "Transferring data"
}

func overlayCenter(base string, overlay string, width int) string {
	if width <= 0 {
		return base
	}

	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	if len(baseLines) == 0 || len(overlayLines) == 0 {
		return base
	}

	overlayWidth := 0
	for _, line := range overlayLines {
		overlayWidth = max(overlayWidth, ansi.StringWidth(line))
	}
	startY := max((len(baseLines)-len(overlayLines))/2, 0)
	startX := max((width-overlayWidth)/2, 0)
	endX := startX + overlayWidth

	for idx, line := range overlayLines {
		targetY := startY + idx
		if targetY >= len(baseLines) {
			break
		}
		baseLine := baseLines[targetY]
		left := ansi.Cut(baseLine, 0, startX)
		right := ansi.Cut(baseLine, endX, width)
		baseLines[targetY] = left + line + right
	}

	return strings.Join(baseLines, "\n")
}

func renderPreviewContent(viewportModel *viewport.Model, palette theme.Palette, width int, height int) string {
	outerWidth := max(width-2, 1)
	innerWidth := max(outerWidth-2, 1)
	innerHeight := max(height, 1)

	body := lipgloss.NewStyle().
		Width(innerWidth).
		Height(max(innerHeight, 1)).
		Padding(0, 1).
		Background(palette.Panel).
		Render(viewportModel.View())

	return lipgloss.NewStyle().
		Width(outerWidth).
		Height(innerHeight).
		Background(palette.Panel).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(palette.Border).
		BorderBackground(palette.Panel).
		Render(body)
}

func previewIcon(preview vfs.Preview) string {
	switch preview.Kind {
	case vfs.PreviewKindDirectory:
		return ""
	case vfs.PreviewKindImage:
		return "󰋩"
	case vfs.PreviewKindText:
		return "󰈙"
	case vfs.PreviewKindBinary:
		return "󰈔"
	case vfs.PreviewKindError:
		return ""
	default:
		return "󰇙"
	}
}

func (p pendingOperation) cmd() tea.Cmd {
	switch p.kind {
	case opMove:
		return nil
	case opDelete:
		return deletePathsCmd(p.sourcePaths)
	default:
		return nil
	}
}

func operationCmd(kind fileOpKind, sourcePath, targetDir string, overwrite bool) tea.Cmd {
	switch kind {
	case opMove:
		return moveCmd(sourcePath, targetDir, overwrite)
	default:
		return nil
	}
}

func dirSizeCmd(path string) tea.Cmd {
	return func() tea.Msg {
		size, err := vfs.DirectorySize(path)
		return dirSizeMsg{path: path, size: size, err: err}
	}
}

func copyPlanCmd(kind fileOpKind, sourcePaths []string, targetDir string, overwrite bool, existingTargets int) tea.Cmd {
	return func() tea.Msg {
		stats := vfs.TransferStats{}
		var err error
		for _, sourcePath := range sourcePaths {
			part, statErr := vfs.CopyStats(sourcePath)
			if statErr != nil {
				err = statErr
				break
			}
			stats.FilesTotal += part.FilesTotal
			stats.BytesTotal += part.BytesTotal
		}
		return copyPlanMsg{
			kind:            kind,
			sourcePaths:     append([]string(nil), sourcePaths...),
			targetDir:       targetDir,
			overwrite:       overwrite,
			existingTargets: existingTargets,
			stats:           stats,
			err:             err,
		}
	}
}

func deletePlanCmd(sourcePaths []string) tea.Cmd {
	return func() tea.Msg {
		stats := vfs.TransferStats{}
		var err error
		for _, sourcePath := range sourcePaths {
			part, statErr := vfs.CopyStats(sourcePath)
			if statErr != nil {
				err = statErr
				break
			}
			stats.FilesTotal += part.FilesTotal
			stats.BytesTotal += part.BytesTotal
		}
		return deletePlanMsg{
			sourcePaths: append([]string(nil), sourcePaths...),
			stats:       stats,
			err:         err,
		}
	}
}

func waitCopyProgressCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func dismissNoticeCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return dismissNoticeMsg{}
	})
}

func (m *Model) startCopyJob(kind fileOpKind, sourcePaths []string, targetDir string, overwrite bool, stats vfs.TransferStats) tea.Cmd {
	m.nextCopyJob++
	jobID := m.nextCopyJob
	ctx, cancel := context.WithCancel(context.Background())
	m.copyJob = &copyJobState{
		id:          jobID,
		kind:        kind,
		sourcePaths: append([]string(nil), sourcePaths...),
		targetDir:   targetDir,
		overwrite:   overwrite,
		progress: vfs.CopyProgress{
			FilesDone:   0,
			FilesTotal:  stats.FilesTotal,
			BytesDone:   0,
			BytesTotal:  stats.BytesTotal,
			CurrentPath: sourcePaths[0],
		},
		cancel:    cancel,
		startedAt: time.Now(),
	}
	m.modal = modalState{kind: modalCopyProgress}
	m.status = strings.Title(operationVerb(kind)) + " started"

	return tea.Batch(
		func() tea.Msg {
			go func() {
				doneFiles := 0
				var doneBytes int64
				for _, sourcePath := range sourcePaths {
					entryStats, statErr := vfs.CopyStats(sourcePath)
					if statErr != nil {
						m.copyProgress <- copyDoneMsg{
							jobID:       jobID,
							kind:        kind,
							sourcePaths: append([]string(nil), sourcePaths...),
							targetDir:   targetDir,
							err:         statErr,
						}
						return
					}
					progressFn := func(progress vfs.CopyProgress) {
						m.copyProgress <- copyProgressMsg{
							jobID: jobID,
							progress: vfs.CopyProgress{
								FilesDone:   doneFiles + progress.FilesDone,
								FilesTotal:  stats.FilesTotal,
								BytesDone:   doneBytes + progress.BytesDone,
								BytesTotal:  stats.BytesTotal,
								CurrentPath: progress.CurrentPath,
								Stage:       progress.Stage,
							},
						}
					}
					switch kind {
					case opMove:
						_, statErr = vfs.MovePathWithProgressContext(ctx, sourcePath, targetDir, overwrite, entryStats, progressFn)
					default:
						_, statErr = vfs.CopyPathWithProgressContext(ctx, sourcePath, targetDir, overwrite, entryStats, progressFn)
					}
					if statErr != nil {
						m.copyProgress <- copyDoneMsg{
							jobID:       jobID,
							kind:        kind,
							sourcePaths: append([]string(nil), sourcePaths...),
							targetDir:   targetDir,
							err:         statErr,
						}
						return
					}
					doneFiles += entryStats.FilesTotal
					doneBytes += entryStats.BytesTotal
				}
				m.copyProgress <- copyDoneMsg{
					jobID:       jobID,
					kind:        kind,
					sourcePaths: append([]string(nil), sourcePaths...),
					targetDir:   targetDir,
				}
			}()
			return nil
		},
		waitCopyProgressCmd(m.copyProgress),
	)
}

func moveCmd(sourcePath, targetDir string, overwrite bool) tea.Cmd {
	return func() tea.Msg {
		targetPath, err := vfs.MovePath(sourcePath, targetDir, overwrite)
		return opMsg{kind: opMove, sourcePath: sourcePath, targetPath: targetPath, err: err}
	}
}

func deletePathsCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		for _, path := range paths {
			if err := vfs.DeletePath(path); err != nil {
				return opMsg{kind: opDelete, sourcePath: path, err: err}
			}
		}
		return opMsg{kind: opDelete}
	}
}

func mkdirCmd(parent, name string) tea.Cmd {
	return func() tea.Msg {
		targetPath, err := vfs.MakeDir(parent, name)
		return opMsg{kind: opMkdir, targetPath: targetPath, err: err}
	}
}

func selectedName(pane *BrowserPane) string {
	selected, ok := pane.Selected()
	if !ok {
		return ""
	}
	return selected.Name
}

func metaSize(meta vfs.Metadata) string {
	if !meta.SizeKnown {
		return "press Space"
	}
	return vfs.HumanSize(meta.Size)
}

func fallback(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func formatSize(size int64, human bool) string {
	if human {
		return vfs.HumanSize(size)
	}
	return fmt.Sprintf("%d", size)
}

func formatCopyStatus(kind fileOpKind, progress vfs.CopyProgress) string {
	return fmt.Sprintf(
		"%s in background: %d/%d files, %s/%s",
		strings.Title(operationVerb(kind)),
		progress.FilesDone,
		progress.FilesTotal,
		formatSize(progress.BytesDone, true),
		formatSize(progress.BytesTotal, true),
	)
}

func transferSourceLabel(paths []string) string {
	if len(paths) == 0 {
		return "n/a"
	}
	if len(paths) == 1 {
		return paths[0]
	}
	return fmt.Sprintf("%d selected entries", len(paths))
}

func pluralSuffix(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func progressTitle(kind fileOpKind) string {
	switch kind {
	case opMove:
		return "Moving"
	default:
		return "Copying"
	}
}

func operationDoneLabel(kind fileOpKind) string {
	switch kind {
	case opMove:
		return "Moved"
	case opCopy:
		return "Copied"
	case opDelete:
		return "Deleted"
	default:
		return "Done"
	}
}

func operationVerb(kind fileOpKind) string {
	switch kind {
	case opCopy:
		return "copy"
	case opMove:
		return "move"
	case opDelete:
		return "delete"
	default:
		return "operate on"
	}
}

func externalCommand(envVar string, fallbacks []string, path string) (*exec.Cmd, string, error) {
	envVars := []string{}
	if envVar != "" {
		envVars = append(envVars, envVar)
	}
	return externalCommandFromEnv(envVars, fallbacks, path)
}

func externalCommandFromEnv(envVars []string, fallbacks []string, path string) (*exec.Cmd, string, error) {
	commandLine := ""
	source := "fallbacks"
	for _, envVar := range envVars {
		commandLine = strings.TrimSpace(os.Getenv(envVar))
		if commandLine != "" {
			source = envVar
			break
		}
	}
	if commandLine == "" {
		for _, candidate := range fallbacks {
			if resolved, err := exec.LookPath(candidate); err == nil {
				commandLine = resolved
				source = candidate
				break
			}
		}
	}
	if commandLine == "" {
		if len(envVars) > 0 {
			return nil, "", fmt.Errorf("no command for %s", strings.Join(envVars, "/"))
		}
		return nil, "", fmt.Errorf("no fallback command found")
	}

	parts := strings.Fields(commandLine)
	if len(parts) == 0 {
		return nil, "", fmt.Errorf("invalid command for %s", source)
	}

	args := append(parts[1:], path)
	return exec.Command(parts[0], args...), filepath.Base(parts[0]), nil
}

func enableMouseCmd() tea.Cmd {
	return func() tea.Msg {
		return tea.EnableMouseCellMotion()
	}
}

func disableMouseCmd() tea.Cmd {
	return func() tea.Msg {
		return tea.DisableMouse()
	}
}

func resolveStartPath(raw string, fallback string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("startup path is not a directory: %s", abs)
	}
	return abs, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) mouseTarget(x, y int) (PaneID, int, bool) {
	if m.width <= 0 || m.height <= 0 {
		return "", 0, false
	}

	leftWidth, previewWidth, rightWidth := m.layoutWidths()
	top := 0
	bodyHeight := m.bodyHeight()
	if y < top || y >= top+bodyHeight {
		return "", 0, false
	}

	gap := m.cfg.UI.PaneGap
	leftStart := 0
	rightStart := leftWidth + gap
	if m.infoMode && m.active == PaneRight {
		rightStart = previewWidth + gap
	}

	switch {
	case x >= leftStart && x < leftStart+leftWidth:
		if m.infoMode && m.active == PaneRight {
			return "", 0, false
		}
		index, ok := paneIndexFromMouse(y-top, bodyHeight, &m.left)
		if !ok {
			return "", 0, false
		}
		return PaneLeft, index, true
	case x >= rightStart && x < rightStart+rightWidth:
		if m.infoMode && m.active == PaneLeft {
			return "", 0, false
		}
		index, ok := paneIndexFromMouse(y-top, bodyHeight, &m.right)
		if !ok {
			return "", 0, false
		}
		return PaneRight, index, true
	default:
		return "", 0, false
	}
}

func paneIndexFromMouse(localY int, height int, pane *BrowserPane) (int, bool) {
	const listStartY = 3
	if localY < listStartY || localY >= height-1 {
		return 0, false
	}
	row := localY - listStartY
	index := pane.Offset + row
	if index < 0 {
		index = 0
	}
	if index >= len(pane.Entries) {
		return 0, false
	}
	return index, true
}

func isEditableEntry(entry vfs.Entry) bool {
	switch entry.Category() {
	case "text", "config", "executable":
		return true
	default:
		return false
	}
}

func (m *Model) hoverIndexFor(pane PaneID) int {
	if m.hover.ok && m.hover.pane == pane {
		return m.hover.index
	}
	return -1
}

func (m *Model) mouseOverPreview(x, y int) bool {
	if !m.infoMode || m.width <= 0 || m.height <= 0 {
		return false
	}

	leftWidth, previewWidth, _ := m.layoutWidths()
	top := 0
	bodyHeight := m.bodyHeight()
	if y < top || y >= top+bodyHeight {
		return false
	}

	gap := m.cfg.UI.PaneGap
	if m.active == PaneLeft {
		startX := leftWidth + gap
		return x >= startX && x < startX+previewWidth
	}

	return x >= 0 && x < previewWidth
}
