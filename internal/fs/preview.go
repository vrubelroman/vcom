package vfs

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var sgrRegexp = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)
var sgrNumberRegexp = regexp.MustCompile(`\d+`)

type PreviewKind string

const (
	PreviewKindEmpty     PreviewKind = "empty"
	PreviewKindDirectory PreviewKind = "directory"
	PreviewKindText      PreviewKind = "text"
	PreviewKindImage     PreviewKind = "image"
	PreviewKindBinary    PreviewKind = "binary"
	PreviewKindError     PreviewKind = "error"
)

type Metadata struct {
	Path        string
	Kind        string
	Size        int64
	SizeKnown   bool
	ModifiedAt  string
	CreatedAt   string
	Permissions string
	ImageFormat string
	ImageSize   string
	Extension   string
}

type Preview struct {
	Kind      PreviewKind
	Title     string
	Body      string
	PlainBody string
	Metadata  Metadata
}

type PreviewOptions struct {
	ShowHidden            bool
	DirsFirst             bool
	SortBy                string
	SortReverse           bool
	MaxPreviewBytes       int64
	DirectoryPreviewLimit int
	HumanReadableSize     bool
	ThemeName             string
	UseNerdIcons          bool
	ImagePreviewWidth     int
	ImagePreviewHeight    int
}

func BuildPreview(entry Entry, options PreviewOptions) Preview {
	preview := Preview{
		Kind:  PreviewKindEmpty,
		Title: entry.DisplayName(),
		Metadata: Metadata{
			Path:        entry.Path,
			Kind:        kindLabel(entry),
			Permissions: Permissions(entry.Mode),
			ModifiedAt:  ShortTime(entry.ModifiedAt),
			CreatedAt:   "n/a",
			Extension:   entry.Extension,
		},
	}

	if entry.CreatedKnown {
		preview.Metadata.CreatedAt = ShortTime(entry.CreatedAt)
	}

	if entry.IsDir {
		preview.Kind = PreviewKindDirectory
		preview.Metadata.Size = entry.Size
		preview.Metadata.SizeKnown = entry.DirSizeKnown
		preview.Body = buildDirectoryPreview(entry.Path, options)
		preview.PlainBody = preview.Body
		return preview
	}

	preview.Metadata.Size = entry.Size
	preview.Metadata.SizeKnown = true

	file, err := os.Open(entry.Path)
	if err != nil {
		preview.Kind = PreviewKindError
		preview.Body = fmt.Sprintf("Could not open file:\n\n%s", err)
		preview.PlainBody = preview.Body
		return preview
	}
	defer file.Close()

	buffer := new(bytes.Buffer)
	if _, err := io.CopyN(buffer, file, options.MaxPreviewBytes); err != nil && err != io.EOF {
		preview.Kind = PreviewKindError
		preview.Body = fmt.Sprintf("Could not read preview:\n\n%s", err)
		preview.PlainBody = preview.Body
		return preview
	}

	data := buffer.Bytes()
	if format, dimensions, ok := detectImage(data); ok {
		preview.Kind = PreviewKindImage
		preview.Metadata.ImageFormat = format
		preview.Metadata.ImageSize = dimensions
		inline := renderImageInlinePreview(entry.Path, options.ImagePreviewWidth, options.ImagePreviewHeight)
		if inline != "" {
			preview.Body = inline
		}
		preview.PlainBody = preview.Body
		return preview
	}

	if IsBinarySample(data) {
		preview.Kind = PreviewKindBinary
		preview.Body = "Binary file detected.\n\nSafe inline preview is disabled for this file type."
		preview.PlainBody = preview.Body
		return preview
	}

	preview.Kind = PreviewKindText
	preview.PlainBody = strings.ReplaceAll(string(data), "\t", "    ")
	preview.Body = highlightText(entry.Path, preview.PlainBody, options.ThemeName)
	return preview
}

func highlightText(path string, source string, themeName string) string {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(source)
	}
	if lexer == nil {
		return source
	}

	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		return source
	}

	style := styles.Get(chromaStyleName(themeName))
	if style == nil {
		return source
	}
	style = styleWithoutBackground(style)
	if style == nil {
		return source
	}

	var output bytes.Buffer
	if err := formatters.TTY16m.Format(&output, style, iterator); err != nil {
		return source
	}
	return stripBackgroundSGR(output.String())
}

func styleWithoutBackground(base *chroma.Style) *chroma.Style {
	if base == nil {
		return nil
	}
	builder := base.Builder().Transform(func(entry chroma.StyleEntry) chroma.StyleEntry {
		entry.Background = 0
		return entry
	})
	stripped, err := builder.Build()
	if err != nil {
		return base
	}
	return stripped
}

func stripBackgroundSGR(text string) string {
	return sgrRegexp.ReplaceAllStringFunc(text, func(seq string) string {
		matches := sgrRegexp.FindStringSubmatch(seq)
		if len(matches) != 2 {
			return seq
		}
		filtered := filterSGRParams(matches[1])
		if filtered == "" {
			return ""
		}
		return "\x1b[" + filtered + "m"
	})
}

func filterSGRParams(paramString string) string {
	if paramString == "" {
		return ""
	}

	raw := sgrNumberRegexp.FindAllString(paramString, -1)
	if len(raw) == 0 {
		return ""
	}
	codes := make([]int, 0, len(raw))
	for _, token := range raw {
		value, err := strconv.Atoi(token)
		if err != nil {
			continue
		}
		codes = append(codes, value)
	}

	kept := make([]string, 0, len(codes))
	for i := 0; i < len(codes); i++ {
		code := codes[i]

		if code == 0 {
			// Do not hard-reset background to terminal default.
			// Reset common text attributes + foreground only.
			kept = append(kept, "39", "22", "23", "24", "59")
			continue
		}

		if code == 49 || code == 7 || code == 27 || (code >= 40 && code <= 47) || (code >= 100 && code <= 107) {
			continue
		}

		switch code {
		case 48:
			// Background color payloads:
			// 48;5;n or 48;2;r;g;b (also appears in ':' form; parsed as the same int stream).
			if i+1 < len(codes) {
				mode := codes[i+1]
				switch mode {
				case 5:
					i += 2
				case 2:
					i += 4
				default:
					i++
				}
			}
			continue
		case 38, 58:
			// Preserve foreground (38) and underline color (58) payloads.
			kept = append(kept, strconv.Itoa(code))
			if i+1 < len(codes) {
				mode := codes[i+1]
				kept = append(kept, strconv.Itoa(mode))
				switch mode {
				case 5:
					if i+2 < len(codes) {
						kept = append(kept, strconv.Itoa(codes[i+2]))
					}
					i += 2
				case 2:
					if i+4 < len(codes) {
						kept = append(kept,
							strconv.Itoa(codes[i+2]),
							strconv.Itoa(codes[i+3]),
							strconv.Itoa(codes[i+4]),
						)
					}
					i += 4
				default:
					i++
				}
			}
			continue
		}

		kept = append(kept, strconv.Itoa(code))
	}

	return strings.Join(kept, ";")
}

func chromaStyleName(themeName string) string {
	switch strings.ToLower(strings.TrimSpace(themeName)) {
	case "catppuccin-mocha", "catppuccin-lavender":
		return "catppuccin-mocha"
	case "tokyo-night":
		return "tokyonight-night"
	case "gruvbox-dark", "gruvbox":
		return "gruvbox"
	case "nord-frost", "nord":
		return "nord"
	case "dracula":
		return "dracula"
	case "rose-pine":
		return "rose-pine"
	case "solarized-dark":
		return "solarized-dark"
	default:
		return "catppuccin-mocha"
	}
}

func buildDirectoryPreview(path string, options PreviewOptions) string {
	entries, err := ListDir(path, ListOptions{
		ShowHidden:  options.ShowHidden,
		DirsFirst:   options.DirsFirst,
		SortBy:      options.SortBy,
		SortReverse: options.SortReverse,
	})
	if err != nil {
		return fmt.Sprintf("Could not list directory:\n\n%s", err)
	}
	if len(entries) == 0 {
		return "Directory is empty."
	}

	var lines []string
	for _, entry := range entries {
		if entry.IsParent {
			continue
		}
		icon := previewIcon(entry, options.UseNerdIcons)

		size := ""
		if !entry.IsDir {
			if options.HumanReadableSize {
				size = HumanSize(entry.Size)
			} else {
				size = fmt.Sprintf("%d", entry.Size)
			}
		}

		lines = append(lines, fmt.Sprintf("%s %-36s %12s %s", icon, entry.DisplayName(), size, ShortTime(entry.ModifiedAt)))
		if len(lines) >= options.DirectoryPreviewLimit {
			lines = append(lines, "…")
			break
		}
	}

	return strings.Join(lines, "\n")
}

func previewIcon(entry Entry, useNerdIcons bool) string {
	if !useNerdIcons {
		switch entry.Category() {
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

func renderImageInlinePreview(path string, width int, height int) string {
	return ""
}

func detectImage(data []byte) (string, string, bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "", false
	}
	return format, fmt.Sprintf("%dx%d", cfg.Width, cfg.Height), true
}

func kindLabel(entry Entry) string {
	switch {
	case entry.IsParent:
		return "parent"
	case entry.IsDir:
		return "directory"
	case entry.Extension != "":
		return "file"
	default:
		return strings.TrimPrefix(filepath.Ext(entry.Name), ".")
	}
}
