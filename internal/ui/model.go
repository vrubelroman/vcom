package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"vcom/internal/config"
	vfs "vcom/internal/fs"
	"vcom/internal/fs/remote"
	"vcom/internal/theme"
)

const version = "v0.2.4"

type modalKind int

const (
	modalNone modalKind = iota
	modalMkdir
	modalRename
	modalConfirm
	modalCopyProgress
	modalNotice
	modalHelp
	modalArchiveType
	modalArchiveProgress
	modalSSHConnect
	modalThemeSelect
)

type fileOpKind int

const (
	opCopy fileOpKind = iota
	opMove
	opDelete
	opPermanentDelete
	opMkdir
	opRename
	opEdit
	opView
	opArchive
	opExecute
	opDeleteHost
	opExtractArchive
)

type pendingOperation struct {
	kind            fileOpKind
	sourcePaths     []string
	targetDir       string
	overwrite       bool
	existingTargets int
	stats           vfs.TransferStats

	// Remote operation fields — non-nil when source/target is remote
	srcClient *remote.SSHClient
	dstClient *remote.SSHClient
}

type modalState struct {
	kind    modalKind
	title   string
	body    string
	note    string
	input   textinput.Model
	pending *pendingOperation
}

type themeSelectorState struct {
	names    []string // all theme names in order
	cursor   int      // current cursor index in the list
	original string   // the theme name before opening dialog (for Esc revert)
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

	// Remote operation fields
	srcClient *remote.SSHClient
	dstClient *remote.SSHClient
}

type copyProgressMsg struct {
	jobID    int
	progress vfs.CopyProgress
}

type deletePlanMsg struct {
	kind        fileOpKind
	sourcePaths []string
	stats       vfs.TransferStats
	err         error

	// Remote operation — non-nil when source is remote
	srcClient *remote.SSHClient
}

type archivePlanMsg struct {
	sourcePaths []string
	targetDir   string
	stats       vfs.TransferStats
	err         error
}

type archiveProgressMsg struct {
	jobID    int
	progress vfs.CopyProgress
}

type archiveDoneMsg struct {
	jobID       int
	sourcePaths []string
	targetPath  string
	err         error
}

type copyDoneMsg struct {
	jobID       int
	kind        fileOpKind
	sourcePaths []string
	targetDir   string
	targetPath  string
	err         error
}

type dismissNoticeMsg struct{}
type dismissYankFlashMsg struct{}
type externalOpenMsg struct {
	path string
	err  error
}

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

type archiveJobState struct {
	id          int
	kind        string // "archive" for creation, "extract" for extraction
	sourcePaths []string
	targetPath  string
	progress    vfs.CopyProgress
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
	nerdIcons  bool
	overlay    *imageOverlayManager

	width  int
	height int

	left            BrowserPane
	right           BrowserPane
	active          PaneID
	infoMode        bool
	selectMode      bool
	cursorMode      bool
	cursorLine      int
	cursorCol       int
	visualMode      bool
	visualAnchor    int
	visualAnchorCol int
	viewMode        bool
	viewPrevInfo    bool

	previewModel viewport.Model
	previewData  vfs.Preview

	filterMode   bool
	filterQuery  string
	filterInput  textinput.Model
	filterPaneID PaneID

	modal  modalState
	status string
	busy   bool

	lastClick     mouseClickState
	hover         hoverState
	pendingY      bool
	yankFlashLine int

	copyJob      *copyJobState
	nextCopyJob  int
	copyProgress chan tea.Msg
	copyPath     string

	archiveJob      *archiveJobState
	nextArchiveJob  int
	archiveProgress chan tea.Msg
	archiveFormat   string
	deleteKind      string // "trash" or "permanent" — selected in delete modal
	ssh             *sshState
	preSSHPath      string              // original path before entering SSH mode
	themeSelector   *themeSelectorState // nil when not in theme selector dialog
}

func NewModel(cfg config.Config, configPath string) (Model, error) {
	palette, err := theme.Resolve(cfg.UI.Theme)
	if err != nil {
		return Model{}, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Model{}, fmt.Errorf("resolve home dir: %w", err)
	}

	leftPath, err := resolveStartPath(cfg.Startup.LeftPath, home)
	if err != nil {
		return Model{}, err
	}
	rightPath, err := resolveStartPath(cfg.Startup.RightPath, home)
	if err != nil {
		return Model{}, err
	}

	model := Model{
		cfg:             cfg,
		configPath:      configPath,
		palette:         palette,
		keys:            DefaultKeyMap(),
		overlay:         newImageOverlayManager(),
		left:            BrowserPane{ID: PaneLeft, Path: leftPath},
		right:           BrowserPane{ID: PaneRight, Path: rightPath},
		active:          PaneLeft,
		status:          "Ready",
		copyProgress:    make(chan tea.Msg, 256),
		archiveProgress: make(chan tea.Msg, 256),
	}
	model.nerdIcons, model.status = resolveIconMode(cfg.UI.IconMode)
	if model.status == "" {
		model.status = "Ready"
	}

	filterInput := textinput.New()
	filterInput.Placeholder = "filter by name…"
	filterInput.CharLimit = 64
	filterInput.Width = 40
	model.filterInput = filterInput

	model.previewModel = viewport.New(0, 0)

	sshSt, err := newSSHState()
	if err != nil {
		// Non-fatal: SSH support will be unavailable
		model.status = fmt.Sprintf("SSH init: %v", err)
	} else {
		model.ssh = sshSt
	}

	// Apply saved session (overrides startup paths if a previous session exists)
	applySession(&model)

	leftPreserve := model.left.LoadCursor(model.left.Path)
	rightPreserve := model.right.LoadCursor(model.right.Path)
	if err := model.reloadPane(PaneLeft, leftPreserve); err != nil {
		return Model{}, err
	}
	if err := model.reloadPane(PaneRight, rightPreserve); err != nil {
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
		log.Printf("[EVENT] WindowSizeMsg: %dx%d", msg.Width, msg.Height)
		m.width = msg.Width
		m.height = msg.Height
		m.resizePreview()
		m.syncPreviewContent()
		return m, nil

	case previewMsg:
		log.Printf("[EVENT] previewMsg: path=%s kind=%s", msg.entryPath, msg.preview.Kind)
		if selected, ok := m.activePane().Selected(); ok && selected.Path == msg.entryPath {
			m.applyPreview(msg.preview)
		}
		if m.selectMode && !m.viewMode && msg.preview.Kind != vfs.PreviewKindText {
			m.selectMode = false
			return m, enableMouseCmd()
		}
		if (m.cursorMode || m.visualMode) && msg.preview.Kind != vfs.PreviewKindText {
			m.cursorMode = false
			m.visualMode = false
			m.status = "Text cursor mode: off"
		}
		return m, nil

	case dirSizeMsg:
		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] dirSizeMsg failed: path=%s err=%v", msg.path, msg.err)
			m.status = fmt.Sprintf("Dir size failed: %v", msg.err)
			return m, nil
		}
		log.Printf("[EVENT] dirSizeMsg: path=%s size=%d", msg.path, msg.size)
		m.applyDirSize(msg.path, msg.size)
		m.status = fmt.Sprintf("Directory size calculated: %s", vfs.HumanSize(msg.size))
		return m, m.loadPreviewCmd()

	case sshConnectMsg:
		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] sshConnectMsg failed: host=%s err=%v", msg.hostName, msg.err)
			m.status = fmt.Sprintf("SSH connection failed: %v", msg.err)
			return m, nil
		}
		if msg.client == nil {
			log.Printf("[ERROR] sshConnectMsg: no client returned for host=%s", msg.hostName)
			m.status = "SSH connection failed: no client returned"
			return m, nil
		}
		pane := m.activePane()
		host := m.ssh.store.FindByName(msg.hostName)
		if host == nil {
			msg.client.Close()
			log.Printf("[ERROR] sshConnectMsg: host %q not found after connection", msg.hostName)
			m.status = fmt.Sprintf("Host %q not found after connection", msg.hostName)
			return m, nil
		}
		pane.PushRemote(RemoteMount{
			Host:       *host,
			RemotePath: "/",
			Client:     msg.client,
			Connected:  true,
		})
		pane.Path = "/"
		log.Printf("[ACTION] SSH connected: host=%s pane=%s remotePath=/", host.DisplayName(), pane.ID)
		if err := m.reloadRemotePane(pane.ID, ""); err != nil {
			log.Printf("[ERROR] reloadRemotePane after SSH: %v", err)
			m.status = fmt.Sprintf("Remote dir: %v", err)
			return m, nil
		}
		if m.ssh != nil {
			m.ssh.connectedHosts[msg.hostName] = true
		}
		m.status = fmt.Sprintf("Connected to %s", host.DisplayName())
		return m, m.loadPreviewCmd()

	case sshAddHostResultMsg:
		m.busy = false
		if m.ssh != nil {
			m.ssh.testingConn = false
		}
		if msg.err != nil {
			log.Printf("[ERROR] sshAddHostResultMsg: connection test failed: %v", msg.err)
			m.status = fmt.Sprintf("Connection failed: %v", msg.err)
			return m, nil
		}

		log.Printf("[ACTION] sshAddHostResultMsg: connection OK, saving host %q", msg.host.Name)
		if m.ssh == nil {
			m.status = "SSH not available"
			return m, nil
		}
		if err := m.ssh.store.AddHost(msg.host); err != nil {
			m.status = fmt.Sprintf("Failed to save host: %v", err)
			return m, nil
		}

		m.status = fmt.Sprintf("Host %q added and verified", msg.host.Name)

		// Refresh the SSH host list if currently on it
		pane := m.activePane()
		if pane.Path == "ssh://" {
			entries := buildSSHHostEntries(m.ssh.store, m.ssh.connectedHosts)
			pane.SetEntries(entries, "")
		}
		return m, nil

	case opMsg:
		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] opMsg failed: kind=%d sourcePath=%s err=%v", msg.kind, msg.sourcePath, msg.err)
			m.status = msg.err.Error()
			return m, nil
		}

		m.modal = modalState{}

		// Capture current selections BEFORE switch, so mkdir/rename can override
		leftSelection := selectedName(&m.left)
		rightSelection := selectedName(&m.right)

		switch msg.kind {
		case opCopy:
			log.Printf("[ACTION] opMsg: Copy done — targetPath=%s", msg.targetPath)
			m.status = fmt.Sprintf("Copied to %s", msg.targetPath)
			m.activePane().ClearMarks()
		case opMove:
			log.Printf("[ACTION] opMsg: Move done — targetPath=%s", msg.targetPath)
			m.status = fmt.Sprintf("Moved to %s", msg.targetPath)
			m.activePane().ClearMarks()
		case opDelete:
			log.Printf("[ACTION] opMsg: Delete done — moved %s to trash", msg.sourcePath)
			m.status = "Moved to trash"
			m.activePane().ClearMarks()
			// Move cursor to the item above if not at the top.
			// If already at the top (cursor == 0), stay at position 0 —
			// the next entry shifts into the deleted item's place.
			active := m.activePane()
			if active.Cursor > 0 {
				active.Cursor--
			}
			if m.active == PaneLeft {
				leftSelection = ""
			} else {
				rightSelection = ""
			}
		case opPermanentDelete:
			log.Printf("[ACTION] opMsg: PermanentDelete done — sourcePath=%s", msg.sourcePath)
			m.status = "Permanently deleted"
			m.activePane().ClearMarks()
			// Move cursor to the item above if not at the top.
			// If already at the top (cursor == 0), stay at position 0 —
			// the next entry shifts into the deleted item's place.
			active := m.activePane()
			if active.Cursor > 0 {
				active.Cursor--
			}
			if m.active == PaneLeft {
				leftSelection = ""
			} else {
				rightSelection = ""
			}
		case opMkdir:
			log.Printf("[ACTION] opMsg: Mkdir done — targetPath=%s", msg.targetPath)
			m.status = fmt.Sprintf("Created %s", msg.targetPath)
			dirName := filepath.Base(msg.targetPath)
			if m.active == PaneLeft {
				leftSelection = dirName
			} else {
				rightSelection = dirName
			}
		case opRename:
			log.Printf("[ACTION] opMsg: Rename done — targetPath=%s", msg.targetPath)
			m.status = fmt.Sprintf("Renamed to %s", filepath.Base(msg.targetPath))
			if msg.targetPath != "" {
				renamed := filepath.Base(msg.targetPath)
				if m.active == PaneLeft {
					leftSelection = renamed
				} else {
					rightSelection = renamed
				}
			}
		case opEdit:
			m.status = "Editor closed"
			return m, tea.Batch(m.loadPreviewCmd(), enableMouseCmd())
		case opView:
			m.status = "Viewer closed"
			return m, enableMouseCmd()
		case opExecute:
			m.status = "Executable closed"
			return m, tea.Batch(m.loadPreviewCmd(), enableMouseCmd())
		}

		// Reload panes — use remote reload for remote mounts
		if m.left.InRemote() {
			_ = m.reloadRemotePane(PaneLeft, leftSelection)
		} else {
			_ = m.reloadPane(PaneLeft, leftSelection)
		}
		if m.right.InRemote() {
			_ = m.reloadRemotePane(PaneRight, rightSelection)
		} else {
			_ = m.reloadPane(PaneRight, rightSelection)
		}

		// For copy/move (including remote SFTP), position cursor on the
		// newly created item in the passive (target) pane.
		if (msg.kind == opCopy || msg.kind == opMove) && msg.targetPath != "" {
			targetName := strings.ToLower(filepath.Base(msg.targetPath))
			passive := m.passivePane()
			if idx := vfs.FindSelected(passive.Entries, targetName); idx >= 0 {
				passive.Cursor = idx
				if passive.Offset > idx {
					passive.Offset = idx
				}
			}
		}

		return m, m.loadPreviewCmd()

	case copyPlanMsg:
		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] copyPlanMsg: err=%v", msg.err)
			m.status = msg.err.Error()
			return m, nil
		}

		remoteInfo := ""
		if msg.srcClient != nil {
			remoteInfo = " source=remote"
		}
		if msg.dstClient != nil {
			remoteInfo += " target=remote"
		}
		log.Printf("[PLAN] copyPlanMsg: kind=%d sources=%d files=%d size=%d targetDir=%s%s",
			msg.kind, len(msg.sourcePaths), msg.stats.FilesTotal, msg.stats.BytesTotal, msg.targetDir, remoteInfo)

		verb := operationVerb(msg.kind)
		title := fmt.Sprintf("%s selected entry?", strings.Title(verb))
		if msg.srcClient != nil || msg.dstClient != nil {
			title = fmt.Sprintf("%s selected entry via SFTP?", strings.Title(verb))
		}
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
			srcClient:       msg.srcClient,
			dstClient:       msg.dstClient,
		})
		return m, nil

	case deletePlanMsg:
		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] deletePlanMsg: err=%v", msg.err)
			m.status = msg.err.Error()
			return m, nil
		}

		log.Printf("[PLAN] deletePlanMsg: kind=%d sources=%d files=%d size=%d remote=%v",
			msg.kind, len(msg.sourcePaths), msg.stats.FilesTotal, msg.stats.BytesTotal, msg.srcClient != nil)

		title := "Move selected entr" + pluralSuffix(len(msg.sourcePaths), "y", "ies") + " to trash?"
		if msg.srcClient != nil {
			title = "Delete selected entr" + pluralSuffix(len(msg.sourcePaths), "y", "ies") + " from remote?"
		} else if m.deleteKind == "permanent" {
			title = "Permanently delete selected entr" + pluralSuffix(len(msg.sourcePaths), "y", "ies") + "?"
		}
		bodyLines := []string{
			fmt.Sprintf("Items: %d", len(msg.sourcePaths)),
			fmt.Sprintf("Files: %d", msg.stats.FilesTotal),
			fmt.Sprintf("Size:  %s", formatSize(msg.stats.BytesTotal, true)),
		}
		note := "confirm-actions"
		if msg.srcClient == nil {
			note = fmt.Sprintf("Mode: %s  (D/d to change)\nEnter / y to confirm, Esc / n to cancel", m.deleteKind)
		}
		m.openConfirmModal(
			title,
			strings.Join(bodyLines, "\n"),
			note,
			pendingOperation{
				kind:        msg.kind,
				sourcePaths: append([]string(nil), msg.sourcePaths...),
				stats:       msg.stats,
				srcClient:   msg.srcClient,
			},
		)
		return m, nil

	case archivePlanMsg:
		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] archivePlanMsg: err=%v", msg.err)
			m.status = msg.err.Error()
			return m, nil
		}

		log.Printf("[PLAN] archivePlanMsg: sources=%d files=%d size=%d targetDir=%s",
			len(msg.sourcePaths), msg.stats.FilesTotal, msg.stats.BytesTotal, msg.targetDir)

		m.archiveFormat = "zip"
		bodyLines := []string{
			fmt.Sprintf("Items: %d", len(msg.sourcePaths)),
			fmt.Sprintf("Files: %d", msg.stats.FilesTotal),
			fmt.Sprintf("Size:  %s", formatSize(msg.stats.BytesTotal, true)),
		}
		m.modal = modalState{
			kind:  modalArchiveType,
			title: "Archive selected files?",
			body:  strings.Join(bodyLines, "\n"),
			note: fmt.Sprintf(
				"Format: %s  (f to change)\nEnter / y to confirm, Esc / n to cancel",
				m.archiveFormat,
			),
			pending: &pendingOperation{
				kind:        opArchive,
				sourcePaths: append([]string(nil), msg.sourcePaths...),
				targetDir:   msg.targetDir,
				stats:       msg.stats,
			},
		}
		return m, nil

	case archiveProgressMsg:
		if m.archiveJob == nil || msg.jobID != m.archiveJob.id {
			return m, nil
		}
		m.archiveJob.progress = msg.progress
		if m.archiveJob.background {
			m.status = formatArchiveStatus(msg.progress)
		}
		log.Printf("[PROGRESS] archive: job=%d files=%d/%d bytes=%d/%d",
			msg.jobID, msg.progress.FilesDone, msg.progress.FilesTotal,
			msg.progress.BytesDone, msg.progress.BytesTotal)
		return m, waitArchiveProgressCmd(m.archiveProgress)

	case archiveDoneMsg:
		if m.archiveJob == nil || msg.jobID != m.archiveJob.id {
			return m, nil
		}

		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] archiveDoneMsg: job=%d kind=%s err=%v", msg.jobID, m.archiveJob.kind, msg.err)
			activeSelection := selectedName(m.activePane())
			_ = m.reloadPane(PaneLeft, activeSelection)
			_ = m.reloadPane(PaneRight, activeSelection)
			if msg.err == context.Canceled {
				switch m.archiveJob.kind {
				case "extract":
					m.status = "Extraction cancelled"
				case "delete":
					m.status = "Delete cancelled"
				default:
					m.status = "Archiving cancelled"
				}
			} else {
				switch m.archiveJob.kind {
				case "extract":
					m.status = fmt.Sprintf("Extraction failed: %v", msg.err)
				case "delete":
					m.status = fmt.Sprintf("Delete failed: %v", msg.err)
				default:
					m.status = fmt.Sprintf("Archiving failed: %v", msg.err)
				}
			}
			m.archiveJob = nil
			if m.modal.kind == modalArchiveProgress {
				m.modal = modalState{}
			}
			return m, m.loadPreviewCmd()
		}

		// Extraction completion — reload only the passive pane
		if m.archiveJob.kind == "extract" {
			log.Printf("[DONE] extractDoneMsg: job=%d targetDir=%s source=%s", msg.jobID, msg.targetPath, msg.sourcePaths[0])
			m.status = fmt.Sprintf("Extracted to %s", msg.targetPath)
			targetID := PaneRight
			if m.active == PaneRight {
				targetID = PaneLeft
			}
			background := m.archiveJob.background
			m.archiveJob = nil
			cmd := m.loadPreviewCmd()
			_ = m.reloadPane(targetID, "")
			if m.modal.kind == modalArchiveProgress {
				m.modal = modalState{}
			}
			if background {
				m.modal = modalState{
					kind:  modalNotice,
					title: "Extraction complete",
					body:  "Archive extracted successfully.",
					note:  "Press Esc to close",
				}
			}
			return m, cmd
		}

		// Delete completion — reload both panes, clear marks
		if m.archiveJob.kind == "delete" {
			log.Printf("[DONE] deleteDoneMsg: job=%d sources=%d", msg.jobID, len(msg.sourcePaths))
			sourceCount := len(m.archiveJob.sourcePaths)
			verb := "moved to trash"
			if m.archiveJob.progress.Stage == "delete permanently" {
				verb = "deleted"
			}
			m.status = fmt.Sprintf("%d entr%s %s", sourceCount, pluralSuffix(sourceCount, "y", "ies"), verb)
			activeSelection := selectedName(m.activePane())
			_ = m.reloadPane(PaneLeft, activeSelection)
			_ = m.reloadPane(PaneRight, activeSelection)
			background := m.archiveJob.background
			m.archiveJob = nil
			m.activePane().ClearMarks()

			cmd := m.loadPreviewCmd()
			if m.modal.kind == modalArchiveProgress {
				m.modal = modalState{}
			}
			if background {
				m.modal = modalState{
					kind:  modalNotice,
					title: "Delete complete",
					body:  fmt.Sprintf("%d entr%s %s.", sourceCount, pluralSuffix(sourceCount, "y", "ies"), verb),
					note:  "Press Esc to close",
				}
			}
			return m, cmd
		}

		// Archive creation completion
		log.Printf("[DONE] archiveDoneMsg: job=%d targetPath=%s sources=%d", msg.jobID, msg.targetPath, len(msg.sourcePaths))
		m.status = fmt.Sprintf("Archived %d entr%s to %s", len(msg.sourcePaths), pluralSuffix(len(msg.sourcePaths), "y", "ies"), msg.targetPath)
		activeSelection := selectedName(m.activePane())
		_ = m.reloadPane(PaneLeft, activeSelection)
		_ = m.reloadPane(PaneRight, activeSelection)
		background := m.archiveJob.background
		sourceCount := len(m.archiveJob.sourcePaths)
		m.archiveJob = nil
		m.activePane().ClearMarks()

		cmd := m.loadPreviewCmd()
		if m.modal.kind == modalArchiveProgress {
			m.modal = modalState{}
		}
		if background {
			doneBody := fmt.Sprintf("%d entr%s archived successfully.", sourceCount, pluralSuffix(sourceCount, "y", "ies"))
			if sourceCount == 1 && len(msg.sourcePaths) == 1 {
				doneBody = filepath.Base(msg.sourcePaths[0]) + " archived successfully."
			}
			m.modal = modalState{
				kind:  modalNotice,
				title: "Archive complete",
				body:  doneBody,
				note:  "Press Esc to close",
			}
		}
		return m, cmd

	case copyProgressMsg:
		if m.copyJob == nil || msg.jobID != m.copyJob.id {
			return m, nil
		}
		m.copyJob.progress = msg.progress
		if m.copyJob.background {
			m.status = formatCopyStatus(m.copyJob.kind, msg.progress)
		}
		log.Printf("[PROGRESS] copy: job=%d kind=%d file=%s files=%d/%d bytes=%d/%d",
			msg.jobID, m.copyJob.kind, msg.progress.CurrentPath,
			msg.progress.FilesDone, msg.progress.FilesTotal,
			msg.progress.BytesDone, msg.progress.BytesTotal)
		return m, waitCopyProgressCmd(m.copyProgress)

	case copyDoneMsg:
		if m.copyJob == nil || msg.jobID != m.copyJob.id {
			return m, nil
		}

		m.busy = false
		if msg.err != nil {
			log.Printf("[ERROR] copyDoneMsg: job=%d kind=%d err=%v targetDir=%s",
				msg.jobID, msg.kind, msg.err, msg.targetDir)
			activeSelection := selectedName(m.activePane())
			if m.left.InRemote() {
				_ = m.reloadRemotePane(PaneLeft, activeSelection)
			} else {
				_ = m.reloadPane(PaneLeft, activeSelection)
			}
			if m.right.InRemote() {
				_ = m.reloadRemotePane(PaneRight, activeSelection)
			} else {
				_ = m.reloadPane(PaneRight, activeSelection)
			}
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

		log.Printf("[DONE] copyDoneMsg: job=%d kind=%d targetDir=%s sources=%d",
			msg.jobID, msg.kind, msg.targetDir, len(msg.sourcePaths))
		m.status = fmt.Sprintf("%s %d entr%s to %s", operationDoneLabel(msg.kind), len(msg.sourcePaths), pluralSuffix(len(msg.sourcePaths), "y", "ies"), msg.targetDir)
		activeSelection := selectedName(m.activePane())
		if m.left.InRemote() {
			_ = m.reloadRemotePane(PaneLeft, activeSelection)
		} else {
			_ = m.reloadPane(PaneLeft, activeSelection)
		}
		if m.right.InRemote() {
			_ = m.reloadRemotePane(PaneRight, activeSelection)
		} else {
			_ = m.reloadPane(PaneRight, activeSelection)
		}

		// Position cursor on the newly created item in the passive (target) pane
		if (msg.kind == opCopy || msg.kind == opMove) && msg.targetPath != "" {
			targetName := strings.ToLower(filepath.Base(msg.targetPath))
			passive := m.passivePane()
			if idx := vfs.FindSelected(passive.Entries, targetName); idx >= 0 {
				passive.Cursor = idx
				if passive.Offset > idx {
					passive.Offset = idx
				}
			}
		}

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

	case dismissYankFlashMsg:
		m.yankFlashLine = -1
		m.syncPreviewContent()
		return m, nil

	case externalOpenMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Open failed: %v", msg.err)
			return m, nil
		}
		m.status = fmt.Sprintf("Opened %s", filepath.Base(msg.path))
		return m, nil

	case tea.KeyMsg:
		msg = translateKeyMsg(msg)
		if msg.String() != "y" {
			m.pendingY = false
		}
		if m.modal.kind != modalNone {
			return m.handleModalKey(msg)
		}
		if m.viewMode {
			if m.previewData.Kind == vfs.PreviewKindText && m.infoMode && !m.selectMode {
				switch {
				case key.Matches(msg, m.keys.Visual):
					return m.toggleVisualMode()
				case msg.String() == "y":
					return m.yankVisualSelection()
				}
			}
			switch {
			case key.Matches(msg, m.keys.Cancel):
				if m.visualMode {
					return m.exitVisualMode("Visual mode: off")
				}
				return m.exitViewMode()
			case key.Matches(msg, m.keys.View), msg.String() == "q":
				return m.exitViewMode()
			case key.Matches(msg, m.keys.Up):
				if m.visualMode {
					m.moveTextCursorLine(-1)
				} else {
					m.previewModel.LineUp(1)
				}
				return m, nil
			case msg.String() == "left":
				if m.visualMode {
					m.moveTextCursorCol(-1)
				}
				return m, nil
			case msg.String() == "h":
				if m.visualMode {
					m.moveTextCursorCol(-1)
				}
				return m, nil
			case key.Matches(msg, m.keys.Down):
				if m.visualMode {
					m.moveTextCursorLine(1)
				} else {
					m.previewModel.LineDown(1)
				}
				return m, nil
			case msg.String() == "right":
				if m.visualMode {
					m.moveTextCursorCol(1)
				}
				return m, nil
			case msg.String() == "l":
				if m.visualMode {
					m.moveTextCursorCol(1)
				}
				return m, nil
			case key.Matches(msg, m.keys.PageUp):
				if m.visualMode {
					m.moveTextCursorLine(-max(m.previewModel.Height-2, 1))
				} else {
					m.previewModel.LineUp(max(m.previewModel.Height-2, 1))
				}
				return m, nil
			case key.Matches(msg, m.keys.PageDown):
				if m.visualMode {
					m.moveTextCursorLine(max(m.previewModel.Height-2, 1))
				} else {
					m.previewModel.LineDown(max(m.previewModel.Height-2, 1))
				}
				return m, nil
			default:
				return m, nil
			}
		}

		if (m.cursorMode || m.visualMode) && m.previewData.Kind == vfs.PreviewKindText {
			switch {
			case key.Matches(msg, m.keys.Caret):
				return m.toggleCaretMode()
			case key.Matches(msg, m.keys.Visual):
				if m.visualMode {
					return m.exitVisualMode("Visual mode: off")
				}
				if m.cursorMode {
					return m.toggleVisualMode()
				}
				return m, nil
			case key.Matches(msg, m.keys.Cancel), msg.String() == "q":
				if m.visualMode {
					return m.exitVisualMode("Visual mode: off")
				}
				return m.exitCaretMode("Caret mode: off")
			case msg.String() == "y":
				if !m.visualMode {
					if m.pendingY {
						m.pendingY = false
						return m.yankCursorLine()
					}
					m.pendingY = true
					m.status = "Press y again to copy current line"
					return m, nil
				}
				return m.yankVisualSelection()
			case key.Matches(msg, m.keys.Up):
				m.moveTextCursorLine(-1)
				return m, nil
			case msg.String() == "left":
				m.moveTextCursorCol(-1)
				return m, nil
			case msg.String() == "h":
				m.moveTextCursorCol(-1)
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.moveTextCursorLine(1)
				return m, nil
			case msg.String() == "right":
				m.moveTextCursorCol(1)
				return m, nil
			case msg.String() == "l":
				m.moveTextCursorCol(1)
				return m, nil
			case key.Matches(msg, m.keys.PageUp):
				m.moveTextCursorLine(-max(m.previewModel.Height-2, 1))
				return m, nil
			case key.Matches(msg, m.keys.PageDown):
				m.moveTextCursorLine(max(m.previewModel.Height-2, 1))
				return m, nil
			case msg.String() == "w":
				m.moveTextCursorWordForward()
				return m, nil
			case msg.String() == "b":
				m.moveTextCursorWordBackward()
				return m, nil
			default:
				return m, nil
			}
		}

		// Copy file path when info mode is open
		if m.infoMode && m.copyPath != "" {
			switch msg.String() {
			case "y", "Y", "ctrl+c":
				if err := clipboard.WriteAll(m.copyPath); err != nil {
					m.status = fmt.Sprintf("Copy path error: %v", err)
				} else {
					m.status = fmt.Sprintf("Path copied: %s", m.copyPath)
				}
				return m, nil
			}
		}

		// Filter mode: route keys to the filter input
		if m.filterMode {
			switch {
			case msg.String() == "esc":
				log.Printf("[KEY] Filter: Esc — clear filter")
				m.filterQuery = ""
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.filterMode = false
				m.status = "Filter cleared"
				return m, nil
			case key.Matches(msg, m.keys.Confirm):
				log.Printf("[KEY] Filter: Enter — query=%s", m.filterQuery)
				m.filterMode = false
				m.filterInput.Blur()
				m.status = fmt.Sprintf("Filter: %s", m.filterQuery)
				return m, nil
			case key.Matches(msg, m.keys.Up):
				m.moveFilteredCursor(-1)
				return m, m.loadPreviewCmd()
			case key.Matches(msg, m.keys.Down):
				m.moveFilteredCursor(1)
				return m, m.loadPreviewCmd()
			case key.Matches(msg, m.keys.PageUp):
				m.moveFilteredCursor(-max(m.bodyHeight()-6, 5))
				return m, m.loadPreviewCmd()
			case key.Matches(msg, m.keys.PageDown):
				m.moveFilteredCursor(max(m.bodyHeight()-6, 5))
				return m, m.loadPreviewCmd()
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				m.filterQuery = m.filterInput.Value()
				m.snapFilterCursor()
				return m, cmd
			}
		}

		// Toggle filter mode — attaches filter to the currently active pane
		if key.Matches(msg, m.keys.Filter) {
			log.Printf("[KEY] Filter toggle — pane=%s", m.active)
			m.filterMode = true
			m.filterPaneID = m.active
			m.filterInput.Focus()
			m.filterInput.SetValue(m.filterQuery)
			m.status = "Filter: type to filter, Enter to confirm, Esc to clear"
			return m, nil
		}

		// Log pane state for every key event
		activePane := m.activePane()
		remotePrefix := ""
		if activePane.InRemote() {
			remotePrefix = " [REMOTE]"
		}
		_ = remotePrefix // used in logs below

		switch {
		case key.Matches(msg, m.keys.Quit):
			log.Printf("[KEY] Quit — exiting application")
			m.saveSession()
			m.cleanupArchiveMounts()
			m.cleanupImageOverlay()
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			log.Printf("[KEY] Help — open help modal")
			m.openHelpModal()
			return m, nil
		case key.Matches(msg, m.keys.Rename):
			log.Printf("[KEY] Rename — active=%s", m.active)
			m.openRenameModal()
			return m, nil
		case key.Matches(msg, m.keys.Cancel), msg.String() == "q":
			if m.filterQuery != "" && m.filterPaneID == m.active {
				log.Printf("[KEY] Esc — clear filter (active=%s)", m.active)
				m.clearFilter()
				m.status = "Filter cleared"
				return m, nil
			}
			if m.infoMode {
				log.Printf("[KEY] Esc/q — close info pane")
				m.infoMode = false
				m.selectMode = false
				m.cursorMode = false
				m.visualMode = false
				m.copyPath = ""
				m.status = "Info pane closed"
				return m, nil
			}
			if len(m.activePane().MarkedEntries()) > 0 {
				log.Printf("[KEY] Esc — clear selection (%d items)", len(m.activePane().MarkedEntries()))
				m.activePane().ClearMarks()
				m.status = "Selection cleared"
				return m, m.loadPreviewCmd()
			}
			return m, nil
		case key.Matches(msg, m.keys.View):
			return m.handleView()
		case key.Matches(msg, m.keys.Caret):
			log.Printf("[KEY] Caret mode toggle")
			return m.toggleCaretMode()
		case key.Matches(msg, m.keys.Visual):
			if m.cursorMode {
				log.Printf("[KEY] Visual mode toggle")
				return m.toggleVisualMode()
			}
			return m, nil
		case key.Matches(msg, m.keys.Archive):
			return m.handleArchive()
		case key.Matches(msg, m.keys.Info):
			log.Printf("[KEY] Info toggle")
			return m.toggleInfo()
		case key.Matches(msg, m.keys.SelectText):
			log.Printf("[KEY] SelectText toggle")
			return m.toggleSelectMode()
		case key.Matches(msg, m.keys.ToggleHidden):
			log.Printf("[KEY] Toggle hidden files")
			return m.toggleHidden()
		case key.Matches(msg, m.keys.CycleTheme):
			log.Printf("[KEY] Cycle theme")
			return m.openThemeSelector()
		case key.Matches(msg, m.keys.CycleSort):
			log.Printf("[KEY] Cycle sort")
			return m.cycleSort()
		case key.Matches(msg, m.keys.Switch):
			nextPane := PaneRight
			if m.active == PaneRight {
				nextPane = PaneLeft
			}
			log.Printf("[KEY] Switch pane — was=%s now=%s", m.active, nextPane)
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
			log.Printf("[KEY] Back (goParent) — pane=%s path=%s", m.active, activePane.Path)
			if err := m.goParent(); err != nil {
				m.status = err.Error()
			}
			return m, m.loadPreviewCmd()
		case key.Matches(msg, m.keys.HistoryBack):
			log.Printf("[KEY] HistoryBack — pane=%s", m.active)
			return m.historyBack()
		case key.Matches(msg, m.keys.HistoryForward):
			log.Printf("[KEY] HistoryForward — pane=%s", m.active)
			return m.historyForward()
		case key.Matches(msg, m.keys.Refresh):
			log.Printf("[KEY] Refresh all panes")
			return m.refreshAllPanes("Refreshed")
		case key.Matches(msg, m.keys.DirSize):
			return m.handleDirSize()
		case key.Matches(msg, m.keys.Copy):
			log.Printf("[KEY] Copy (F5) — active=%s path=%s%s", m.active, activePane.Path, remotePrefix)
			return m.handleTransfer(opCopy)
		case key.Matches(msg, m.keys.Move):
			log.Printf("[KEY] Move (F6) — active=%s path=%s%s", m.active, activePane.Path, remotePrefix)
			return m.handleTransfer(opMove)
		case key.Matches(msg, m.keys.Mkdir):
			log.Printf("[KEY] Mkdir (F7) — active=%s path=%s%s", m.active, activePane.Path, remotePrefix)
			m.openMkdirModal()
			return m, nil
		case key.Matches(msg, m.keys.Delete):
			return m.handleDelete()
		case key.Matches(msg, m.keys.Unpack):
			return m.handleUnpack()
		case key.Matches(msg, m.keys.Mirror):
			return m.handleMirrorPane()
		case key.Matches(msg, m.keys.SSH):
			log.Printf("[KEY] SSH toggle — active=%s path=%s", m.active, activePane.Path)
			return m.handleSSHToggle()
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

	// Filter is sticky: once activated on a pane it stays on that pane
	// even after switching to the other pane with Tab.
	leftPane := m.left
	rightPane := m.right
	if m.filterQuery != "" {
		switch m.filterPaneID {
		case PaneLeft:
			leftPane = m.filteredPane(m.left)
		case PaneRight:
			rightPane = m.filteredPane(m.right)
		}
	}

	var panels string
	if m.viewMode && m.previewData.Kind == vfs.PreviewKindImage {
		panels = lipgloss.NewStyle().
			Width(m.width).
			Height(bodyHeight).
			Background(m.palette.Background).
			Render("")
	} else if m.viewMode && m.previewData.Kind == vfs.PreviewKindText {
		panels = renderSelectionPane(m.previewData, &m.previewModel, m.palette, m.width, bodyHeight)
	} else if m.viewMode {
		panels = renderPreviewPane(m.previewData, &m.previewModel, m.cfg, m.palette, m.width, bodyHeight, m.nerdIcons)
	} else if m.selectMode && m.infoMode {
		panels = renderSelectionPane(m.previewData, &m.previewModel, m.palette, m.width, bodyHeight)
	} else if m.infoMode {
		if m.active == PaneLeft {
			panels = lipgloss.JoinHorizontal(
				lipgloss.Top,
				renderPane(leftPane, m.cfg, m.palette, leftWidth, bodyHeight, true, m.hoverIndexFor(PaneLeft), m.nerdIcons),
				gap,
				renderPreviewPane(m.previewData, &m.previewModel, m.cfg, m.palette, previewWidth, bodyHeight, m.nerdIcons),
			)
		} else {
			panels = lipgloss.JoinHorizontal(
				lipgloss.Top,
				renderPreviewPane(m.previewData, &m.previewModel, m.cfg, m.palette, previewWidth, bodyHeight, m.nerdIcons),
				gap,
				renderPane(rightPane, m.cfg, m.palette, rightWidth, bodyHeight, true, m.hoverIndexFor(PaneRight), m.nerdIcons),
			)
		}
	} else {
		panels = lipgloss.JoinHorizontal(
			lipgloss.Top,
			renderPane(leftPane, m.cfg, m.palette, leftWidth, bodyHeight, m.active == PaneLeft, m.hoverIndexFor(PaneLeft), m.nerdIcons),
			gap,
			renderPane(rightPane, m.cfg, m.palette, rightWidth, bodyHeight, m.active == PaneRight, m.hoverIndexFor(PaneRight), m.nerdIcons),
		)
	}

	parts := make([]string, 0, 3)
	parts = append(parts, panels)
	if m.filterMode {
		parts = append(parts, renderFilterBar(m))
	}
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
		if m.overlay != nil {
			m.overlay.hide()
		}
		modalWidth := min(72, m.width-8)
		if m.modal.kind == modalHelp || m.modal.kind == modalThemeSelect {
			modalWidth = min(96, m.width-8)
		}
		view = overlayCenter(view, renderModal(m, m.palette, modalWidth), m.width)
		return view
	}
	m.syncImageOverlay(leftWidth, previewWidth, bodyHeight)
	return view
}

func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal.kind {
	case modalMkdir, modalRename:
		switch {
		case msg.String() == "esc":
			log.Printf("[MODAL] %d — cancelled (Esc)", m.modal.kind)
			m.modal = modalState{}
			m.status = "Cancelled"
			return m, nil
		case key.Matches(msg, m.keys.Confirm):
			value := strings.TrimSpace(m.modal.input.Value())
			if value == "" {
				if m.modal.kind == modalMkdir {
					m.status = "Directory name must not be empty"
				} else {
					m.status = "Name must not be empty"
				}
				return m, nil
			}
			m.busy = true
			if m.modal.kind == modalMkdir {
				log.Printf("[MODAL] Mkdir confirmed — path=%s name=%s", m.activePane().Path, value)
				return m, m.mkdirCmd(m.activePane().Path, value)
			}
			selected, ok := m.activePane().Selected()
			if !ok || selected.IsParent {
				m.busy = false
				m.modal = modalState{}
				m.status = "No entry selected"
				return m, nil
			}
			log.Printf("[MODAL] Rename confirmed — src=%s new=%s", selected.Path, value)
			return m, renameCmd(selected.Path, value)
		}

		var cmd tea.Cmd
		m.modal.input, cmd = m.modal.input.Update(msg)
		return m, cmd

	case modalConfirm:
		switch {
		case isModalCloseKey(msg, m.keys):
			log.Printf("[MODAL] Confirm — cancelled (Esc)")
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
				// Remote copy/move
				if pending.srcClient != nil || pending.dstClient != nil {
					if m.copyJob != nil {
						m.status = "Transfer is already running"
						return m, nil
					}
					m.busy = true
					sourceIsRemote := pending.srcClient != nil
					targetIsRemote := pending.dstClient != nil
					log.Printf("[MODAL] Confirm — remote %s sources=%d targetDir=%s srcRemote=%v dstRemote=%v",
						operationVerb(pending.kind), len(pending.sourcePaths), pending.targetDir, sourceIsRemote, targetIsRemote)
					return m, m.startRemoteCopyJob(pending.kind, pending.sourcePaths, pending.targetDir,
						sourceIsRemote, targetIsRemote, pending.srcClient, pending.dstClient, pending.stats)
				}
				// Local copy/move
				if m.copyJob != nil {
					m.status = "Transfer is already running"
					return m, nil
				}
				m.busy = true
				log.Printf("[MODAL] Confirm — local %s sources=%d targetDir=%s overwrite=%v stats=%+v",
					operationVerb(pending.kind), len(pending.sourcePaths), pending.targetDir, pending.overwrite, pending.stats)
				return m, m.startCopyJob(pending.kind, pending.sourcePaths, pending.targetDir, pending.overwrite, pending.stats)
			}

			// SSH host delete (no busy state — simple store operation)
			if pending.kind == opDeleteHost {
				hostName := ""
				if len(pending.sourcePaths) > 0 {
					hostName = pending.sourcePaths[0]
				}
				log.Printf("[MODAL] Confirm — SSH host delete: %q", hostName)
				return m.executeSSHHostDelete(hostName)
			}

			// Remote delete
			if pending.srcClient != nil {
				m.busy = true
				log.Printf("[MODAL] Confirm — remote delete sources=%d", len(pending.sourcePaths))
				return m, m.remoteDeleteCmd(pending.sourcePaths, pending.srcClient)
			}

			// Adjust delete kind based on mode toggle
			if pending.kind == opDelete || pending.kind == opPermanentDelete {
				if m.deleteKind == "permanent" && pending.kind == opDelete {
					pending.kind = opPermanentDelete
				} else if m.deleteKind == "trash" && pending.kind == opPermanentDelete {
					pending.kind = opDelete
				}
			}

			// Local delete with progress dialog
			if (pending.kind == opDelete || pending.kind == opPermanentDelete) && pending.srcClient == nil {
				if m.archiveJob != nil {
					m.status = "Delete is already running"
					return m, nil
				}
				log.Printf("[MODAL] Confirm — local %s sources=%d", operationVerb(pending.kind), len(pending.sourcePaths))
				return m, m.startDeleteJob(pending.kind, pending.sourcePaths)
			}

			// Extract archive
			if pending.kind == opExtractArchive {
				if m.archiveJob != nil {
					m.status = "Extraction is already running"
					return m, nil
				}
				log.Printf("[MODAL] Confirm — extract archive source=%s target=%s", pending.sourcePaths[0], pending.targetDir)
				return m, m.startExtractJob(pending.sourcePaths[0], pending.targetDir)
			}

			m.busy = true
			log.Printf("[MODAL] Confirm — %s sources=%d", operationVerb(pending.kind), len(pending.sourcePaths))
			return m, pending.cmd()
		case msg.String() == "d" || msg.String() == "D":
			if m.modal.pending == nil {
				return m, nil
			}
			pkind := m.modal.pending.kind
			if (pkind == opDelete || pkind == opPermanentDelete) && m.modal.pending.srcClient == nil {
				if m.deleteKind == "permanent" {
					m.deleteKind = "trash"
				} else {
					m.deleteKind = "permanent"
				}
				sources := m.modal.pending.sourcePaths
				if m.deleteKind == "permanent" {
					m.modal.title = "Permanently delete selected entr" + pluralSuffix(len(sources), "y", "ies") + "?"
				} else {
					m.modal.title = "Move selected entr" + pluralSuffix(len(sources), "y", "ies") + " to trash?"
				}
				m.modal.note = fmt.Sprintf(
					"Mode: %s  (D/d to change)\nEnter / y to confirm, Esc / n to cancel",
					m.deleteKind,
				)
				return m, nil
			}
		}

	case modalArchiveType:
		switch {
		case isModalCloseKey(msg, m.keys):
			log.Printf("[MODAL] Archive — cancelled (Esc)")
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
			if m.archiveJob != nil {
				m.status = "Archive is already running"
				return m, nil
			}
			m.busy = true
			log.Printf("[MODAL] Archive confirmed — sources=%d targetDir=%s format=%s", len(pending.sourcePaths), pending.targetDir, m.archiveFormat)
			return m, m.startArchiveJob(pending.sourcePaths, pending.targetDir, m.archiveFormat, pending.stats)
		case msg.String() == "f" || msg.String() == "F":
			switch m.archiveFormat {
			case "zip":
				m.archiveFormat = "tar"
			case "tar":
				m.archiveFormat = "tar.gz"
			default:
				m.archiveFormat = "zip"
			}
			m.modal.note = fmt.Sprintf(
				"Format: %s  (F/f to change)\nEnter / y to confirm, Esc / n to cancel",
				m.archiveFormat,
			)
			return m, nil
		}
		return m, nil

	case modalSSHConnect:
		switch {
		case isModalCloseKey(msg, m.keys):
			log.Printf("[MODAL] SSH connect — cancelled (Esc)")
			m.modal = modalState{}
			if m.ssh != nil && m.ssh.testingConn {
				m.ssh.testingConn = false
				if m.ssh.cancelTest != nil {
					m.ssh.cancelTest()
				}
				m.busy = false
			}
			m.status = "Cancelled"
			return m, nil
		case key.Matches(msg, m.keys.Confirm):
			// If a test is already in progress, ignore
			if m.ssh != nil && m.ssh.testingConn {
				return m, nil
			}
			return m.handleSSHAddHostConfirm()
		case msg.String() == "tab":
			m.ssh.cycleInput(1)
			return m, nil
		case msg.String() == "shift+tab":
			m.ssh.cycleInput(-1)
			return m, nil
		case key.Matches(msg, m.keys.Help):
			m.ssh.showHelp = !m.ssh.showHelp
			return m, nil
		case msg.String() == "?":
			m.ssh.showHelp = !m.ssh.showHelp
			return m, nil
		default:
			var cmd tea.Cmd
			m.ssh.inputs[m.ssh.inputFocus], cmd = m.ssh.inputs[m.ssh.inputFocus].Update(msg)
			return m, cmd
		}

	case modalThemeSelect:
		switch {
		case msg.String() == "up" || msg.String() == "k":
			if m.themeSelector == nil {
				return m, nil
			}
			m.themeSelector.cursor--
			if m.themeSelector.cursor < 0 {
				m.themeSelector.cursor = 0
			}
			selected := m.themeSelector.names[m.themeSelector.cursor]
			m.applyThemePreview(selected)
			log.Printf("[THEME] Preview: %s", selected)
			return m, nil
		case msg.String() == "down" || msg.String() == "j":
			if m.themeSelector == nil {
				return m, nil
			}
			m.themeSelector.cursor++
			if m.themeSelector.cursor >= len(m.themeSelector.names) {
				m.themeSelector.cursor = len(m.themeSelector.names) - 1
			}
			selected := m.themeSelector.names[m.themeSelector.cursor]
			m.applyThemePreview(selected)
			log.Printf("[THEME] Preview: %s", selected)
			return m, nil
		case key.Matches(msg, m.keys.Confirm):
			if m.themeSelector == nil {
				return m, nil
			}
			selected := m.themeSelector.names[m.themeSelector.cursor]
			m.finalizeTheme(selected)
			m.themeSelector = nil
			m.modal = modalState{}
			m.status = fmt.Sprintf("Theme: %s", selected)
			log.Printf("[THEME] Applied: %s", selected)
			return m, nil
		case isModalCloseKey(msg, m.keys):
			if m.themeSelector == nil {
				return m, nil
			}
			m.applyThemePreview(m.themeSelector.original)
			m.themeSelector = nil
			m.modal = modalState{}
			m.status = "Theme unchanged"
			log.Printf("[THEME] Reverted to: %s", m.cfg.UI.Theme)
			return m, nil
		}
		return m, nil

	case modalArchiveProgress:
		if key.Matches(msg, m.keys.Background) {
			if m.archiveJob == nil {
				m.modal = modalState{}
				return m, nil
			}
			m.archiveJob.background = true
			m.modal = modalState{}
			m.status = "Archive continues in background"
			log.Printf("[MODAL] ArchiveProgress — moved to background (job=%d)", m.archiveJob.id)
			return m, nil
		}
		if key.Matches(msg, m.keys.ProgressCancel) {
			if m.archiveJob == nil {
				m.modal = modalState{}
				return m, nil
			}
			if m.archiveJob.cancel != nil {
				m.archiveJob.cancel()
			}
			m.status = "Archiving cancelling..."
			log.Printf("[MODAL] ArchiveProgress — cancel requested (job=%d)", m.archiveJob.id)
			return m, nil
		}
		return m, nil

	case modalCopyProgress:
		if key.Matches(msg, m.keys.Background) {
			if m.copyJob == nil {
				m.modal = modalState{}
				return m, nil
			}
			m.copyJob.background = true
			m.modal = modalState{}
			m.status = "Transfer continues in background"
			log.Printf("[MODAL] CopyProgress — background requested (job=%d)", m.copyJob.id)
			return m, nil
		}
		if key.Matches(msg, m.keys.ProgressCancel) {
			if m.copyJob == nil {
				m.modal = modalState{}
				return m, nil
			}
			if m.copyJob.cancel != nil {
				m.copyJob.cancel()
				log.Printf("[MODAL] CopyProgress — cancel requested (job=%d)", m.copyJob.id)
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
		log.Printf("[ERROR] reloadPane: id=%s path=%s err=%v", id, pane.Path, err)
		return err
	}
	pane.SetEntries(entries, strings.ToLower(preserve))
	log.Printf("[PANEL] reloadPane: id=%s path=%s entries=%d preserve=%s", id, pane.Path, len(entries), preserve)
	return nil
}

func (m *Model) refreshAllPanes(status string) (tea.Model, tea.Cmd) {
	leftSelected := selectedName(&m.left)
	rightSelected := selectedName(&m.right)
	log.Printf("[PANEL] refreshAllPanes: left=%s right=%s", leftSelected, rightSelected)
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
	// When a filter query is active on this pane, move through filtered entries
	// only, so the cursor always lands on a matching item.
	if m.filterQuery != "" && m.filterPaneID == m.active {
		m.moveFilteredCursor(delta)
		return
	}
	pane := m.activePane()
	pane.Move(delta, max(m.bodyHeight()-4, 1))
	m.hover = hoverState{}
}

// snapFilterCursor moves the real cursor to the nearest entry matching the
// current filter query. Called after each filter keystroke so the user
// always sees a selected item in the filtered view.
func (m *Model) snapFilterCursor() {
	pane := m.activePane()
	if m.filterQuery == "" || len(pane.Entries) == 0 {
		return
	}
	query := strings.ToLower(m.filterQuery)

	// If current cursor position already matches, keep it.
	if pane.Cursor >= 0 && pane.Cursor < len(pane.Entries) {
		entry := pane.Entries[pane.Cursor]
		if entry.IsParent || strings.Contains(strings.ToLower(entry.DisplayName()), query) {
			return
		}
	}

	// Search forward from cursor.
	for i := pane.Cursor + 1; i < len(pane.Entries); i++ {
		entry := pane.Entries[i]
		if entry.IsParent || strings.Contains(strings.ToLower(entry.DisplayName()), query) {
			pane.Cursor = i
			return
		}
	}

	// Search backward from cursor.
	for i := pane.Cursor - 1; i >= 0; i-- {
		entry := pane.Entries[i]
		if entry.IsParent || strings.Contains(strings.ToLower(entry.DisplayName()), query) {
			pane.Cursor = i
			return
		}
	}
}

// moveFilteredCursor moves the real cursor to the next/prev entry matching
// the current filter query. Used for Up/Down navigation in filter mode.
func (m *Model) moveFilteredCursor(delta int) {
	pane := m.activePane()
	if m.filterQuery == "" || len(pane.Entries) == 0 {
		m.moveCursor(delta)
		return
	}
	query := strings.ToLower(m.filterQuery)
	maxIdx := len(pane.Entries) - 1

	idx := pane.Cursor + delta
	for idx >= 0 && idx <= maxIdx {
		entry := pane.Entries[idx]
		if entry.IsParent || strings.Contains(strings.ToLower(entry.DisplayName()), query) {
			pane.Cursor = idx
			pageSize := max(m.bodyHeight()-4, 1)
			if pane.Cursor < pane.Offset {
				pane.Offset = pane.Cursor
			}
			if pageSize > 0 && pane.Cursor >= pane.Offset+pageSize {
				pane.Offset = pane.Cursor - pageSize + 1
			}
			if pane.Offset < 0 {
				pane.Offset = 0
			}
			m.hover = hoverState{}
			return
		}
		idx += delta
	}
}

// filteredCount returns the number of entries matching the current filter.
func (m *Model) filteredCount(pane *BrowserPane) int {
	if m.filterQuery == "" {
		return len(pane.Entries)
	}
	query := strings.ToLower(m.filterQuery)
	count := 0
	for _, entry := range pane.Entries {
		if strings.Contains(strings.ToLower(entry.DisplayName()), query) {
			count++
		}
	}
	return count
}

// filteredPane returns a copy of the pane with entries filtered by the current query.
// The cursor in the returned copy reflects the position of the real cursor
// within the filtered subset, so Selected() on the original pane still returns
// the correct entry.  Offset is recomputed in filtered-entry space so the
// viewport does not inherit the real-list offset (which would be out of range).
func (m Model) filteredPane(pane BrowserPane) BrowserPane {
	if m.filterQuery == "" {
		return pane
	}
	query := strings.ToLower(m.filterQuery)
	filtered := make([]vfs.Entry, 0, len(pane.Entries))
	filteredCursor := 0
	for i, entry := range pane.Entries {
		if entry.IsParent || strings.Contains(strings.ToLower(entry.DisplayName()), query) {
			if i == pane.Cursor {
				filteredCursor = len(filtered)
			}
			filtered = append(filtered, entry)
		}
	}
	pane.Entries = filtered
	pane.Cursor = filteredCursor
	if pane.Cursor >= len(filtered) {
		pane.Cursor = max(len(filtered)-1, 0)
	}

	// Recompute offset in filtered-entry space.  The source offset is in
	// real-entry-index space and is meaningless for the shorter filtered list.
	pageSize := max(m.bodyHeight()-4, 1)
	offset := 0
	if pane.Cursor >= pageSize {
		offset = pane.Cursor - pageSize + 1
	}
	pane.Offset = offset

	return pane
}

func (m *Model) selectMoveCursor(delta int) {
	pane := m.activePane()
	if selected, ok := pane.Selected(); ok && !selected.IsParent {
		pane.ToggleMarked(selected.Path)
	}
	if m.filterQuery != "" && m.filterPaneID == m.active {
		m.moveFilteredCursor(delta)
	} else {
		pane.Move(delta, max(m.bodyHeight()-4, 1))
	}
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
	// Save cursor position for the current directory before navigating away.
	pane.SaveCursor(pane.Path, selected.Name)
	// Save current directory to history before navigating.
	pane.PushHistory(pane.Path)
	log.Printf("[NAV] enterSelected — pane=%s from=%s to=%s", pane.ID, pane.Path, selected.Path)
	pane.Path = selected.Path
	pane.Cursor = 0
	pane.Offset = 0
	// Restore cursor position if we've visited this directory before in this session.
	preserve := pane.LoadCursor(pane.Path)
	if err := m.reloadPane(pane.ID, preserve); err != nil {
		return err
	}
	m.status = fmt.Sprintf("Entered %s", pane.Path)
	return nil
}

func (m *Model) clearFilter() {
	m.filterQuery = ""
	m.filterInput.SetValue("")
	m.filterInput.Blur()
	m.filterMode = false
	m.filterPaneID = ""
}

func (m *Model) handleOpenSelected() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok {
		log.Printf("[ACTION] OpenSelected — nothing selected")
		return m, nil
	}

	log.Printf("[ACTION] OpenSelected — entry=%s isDir=%v isRemote=%v isArchive=%v category=%s",
		selected.Path, selected.IsDir, selected.IsRemote, isArchiveEntry(selected), selected.Category())

	// Navigating up via ".." — use goParent which preserves the cursor
	// position on the directory/archive we came from (by finding its name
	// in the parent listing via FindSelected). This applies both inside
	// archive mounts (where pane.Path must stay within the temp mount)
	// and regular directories (for consistent cursor placement).
	if selected.IsParent {
		if err := m.goParent(); err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m, m.loadPreviewCmd()
	}

	// SSH host entry — connect to remote host
	if selected.IsRemote && !isRemoteAddHostEntry(selected) {
		log.Printf("[ACTION] OpenSelected — SSH connect to host=%s", selected.RemoteHostName)
		return m.handleSSHConnectHost()
	}
	// "Add host" entry — open connect dialog
	if isRemoteAddHostEntry(selected) {
		m.handleSSHOpenAddHost()
		return m, nil
	}

	if selected.IsDir {
		if m.activePane().InRemote() {
			return m.enterRemoteDir(selected)
		}
		if err := m.enterSelected(); err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m, m.loadPreviewCmd()
	}

	if isArchiveEntry(selected) {
		if err := m.enterArchive(selected); err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m, m.loadPreviewCmd()
	}

	switch selected.Category() {
	case "text", "config":
		return m.handleEdit()
	case "executable":
		return m.handleExecute(selected)
	default:
		return m.handleOpenExternal()
	}
}

func (m *Model) goParent() error {
	m.hover = hoverState{}
	pane := m.activePane()
	log.Printf("[NAV] goParent — pane=%s path=%s inRemote=%v inArchive=%v", pane.ID, pane.Path, pane.InRemote(), pane.InArchive())

	// SSH host list — close it and restore previous directory
	if pane.Path == "ssh://" {
		if prevPath, ok := pane.PopHistory(); ok {
			log.Printf("[NAV] goParent — closing SSH host list, restoring path=%s", prevPath)
			pane.Path = prevPath
			return m.reloadPane(pane.ID, "")
		}
		// No history — just clear the host list
		pane.Path = "/"
		return m.reloadPane(pane.ID, "")
	}

	// Remote mount — pop back to host list or go up one level
	if mount, ok := pane.CurrentRemote(); ok {
		current := pane.Path
		if current == "/" || current == "" {
			// At remote root — pop remote, disconnect, and return to host list
			log.Printf("[NAV] goParent — closing remote mount, returning to host list")
			pane.PopRemote()
			mount.Client.Close()
			if m.ssh != nil {
				m.ssh.connectedHosts[mount.Host.Name] = false
				entries := buildSSHHostEntries(m.ssh.store, m.ssh.connectedHosts)
				pane.SetEntries(entries, "")
				pane.Path = "ssh://"
				m.status = "SSH host list"
			}
			return nil
		}
		// Inside remote subdirectory — go up one level
		if selected, ok := pane.Selected(); ok && !selected.IsParent {
			pane.SaveCursor(pane.Path, selected.Name)
		}
		pane.PushHistory(pane.Path)
		parent := path.Dir(current)
		if parent == "." {
			parent = "/"
		}
		log.Printf("[NAV] goParent — remote: %s -> %s", current, parent)
		pane.Path = parent
		return m.reloadRemotePane(pane.ID, path.Base(current))
	}

	if mount, ok := pane.CurrentArchive(); ok {
		root := filepath.Clean(mount.RootPath)
		current := filepath.Clean(pane.Path)
		if current == root {
			// At archive root — pop archive and return to the directory containing it
			log.Printf("[NAV] goParent — closing archive %s", mount.SourcePath)
			pane.PushHistory(pane.Path)
			if _, popped := pane.PopArchive(); popped {
				_ = os.RemoveAll(mount.TempDir)
			}
			pane.Path = mount.ParentPath
			if err := m.reloadPane(pane.ID, filepath.Base(mount.SourcePath)); err != nil {
				return err
			}
			m.status = fmt.Sprintf("Closed archive %s", filepath.Base(mount.SourcePath))
			return nil
		}
		// Inside archive subdirectory — go up one level within the archive
		pane.PushHistory(pane.Path)
		parent := filepath.Dir(current)
		log.Printf("[NAV] goParent — archive: %s -> %s", current, parent)
		pane.Path = parent
		if err := m.reloadPane(pane.ID, filepath.Base(current)); err != nil {
			return err
		}
		m.status = fmt.Sprintf("Moved to %s", parent)
		return nil
	}

	// Save cursor position before leaving this directory.
	if selected, ok := pane.Selected(); ok && !selected.IsParent {
		pane.SaveCursor(pane.Path, selected.Name)
	}
	parent := filepath.Dir(pane.Path)
	if parent == pane.Path {
		return nil
	}
	pane.PushHistory(pane.Path)
	currentName := filepath.Base(pane.Path)
	log.Printf("[NAV] goParent — local: %s -> %s", pane.Path, parent)
	pane.Path = parent
	if err := m.reloadPane(pane.ID, currentName); err != nil {
		return err
	}
	m.status = fmt.Sprintf("Moved to %s", parent)
	return nil
}

// historyBack navigates the active pane to the previous directory in its history.
func (m *Model) historyBack() (tea.Model, tea.Cmd) {
	pane := m.activePane()
	prevPath, ok := pane.PopHistory()
	if !ok {
		m.status = "No directory history"
		return m, nil
	}
	// Save cursor position for the current directory before navigating away.
	if selected, ok := pane.Selected(); ok && !selected.IsParent {
		pane.SaveCursor(pane.Path, selected.Name)
	}
	// Save current path to forward-stack so forward navigation can restore it.
	pane.PushFuture(pane.Path)
	log.Printf("[NAV] historyBack — pane=%s cur=%s prev=%s", pane.ID, pane.Path, prevPath)
	pane.Path = prevPath
	pane.Cursor = 0
	pane.Offset = 0
	// Restore cursor position if we've visited this directory before in this session.
	preserve := pane.LoadCursor(pane.Path)
	if err := m.reloadPane(pane.ID, preserve); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("History back to %s", prevPath)
	return m, m.loadPreviewCmd()
}

// historyForward navigates the active pane to the next directory in its forward-stack.
func (m *Model) historyForward() (tea.Model, tea.Cmd) {
	pane := m.activePane()
	nextPath, ok := pane.PopFuture()
	if !ok {
		m.status = "No forward history"
		return m, nil
	}
	// Save cursor position for the current directory before navigating away.
	if selected, ok := pane.Selected(); ok && !selected.IsParent {
		pane.SaveCursor(pane.Path, selected.Name)
	}
	// Save current path to back-stack so back navigation can restore it.
	pane.PushHistory(pane.Path)
	log.Printf("[NAV] historyForward — pane=%s cur=%s next=%s", pane.ID, pane.Path, nextPath)
	pane.Path = nextPath
	pane.Cursor = 0
	pane.Offset = 0
	// Restore cursor position if we've visited this directory before in this session.
	preserve := pane.LoadCursor(pane.Path)
	if err := m.reloadPane(pane.ID, preserve); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("History forward to %s", nextPath)
	return m, m.loadPreviewCmd()
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

	// Remote preview: read file via SFTP and build a text preview
	if mount, ok := m.activePane().CurrentRemote(); ok {
		return func() tea.Msg {
			rc, err := mount.Client.ReadFile(selected.Path)
			if err != nil {
				return previewMsg{
					entryPath: selected.Path,
					preview: vfs.Preview{
						Kind:  vfs.PreviewKindError,
						Title: selected.DisplayName(),
						Body:  fmt.Sprintf("Could not read remote file:\n\n%s", err),
					},
				}
			}
			defer rc.Close()
			maxBytes := int64(m.cfg.Preview.MaxPreviewBytes)
			limited := io.LimitReader(rc, maxBytes)
			raw, readErr := io.ReadAll(limited)
			if readErr != nil {
				return previewMsg{
					entryPath: selected.Path,
					preview: vfs.Preview{
						Kind:  vfs.PreviewKindError,
						Title: selected.DisplayName(),
						Body:  fmt.Sprintf("Could not read remote file:\n\n%s", readErr),
					},
				}
			}
			body := string(raw)
			if int64(len(raw)) >= maxBytes {
				body += "\n\n[... truncated ...]"
			}
			return previewMsg{
				entryPath: selected.Path,
				preview: vfs.Preview{
					Kind:      vfs.PreviewKindText,
					Title:     selected.DisplayName(),
					Body:      body,
					PlainBody: body,
					Metadata: vfs.Metadata{
						Path:        selected.Path,
						Size:        selected.Size,
						SizeKnown:   true,
						Extension:   selected.Extension,
						Permissions: vfs.Permissions(selected.Mode),
						ModifiedAt:  vfs.ShortTime(selected.ModifiedAt),
					},
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
		UseNerdIcons:          m.nerdIcons,
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

	log.Printf("[ACTION] DirSize: path=%s pane=%s", selected.Path, m.active)
	m.busy = true
	m.status = fmt.Sprintf("Calculating directory size for %s", selected.DisplayName())
	if mount, ok := m.activePane().CurrentRemote(); ok {
		return m, remoteDirSizeCmd(mount.Client, selected.Path)
	}
	return m, dirSizeCmd(selected.Path)
}

func (m *Model) handleTransfer(kind fileOpKind) (tea.Model, tea.Cmd) {
	if m.activePane().InArchive() && kind != opCopy {
		log.Printf("[SKIP] Transfer: archive read-only, kind=%d", kind)
		m.status = "Archive mode is read-only; only copy is allowed"
		return m, nil
	}

	sources := m.operationSources()
	if len(sources) == 0 {
		log.Printf("[SKIP] Transfer: no sources, kind=%d", kind)
		m.status = fmt.Sprintf("Nothing to %s", operationVerb(kind))
		return m, nil
	}

	srcPane := m.activePane()
	dstPane := m.passivePane()
	targetDir := dstPane.Path

	srcRemote, srcHasRemote := srcPane.CurrentRemote()
	dstRemote, dstHasRemote := dstPane.CurrentRemote()

	log.Printf("[ACTION] Transfer: kind=%d active=%s srcPath=%s dstPath=%s srcRemote=%v dstRemote=%v sources=%v",
		kind, m.active, srcPane.Path, dstPane.Path, srcHasRemote, dstHasRemote, sources)

	// Check for existing targets (fast — one Stat per top-level item)
	// Skip when target is remote — local fs check doesn't apply.
	existingTargets := 0
	if !dstHasRemote {
		for _, sourcePath := range sources {
			targetPath := filepath.Join(targetDir, filepath.Base(sourcePath))
			exists, err := vfs.PathExists(targetPath)
			if err != nil {
				log.Printf("[ERROR] Transfer: path check failed: %s err=%v", targetPath, err)
				m.status = err.Error()
				return m, nil
			}
			if exists {
				existingTargets++
			}
		}
	}
	overwrite := existingTargets > 0
	if existingTargets > 0 && !m.cfg.Behavior.ConfirmOverwrite {
		overwrite = true
	}

	// Show confirm dialog immediately (no plan — skip walking & counting files)
	verb := strings.Title(operationVerb(kind))
	title := fmt.Sprintf("%s selected entry?", verb)
	if srcHasRemote || dstHasRemote {
		title = fmt.Sprintf("%s selected entry via SFTP?", verb)
	}
	bodyLines := []string{
		fmt.Sprintf("Items: %d", len(sources)),
		fmt.Sprintf("Source: %s", srcPane.Path),
		fmt.Sprintf("Target: %s", targetDir),
	}
	if existingTargets > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("Overwrite: %d existing target(s)", existingTargets))
	}
	if srcHasRemote || dstHasRemote {
		bodyLines = append(bodyLines, "")
		if srcHasRemote {
			hostName := srcRemote.Host.Name
			bodyLines = append(bodyLines, fmt.Sprintf("Source host: %s", hostName))
		}
		if dstHasRemote {
			hostName := dstRemote.Host.Name
			bodyLines = append(bodyLines, fmt.Sprintf("Target host: %s", hostName))
		}
	}
	m.openConfirmModal(title, strings.Join(bodyLines, "\n"), "confirm-actions", pendingOperation{
		kind:            kind,
		sourcePaths:     append([]string(nil), sources...),
		targetDir:       targetDir,
		overwrite:       overwrite,
		existingTargets: existingTargets,
		srcClient:       srcRemote.Client,
		dstClient:       dstRemote.Client,
	})
	return m, nil
}

func (m *Model) handleArchive() (tea.Model, tea.Cmd) {
	sources := m.operationSources()
	if len(sources) == 0 {
		log.Printf("[SKIP] Archive: no sources")
		m.status = "Nothing to archive"
		return m, nil
	}

	if m.archiveJob != nil {
		log.Printf("[SKIP] Archive: already running")
		m.status = "Archive is already running"
		return m, nil
	}

	log.Printf("[ACTION] Archive: sources=%d targetDir=%s", len(sources), m.passivePane().Path)
	m.busy = true
	m.status = "Calculating archive size"
	return m, archivePlanCmd(sources, m.passivePane().Path)
}

func (m *Model) handleDelete() (tea.Model, tea.Cmd) {
	if m.activePane().InArchive() {
		log.Printf("[SKIP] Delete: archive read-only")
		m.status = "Archive mode is read-only; delete is disabled"
		return m, nil
	}

	// SSH host list — delete user-added hosts
	if m.activePane().Path == "ssh://" {
		return m.handleSSHHostDelete()
	}

	sources := m.operationSources()
	if len(sources) == 0 {
		log.Printf("[SKIP] Delete: no sources")
		m.status = "Nothing to delete"
		return m, nil
	}

	log.Printf("[ACTION] Delete: sources=%v remote=%v", sources, m.activePane().InRemote())
	m.busy = false

	// Remote delete via SFTP (no trash on remote — always permanent)
	if mount, ok := m.activePane().CurrentRemote(); ok {
		log.Printf("[ACTION] Delete: remote — sources=%v", sources)
		title := "Delete selected entr" + pluralSuffix(len(sources), "y", "ies") + " from remote?"
		body := fmt.Sprintf("Items: %d", len(sources))
		note := "Enter / y to confirm, Esc / n to cancel"
		m.openConfirmModal(title, body, note, pendingOperation{
			kind:        opDelete,
			sourcePaths: append([]string(nil), sources...),
			srcClient:   mount.Client,
		})
		return m, nil
	}

	// Default to permanent delete
	m.deleteKind = "permanent"

	// Show confirm dialog with mode toggle (no plan — skip counting files)
	title := "Permanently delete selected entr" + pluralSuffix(len(sources), "y", "ies") + "?"
	body := fmt.Sprintf("Items: %d", len(sources))
	note := fmt.Sprintf("Mode: %s  (D/d to change)\nEnter / y to confirm, Esc / n to cancel", m.deleteKind)
	m.openConfirmModal(title, body, note, pendingOperation{
		kind:        opDelete,
		sourcePaths: append([]string(nil), sources...),
	})
	return m, nil
}

func (m *Model) handleUnpack() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok || !isArchiveEntry(selected) {
		log.Printf("[SKIP] Unpack: no archive selected")
		m.status = "Select an archive file to unpack"
		return m, nil
	}

	// Target is the opposite pane's current directory
	targetPane := m.passivePane()
	targetDir := targetPane.Path

	log.Printf("[ACTION] Unpack: source=%s target=%s", selected.Path, targetDir)

	// Show confirm dialog before extracting
	title := fmt.Sprintf("Unpack %s?", selected.DisplayName())
	body := fmt.Sprintf("Archive: %s\nTarget:  %s", selected.Path, targetDir)
	note := "Enter / y to confirm, Esc / n to cancel"
	pending := pendingOperation{
		kind:        opExtractArchive,
		sourcePaths: []string{selected.Path},
		targetDir:   targetDir,
	}
	m.openConfirmModal(title, body, note, pending)
	return m, nil
}

func (m *Model) handleMirrorPane() (tea.Model, tea.Cmd) {
	active := m.activePane()
	passive := m.passivePane()

	// Determine the target path from the active pane
	targetPath := active.Path
	log.Printf("[ACTION] MirrorPane — active=%s path=%s inRemote=%v inArchive=%v", m.active, targetPath, active.InRemote(), active.InArchive())

	// If active is in a remote mount, clone the remote stack to passive
	if mount, ok := active.CurrentRemote(); ok {
		// If passive already has a remote connection, clean it up first
		if existingMount, exists := passive.CurrentRemote(); exists {
			existingMount.Client.Close()
			passive.PopRemote()
		}
		// Clone the mount — don't clone the client, open a new connection
		// For simplicity, just set the path; the mount will reconnect on reload
		passive.ClearRemotes()
		passive.Remote = append(passive.Remote, mount)
		passive.Path = active.Path
		if err := m.reloadRemotePane(passive.ID, ""); err != nil {
			m.status = fmt.Sprintf("Mirror failed: %v", err)
			return m, nil
		}
	} else if active.InArchive() {
		// Archive mounts: set path directly, archive data stays on active pane
		passive.Path = targetPath
		if err := m.reloadPane(passive.ID, ""); err != nil {
			m.status = fmt.Sprintf("Mirror failed: %v", err)
			return m, nil
		}
	} else {
		// Local directory — simple path copy
		passive.ClearRemotes()
		passive.Path = targetPath
		if err := m.reloadPane(passive.ID, ""); err != nil {
			m.status = fmt.Sprintf("Mirror failed: %v", err)
			return m, nil
		}
	}

	m.status = fmt.Sprintf("Mirrored: %s", targetPath)
	return m, m.loadPreviewCmd()
}

func (m *Model) handleView() (tea.Model, tea.Cmd) {
	selected, ok := m.activePane().Selected()
	if !ok || selected.IsParent || selected.IsDir {
		log.Printf("[SKIP] View: no valid selection (ok=%v isParent=%v isDir=%v)", ok, selected.IsParent, selected.IsDir)
		m.status = "Select a file to view"
		return m, nil
	}
	if m.viewMode {
		log.Printf("[ACTION] View: exit view mode")
		return m.exitViewMode()
	}

	log.Printf("[ACTION] View: file=%s", selected.Path)
	m.viewPrevInfo = m.infoMode
	m.infoMode = true
	m.selectMode = m.previewData.Kind == vfs.PreviewKindText
	m.visualMode = false
	m.viewMode = true
	m.resizePreview()
	m.syncPreviewContent()
	m.status = "View mode: F3/Esc/q to close"
	if m.selectMode {
		return m, tea.Batch(m.loadPreviewCmd(), disableMouseCmd())
	}
	return m, tea.Batch(m.loadPreviewCmd(), enableMouseCmd())
}

func (m *Model) exitViewMode() (tea.Model, tea.Cmd) {
	if !m.viewMode {
		return m, nil
	}
	log.Printf("[ACTION] exitViewMode")
	m.viewMode = false
	m.selectMode = false
	m.visualMode = false
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

	log.Printf("[ACTION] OpenExternal: file=%s opener=%s", selected.Path, name)
	m.cleanupImageOverlay()
	m.status = fmt.Sprintf("Opening %s with %s", selected.DisplayName(), name)
	return m, startExternalOpenCmd(command, selected.Path)
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

	log.Printf("[ACTION] Edit: file=%s editor=%s command=%v", selected.Path, name, command)
	m.cleanupImageOverlay()
	m.status = fmt.Sprintf("Opening %s with %s", selected.DisplayName(), name)
	return m, tea.ExecProcess(command, func(err error) tea.Msg {
		return opMsg{kind: opEdit, sourcePath: selected.Path, err: err}
	})
}

func (m *Model) handleExecute(entry vfs.Entry) (tea.Model, tea.Cmd) {
	log.Printf("[ACTION] Execute: path=%s", entry.Path)
	m.cleanupImageOverlay()
	cmd := exec.Command(entry.Path)
	cmd.Dir = filepath.Dir(entry.Path)
	m.status = fmt.Sprintf("Executing %s", entry.DisplayName())
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return opMsg{kind: opExecute, sourcePath: entry.Path, err: err}
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
		log.Printf("[MOUSE] Left click at (%d,%d)", msg.X, msg.Y)
		if m.copyPath != "" && m.mouseOverPathLine(msg.X, msg.Y) {
			if err := clipboard.WriteAll(m.copyPath); err != nil {
				m.status = fmt.Sprintf("Copy path error: %v", err)
			} else {
				m.status = fmt.Sprintf("Path copied: %s", m.copyPath)
			}
			return m, nil
		}

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
			m.cursorMode = false
			m.visualMode = false
			m.resizePreview()
			m.syncPreviewContent()
			m.copyPath = ""
			m.status = "Info mode: off"
			return m, nil
		}

		paneID, index, ok := m.mouseTarget(msg.X, msg.Y)
		if !ok {
			return m, nil
		}

		if m.infoMode && paneID == m.active && index == m.activePane().Cursor {
			m.infoMode = false
			m.cursorMode = false
			m.visualMode = false
			m.hover = hoverState{pane: paneID, index: index, ok: true}
			m.copyPath = ""
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
	log.Printf("[TOGGLE] Info mode: %v (active=%s)", m.infoMode, m.active)
	m.resizePreview()
	m.syncPreviewContent()
	if m.infoMode {
		m.status = fmt.Sprintf("Info mode: %s selection", strings.ToUpper(string(m.active)))
		return m, m.loadPreviewCmd()
	}
	wasSelect := m.selectMode
	if m.selectMode {
		m.selectMode = false
	}
	if m.cursorMode {
		m.cursorMode = false
	}
	if m.visualMode {
		m.visualMode = false
	}
	if wasSelect {
		return m, enableMouseCmd()
	}
	m.status = "Info mode: off"
	return m, nil
}

func (m *Model) toggleSelectMode() (tea.Model, tea.Cmd) {
	if m.viewMode {
		m.status = "Use v/y in F3 view for keyboard selection"
		return m, nil
	}
	if m.selectMode {
		m.selectMode = false
		log.Printf("[TOGGLE] Select mode: off")
		m.status = "Text selection mode: off"
		return m, enableMouseCmd()
	}
	if !m.infoMode || m.previewData.Kind != vfs.PreviewKindText {
		m.status = "Text selection mode works only for text preview in info pane"
		return m, nil
	}
	m.selectMode = true
	log.Printf("[TOGGLE] Select mode: on")
	m.status = "Text selection mode: on"
	return m, disableMouseCmd()
}

func (m *Model) toggleCaretMode() (tea.Model, tea.Cmd) {
	if m.viewMode {
		m.status = "F3 uses plain text mouse selection"
		return m, nil
	}
	if m.selectMode {
		m.status = "Disable Ctrl+T mouse selection first"
		return m, nil
	}
	if !m.infoMode || m.previewData.Kind != vfs.PreviewKindText {
		m.status = "Caret mode works only for text preview in info pane"
		return m, nil
	}
	if m.cursorMode {
		log.Printf("[TOGGLE] Caret mode: off")
		return m.exitCaretMode("Caret mode: off")
	}

	lineCount := len(m.previewPlainLines())
	if lineCount == 0 {
		m.status = "Nothing to navigate"
		return m, nil
	}
	m.cursorMode = true
	m.cursorLine = clamp(m.previewModel.YOffset, 0, lineCount-1)
	m.cursorCol = clamp(m.cursorCol, 0, m.lineRuneCount(m.cursorLine))
	m.visualMode = false
	m.ensureTextCursorVisible()
	m.syncPreviewContent()
	log.Printf("[TOGGLE] Caret mode: on (line=%d col=%d)", m.cursorLine, m.cursorCol)
	m.status = "Caret mode: h/j/k/l move, v select, Esc exit"
	return m, nil
}

func (m *Model) exitCaretMode(status string) (tea.Model, tea.Cmd) {
	if !m.cursorMode {
		m.status = status
		return m, nil
	}
	log.Printf("[TOGGLE] exitCaretMode: %s", status)
	m.cursorMode = false
	m.visualMode = false
	m.syncPreviewContent()
	m.status = status
	return m, nil
}

func (m *Model) toggleVisualMode() (tea.Model, tea.Cmd) {
	if m.viewMode {
		m.status = "F3 uses plain text mouse selection; visual mode is for info pane"
		return m, nil
	}
	if m.selectMode {
		m.status = "Disable Ctrl+T mouse selection first"
		return m, nil
	}
	if !m.infoMode || m.previewData.Kind != vfs.PreviewKindText {
		m.status = "Visual mode works only for text preview"
		return m, nil
	}
	if m.visualMode {
		log.Printf("[TOGGLE] Visual mode: off")
		return m.exitVisualMode("Visual mode: off")
	}

	lineCount := len(m.previewPlainLines())
	if lineCount == 0 {
		m.status = "Nothing to select"
		return m, nil
	}
	start := clamp(m.previewModel.YOffset, 0, lineCount-1)
	if m.cursorMode {
		start = clamp(m.cursorLine, 0, lineCount-1)
	} else {
		m.cursorMode = true
		m.cursorLine = start
		m.cursorCol = 0
	}
	m.visualMode = true
	m.visualAnchor = start
	m.visualAnchorCol = m.cursorCol
	m.ensureTextCursorVisible()
	m.syncPreviewContent()
	log.Printf("[TOGGLE] Visual mode: on (anchor=%d col=%d)", m.visualAnchor, m.visualAnchorCol)
	m.status = "Visual mode: h/j/k/l move, y copy, Esc exit"
	return m, nil
}

func (m *Model) exitVisualMode(status string) (tea.Model, tea.Cmd) {
	if !m.visualMode {
		m.status = status
		return m, nil
	}
	log.Printf("[TOGGLE] exitVisualMode: %s", status)
	m.visualMode = false
	m.syncPreviewContent()
	m.status = status
	return m, nil
}

func (m *Model) toggleHidden() (tea.Model, tea.Cmd) {
	m.cfg.Browser.ShowHidden = !m.cfg.Browser.ShowHidden
	log.Printf("[TOGGLE] ShowHidden: %v", m.cfg.Browser.ShowHidden)
	return m.refreshAllPanes(fmt.Sprintf("Show hidden: %t", m.cfg.Browser.ShowHidden))
}

func (m *Model) cycleTheme() (tea.Model, tea.Cmd) {
	next := theme.Next(m.cfg.UI.Theme)
	palette, err := theme.Resolve(next)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	log.Printf("[TOGGLE] CycleTheme: %s -> %s", m.cfg.UI.Theme, next)
	m.cfg.UI.Theme = next
	m.palette = palette
	savedPath, saveErr := config.Save(m.cfg, m.configPath)
	if saveErr != nil {
		m.status = fmt.Sprintf("Theme: %s (save failed: %v)", next, saveErr)
		return m, nil
	}
	m.configPath = savedPath
	m.status = fmt.Sprintf("Theme: %s (saved)", next)
	return m, nil
}

func (m *Model) openThemeSelector() (tea.Model, tea.Cmd) {
	names := theme.Names()
	current := m.cfg.UI.Theme
	cursor := 0
	for i, name := range names {
		if name == current {
			cursor = i
			break
		}
	}
	m.themeSelector = &themeSelectorState{
		names:    names,
		cursor:   cursor,
		original: current,
	}
	m.modal = modalState{
		kind: modalThemeSelect,
	}
	log.Printf("[THEME] Theme selector opened — current=%s cursor=%d", current, cursor)
	return m, nil
}

func (m *Model) applyThemePreview(name string) {
	palette, err := theme.Resolve(name)
	if err != nil {
		return
	}
	m.palette = palette
}

func (m *Model) finalizeTheme(name string) {
	palette, err := theme.Resolve(name)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.cfg.UI.Theme = name
	m.palette = palette
	savedPath, saveErr := config.Save(m.cfg, m.configPath)
	if saveErr != nil {
		m.status = fmt.Sprintf("Theme: %s (save failed: %v)", name, saveErr)
		return
	}
	m.configPath = savedPath
}

func copyTextToClipboard(text string) error {
	if err := clipboard.WriteAll(text); err == nil {
		return nil
	}
	_, err := fmt.Fprint(os.Stderr, ansi.SetSystemClipboard(text))
	return err
}

func (m *Model) yankVisualSelection() (tea.Model, tea.Cmd) {
	if !m.visualMode || m.previewData.Kind != vfs.PreviewKindText {
		m.status = "Visual mode is not active"
		return m, nil
	}

	lines := m.previewPlainLines()
	if len(lines) == 0 {
		return m.exitVisualMode("Nothing to copy")
	}
	startLine, startCol, endLine, endCol := m.visualSelectionBounds()
	startLine = clamp(startLine, 0, len(lines)-1)
	endLine = clamp(endLine, 0, len(lines)-1)

	parts := make([]string, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		raw := lines[line]
		lineStart := 0
		lineEnd := len([]rune(raw))
		if line == startLine {
			lineStart = clamp(startCol, 0, lineEnd)
		}
		if line == endLine {
			lineEnd = clamp(endCol, lineStart, len([]rune(raw)))
		}
		parts = append(parts, sliceRunes(raw, lineStart, lineEnd))
	}
	text := strings.Join(parts, "\n")
	if err := copyTextToClipboard(text); err != nil {
		m.status = fmt.Sprintf("Copy failed: %v", err)
		return m, nil
	}
	return m.exitVisualMode("Copied selection")
}

func (m *Model) yankCursorLine() (tea.Model, tea.Cmd) {
	if !m.cursorMode || m.previewData.Kind != vfs.PreviewKindText {
		m.status = "Caret mode is not active"
		return m, nil
	}
	lines := m.previewPlainLines()
	if len(lines) == 0 {
		m.status = "Nothing to copy"
		return m, nil
	}
	line := clamp(m.cursorLine, 0, len(lines)-1)
	if err := copyTextToClipboard(lines[line]); err != nil {
		m.status = fmt.Sprintf("Copy failed: %v", err)
		return m, nil
	}
	m.yankFlashLine = line
	m.syncPreviewContent()
	m.status = "Copied current line"
	return m, dismissYankFlashCmd(140 * time.Millisecond)
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
	log.Printf("[TOGGLE] CycleSort: %s -> %s", current, next)
	m.cfg.Browser.Sort.By = next
	return m.refreshAllPanes(fmt.Sprintf("Sort: %s", next))
}

func (m *Model) openMkdirModal() {
	if m.activePane().InArchive() {
		m.status = "Archive mode is read-only; create directory is disabled"
		return
	}

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

func (m *Model) openRenameModal() {
	if m.activePane().InArchive() {
		m.status = "Archive mode is read-only; rename is disabled"
		return
	}

	selected, ok := m.activePane().Selected()
	if !ok || selected.IsParent {
		m.status = "Select an entry to rename"
		return
	}

	input := textinput.New()
	input.SetValue(selected.Name)
	input.Focus()
	input.CharLimit = 255
	input.Width = 42

	m.modal = modalState{
		kind:  modalRename,
		title: "Rename entry",
		body:  fmt.Sprintf("Path: %s", selected.Path),
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
		"  PgUp / b       page up",
		"  Enter / Right  open selected entry",
		"  Backspace/Left  go to parent directory",
		"  Tab / h / l    switch active pane",
		"  Alt+Left       directory history back",
		"  Alt+Right      directory history forward",
		"  /              filter entries by name in current pane",
		"  Ctrl+r         refresh both panes",
		"",
		"View and Panels",
		"  i              show text caret in preview pane",
		"  v              start visual selection from caret",
		"  Esc            close view/info/caret mode",
		"  yy             copy current line in caret mode",
		"  y              copy visual selection to clipboard",
		"  h / l          move caret left/right",
		"  w / b          move caret by word",
		"  Ctrl+t         mouse selection mode in text preview pane",
		"  Space          calculate selected directory size",
		"  p              mirror current path to opposite pane",
		"  g              cycle sort mode",
		"  .              toggle hidden files",
		"  t              select theme",
	}

	m.modal = modalState{
		kind:  modalHelp,
		title: "Keyboard Help",
		body:  strings.Join(sections, "\n"),
		note:  version + "  —  F1/? or Esc to close",
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
	if m.infoMode {
		m.copyPath = preview.Metadata.Path
	}
}

func (m *Model) syncPreviewContent() {
	content := m.previewData.Body
	if (m.cursorMode || m.visualMode) && m.previewData.Kind == vfs.PreviewKindText {
		content = m.renderTextCursorContent()
	}
	if m.cfg.Preview.WrapText && m.previewModel.Width > 0 {
		content = lipgloss.NewStyle().Width(m.previewModel.Width).Render(content)
	}
	m.previewModel.SetContent(content)
}

func (m Model) previewPlainLines() []string {
	content := m.previewData.PlainBody
	if content == "" {
		content = m.previewData.Body
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.Split(content, "\n")
}

func (m Model) previewRenderedLines() []string {
	content := m.previewData.Body
	if content == "" {
		content = m.previewData.PlainBody
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.Split(content, "\n")
}

func (m Model) lineRuneCount(line int) int {
	lines := m.previewPlainLines()
	if line < 0 || line >= len(lines) {
		return 0
	}
	return len([]rune(lines[line]))
}

func sliceRunes(text string, start int, end int) string {
	runes := []rune(text)
	start = clamp(start, 0, len(runes))
	end = clamp(end, start, len(runes))
	return string(runes[start:end])
}

func isWordRune(r rune) bool {
	return r == '_' || r == '-' || r == '.' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= 'А' && r <= 'я') ||
		(r >= 'Ё' && r <= 'ё')
}

func normalizeSelection(startLine, startCol, endLine, endCol int) (int, int, int, int) {
	if startLine == endLine && startCol == endCol {
		return startLine, startCol, endLine, endCol + 1
	}
	if startLine < endLine || (startLine == endLine && startCol <= endCol) {
		return startLine, startCol, endLine, endCol + 1
	}
	return endLine, endCol, startLine, startCol + 1
}

func (m Model) visualSelectionBounds() (int, int, int, int) {
	return normalizeSelection(m.visualAnchor, m.visualAnchorCol, m.cursorLine, m.cursorCol)
}

func (m *Model) renderTextCursorContent() string {
	lines := append([]string(nil), m.previewRenderedLines()...)
	plainLines := m.previewPlainLines()
	if len(lines) == 0 {
		return ""
	}
	startLine, startCol, endLine, endCol := m.visualSelectionBounds()
	hasSelection := false
	if m.visualMode {
		startLine = clamp(startLine, 0, len(lines)-1)
		endLine = clamp(endLine, 0, len(lines)-1)
		hasSelection = startLine != endLine || startCol != endCol
	}

	selected := lipgloss.NewStyle().
		Background(m.palette.Marked).
		Foreground(m.palette.Text)
	flashed := lipgloss.NewStyle().
		Background(m.palette.Accent).
		Foreground(m.palette.Background).
		Bold(true)
	cursor := lipgloss.NewStyle().
		Background(m.palette.Warning).
		Foreground(m.palette.Background).
		Bold(true)
	gutterBase := lipgloss.NewStyle().
		Width(2).
		Foreground(m.palette.Muted)
	gutterAnchor := lipgloss.NewStyle().
		Width(2).
		Foreground(m.palette.Info).
		Bold(true)
	gutterCursor := lipgloss.NewStyle().
		Width(2).
		Foreground(m.palette.Accent).
		Bold(true)
	gutterBoth := lipgloss.NewStyle().
		Width(2).
		Foreground(m.palette.Warning).
		Bold(true)

	for idx := range lines {
		marker := "  "
		switch {
		case m.visualMode && idx == m.visualAnchor && idx == m.cursorLine:
			marker = gutterBoth.Render("◆ ")
		case m.visualMode && idx == m.visualAnchor:
			marker = gutterAnchor.Render("│ ")
		case idx == m.cursorLine:
			marker = gutterCursor.Render("▶ ")
		default:
			marker = gutterBase.Render("  ")
		}

		line := lines[idx]
		plain := ""
		if idx < len(plainLines) {
			plain = plainLines[idx]
		}
		lineLen := len([]rune(plain))
		cursorCol := clamp(m.cursorCol, 0, lineLen)

		if hasSelection && idx >= startLine && idx <= endLine {
			segStart := 0
			segEnd := lineLen
			if idx == startLine {
				segStart = clamp(startCol, 0, lineLen)
			}
			if idx == endLine {
				segEnd = clamp(endCol, segStart, lineLen)
			}
			left := ansi.Cut(line, 0, segStart)
			mid := ansi.Cut(line, segStart, segEnd)
			right := ansi.Cut(line, segEnd, lineLen)
			if mid != "" {
				line = left + selected.Render(mid) + right
			}
		}

		if idx == m.cursorLine {
			left := ansi.Cut(line, 0, cursorCol)
			mid := ansi.Cut(line, cursorCol, min(cursorCol+1, max(lineLen, cursorCol+1)))
			right := ansi.Cut(line, min(cursorCol+1, lineLen), lineLen)
			if cursorCol >= lineLen {
				mid = cursor.Render(" ")
				right = ""
			} else {
				mid = cursor.Render(mid)
			}
			line = left + mid + right
		}
		if idx == m.yankFlashLine {
			lines[idx] = flashed.Render(marker + line)
			continue
		}
		lines[idx] = marker + line
	}
	result := strings.Join(lines, "\n")

	// Replace full ANSI resets with background-preserving resets.
	// lipgloss.Render() appends \x1b[0m which resets the panel background
	// set by the outer renderPreviewContent wrapper. Instead, reset only
	// foreground and text attributes, then restore the panel background
	// so that gutter markers, cursor highlights, and selection highlights
	// don't break the panel background for subsequent text on the line.
	panelBG := lipgloss.NewStyle().Background(m.palette.Panel).Render("")
	bgCode := strings.TrimSuffix(panelBG, "\x1b[0m")
	inner := bgCode[2 : len(bgCode)-1]
	safeReset := "\x1b[39;22;23;24;59;" + inner + "m"
	result = strings.ReplaceAll(result, "\x1b[0m", safeReset)
	return result
}

func (m *Model) moveTextCursorWordForward() {
	if !m.cursorMode {
		return
	}
	lines := m.previewPlainLines()
	if len(lines) == 0 {
		return
	}

	line := clamp(m.cursorLine, 0, len(lines)-1)
	col := clamp(m.cursorCol, 0, len([]rune(lines[line])))
	for {
		runes := []rune(lines[line])
		for col < len(runes) && isWordRune(runes[col]) {
			col++
		}
		for col < len(runes) && !isWordRune(runes[col]) {
			col++
		}
		if col < len(runes) {
			m.cursorLine = line
			m.cursorCol = col
			m.ensureTextCursorVisible()
			m.syncPreviewContent()
			return
		}
		if line >= len(lines)-1 {
			m.cursorLine = line
			m.cursorCol = len(runes)
			m.ensureTextCursorVisible()
			m.syncPreviewContent()
			return
		}
		line++
		col = 0
	}
}

func (m *Model) moveTextCursorWordBackward() {
	if !m.cursorMode {
		return
	}
	lines := m.previewPlainLines()
	if len(lines) == 0 {
		return
	}

	line := clamp(m.cursorLine, 0, len(lines)-1)
	col := clamp(m.cursorCol, 0, len([]rune(lines[line])))
	for {
		runes := []rune(lines[line])
		if col > len(runes) {
			col = len(runes)
		}

		// Start from the character immediately before the cursor.
		if col == 0 {
			if line == 0 {
				m.cursorLine = 0
				m.cursorCol = 0
				m.ensureTextCursorVisible()
				m.syncPreviewContent()
				return
			}
			line--
			col = len([]rune(lines[line]))
			continue
		}

		col--
		for {
			runes = []rune(lines[line])
			for col >= 0 && !isWordRune(runes[col]) {
				col--
			}
			if col >= 0 {
				break
			}
			if line == 0 {
				m.cursorLine = 0
				m.cursorCol = 0
				m.ensureTextCursorVisible()
				m.syncPreviewContent()
				return
			}
			line--
			runes = []rune(lines[line])
			col = len(runes) - 1
		}

		for col > 0 && isWordRune(runes[col-1]) {
			col--
		}
		m.cursorLine = line
		m.cursorCol = col
		m.ensureTextCursorVisible()
		m.syncPreviewContent()
		return
	}
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

func (m *Model) moveTextCursorLine(delta int) {
	lines := m.previewPlainLines()
	if len(lines) == 0 {
		return
	}
	if !m.cursorMode {
		return
	}
	m.cursorLine = clamp(m.cursorLine+delta, 0, len(lines)-1)
	m.cursorCol = clamp(m.cursorCol, 0, m.lineRuneCount(m.cursorLine))
	m.ensureTextCursorVisible()
	m.syncPreviewContent()
}

func (m *Model) moveTextCursorCol(delta int) {
	if !m.cursorMode {
		return
	}
	m.cursorCol = clamp(m.cursorCol+delta, 0, m.lineRuneCount(m.cursorLine))
	m.ensureTextCursorVisible()
	m.syncPreviewContent()
}

func (m *Model) ensureTextCursorVisible() {
	if !m.cursorMode {
		return
	}
	visible := max(m.previewModel.Height, 1)
	if m.cursorLine < m.previewModel.YOffset {
		m.previewModel.SetYOffset(m.cursorLine)
		return
	}
	bottom := m.previewModel.YOffset + visible - 1
	if m.cursorLine > bottom {
		m.previewModel.SetYOffset(m.cursorLine - visible + 1)
	}
}

func renderPreviewPane(preview vfs.Preview, viewportModel *viewport.Model, cfg config.Config, palette theme.Palette, width int, height int, useNerdfont bool) string {
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
		Background(palette.Info).
		Foreground(palette.Background).
		Bold(true).
		Render("PREVIEW " + previewIcon(preview) + " " + preview.Title)

	parts := []string{title}
	usedHeight := lipgloss.Height(title)
	if cfg.Preview.ShowMetadata {
		metaView := renderMetadata(preview.Metadata, palette, innerWidth, useNerdfont)
		parts = append(parts, metaView)
		usedHeight += lipgloss.Height(metaView)
	}
	contentHeight := max(innerHeight-usedHeight, 3)
	viewportModel.Width = max(innerWidth-2, 10)
	viewportModel.Height = max(contentHeight-3, 1)

	// Directory previews: borrow the column layout from the browser pane
	// (renderPaneRows + renderColumnsHeader at the same innerWidth),
	// but non-interactive (no cursor, no selection).
	if preview.Kind == vfs.PreviewKindDirectory && len(preview.Entries) > 0 {
		dirPane := BrowserPane{Entries: preview.Entries}
		headerRow := renderColumnsHeader(cfg, innerWidth, palette, palette.Panel, useNerdfont, false)
		rows := renderPaneRows(dirPane, cfg, palette, innerWidth, contentHeight, false, -1, palette.Panel, useNerdfont, false)
		parts = append(parts, lipgloss.JoinVertical(lipgloss.Left, headerRow, rows))
	} else {
		parts = append(parts, renderPreviewContent(viewportModel, palette, innerWidth, contentHeight))
	}

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

func renderMetadata(meta vfs.Metadata, palette theme.Palette, width int, useNerdfont bool) string {
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
	if meta.Duration != "" {
		rightRows = append(rightRows, fmt.Sprintf("duration: %s", meta.Duration))
	}
	if meta.Bitrate != "" {
		rightRows = append(rightRows, fmt.Sprintf("bitrate: %s", meta.Bitrate))
	}
	if meta.AudioCodec != "" {
		rightRows = append(rightRows, fmt.Sprintf("audio: %s", meta.AudioCodec))
	}
	if meta.VideoCodec != "" {
		rightRows = append(rightRows, fmt.Sprintf("video: %s", meta.VideoCodec))
	}
	if meta.Dimensions != "" {
		rightRows = append(rightRows, fmt.Sprintf("resolution: %s", meta.Dimensions))
	}
	if meta.SampleRate != "" {
		rightRows = append(rightRows, fmt.Sprintf("rate: %s", meta.SampleRate))
	}
	if meta.Channels != "" {
		rightRows = append(rightRows, fmt.Sprintf("channels: %s", meta.Channels))
	}
	if meta.PageCount != "" {
		rightRows = append(rightRows, fmt.Sprintf("pages: %s", meta.PageCount))
	}

	leftWidth := max(innerWidth/2, 18)
	if leftWidth > innerWidth {
		leftWidth = innerWidth
	}
	rightWidth := max(innerWidth-leftWidth, 0)
	columnHeight := max(len(leftRows), len(rightRows))
	left := lipgloss.NewStyle().
		Width(leftWidth).
		Height(columnHeight).
		Background(palette.PanelElevated).
		Foreground(palette.Muted).
		Render(strings.Join(leftRows, "\n"))
	right := lipgloss.NewStyle().
		Width(rightWidth).
		Height(columnHeight).
		Background(palette.PanelElevated).
		Foreground(palette.Text).
		Render(strings.Join(rightRows, "\n"))

	copyIcon := "📋"
	if useNerdfont {
		copyIcon = "󰆏"
	}
	iconWidth := lipgloss.Width(copyIcon)
	pathAvailable := max(innerWidth-6-iconWidth-3, 10) // "path: "=6, icon, spacing
	pathLine := lipgloss.NewStyle().
		Width(innerWidth).
		Background(palette.PanelElevated).
		Foreground(palette.Text).
		Render(fmt.Sprintf("path: %s %s", truncateMiddle(meta.Path, pathAvailable), copyIcon))

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

func renderFooter(m Model) string {
	parts := make([]string, 0, 8)
	sep := lipgloss.NewStyle().Background(m.palette.Footer).Render("   ")
	prefix := lipgloss.NewStyle().Background(m.palette.Footer).Render(" ")
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
	line := strings.Join(parts, sep)
	if m.selectMode {
		modeLabel := lipgloss.NewStyle().
			Background(m.palette.Footer).
			Foreground(m.palette.Info).
			Bold(true).
			Render("SELECT TEXT MODE")
		if line != "" {
			line += sep
		}
		line += modeLabel
	}
	if m.visualMode {
		modeLabel := lipgloss.NewStyle().
			Background(m.palette.Footer).
			Foreground(m.palette.Marked).
			Bold(true).
			Render("VISUAL MODE")
		if line != "" {
			line += sep
		}
		line += modeLabel
	}
	line = prefix + line
	line = ansi.Truncate(line, m.width, "")
	fill := m.width - ansi.StringWidth(line)
	if fill > 0 {
		line += lipgloss.NewStyle().
			Background(m.palette.Footer).
			Render(strings.Repeat(" ", fill))
	}
	return line
}

func renderFilterBar(m Model) string {
	prompt := lipgloss.NewStyle().
		Background(m.palette.Footer).
		Foreground(m.palette.FooterKey).
		Bold(true).
		Render(" / ")
	inputView := m.filterInput.View()
	inputStyle := lipgloss.NewStyle().
		Background(m.palette.Footer).
		Foreground(m.palette.Text)
	line := prompt + inputStyle.Render(inputView)
	filtered := m.filteredCount(m.activePane())
	if m.filterQuery != "" {
		countStyle := lipgloss.NewStyle().
			Background(m.palette.Footer).
			Foreground(m.palette.Muted)
		line += countStyle.Render(fmt.Sprintf(" (%d)", filtered))
	}
	line = ansi.Truncate(line, m.width, "")
	fill := m.width - ansi.StringWidth(line)
	if fill > 0 {
		line += lipgloss.NewStyle().
			Background(m.palette.Footer).
			Render(strings.Repeat(" ", fill))
	}
	return line
}

func renderModal(m Model, palette theme.Palette, width int) string {
	if m.modal.kind == modalCopyProgress && m.copyJob != nil {
		return renderCopyProgressModal(*m.copyJob, palette, width)
	}
	if m.modal.kind == modalArchiveProgress && m.archiveJob != nil {
		return renderArchiveProgressModal(*m.archiveJob, palette, width)
	}
	if m.modal.kind == modalHelp {
		return renderHelpModal(m.modal, palette, width)
	}

	if m.modal.kind == modalSSHConnect {
		return renderSSHConnectModal(m, palette, width)
	}

	if m.modal.kind == modalThemeSelect {
		return renderThemeSelectModal(m, palette, width)
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

	if modal.kind == modalMkdir || modal.kind == modalRename {
		lines = append(lines, spacer, lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Render(modal.input.View()))
	}
	if modal.note != "" {
		lines = append(lines, spacer)
		if modal.note == "confirm-actions" {
			lines = append(lines, renderModalNoteLine("Enter to confirm, Esc to cancel", contentWidth, palette, noteStyle))
		} else {
			for _, raw := range strings.Split(modal.note, "\n") {
				lines = append(lines, renderModalNoteLine(raw, contentWidth, palette, noteStyle))
			}
		}
	}

	return box.Render(strings.Join(lines, "\n"))
}

func renderThemeSelectModal(m Model, palette theme.Palette, width int) string {
	outerWidth := max(width, 8)
	contentWidth := max(outerWidth-6, 1)

	// Pre-compute name column width (align names left, swatches right)
	themeNames := m.themeSelector.names
	maxNameLen := 0
	for _, name := range themeNames {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}
	// Leave room for cursor (2 chars), name, spacing, and 5 swatches (10 chars)
	swatchArea := 12                              // "  " gap + 5*"  " swatches = 12
	nameColWidth := contentWidth - swatchArea - 3 // -3 for padding
	if nameColWidth < 10 {
		nameColWidth = 10
	}

	titleStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Background(palette.Panel).
		Bold(true).
		Foreground(palette.Accent)

	padStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Background(palette.Panel)

	textColStyle := lipgloss.NewStyle().
		Width(nameColWidth).
		Background(palette.Panel).
		Foreground(palette.Text)

	textColSelStyle := lipgloss.NewStyle().
		Width(nameColWidth).
		Background(palette.Selection).
		Foreground(palette.Text)

	cursorStyle := lipgloss.NewStyle().
		Width(2).
		Background(palette.Panel).
		Foreground(palette.Accent)

	cursorSelStyle := lipgloss.NewStyle().
		Width(2).
		Background(palette.Selection).
		Foreground(palette.Accent)

	box := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(1, 2).
		Background(palette.Panel).
		Foreground(palette.Text).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(palette.BorderActive).
		BorderBackground(palette.Panel)

	spacer := padStyle.Render(" ")

	lines := []string{
		titleStyle.Render("Select Theme"),
		spacer,
	}

	for i, name := range themeNames {
		tp, err := theme.Resolve(name)
		if err != nil {
			continue
		}

		// Determine if this is the selected row
		isSelected := i == m.themeSelector.cursor

		// Cursor indicator
		var cursorPart string
		if isSelected {
			cursorPart = cursorSelStyle.Render("▸")
		} else {
			cursorPart = cursorStyle.Render(" ")
		}

		// Name part with padding to align
		paddedName := fmt.Sprintf("%-*s", maxNameLen, name)
		var namePart string
		if isSelected {
			namePart = textColSelStyle.Render(paddedName)
		} else {
			namePart = textColStyle.Render(paddedName)
		}

		// Swatches use the resolved theme's palette colors
		// Background for swatches depends on whether row is selected
		swatchBg := palette.Panel
		if isSelected {
			swatchBg = palette.Selection
		}
		swatch := lipgloss.NewStyle().Background(swatchBg).Render(
			lipgloss.NewStyle().Background(tp.Background).Render("  ") +
				lipgloss.NewStyle().Background(tp.Panel).Render("  ") +
				lipgloss.NewStyle().Background(tp.Accent).Render("  ") +
				lipgloss.NewStyle().Background(tp.Text).Render("  ") +
				lipgloss.NewStyle().Background(tp.Selection).Render("  "),
		)

		// Gap between name and swatches
		gapStyle := lipgloss.NewStyle().
			Width(1).
			Background(swatchBg)
		gap := gapStyle.Render(" ")

		// Build the row
		row := cursorPart + namePart + gap + swatch
		lines = append(lines, row)
	}

	// Instruction line at the bottom with colored Enter/Esc tokens
	lines = append(lines, spacer)
	hintRaw := "↑/↓ navigate · Enter apply · Esc cancel"
	if highlighted, ok := renderModalHintTokens(hintRaw, contentWidth, palette, palette.Muted); ok {
		lines = append(lines, highlighted)
	} else {
		noteStyle := lipgloss.NewStyle().
			Width(contentWidth).
			Background(palette.Panel).
			Foreground(palette.Muted)
		lines = append(lines, noteStyle.Render(hintRaw))
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

	if highlighted, ok := renderModalHintTokens(raw, width, palette, palette.Muted); ok {
		return highlighted
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
				Foreground(palette.Accent).
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

func renderModalHintTokens(raw string, width int, palette theme.Palette, baseColor lipgloss.Color) (string, bool) {
	type tokenStyle struct {
		token string
		color lipgloss.Color
	}
	tokens := []tokenStyle{
		{token: "Background", color: palette.Accent},
		{token: "Cancel", color: palette.CancelButton},
		{token: "Enter", color: palette.Accent},
		{token: "Esc", color: palette.CancelButton},
	}
	contains := false
	for _, entry := range tokens {
		if strings.Contains(raw, entry.token) {
			contains = true
			break
		}
	}
	if !contains {
		return "", false
	}

	var line strings.Builder
	rest := raw
	for len(rest) > 0 {
		nextIdx := -1
		nextToken := ""
		nextColor := baseColor
		for _, entry := range tokens {
			idx := strings.Index(rest, entry.token)
			if idx >= 0 && (nextIdx == -1 || idx < nextIdx) {
				nextIdx = idx
				nextToken = entry.token
				nextColor = entry.color
			}
		}
		if nextIdx == -1 {
			line.WriteString(lipgloss.NewStyle().
				Background(palette.Panel).
				Foreground(baseColor).
				Render(rest))
			break
		}
		if nextIdx > 0 {
			line.WriteString(lipgloss.NewStyle().
				Background(palette.Panel).
				Foreground(baseColor).
				Render(rest[:nextIdx]))
		}
		line.WriteString(lipgloss.NewStyle().
			Background(palette.Panel).
			Foreground(nextColor).
			Bold(true).
			Render(nextToken))
		rest = rest[nextIdx+len(nextToken):]
	}

	return lipgloss.NewStyle().
		Width(width).
		Background(palette.Panel).
		Render(line.String()), true
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
		lines = append(lines, spacer)
		if highlighted, ok := renderModalHintTokens(modal.note, contentWidth, palette, palette.FooterKey); ok {
			lines = append(lines, highlighted)
		} else {
			lines = append(lines, noteStyle.Render(modal.note))
		}
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
	filesLabel := fmt.Sprintf("%d / ?", progress.FilesDone)
	if progress.FilesTotal > 0 {
		filesLabel = fmt.Sprintf("%d / %d", progress.FilesDone, progress.FilesTotal)
	}

	ratio := 0.0
	if progress.FilesTotal > 0 {
		ratio = float64(progress.FilesDone) / float64(progress.FilesTotal)
	}

	lines := []string{
		titleStyle.Render(progressTitle(job.kind)),
		spacer,
		renderProgressStatLine("Stage:", progressStageLabel(progress, job.kind), contentWidth, palette),
		renderProgressStatLine("Target:", job.targetDir, contentWidth, palette),
		spacer,
		renderProgressStatLine("Files:", filesLabel, contentWidth, palette),
	}
	if progress.FilesTotal > 0 {
		barAndPct := []string{
			spacer,
			renderProgressBarLine(ratio, contentWidth, palette),
			renderProgressPercentLine(ratio, contentWidth, palette),
		}
		lines = append(lines, barAndPct...)
	}
	lines = append(lines, spacer, renderModalNoteLine("Background / b, Cancel / c", contentWidth, palette, mutedStyle))
	if job.background {
		lines = append(lines, mutedStyle.Render("Transfer continues in background"))
	}

	return box.Render(strings.Join(lines, "\n"))
}

func renderArchiveProgressModal(job archiveJobState, palette theme.Palette, width int) string {
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
	switch job.kind {
	case "extract", "delete":
		// Extraction/delete: progress by file count (no byte tracking)
		if progress.FilesTotal > 0 {
			ratio = float64(progress.FilesDone) / float64(progress.FilesTotal)
		}
	default:
		if progress.BytesTotal > 0 {
			ratio = float64(progress.BytesDone) / float64(progress.BytesTotal)
		}
	}

	stage := progress.Stage
	if stage == "" {
		switch job.kind {
		case "extract":
			stage = "Extracting data"
		case "delete":
			stage = "Deleting files"
		default:
			stage = "Archiving data"
		}
	}

	var title string
	switch job.kind {
	case "extract":
		title = "Extracting"
	case "delete":
		title = "Deleting"
	default:
		title = "Archiving"
	}

	lines := []string{
		titleStyle.Render(title),
		spacer,
		renderProgressBarLine(ratio, contentWidth, palette),
		spacer,
		renderProgressPercentLine(ratio, contentWidth, palette),
		renderProgressStatLine("Stage:", stage, contentWidth, palette),
		spacer,
		renderProgressStatLine("Files:", fmt.Sprintf("%d / %d", progress.FilesDone, progress.FilesTotal), contentWidth, palette),
	}

	// Only show size and speed for archive creation (not tracked during extraction/delete)
	if job.kind == "archive" {
		lines = append(lines,
			renderProgressStatLine("Size:", fmt.Sprintf("%s / %s", formatSize(progress.BytesDone, true), formatSize(progress.BytesTotal, true)), contentWidth, palette),
			renderProgressStatLine("Speed:", transferSpeed(progress.BytesDone, job.startedAt), contentWidth, palette),
		)
	}

	lines = append(lines, spacer, renderModalNoteLine("Background / b, Cancel / c", contentWidth, palette, mutedStyle))
	if job.background {
		switch job.kind {
		case "extract":
			lines = append(lines, mutedStyle.Render("Extraction continues in background"))
		case "delete":
			lines = append(lines, mutedStyle.Render("Delete continues in background"))
		default:
			lines = append(lines, mutedStyle.Render("Archive continues in background"))
		}
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
	case vfs.PreviewKindPDF:
		return "󰷉"
	case vfs.PreviewKindAudio:
		return "󰋋"
	case vfs.PreviewKindVideo:
		return "󰋲"
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
		return trashPathsCmd(p.sourcePaths)
	case opPermanentDelete:
		return deletePathsPermanentCmd(p.sourcePaths)
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

func remoteDirSizeCmd(client *remote.SSHClient, path string) tea.Cmd {
	return func() tea.Msg {
		size, err := client.DirectorySize(path)
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

// remoteCopyPlanCmd computes transfer stats for remote-involved copy/move operations.
// For local sources it uses vfs.CopyStats; for remote sources it walks the SFTP tree.
func (m *Model) remoteCopyPlanCmd(kind fileOpKind, sourcePaths []string, targetDir string,
	sourceIsRemote, targetIsRemote bool, srcClient, dstClient *remote.SSHClient) tea.Cmd {

	return func() tea.Msg {
		stats := vfs.TransferStats{}
		var err error

		for _, sourcePath := range sourcePaths {
			if !sourceIsRemote {
				// Local source — use existing stats function
				part, statErr := vfs.CopyStats(sourcePath)
				if statErr != nil {
					err = statErr
					break
				}
				stats.FilesTotal += part.FilesTotal
				stats.BytesTotal += part.BytesTotal
			} else {
				// Remote source — walk via SFTP
				info, statErr := srcClient.Lstat(sourcePath)
				if statErr != nil {
					err = statErr
					break
				}
				if !info.IsDir() {
					stats.FilesTotal++
					stats.BytesTotal += info.Size()
				} else {
					walkErr := srcClient.Walk(sourcePath, func(walkPath string, info os.FileInfo, walkErr error) error {
						if walkErr != nil {
							return walkErr
						}
						if !info.IsDir() {
							stats.FilesTotal++
							stats.BytesTotal += info.Size()
						}
						return nil
					})
					if walkErr != nil {
						err = walkErr
						break
					}
				}
			}
		}

		return copyPlanMsg{
			kind:        kind,
			sourcePaths: append([]string(nil), sourcePaths...),
			targetDir:   targetDir,
			stats:       stats,
			err:         err,
			srcClient:   srcClient,
			dstClient:   dstClient,
		}
	}
}

// remoteDeletePlanCmd computes delete stats for remote paths via SFTP.
func (m *Model) remoteDeletePlanCmd(sources []string, client *remote.SSHClient) tea.Cmd {
	return func() tea.Msg {
		stats := vfs.TransferStats{}
		var err error

		for _, sourcePath := range sources {
			info, statErr := client.Lstat(sourcePath)
			if statErr != nil {
				err = statErr
				break
			}
			if !info.IsDir() {
				stats.FilesTotal++
				stats.BytesTotal += info.Size()
			} else {
				walkErr := client.Walk(sourcePath, func(walkPath string, info os.FileInfo, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if !info.IsDir() {
						stats.FilesTotal++
						stats.BytesTotal += info.Size()
					}
					return nil
				})
				if walkErr != nil {
					err = walkErr
					break
				}
			}
		}

		return deletePlanMsg{
			kind:        opDelete,
			sourcePaths: append([]string(nil), sources...),
			stats:       stats,
			err:         err,
			srcClient:   client,
		}
	}
}

func (m *Model) enterArchive(selected vfs.Entry) error {
	pane := m.activePane()
	// Save current path to history before opening the archive.
	pane.PushHistory(pane.Path)
	tempDir, err := vfs.ExtractArchiveToTemp(selected.Path)
	if err != nil {
		return err
	}
	pane.PushArchive(ArchiveMount{
		SourcePath: selected.Path,
		ParentPath: pane.Path,
		RootPath:   tempDir,
		TempDir:    tempDir,
	})
	pane.Path = tempDir
	pane.Cursor = 0
	pane.Offset = 0
	if err := m.reloadPane(pane.ID, ""); err != nil {
		_ = os.RemoveAll(tempDir)
		_, _ = pane.PopArchive()
		return err
	}
	m.status = fmt.Sprintf("Opened archive %s", selected.DisplayName())
	return nil
}

func (m *Model) cleanupArchiveMounts() {
	for _, pane := range []*BrowserPane{&m.left, &m.right} {
		for _, mount := range pane.ClearArchives() {
			_ = os.RemoveAll(mount.TempDir)
		}
	}
}

func archivePlanCmd(sourcePaths []string, targetDir string) tea.Cmd {
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
		return archivePlanMsg{
			sourcePaths: append([]string(nil), sourcePaths...),
			targetDir:   targetDir,
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

func waitArchiveProgressCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func dismissNoticeCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return dismissNoticeMsg{}
	})
}

func dismissYankFlashCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return dismissYankFlashMsg{}
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
			Stage:       "Counting files...",
		},
		cancel:    cancel,
		startedAt: time.Now(),
	}
	m.modal = modalState{kind: modalCopyProgress}
	m.status = strings.Title(operationVerb(kind)) + " started"

	return tea.Batch(
		func() tea.Msg {
			go func() {
				var statErr error
				doneFiles := 0
				totalFiles := 0

				// Phase 1: count files across all sources
				entriesStats := make([]vfs.TransferStats, len(sourcePaths))
				for i, sourcePath := range sourcePaths {
					entryStats, err := vfs.CopyStats(sourcePath)
					if err != nil {
						m.copyProgress <- copyDoneMsg{
							jobID:       jobID,
							kind:        kind,
							sourcePaths: append([]string(nil), sourcePaths...),
							targetDir:   targetDir,
							err:         err,
						}
						return
					}
					entriesStats[i] = entryStats
					totalFiles += entryStats.FilesTotal
					m.copyProgress <- copyProgressMsg{
						jobID: jobID,
						progress: vfs.CopyProgress{
							FilesDone:   0,
							FilesTotal:  totalFiles,
							CurrentPath: sourcePath,
							Stage:       fmt.Sprintf("Counting files... %d found", totalFiles),
						},
					}
				}

				// Phase 2: copy with known total
				for i, sourcePath := range sourcePaths {
					entryStats := entriesStats[i]
					progressFn := func(progress vfs.CopyProgress) {
						m.copyProgress <- copyProgressMsg{
							jobID: jobID,
							progress: vfs.CopyProgress{
								FilesDone:   doneFiles + progress.FilesDone,
								FilesTotal:  totalFiles,
								BytesDone:   0,
								BytesTotal:  0,
								CurrentPath: progress.CurrentPath,
								Stage:       "Copying files...",
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

func (m *Model) startArchiveJob(sourcePaths []string, targetDir string, format string, stats vfs.TransferStats) tea.Cmd {
	m.nextArchiveJob++
	jobID := m.nextArchiveJob
	ctx, cancel := context.WithCancel(context.Background())

	archiveName := vfs.ArchiveName(sourcePaths, format)
	archivePath := filepath.Join(targetDir, archiveName)

	m.archiveJob = &archiveJobState{
		id:          jobID,
		kind:        "archive",
		sourcePaths: append([]string(nil), sourcePaths...),
		targetPath:  archivePath,
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
	m.modal = modalState{kind: modalArchiveProgress}
	m.status = "Archiving started"

	return tea.Batch(
		func() tea.Msg {
			go func() {
				emitProgress := func(p vfs.CopyProgress) {
					m.archiveProgress <- archiveProgressMsg{
						jobID:    jobID,
						progress: p,
					}
				}
				err := vfs.CreateArchive(ctx, sourcePaths, archivePath, emitProgress)
				if err != nil {
					m.archiveProgress <- archiveDoneMsg{
						jobID:       jobID,
						sourcePaths: append([]string(nil), sourcePaths...),
						targetPath:  archivePath,
						err:         err,
					}
					return
				}
				m.archiveProgress <- archiveDoneMsg{
					jobID:       jobID,
					sourcePaths: append([]string(nil), sourcePaths...),
					targetPath:  archivePath,
				}
			}()
			return nil
		},
		waitArchiveProgressCmd(m.archiveProgress),
	)
}

func (m *Model) startExtractJob(sourcePath, targetDir string) tea.Cmd {
	m.nextArchiveJob++
	jobID := m.nextArchiveJob
	ctx, cancel := context.WithCancel(context.Background())

	m.archiveJob = &archiveJobState{
		id:          jobID,
		kind:        "extract",
		sourcePaths: []string{sourcePath},
		targetPath:  targetDir,
		progress: vfs.CopyProgress{
			FilesDone:   0,
			FilesTotal:  0,
			BytesDone:   0,
			BytesTotal:  0,
			CurrentPath: sourcePath,
		},
		cancel:    cancel,
		startedAt: time.Now(),
	}
	m.modal = modalState{kind: modalArchiveProgress}
	m.status = "Extracting started"

	return tea.Batch(
		func() tea.Msg {
			go func() {
				emitProgress := func(p vfs.CopyProgress) {
					m.archiveProgress <- archiveProgressMsg{
						jobID:    jobID,
						progress: p,
					}
				}
				err := vfs.ExtractArchiveToDir(ctx, sourcePath, targetDir, emitProgress)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						err = context.Canceled
					}
					m.archiveProgress <- archiveDoneMsg{
						jobID:       jobID,
						sourcePaths: []string{sourcePath},
						targetPath:  targetDir,
						err:         err,
					}
					return
				}
				m.archiveProgress <- archiveDoneMsg{
					jobID:       jobID,
					sourcePaths: []string{sourcePath},
					targetPath:  targetDir,
				}
			}()
			return nil
		},
		waitArchiveProgressCmd(m.archiveProgress),
	)
}

func (m *Model) startDeleteJob(kind fileOpKind, sources []string) tea.Cmd {
	m.nextArchiveJob++
	jobID := m.nextArchiveJob
	ctx, cancel := context.WithCancel(context.Background())

	m.archiveJob = &archiveJobState{
		id:          jobID,
		kind:        "delete",
		sourcePaths: append([]string(nil), sources...),
		targetPath:  "",
		progress: vfs.CopyProgress{
			FilesDone:   0,
			FilesTotal:  len(sources),
			BytesDone:   0,
			BytesTotal:  0,
			CurrentPath: sources[0],
		},
		cancel:    cancel,
		startedAt: time.Now(),
	}
	m.modal = modalState{kind: modalArchiveProgress}
	verb := "Moving to trash"
	if kind == opPermanentDelete {
		verb = "Deleting"
	}
	m.status = verb + " started"

	return tea.Batch(
		func() tea.Msg {
			go func() {
				verb := "move to trash"
				if kind == opPermanentDelete {
					verb = "delete permanently"
				}
				for i, sourcePath := range sources {
					select {
					case <-ctx.Done():
						m.archiveProgress <- archiveDoneMsg{
							jobID:       jobID,
							sourcePaths: append([]string(nil), sources...),
							err:         context.Canceled,
						}
						return
					default:
					}

					var err error
					if kind == opPermanentDelete {
						err = vfs.DeletePath(sourcePath)
					} else {
						err = vfs.MoveToTrash(sourcePath)
					}
					if err != nil {
						m.archiveProgress <- archiveDoneMsg{
							jobID:       jobID,
							sourcePaths: append([]string(nil), sources...),
							err:         fmt.Errorf("%s %s: %w", verb, sourcePath, err),
						}
						return
					}

					m.archiveProgress <- archiveProgressMsg{
						jobID: jobID,
						progress: vfs.CopyProgress{
							FilesDone:   i + 1,
							FilesTotal:  len(sources),
							CurrentPath: sourcePath,
							Stage:       verb,
						},
					}
				}
				m.archiveProgress <- archiveDoneMsg{
					jobID:       jobID,
					sourcePaths: append([]string(nil), sources...),
				}
			}()
			return nil
		},
		waitArchiveProgressCmd(m.archiveProgress),
	)
}

func moveCmd(sourcePath, targetDir string, overwrite bool) tea.Cmd {
	return func() tea.Msg {
		targetPath, err := vfs.MovePath(sourcePath, targetDir, overwrite)
		return opMsg{kind: opMove, sourcePath: sourcePath, targetPath: targetPath, err: err}
	}
}

func trashPathsCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		for _, path := range paths {
			if err := vfs.MoveToTrash(path); err != nil {
				return opMsg{kind: opDelete, sourcePath: path, err: err}
			}
		}
		return opMsg{kind: opDelete}
	}
}

func deletePathsPermanentCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		for _, path := range paths {
			if err := vfs.DeletePath(path); err != nil {
				return opMsg{kind: opPermanentDelete, sourcePath: path, err: err}
			}
		}
		return opMsg{kind: opPermanentDelete}
	}
}

func trashPlanCmd(sourcePaths []string) tea.Cmd {
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
			kind:        opDelete,
			sourcePaths: append([]string(nil), sourcePaths...),
			stats:       stats,
			err:         err,
		}
	}
}

func deletePlanPermanentCmd(sourcePaths []string) tea.Cmd {
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
			kind:        opPermanentDelete,
			sourcePaths: append([]string(nil), sourcePaths...),
			stats:       stats,
			err:         err,
		}
	}
}

func (m *Model) mkdirCmd(parent, name string) tea.Cmd {
	// Remote mkdir via SFTP
	if mount, ok := m.activePane().CurrentRemote(); ok {
		return func() tea.Msg {
			targetPath := path.Join(parent, name)
			err := mount.Client.MkdirAll(targetPath)
			return opMsg{kind: opMkdir, targetPath: targetPath, err: err}
		}
	}
	// Local mkdir
	return func() tea.Msg {
		targetPath, err := vfs.MakeDir(parent, name)
		return opMsg{kind: opMkdir, targetPath: targetPath, err: err}
	}
}

func renameCmd(sourcePath, newName string) tea.Cmd {
	return func() tea.Msg {
		targetPath, err := vfs.RenamePath(sourcePath, newName)
		return opMsg{kind: opRename, sourcePath: sourcePath, targetPath: targetPath, err: err}
	}
}

func selectedName(pane *BrowserPane) string {
	selected, ok := pane.Selected()
	if !ok {
		return ""
	}
	return selected.Name
}

func (m *Model) saveSession() {
	leftMem := copyCursorMemory(m.left.cursorMemory)
	rightMem := copyCursorMemory(m.right.cursorMemory)

	leftPath := m.left.Path
	if m.left.InRemote() {
		leftPath = ""
		log.Printf("[SESSION] pane left is in remote mode, clearing saved path")
	}
	rightPath := m.right.Path
	if m.right.InRemote() {
		rightPath = ""
		log.Printf("[SESSION] pane right is in remote mode, clearing saved path")
	}

	// Always include current directory in cursor memory
	if name := selectedName(&m.left); name != "" {
		leftMem[m.left.Path] = name
	}
	if name := selectedName(&m.right); name != "" {
		rightMem[m.right.Path] = name
	}
	s := config.SessionState{
		ActivePane: string(m.active),
		Left: config.PaneSession{
			Path:         leftPath,
			EntryName:    selectedName(&m.left),
			CursorMemory: leftMem,
		},
		Right: config.PaneSession{
			Path:         rightPath,
			EntryName:    selectedName(&m.right),
			CursorMemory: rightMem,
		},
	}
	if err := config.SaveSession(s); err != nil {
		log.Printf("[SESSION] save failed: %v", err)
	} else {
		log.Printf("[SESSION] saved: active=%s left=%s right=%s cursorMem left=%d right=%d",
			m.active, m.left.Path, m.right.Path, len(leftMem), len(rightMem))
	}
}

func copyCursorMemory(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func applySession(m *Model) {
	s, err := config.LoadSession()
	if err != nil {
		log.Printf("[SESSION] load failed: %v", err)
		return
	}
	if s.ActivePane == "" && s.Left.Path == "" && s.Right.Path == "" {
		log.Printf("[SESSION] no previous session found")
		return
	}
	log.Printf("[SESSION] loaded: active=%s left=%s right=%s", s.ActivePane, s.Left.Path, s.Right.Path)

	applyPaneSession(&m.left, s.Left)
	applyPaneSession(&m.right, s.Right)

	if s.ActivePane == string(PaneRight) {
		m.active = PaneRight
	}
}

func applyPaneSession(pane *BrowserPane, ps config.PaneSession) {
	if ps.Path == "" {
		return
	}
	abs, err := filepath.Abs(ps.Path)
	if err != nil {
		log.Printf("[SESSION] skip pane path=%s: %v", ps.Path, err)
		return
	}
	if err := canReadDir(abs); err != nil {
		log.Printf("[SESSION] skip pane path=%s: %v", abs, err)
		return
	}
	pane.Path = abs
	// Restore per-directory cursor memory from previous session
	if len(ps.CursorMemory) > 0 {
		pane.cursorMemory = make(map[string]string, len(ps.CursorMemory))
		for k, v := range ps.CursorMemory {
			pane.cursorMemory[k] = v
		}
	}
	// Ensure current directory's entry is also in cursor memory
	if ps.EntryName != "" {
		if pane.cursorMemory == nil {
			pane.cursorMemory = make(map[string]string)
		}
		pane.cursorMemory[abs] = ps.EntryName
	}
}

func canReadDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	return nil
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
	filesLabel := fmt.Sprintf("%d/?", progress.FilesDone)
	if progress.FilesTotal > 0 {
		filesLabel = fmt.Sprintf("%d/%d", progress.FilesDone, progress.FilesTotal)
	}
	return fmt.Sprintf(
		"%s in background: %s files",
		strings.Title(operationVerb(kind)),
		filesLabel,
	)
}

func formatArchiveStatus(progress vfs.CopyProgress) string {
	return fmt.Sprintf(
		"Archiving in background: %d/%d files, %s/%s",
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
	case opArchive:
		return "Archiving"
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
		return "Moved to trash"
	case opPermanentDelete:
		return "Permanently deleted"
	case opArchive:
		return "Archived"
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
		return "trash"
	case opPermanentDelete:
		return "permanent delete"
	case opArchive:
		return "archive"
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

func startExternalOpenCmd(command *exec.Cmd, path string) tea.Cmd {
	return func() tea.Msg {
		command.Stdin = nil
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			return externalOpenMsg{path: path, err: err}
		}
		return externalOpenMsg{path: path}
	}
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

func clamp(n, low, high int) int {
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
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
	case "text", "config":
		return true
	default:
		return false
	}
}

func isArchiveEntry(entry vfs.Entry) bool {
	return !entry.IsDir && !entry.IsParent && entry.Category() == "archive"
}

func (m Model) syncImageOverlay(leftWidth int, previewWidth int, bodyHeight int) {
	if m.overlay == nil {
		return
	}
	if m.modal.kind != modalNone {
		m.overlay.hide()
		return
	}
	if m.previewData.Kind != vfs.PreviewKindImage {
		m.overlay.hide()
		return
	}
	imagePath := strings.TrimSpace(m.previewData.Metadata.Path)
	if imagePath == "" {
		m.overlay.hide()
		return
	}

	rect := overlayRect{}
	if m.viewMode {
		rect = overlayRect{
			x:      1,
			y:      1,
			width:  max(m.width-2, 1),
			height: max(bodyHeight-2, 1),
		}
	} else if m.infoMode {
		startX := 0
		if m.active == PaneLeft {
			startX = leftWidth + m.cfg.UI.PaneGap
		}
		innerWidth := max(previewWidth-2, 1)
		metaHeight := 0
		if m.cfg.Preview.ShowMetadata {
			metaHeight = lipgloss.Height(renderMetadata(m.previewData.Metadata, m.palette, innerWidth, m.nerdIcons))
		}
		titleHeight := 1
		topInset := 1
		contentBorder := 1
		safetyGap := 1
		contentTop := topInset + titleHeight + metaHeight + contentBorder + safetyGap
		rect = overlayRect{
			x:      startX + 3,
			y:      contentTop,
			width:  max(previewWidth-6, 1),
			height: max(bodyHeight-contentTop-2, 1),
		}
	} else {
		m.overlay.hide()
		return
	}

	if err := m.overlay.show(imagePath, rect); err != nil {
		m.overlay.hide()
	}
}

func (m *Model) cleanupImageOverlay() {
	if m.overlay == nil {
		return
	}
	m.overlay.stop()
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

func (m *Model) mouseOverPathLine(x, y int) bool {
	if !m.infoMode || !m.cfg.Preview.ShowMetadata || m.width <= 0 || m.height <= 0 {
		return false
	}

	leftWidth, previewWidth, _ := m.layoutWidths()
	bodyHeight := m.bodyHeight()
	if y < 0 || y >= bodyHeight {
		return false
	}

	// Preview pane x-range
	var startX int
	if m.active == PaneLeft {
		startX = leftWidth + m.cfg.UI.PaneGap
	} else {
		startX = 0
	}
	if x < startX || x >= startX+previewWidth {
		return false
	}

	// The path line is within the metadata section (approximate Y range 1-7).
	// Check that Y is in the metadata area and X is in the right half where the icon is.
	if y < 1 || y > 7 {
		return false
	}
	// The icon is at the far-right end of the preview pane content area
	iconStartX := startX + previewWidth/2
	return x >= iconStartX
}

// ─── SSH handlers ──────────────────────────────────────────────────────────

// handleSSHToggle toggles SSH host list display in the active pane.
func (m *Model) handleSSHToggle() (tea.Model, tea.Cmd) {
	if m.ssh == nil {
		m.status = "SSH init failed — check ~/.ssh/config"
		return m, nil
	}

	pane := m.activePane()

	// If already in SSH mode, exit it
	if pane.InRemote() {
		return m.exitSSHMode()
	}
	// If path is "ssh://" (host list already shown), close it too
	if pane.Path == "ssh://" {
		// Restore saved pre-SSH path if available
		if m.preSSHPath != "" {
			pane.Path = m.preSSHPath
			m.preSSHPath = ""
			if err := m.reloadPane(pane.ID, ""); err != nil {
				m.status = err.Error()
			}
			return m, nil
		}
		// Fallback: try history
		if prevPath, ok := pane.PopHistory(); ok {
			pane.Path = prevPath
			if err := m.reloadPane(pane.ID, ""); err != nil {
				m.status = err.Error()
			}
			return m, nil
		}
		m.status = "SSH mode closed"
		return m, nil
	}

	// Save current path, show SSH host list
	m.preSSHPath = pane.Path
	entries := buildSSHHostEntries(m.ssh.store, m.ssh.connectedHosts)
	pane.SetEntries(entries, "")
	pane.Path = "ssh://"
	m.status = "SSH hosts — select a host and press Enter"
	return m, nil
}

// exitSSHMode restores the pane to its previous directory and closes all connections.
func (m *Model) exitSSHMode() (tea.Model, tea.Cmd) {
	pane := m.activePane()

	// Close all remote mounts
	for {
		mount, ok := pane.CurrentRemote()
		if !ok {
			break
		}
		if m.ssh != nil {
			m.ssh.connectedHosts[mount.Host.Name] = false
		}
		pane.PopRemote()
		mount.Client.Close()
	}

	// Close all stored active clients (connections kept alive in host list)
	if m.ssh != nil {
		for name, client := range m.ssh.activeClients {
			log.Printf("[ACTION] exitSSHMode: closing stored connection for host=%s", name)
			client.Close()
			delete(m.ssh.activeClients, name)
		}
	}

	// Restore original pre-SSH path
	prevPath := m.preSSHPath
	m.preSSHPath = ""
	if prevPath != "" {
		pane.Path = prevPath
		if err := m.reloadPane(pane.ID, ""); err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.status = "Exited SSH mode"
		return m, m.loadPreviewCmd()
	}

	// Fallback: try history
	if prevPath, ok := pane.PopHistory(); ok {
		pane.Path = prevPath
		if err := m.reloadPane(pane.ID, ""); err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.status = "Exited SSH mode"
		return m, m.loadPreviewCmd()
	}

	// Last resort: reload current directory
	if err := m.reloadPane(pane.ID, ""); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = "Exited SSH mode"
	return m, m.loadPreviewCmd()
}

// handleSSHConnectHost connects to a selected SSH host.
func (m *Model) handleSSHConnectHost() (tea.Model, tea.Cmd) {
	if m.ssh == nil {
		m.status = "SSH not available"
		return m, nil
	}

	selected, ok := m.activePane().Selected()
	if !ok {
		return m, nil
	}

	// Find the host config by name
	host := m.ssh.store.FindByName(selected.RemoteHostName)
	if host == nil {
		m.status = fmt.Sprintf("Host %q not found in config", selected.RemoteHostName)
		return m, nil
	}

	// Check for an existing active connection we can reuse
	if client, ok := m.ssh.activeClients[host.Name]; ok {
		log.Printf("[ACTION] SSHConnectHost: reusing existing connection for host=%s", host.Name)
		delete(m.ssh.activeClients, host.Name)
		return m, func() tea.Msg {
			return sshConnectMsg{hostName: host.Name, client: client, err: nil}
		}
	}

	log.Printf("[ACTION] SSHConnectHost: host=%s user=%s hostname=%s port=%s",
		host.Name, host.User, host.HostName, host.Port)
	m.status = fmt.Sprintf("Connecting to %s@%s...", host.User, host.HostName)

	// Connect in a goroutine
	return m, func() tea.Msg {
		client, err := remote.Connect(*host)
		if err != nil {
			log.Printf("[ERROR] SSHConnectHost failed: host=%s err=%v", host.Name, err)
			return sshConnectMsg{hostName: host.Name, err: err}
		}
		log.Printf("[ACTION] SSHConnectHost success: host=%s", host.Name)
		return sshConnectMsg{hostName: host.Name, client: client, err: nil}
	}
}

// sshConnectMsg is sent when an SSH connection attempt completes (from host list).
type sshConnectMsg struct {
	hostName string
	client   *remote.SSHClient
	err      error
}

// sshAddHostResultMsg is sent when an SSH add-host connection test completes.
type sshAddHostResultMsg struct {
	host remote.SSHHost
	err  error
}

// reloadRemotePane loads directory listing from a remote SSH mount.
func (m *Model) reloadRemotePane(id PaneID, preserve string) error {
	pane := m.paneByID(id)
	mount, ok := pane.CurrentRemote()
	if !ok {
		return fmt.Errorf("not in remote mount")
	}

	entries, err := remoteDirToEntries(pane.Path, mount.Client)
	if err != nil {
		log.Printf("[ERROR] reloadRemotePane: id=%s path=%s err=%v", id, pane.Path, err)
		return err
	}
	pane.SetEntries(entries, strings.ToLower(preserve))
	log.Printf("[PANEL] reloadRemotePane: id=%s path=%s entries=%d preserve=%s", id, pane.Path, len(entries), preserve)
	return nil
}

// handleSSHOpenAddHost opens the SSH connect dialog for adding a new host.
func (m *Model) handleSSHOpenAddHost() {
	if m.ssh == nil {
		m.status = "SSH not available"
		return
	}

	log.Printf("[ACTION] SSHOpenAddHost — opening add host dialog")
	// Reset inputs to defaults
	m.ssh.inputs = SSHConnectDialogInputs()
	m.ssh.inputFocus = 0
	m.ssh.showHelp = false

	m.modal = modalState{
		kind:  modalSSHConnect,
		title: "Add SSH host",
		body:  "Fill in the connection details.\nTab/Shift+Tab to switch fields. F1/? for help.",
		note:  "",
	}
}

// handleSSHAddHostConfirm tests the connection asynchronously, then saves the host.
// The modal stays open during the test so the user can see progress and press Esc to cancel.
func (m *Model) handleSSHAddHostConfirm() (tea.Model, tea.Cmd) {
	if m.ssh == nil {
		m.modal = modalState{}
		m.status = "SSH not available"
		return m, nil
	}

	host := buildSSHHostFromDialog(m.ssh.inputs)
	if host.Name == "" || host.HostName == "" || host.User == "" {
		m.status = "Name, Hostname/IP, and User are required"
		return m, nil
	}

	log.Printf("[ACTION] SSHAddHostConfirm: testing connection to %s@%s", host.User, host.HostName)

	// Mark connection test as in progress so the modal shows "Connecting..." state
	m.ssh.testingConn = true
	m.busy = true
	m.status = fmt.Sprintf("Testing connection to %s@%s...", host.User, host.HostName)

	// Use a context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	m.ssh.cancelTest = cancel

	return m, func() tea.Msg {
		defer cancel()

		// Run connection in a separate goroutine, listen for cancellation
		type connResult struct {
			client *remote.SSHClient
			err    error
		}
		resultCh := make(chan connResult, 1)

		go func() {
			client, err := remote.Connect(host)
			resultCh <- connResult{client: client, err: err}
		}()

		select {
		case <-ctx.Done():
			// Cancelled — wait for connect to finish (if still running) and discard
			select {
			case res := <-resultCh:
				if res.client != nil {
					res.client.Close()
				}
			default:
			}
			return sshAddHostResultMsg{host: host, err: fmt.Errorf("cancelled")}
		case res := <-resultCh:
			if res.err != nil {
				return sshAddHostResultMsg{host: host, err: res.err}
			}
			res.client.Close()
			return sshAddHostResultMsg{host: host, err: nil}
		}
	}
}

// handleSSHHostDelete removes a user-added SSH host from the store.
// Hosts from ~/.ssh/config are read-only and cannot be deleted here.
// Shows a confirmation dialog before removing the host.
func (m *Model) handleSSHHostDelete() (tea.Model, tea.Cmd) {
	if m.ssh == nil {
		m.status = "SSH not available"
		return m, nil
	}

	selected, ok := m.activePane().Selected()
	if !ok {
		m.status = "Nothing to delete"
		return m, nil
	}

	hostName := selected.RemoteHostName
	if hostName == "" {
		// Fallback: extract from path "ssh://hostname"
		hostName = strings.TrimPrefix(selected.Path, "ssh://")
	}

	// Find the host to check if it's from SSH config
	host := m.ssh.store.FindByName(hostName)
	if host == nil {
		m.status = fmt.Sprintf("Host %q not found", hostName)
		return m, nil
	}

	if host.FromSSHConfig {
		log.Printf("[SKIP] SSH host delete: %q is from ~/.ssh/config (read-only)", hostName)
		m.status = fmt.Sprintf("Host %q is from SSH config and cannot be deleted", hostName)
		return m, nil
	}

	// Build host details for the confirmation dialog
	hostAddr := host.HostName
	if host.Port != "" && host.Port != "22" {
		hostAddr = fmt.Sprintf("%s:%s", host.HostName, host.Port)
	}
	hostUser := host.User
	if hostUser == "" {
		hostUser = "(default)"
	}

	title := fmt.Sprintf("Delete host %q?", hostName)
	body := fmt.Sprintf("Name:     %s\nAddress:  %s\nUser:     %s\n\nThis will remove the host from saved list.", hostName, hostAddr, hostUser)

	m.openConfirmModal(
		title,
		body,
		"confirm-actions",
		pendingOperation{
			kind:        opDeleteHost,
			sourcePaths: []string{hostName},
		},
	)
	return m, nil
}

// executeSSHHostDelete performs the actual SSH host removal after confirmation.
func (m *Model) executeSSHHostDelete(hostName string) (tea.Model, tea.Cmd) {
	if m.ssh == nil {
		m.status = "SSH not available"
		return m, nil
	}

	log.Printf("[ACTION] SSH host delete: removing host %q", hostName)

	// Close and clean up active client if any
	if client, ok := m.ssh.activeClients[hostName]; ok {
		client.Close()
		delete(m.ssh.activeClients, hostName)
	}

	if err := m.ssh.store.RemoveHost(hostName); err != nil {
		m.status = fmt.Sprintf("Failed to delete host: %v", err)
		return m, nil
	}

	m.status = fmt.Sprintf("Host %q deleted", hostName)

	// Remove from connected hosts tracking
	delete(m.ssh.connectedHosts, hostName)

	// Refresh the SSH host list
	entries := buildSSHHostEntries(m.ssh.store, m.ssh.connectedHosts)
	m.activePane().SetEntries(entries, "")
	return m, nil
}

// enterRemoteDir navigates into a remote directory on an SSH mount.
func (m *Model) enterRemoteDir(entry vfs.Entry) (tea.Model, tea.Cmd) {
	m.hover = hoverState{}
	pane := m.activePane()

	if !entry.IsDir {
		return m, nil
	}

	// Save cursor position for the current directory before navigating away.
	pane.SaveCursor(pane.Path, entry.Name)

	pane.PushHistory(pane.Path)
	log.Printf("[NAV] enterRemoteDir — pane=%s from=%s to=%s", pane.ID, pane.Path, entry.Path)
	pane.Path = entry.Path
	// Restore cursor position if we've visited this directory before in this session.
	preserve := pane.LoadCursor(pane.Path)
	if preserve == "" {
		preserve = entry.Name
	}
	if err := m.reloadRemotePane(pane.ID, preserve); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Entered %s", pane.Path)
	return m, m.loadPreviewCmd()
}

// remoteDeleteCmd deletes files/directories on a remote host via SFTP.
func (m *Model) remoteDeleteCmd(sources []string, client *remote.SSHClient) tea.Cmd {
	log.Printf("[ACTION] remoteDeleteCmd: sources=%v", sources)
	return func() tea.Msg {
		for _, sourcePath := range sources {
			log.Printf("[JOB] remoteDelete: removing path=%s", sourcePath)
			if err := client.RemoveRecursive(sourcePath); err != nil {
				log.Printf("[ERROR] remoteDelete failed: path=%s err=%v", sourcePath, err)
				return opMsg{kind: opDelete, sourcePath: sourcePath, err: err}
			}
		}
		log.Printf("[DONE] remoteDelete: completed %d sources", len(sources))
		return opMsg{kind: opDelete}
	}
}

// remoteTransferCmd copies or moves files between local and remote filesystems.
// kind: opCopy or opMove
// sources: paths on the source side
// targetDir: path on the target side
// sourceIsRemote: true if sources are remote paths
// targetIsRemote: true if targetDir is a remote path
// srcClient: SFTP client for reading (if source is remote)
// dstClient: SFTP client for writing (if target is remote)
// startRemoteCopyJob copies files between local and remote filesystems with
// progress reporting. Runs the transfer in a goroutine so the UI stays
// responsive, following the same pattern as startCopyJob.
func (m *Model) startRemoteCopyJob(kind fileOpKind, sources []string, targetDir string,
	sourceIsRemote, targetIsRemote bool, srcClient, dstClient *remote.SSHClient,
	stats vfs.TransferStats) tea.Cmd {

	m.nextCopyJob++
	jobID := m.nextCopyJob
	ctx, cancel := context.WithCancel(context.Background())

	m.copyJob = &copyJobState{
		id:          jobID,
		kind:        kind,
		sourcePaths: append([]string(nil), sources...),
		targetDir:   targetDir,
		progress: vfs.CopyProgress{
			FilesDone:   0,
			FilesTotal:  stats.FilesTotal,
			BytesDone:   0,
			BytesTotal:  stats.BytesTotal,
			CurrentPath: sources[0],
			Stage:       "Counting files...",
		},
		cancel:    cancel,
		startedAt: time.Now(),
	}
	m.modal = modalState{kind: modalCopyProgress}
	m.status = strings.Title(operationVerb(kind)) + " started"

	log.Printf("[JOB] startRemoteCopyJob: id=%d kind=%d sources=%d targetDir=%s srcRemote=%v dstRemote=%v stats={files=%d bytes=%d}",
		jobID, kind, len(sources), targetDir, sourceIsRemote, targetIsRemote, stats.FilesTotal, stats.BytesTotal)

	return tea.Batch(
		func() tea.Msg {
			go func() {
				doneFiles := 0
				var doneBytes int64
				totalFiles := 0

				log.Printf("[JOB] remoteCopy — goroutine started: job=%d kind=%d sources=%d srcRemote=%v dstRemote=%v",
					jobID, kind, len(sources), sourceIsRemote, targetIsRemote)

				// Phase 1: count files across all sources
				sourceCounts := make([]int, len(sources))
				for i, sourcePath := range sources {
					if err := ctx.Err(); err != nil {
						m.copyProgress <- copyDoneMsg{jobID: jobID, kind: kind, sourcePaths: sources, targetDir: targetDir, err: err}
						return
					}
					var count int
					if sourceIsRemote {
						count = 1
						info, err := srcClient.Lstat(sourcePath)
						if err != nil {
							log.Printf("[JOB] remoteCopy — Phase1 Lstat failed: job=%d path=%s err=%v", jobID, sourcePath, err)
							m.copyProgress <- copyDoneMsg{jobID: jobID, kind: kind, sourcePaths: sources, targetDir: targetDir, err: err}
							return
						}
						if info.IsDir() {
							err := srcClient.Walk(sourcePath, func(_ string, info os.FileInfo, err error) error {
								if err != nil { return err }
								if !info.IsDir() { count++ }
								return nil
							})
							if err != nil {
								m.copyProgress <- copyDoneMsg{jobID: jobID, kind: kind, sourcePaths: sources, targetDir: targetDir, err: err}
								return
							}
						}
					} else {
						entryStats, err := vfs.CopyStats(sourcePath)
						if err != nil {
							m.copyProgress <- copyDoneMsg{jobID: jobID, kind: kind, sourcePaths: sources, targetDir: targetDir, err: err}
							return
						}
						count = entryStats.FilesTotal
					}
					sourceCounts[i] = count
					totalFiles += count
					m.copyProgress <- copyProgressMsg{
						jobID: jobID,
						progress: vfs.CopyProgress{
							FilesDone:   0,
							FilesTotal:  totalFiles,
							CurrentPath: sourcePath,
							Stage:       fmt.Sprintf("Counting files... %d found", totalFiles),
						},
					}
				}

				// Phase 2: transfer with known total
				log.Printf("[JOB] remoteCopy — Phase2 start: job=%d totalFiles=%d", jobID, totalFiles)
				for i, sourcePath := range sources {
					if err := ctx.Err(); err != nil {
						m.copyProgress <- copyDoneMsg{jobID: jobID, kind: kind, sourcePaths: sources, targetDir: targetDir, err: err}
						return
					}
					baseName := filepath.Base(sourcePath)
					targetPath := filepath.Join(targetDir, baseName)
					log.Printf("[JOB] remoteCopy — Phase2 source[%d]: job=%d path=%s target=%s doneFiles=%d totalFiles=%d",
						i, jobID, sourcePath, targetPath, doneFiles, totalFiles)
					log.Printf("[JOB] remoteCopy — starting: job=%d source=%s target=%s", jobID, sourcePath, targetPath)

					m.copyProgress <- copyProgressMsg{
						jobID: jobID,
						progress: vfs.CopyProgress{
							FilesDone:   doneFiles,
							FilesTotal:  totalFiles,
							CurrentPath: sourcePath,
							Stage:       "Transferring files...",
						},
					}

					var info os.FileInfo
					var err error
					serverSide := false   // true if copy/move happened on server without streaming
					trackedProgress := false // true if per-file progress callback was used

					// Get file info and perform copy depending on direction.
					switch {
					case !sourceIsRemote && targetIsRemote:
						// Local → Remote
						log.Printf("[JOB] remoteCopy — direction: Local→Remote job=%d", jobID)
						info, err = os.Stat(sourcePath)
						if err == nil {
							if info.IsDir() {
								log.Printf("[JOB] remoteCopy — local→remote dir: job=%d source=%s target=%s", jobID, sourcePath, targetPath)
								err = dstClient.CopyDirToRemoteProgress(sourcePath, targetPath, func(path string, done, total int) {
									trackedProgress = true
									m.copyProgress <- copyProgressMsg{
										jobID: jobID,
										progress: vfs.CopyProgress{
											FilesDone:   doneFiles + done,
											FilesTotal:  totalFiles,
											CurrentPath: path,
											Stage:       "Copying files...",
										},
									}
								}, ctx)
							} else {
								log.Printf("[JOB] remoteCopy — local→remote file: job=%d source=%s target=%s size=%d", jobID, sourcePath, targetPath, info.Size())
								err = dstClient.CopyFileToRemote(sourcePath, targetPath)
							}
						}

					case sourceIsRemote && !targetIsRemote:
						// Remote → Local
						log.Printf("[JOB] remoteCopy — direction: Remote→Local job=%d", jobID)
						info, err = srcClient.Lstat(sourcePath)
						if err == nil {
							if info.IsDir() {
								log.Printf("[JOB] remoteCopy — remote→local dir: job=%d source=%s target=%s", jobID, sourcePath, targetPath)
								err = srcClient.CopyDirFromRemoteProgress(sourcePath, targetPath, func(path string, done, total int) {
									trackedProgress = true
									m.copyProgress <- copyProgressMsg{
										jobID: jobID,
										progress: vfs.CopyProgress{
											FilesDone:   doneFiles + done,
											FilesTotal:  totalFiles,
											CurrentPath: path,
											Stage:       "Copying files...",
										},
									}
								}, ctx)
							} else {
								log.Printf("[JOB] remoteCopy — remote→local file: job=%d source=%s target=%s size=%d", jobID, sourcePath, targetPath, info.Size())
								err = srcClient.CopyFileFromRemote(sourcePath, targetPath)
							}
						}

					case sourceIsRemote && targetIsRemote:
						// Remote → Remote
						log.Printf("[JOB] remoteCopy — direction: Remote→Remote job=%d sameHost=%v", jobID, srcClient.SameHostAs(dstClient))
						info, err = srcClient.Lstat(sourcePath)
						if err != nil {
							break
						}

						if srcClient.SameHostAs(dstClient) {
							// Same host: use server-side commands, no local streaming
							srcEscaped := "'" + sourcePath + "'"
							dstEscaped := "'" + targetPath + "'"
							if kind == opMove {
								log.Printf("[JOB] remoteCopy — same-host move: job=%d src=%s dst=%s", jobID, sourcePath, targetPath)
								_, err = srcClient.Exec("mv " + srcEscaped + " " + dstEscaped)
								if err == nil {
									serverSide = true
								} else {
									log.Printf("[JOB] remoteCopy — same-host mv failed, falling back: %v", err)
								}
							}
							if !serverSide {
								log.Printf("[JOB] remoteCopy — same-host copy: job=%d src=%s dst=%s", jobID, sourcePath, targetPath)
								m.copyProgress <- copyProgressMsg{
									jobID: jobID,
									progress: vfs.CopyProgress{
										FilesDone:   doneFiles,
										FilesTotal:  totalFiles,
										CurrentPath: sourcePath,
										Stage:       "Running remote command...",
									},
								}
								cmd := "cp -r"
								if !info.IsDir() {
									cmd = "cp"
								}
								err = srcClient.ExecWithProgress(cmd+" "+srcEscaped+" "+dstEscaped, func(line string) {
									// cp -rv outputs one line per copied file
									doneFiles++
									log.Printf("[JOB] remoteCopy — same-host progress: job=%d doneFiles=%d line=%s", jobID, doneFiles, line)
									m.copyProgress <- copyProgressMsg{
										jobID: jobID,
										progress: vfs.CopyProgress{
											FilesDone:   doneFiles,
											FilesTotal:  totalFiles,
											CurrentPath: line,
											Stage:       "Copying files...",
										},
									}
								})
								if err == nil {
									serverSide = true
								} else {
									log.Printf("[JOB] remoteCopy — same-host cp failed, falling back: %v", err)
								}
							}
						}

						if !serverSide {
							// Different hosts or same-host exec failed: stream through local
							log.Printf("[JOB] remoteCopy — streaming through local: job=%d src=%s dst=%s", jobID, sourcePath, targetPath)
							if info.IsDir() {
								err = remote.CopyDirBetweenRemotesProgress(srcClient, dstClient, sourcePath, targetPath, func(path string, done, total int) {
									trackedProgress = true
									m.copyProgress <- copyProgressMsg{
										jobID: jobID,
										progress: vfs.CopyProgress{
											FilesDone:   doneFiles + done,
											FilesTotal:  totalFiles,
											CurrentPath: path,
											Stage:       "Copying files...",
										},
									}
								}, ctx)
							} else {
								err = remote.CopyFileBetweenRemotes(srcClient, dstClient, sourcePath, targetPath)
							}
						}
					}

					if err != nil {
						log.Printf("[ERROR] remoteCopy — transfer failed: job=%d source=%s target=%s err=%v", jobID, sourcePath, targetPath, err)
						m.copyProgress <- copyDoneMsg{
							jobID: jobID, kind: kind,
							sourcePaths: sources, targetDir: targetDir,
							err: err,
						}
						return
					}

					// For move operations, delete the source after copying
					// Skip if server-side move was used (source already moved)
					if kind == opMove && !serverSide {
						log.Printf("[JOB] remoteCopy — move, removing source: job=%d source=%s", jobID, sourcePath)
						if !sourceIsRemote {
							if err := os.RemoveAll(sourcePath); err != nil {
								log.Printf("[ERROR] remoteCopy — remove local source failed: job=%d source=%s err=%v", jobID, sourcePath, err)
								m.copyProgress <- copyDoneMsg{
									jobID: jobID, kind: kind,
									sourcePaths: sources, targetDir: targetDir,
									err: err,
								}
								return
							}
						} else {
							if err := srcClient.RemoveRecursive(sourcePath); err != nil {
								log.Printf("[ERROR] remoteCopy — remove remote source failed: job=%d source=%s err=%v", jobID, sourcePath, err)
								m.copyProgress <- copyDoneMsg{
									jobID: jobID, kind: kind,
									sourcePaths: sources, targetDir: targetDir,
									err: err,
								}
								return
							}
						}
					}

					log.Printf("[JOB] remoteCopy — source done: job=%d path=%s err=%v doneFiles=%d totalFiles=%d serverSide=%v",
						jobID, sourcePath, err, doneFiles, totalFiles, serverSide)
					if !serverSide || kind == opMove {
						if !trackedProgress {
							doneFiles += sourceCounts[i]
						}
					}
					if info != nil && !info.IsDir() {
						doneBytes += info.Size()
					}

					// Send file-level progress update so the UI stays responsive
					m.copyProgress <- copyProgressMsg{
						jobID: jobID,
						progress: vfs.CopyProgress{
							FilesDone:   doneFiles,
							FilesTotal:  totalFiles,
							BytesDone:   doneBytes,
							BytesTotal:  0,
							CurrentPath: sourcePath,
							Stage:       "Transferring files...",
						},
					}
				}

				firstItem := filepath.Join(targetDir, filepath.Base(sources[0]))
				log.Printf("[DONE] remoteCopy — job=%d completed: kind=%d files=%d bytes=%d firstItem=%s",
					jobID, kind, doneFiles, doneBytes, firstItem)
				m.copyProgress <- copyDoneMsg{
					jobID:       jobID,
					kind:        kind,
					sourcePaths: sources,
					targetDir:   targetDir,
					targetPath:  firstItem,
				}
			}()
			return nil
		},
		waitCopyProgressCmd(m.copyProgress),
	)
}

// renderSSHConnectModal renders the SSH connect/add-host dialog.
func renderSSHConnectModal(m Model, palette theme.Palette, width int) string {
	if m.ssh == nil {
		return ""
	}

	outerWidth := max(width, 8)
	contentWidth := max(outerWidth-6, 1)
	titleStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Bold(true).Foreground(palette.Accent)
	noteStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.Muted)
	spacer := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Render(" ")

	labelStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Background(palette.Panel).
		Foreground(palette.Info).
		Bold(true)

	inputFieldStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Background(palette.Panel)

	// Connecting indicator style
	connectingStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Background(palette.Panel).
		Foreground(palette.Warning).
		Bold(true)

	box := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(1, 2).
		Background(palette.Panel).
		Foreground(palette.Text).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(palette.BorderActive).
		BorderBackground(palette.Panel)

	lines := []string{titleStyle.Render("Add SSH host"), spacer}

	// Show connecting state when test is in progress
	if m.ssh.testingConn {
		lines = append(lines, connectingStyle.Render("⏳ Testing connection..."))
		lines = append(lines, spacer)
		// Show the host details that were entered
		for i, input := range m.ssh.inputs {
			label := sshDialogLabel(i)
			val := input.Value()
			displayVal := val
			if i == 4 && val != "" {
				displayVal = "••••••••"
			}
			row := labelStyle.Render(label) + inputFieldStyle.Render(displayVal)
			lines = append(lines, row)
		}
		lines = append(lines, spacer)
		if hl, ok := renderModalHintTokens("Esc to cancel", contentWidth, palette, palette.Muted); ok {
			lines = append(lines, hl)
		}
	} else if m.ssh.showHelp {
		helpStyle := lipgloss.NewStyle().Width(contentWidth).Background(palette.Panel).Foreground(palette.Muted)
		for _, raw := range strings.Split(formatSSHConnectHelp(), "\n") {
			lines = append(lines, helpStyle.Render(raw))
		}
		lines = append(lines, spacer, noteStyle.Render("Press F1/? to close help"))
	} else {
		// Render input fields with labels
		for i, input := range m.ssh.inputs {
			label := sshDialogLabel(i)
			lines = append(lines, labelStyle.Render(label))
			lines = append(lines, inputFieldStyle.Render(input.View()))
			if i < len(m.ssh.inputs)-1 {
				lines = append(lines, spacer)
			}
		}
		lines = append(lines, spacer)
		// Use hint token renderer to highlight Enter, Esc, F1
		if hl, ok := renderModalHintTokens("Enter to confirm · Esc to cancel · F1/? for help", contentWidth, palette, palette.Muted); ok {
			lines = append(lines, hl)
		} else {
			lines = append(lines, noteStyle.Render("Enter to confirm · Esc to cancel · F1/? for help"))
		}
	}

	return box.Render(strings.Join(lines, "\n"))
}
