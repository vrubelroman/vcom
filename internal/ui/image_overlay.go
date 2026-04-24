package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
)

type overlayRect struct {
	x      int
	y      int
	width  int
	height int
}

type imageOverlayManager struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	running    bool
	identifier string
	visible    bool
	backend    string
	backends   []string
	lastPath   string
	lastRect   overlayRect
	kittyTried bool
	kittyOK    bool
}

func newImageOverlayManager() *imageOverlayManager {
	return &imageOverlayManager{identifier: "vcom-preview"}
}

func (m *imageOverlayManager) isKittyTerminal() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	termProgram := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	return os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(term, "kitty") || strings.Contains(termProgram, "kitty")
}

func (m *imageOverlayManager) canUseKitty() bool {
	if !m.isKittyTerminal() {
		return false
	}
	if m.kittyTried {
		return m.kittyOK
	}
	m.kittyTried = true
	_, err := exec.LookPath("kitty")
	m.kittyOK = err == nil
	return m.kittyOK
}

func (m *imageOverlayManager) showWithKitty(path string, rect overlayRect) error {
	args := []string{
		"+kitten", "icat",
		"--stdin=no",
		"--transfer-mode=stream",
		"--image-id=31337",
		"--place", fmt.Sprintf("%dx%d@%dx%d", rect.width, rect.height, rect.x, rect.y),
		"--scale-up",
		"--no-trailing-newline",
		path,
	}
	cmd := exec.Command("kitty", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (m *imageOverlayManager) clearKitty() {
	cmd := exec.Command("kitty", "+kitten", "icat", "--stdin=no", "--clear")
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

func (m *imageOverlayManager) backendOutput() string {
	term := strings.ToLower(os.Getenv("TERM"))
	order := make([]string, 0, 5)
	switch {
	case strings.Contains(term, "kitty"):
		order = append(order, "kitty")
	case os.Getenv("WAYLAND_DISPLAY") != "":
		order = append(order, "wayland")
	case os.Getenv("DISPLAY") != "":
		order = append(order, "x11")
	}
	order = append(order, "wayland", "x11", "sixel", "kitty")

	unique := make([]string, 0, len(order))
	for _, backend := range order {
		if !slices.Contains(unique, backend) {
			unique = append(unique, backend)
		}
	}
	return strings.Join(unique, ",")
}

func (m *imageOverlayManager) backendList() []string {
	if len(m.backends) != 0 {
		return m.backends
	}
	m.backends = strings.Split(m.backendOutput(), ",")
	return m.backends
}

func (m *imageOverlayManager) startBackend(backend string) error {
	cmd := exec.Command("ueberzugpp", "layer", "-o", backend)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	m.cmd = cmd
	m.stdin = stdin
	m.running = true
	m.backend = backend
	return nil
}

func (m *imageOverlayManager) ensureStarted() error {
	if m.running {
		return nil
	}
	if _, err := exec.LookPath("ueberzugpp"); err != nil {
		return err
	}

	var lastErr error
	for _, backend := range m.backendList() {
		if err := m.startBackend(backend); err != nil {
			lastErr = err
			continue
		}
		// Probe command channel right away; some backends terminate instantly.
		if err := m.send(map[string]any{
			"action":     "remove",
			"identifier": m.identifier,
		}); err != nil {
			lastErr = err
			m.stop()
			continue
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("could not start ueberzugpp overlay")
}

func (m *imageOverlayManager) send(payload map[string]any) error {
	if !m.running || m.stdin == nil {
		return fmt.Errorf("overlay not running")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = io.WriteString(m.stdin, string(data)+"\n")
	return err
}

func (m *imageOverlayManager) show(path string, rect overlayRect) error {
	if rect.width <= 1 || rect.height <= 1 {
		return nil
	}
	if m.visible && m.lastPath == path && m.lastRect == rect {
		return nil
	}

	if m.canUseKitty() {
		if m.backend == "ueberzugpp" {
			m.hide()
		}
		if err := m.showWithKitty(path, rect); err == nil {
			m.backend = "kitty"
			m.visible = true
			m.lastPath = path
			m.lastRect = rect
			return nil
		}
	}

	for i := 0; i < len(m.backendList()); i++ {
		if err := m.ensureStarted(); err != nil {
			return err
		}
		payload := map[string]any{
			"action":     "add",
			"identifier": m.identifier,
			"path":       path,
			"x":          rect.x,
			"y":          rect.y,
			"max_width":  rect.width,
			"max_height": rect.height,
			"scaler":     "fit_contain",
		}
		if err := m.send(payload); err == nil {
			m.backend = "ueberzugpp"
			m.visible = true
			m.lastPath = path
			m.lastRect = rect
			return nil
		}

		m.stop()
		if len(m.backends) > 0 {
			m.backends = append(m.backends[1:], m.backends[0])
		}
	}
	return fmt.Errorf("could not render image overlay")
}

func (m *imageOverlayManager) hide() {
	if !m.visible {
		return
	}
	switch m.backend {
	case "kitty":
		m.clearKitty()
	case "ueberzugpp":
		if m.running {
			_ = m.send(map[string]any{
				"action":     "remove",
				"identifier": m.identifier,
			})
		}
	}
	m.visible = false
	m.lastPath = ""
	m.lastRect = overlayRect{}
}

func (m *imageOverlayManager) stop() {
	m.hide()
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_, _ = m.cmd.Process.Wait()
	}
	m.cmd = nil
	m.running = false
	m.backend = ""
}
