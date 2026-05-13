package ui

import (
	"fmt"
	"path"
	"strings"

	vfs "vcom/internal/fs"
	"vcom/internal/fs/remote"

	"github.com/charmbracelet/bubbles/textinput"
)

// isRemoteHostEntry returns true if the entry represents an SSH host.
func isRemoteHostEntry(entry vfs.Entry) bool {
	return entry.IsRemote
}

// isRemoteAddHostEntry returns true if the entry is the "Add host" special item.
func isRemoteAddHostEntry(entry vfs.Entry) bool {
	return entry.IsRemote && entry.Name == "+ Add host"
}

// buildSSHHostEntries creates virtual directory entries for SSH hosts.
// connectedHosts optionally specifies which hosts have active connections
// (host name -> true). When non-nil, entries show a connection status prefix.
func buildSSHHostEntries(store *remote.HostStore, connectedHosts map[string]bool) []vfs.Entry {
	hosts := store.AllHosts()
	entries := make([]vfs.Entry, 0, len(hosts)+1)

	// Find the longest host name so we can pad them all to the same width,
	// making the (user@host) suffix align vertically across entries.
	maxNameLen := 0
	for _, h := range hosts {
		if l := len(h.Name); l > maxNameLen {
			maxNameLen = l
		}
	}

	for _, h := range hosts {
		isConnected := connectedHosts != nil && connectedHosts[h.Name]

		addr := h.HostName
		if h.Port != "" && h.Port != "22" {
			addr = fmt.Sprintf("%s:%s", addr, h.Port)
		}
		suffix := ""
		if h.User != "" {
			suffix = fmt.Sprintf(" (%s@%s)", h.User, addr)
		} else {
			suffix = fmt.Sprintf(" (%s)", addr)
		}
		paddedName := h.Name + strings.Repeat(" ", maxNameLen-len(h.Name))

		entries = append(entries, vfs.Entry{
			Name:           paddedName + suffix,
			Path:           "ssh://" + h.Name,
			IsDir:          true,
			IsRemote:       true,
			Connected:      isConnected,
			RemoteHostName: h.Name,
			Mode:           0o755,
		})
	}

	// Add "Add host" entry at the bottom
	entries = append(entries, vfs.Entry{
		Name:     "+ Add host",
		Path:     "ssh://add-host",
		IsDir:    false,
		IsRemote: true,
	})

	return entries
}

// SSHConnectDialogInputs creates the text input fields for the SSH connect dialog.
func SSHConnectDialogInputs() []textinput.Model {
	fields := make([]textinput.Model, 5)

	placeholders := []string{"my-server", "192.168.1.100", "22", "root", ""}

	for i := range fields {
		ti := textinput.New()
		ti.Placeholder = placeholders[i]
		ti.CharLimit = 128
		ti.Width = 40
		if i == 4 {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		if i == 0 {
			ti.Focus()
		}
		fields[i] = ti
	}

	return fields
}

// sshDialogLabel returns the label for an SSH dialog field at the given index.
func sshDialogLabel(index int) string {
	lbls := []string{"Name:", "Hostname/IP:", "Port:", "User:", "Password:"}
	if index >= 0 && index < len(lbls) {
		return lbls[index]
	}
	return ""
}

// sshDialogValue returns the value for an SSH dialog field.
func sshDialogValue(inputs []textinput.Model, index int) string {
	if index >= 0 && index < len(inputs) {
		return inputs[index].Value()
	}
	return ""
}

// buildSSHHostFromDialog creates an SSHHost from dialog input values.
func buildSSHHostFromDialog(inputs []textinput.Model) remote.SSHHost {
	host := remote.SSHHost{
		Name:     strings.TrimSpace(sshDialogValue(inputs, 0)),
		HostName: strings.TrimSpace(sshDialogValue(inputs, 1)),
		Port:     strings.TrimSpace(sshDialogValue(inputs, 2)),
		User:     strings.TrimSpace(sshDialogValue(inputs, 3)),
		Password: sshDialogValue(inputs, 4),
	}
	if host.Port == "" {
		host.Port = "22"
	}
	return host
}

// formatSSHConnectHelp returns the help text for the SSH connect dialog.
func formatSSHConnectHelp() string {
	return `Fields:
  Name        — alias for this host (required)
  Hostname/IP — server address (required)
  Port        — SSH port, default 22
  User        — login username (required)
  Password    — password for password-based auth

Navigation: Tab / Shift+Tab between fields
Confirm: Enter
Close: Esc / q
Help: F1 / ?`
}

// remoteDirToEntries reads a remote directory via SFTP and converts to vfs.Entry slice.
func remoteDirToEntries(remotePath string, sshClient *remote.SSHClient) ([]vfs.Entry, error) {
	fileInfos, err := sshClient.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}

	entries := make([]vfs.Entry, 0, len(fileInfos)+1)

	// Add parent directory entry if not at root
	if remotePath != "/" {
		parent := path.Dir(remotePath)
		if parent == "" {
			parent = "/"
		}
		entries = append(entries, vfs.Entry{
			Name:     "..",
			Path:     parent,
			IsDir:    true,
			IsParent: true,
		})
	}

	for _, info := range fileInfos {
		name := info.Name()
		isDir := info.IsDir()
		fullPath := path.Join(remotePath, name)

		ext := ""
		if idx := strings.LastIndex(name, "."); idx > 0 {
			ext = strings.ToLower(name[idx+1:])
		}
		entry := vfs.Entry{
			Name:       name,
			Path:       fullPath,
			Extension:  ext,
			Mode:       info.Mode(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
			IsDir:      isDir,
			IsHidden:   strings.HasPrefix(name, "."),
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// sshState holds SSH-related state for the model.
type sshState struct {
	store      *remote.HostStore
	inputs     []textinput.Model
	inputFocus int
	showHelp   bool

	// Connection test state
	testingConn bool   // true while an async connection test is in progress
	cancelTest  func() // call to abort a pending connection test

	// connectedHosts tracks which host names have active SSH connections.
	// Updated when connections are established or closed.
	connectedHosts map[string]bool

	// activeClients stores SSH clients for hosts whose connections are kept alive
	// after returning to the host list. Clients are closed on full SSH mode exit.
	activeClients map[string]*remote.SSHClient
}

// cycleInput shifts focus between dialog inputs by delta (+1/-1).
func (s *sshState) cycleInput(delta int) {
	if s == nil || len(s.inputs) == 0 {
		return
	}
	s.inputs[s.inputFocus].Blur()
	s.inputFocus = (s.inputFocus + delta) % len(s.inputs)
	if s.inputFocus < 0 {
		s.inputFocus += len(s.inputs)
	}
	s.inputs[s.inputFocus].Focus()
}

// newSSHState creates a new sshState with a HostStore.
func newSSHState() (*sshState, error) {
	store, err := remote.NewHostStore()
	if err != nil {
		return nil, err
	}
	return &sshState{
		store:          store,
		inputs:         SSHConnectDialogInputs(),
		inputFocus:     0,
		activeClients:  make(map[string]*remote.SSHClient),
		showHelp:       false,
		connectedHosts: make(map[string]bool),
	}, nil
}
