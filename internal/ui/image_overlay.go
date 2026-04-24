package ui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

const kittyImageID = 31337
const assumedCellAspect = 0.5
const kittyPixelsPerCell = 16

func minFloat(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
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
	m.kittyOK = true
	return m.kittyOK
}

func writeKittyEscape(control string, payload []byte) error {
	if payload == nil {
		_, err := fmt.Fprintf(os.Stdout, "\x1b_G%s\x1b\\", control)
		return err
	}

	encoded := base64.StdEncoding.EncodeToString(payload)
	_, err := fmt.Fprintf(os.Stdout, "\x1b_G%s;%s\x1b\\", control, encoded)
	return err
}

func resizeNearest(src image.Image, target image.Point) image.Image {
	bounds := src.Bounds()
	srcSize := bounds.Size()
	if target.X <= 0 || target.Y <= 0 || srcSize.X <= target.X && srcSize.Y <= target.Y {
		return src
	}

	dst := image.NewRGBA(image.Rect(0, 0, target.X, target.Y))
	for y := 0; y < target.Y; y++ {
		srcY := bounds.Min.Y + (y * srcSize.Y / target.Y)
		for x := 0; x < target.X; x++ {
			srcX := bounds.Min.X + (x * srcSize.X / target.X)
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func scaleImageToRect(img image.Image, rect overlayRect) image.Image {
	size := img.Bounds().Size()
	if size.X <= 0 || size.Y <= 0 || rect.width <= 0 || rect.height <= 0 {
		return img
	}

	maxWidth := rect.width * kittyPixelsPerCell
	maxHeight := rect.height * kittyPixelsPerCell
	if maxWidth <= 0 || maxHeight <= 0 {
		return img
	}

	scale := minFloat(float64(maxWidth)/float64(size.X), float64(maxHeight)/float64(size.Y))
	if scale >= 1 {
		return img
	}

	target := image.Point{
		X: max(int(float64(size.X)*scale), 1),
		Y: max(int(float64(size.Y)*scale), 1),
	}
	return resizeNearest(img, target)
}

func loadKittyPayload(path string, rect overlayRect) ([]byte, image.Point, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, image.Point{}, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, image.Point{}, err
	}

	img = scaleImageToRect(img, rect)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, image.Point{}, err
	}
	return buf.Bytes(), img.Bounds().Size(), nil
}

func kittyPlacementControl(rect overlayRect, size image.Point) string {
	if size.X <= 0 || size.Y <= 0 {
		return fmt.Sprintf("a=T,f=100,t=d,i=%d,c=%d,C=1", kittyImageID, rect.width)
	}

	availableAspect := (float64(rect.width) * assumedCellAspect) / float64(rect.height)
	imageAspect := float64(size.X) / float64(size.Y)
	if imageAspect >= availableAspect {
		return fmt.Sprintf("a=T,f=100,t=d,i=%d,c=%d,C=1", kittyImageID, rect.width)
	}
	return fmt.Sprintf("a=T,f=100,t=d,i=%d,r=%d,C=1", kittyImageID, rect.height)
}

func (m *imageOverlayManager) showWithKitty(path string, rect overlayRect) error {
	data, size, err := loadKittyPayload(path, rect)
	if err != nil {
		return err
	}

	if err := writeKittyEscape(fmt.Sprintf("a=d,d=I,i=%d", kittyImageID), nil); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "\x1b7\x1b[%d;%dH", rect.y+1, rect.x+1); err != nil {
		return err
	}

	const chunkSize = 4096
	for offset := 0; offset < len(data); offset += chunkSize {
		end := min(offset+chunkSize, len(data))
		chunk := data[offset:end]
		more := 0
		if end < len(data) {
			more = 1
		}

		control := fmt.Sprintf("m=%d", more)
		if offset == 0 {
			control = fmt.Sprintf("%s,m=%d", kittyPlacementControl(rect, size), more)
		}
		if err := writeKittyEscape(control, chunk); err != nil {
			return err
		}
	}

	_, err = fmt.Fprint(os.Stdout, "\x1b8")
	return err
}

func (m *imageOverlayManager) clearKitty() {
	_ = writeKittyEscape(fmt.Sprintf("a=d,d=I,i=%d", kittyImageID), nil)
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

func (m *imageOverlayManager) startLegacyBackend() error {
	cmd := exec.Command("ueberzug", "layer", "--parser", "json")
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
	m.backend = "ueberzug"
	return nil
}

func (m *imageOverlayManager) ensureStarted() error {
	if m.running {
		return nil
	}

	if _, err := exec.LookPath("ueberzugpp"); err == nil {
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
	}

	if _, err := exec.LookPath("ueberzug"); err == nil {
		if err := m.startLegacyBackend(); err != nil {
			return err
		}
		if err := m.send(map[string]any{
			"action":     "remove",
			"identifier": m.identifier,
		}); err != nil {
			m.stop()
			return err
		}
		return nil
	}

	return fmt.Errorf("could not start image overlay backend")
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
		}
		if m.backend == "ueberzug" {
			payload["width"] = rect.width
			payload["height"] = rect.height
		} else {
			payload["max_width"] = rect.width
			payload["max_height"] = rect.height
			payload["scaler"] = "fit_contain"
		}
		if err := m.send(payload); err == nil {
			m.visible = true
			m.lastPath = path
			m.lastRect = rect
			return nil
		}

		m.stop()
		if m.backend != "ueberzug" && len(m.backends) > 0 {
			m.backends = append(m.backends[1:], m.backends[0])
		} else {
			break
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
	case "ueberzugpp", "ueberzug":
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
