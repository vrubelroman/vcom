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
	"strings"
)

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
	Kind     PreviewKind
	Title    string
	Body     string
	Metadata Metadata
}

type PreviewOptions struct {
	ShowHidden            bool
	DirsFirst             bool
	SortBy                string
	SortReverse           bool
	MaxPreviewBytes       int64
	DirectoryPreviewLimit int
	HumanReadableSize     bool
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
		return preview
	}

	preview.Metadata.Size = entry.Size
	preview.Metadata.SizeKnown = true

	file, err := os.Open(entry.Path)
	if err != nil {
		preview.Kind = PreviewKindError
		preview.Body = fmt.Sprintf("Could not open file:\n\n%s", err)
		return preview
	}
	defer file.Close()

	buffer := new(bytes.Buffer)
	if _, err := io.CopyN(buffer, file, options.MaxPreviewBytes); err != nil && err != io.EOF {
		preview.Kind = PreviewKindError
		preview.Body = fmt.Sprintf("Could not read preview:\n\n%s", err)
		return preview
	}

	data := buffer.Bytes()
	if format, dimensions, ok := detectImage(data); ok {
		preview.Kind = PreviewKindImage
		preview.Metadata.ImageFormat = format
		preview.Metadata.ImageSize = dimensions
		preview.Body = fmt.Sprintf(
			"Image preview is metadata-only for now.\n\nFormat: %s\nDimensions: %s\nPath: %s",
			format,
			dimensions,
			entry.Path,
		)
		return preview
	}

	if IsBinarySample(data) {
		preview.Kind = PreviewKindBinary
		preview.Body = "Binary file detected.\n\nSafe inline preview is disabled for this file type."
		return preview
	}

	preview.Kind = PreviewKindText
	preview.Body = strings.ReplaceAll(string(data), "\t", "    ")
	return preview
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
		icon := previewIcon(entry)

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

func previewIcon(entry Entry) string {
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
