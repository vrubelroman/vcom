package vfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
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
	PreviewKindPDF       PreviewKind = "pdf"
	PreviewKindAudio     PreviewKind = "audio"
	PreviewKindVideo     PreviewKind = "video"
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

	// Extended preview metadata
	Duration   string
	Bitrate    string
	AudioCodec string
	VideoCodec string
	SampleRate string
	Channels   string
	PageCount  string
	Dimensions string
}

type Preview struct {
	Kind      PreviewKind
	Title     string
	Body      string
	PlainBody string
	Metadata  Metadata
	Entries   []Entry
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
		preview.Body, preview.Entries = buildDirectoryPreview(entry.Path, options)
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
	if format, dimensions, ok := DetectImage(data); ok {
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

	// Extended preview for PDF, audio, video via external utilities
	if hasExt(pdfExtensions, entry.Extension) {
		return buildPDFPreview(entry, options, preview)
	}
	if hasExt(audioExtensions, entry.Extension) {
		return buildAudioPreview(entry, options, preview)
	}
	if hasExt(videoExtensions, entry.Extension) {
		return buildVideoPreview(entry, options, preview)
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

func buildDirectoryPreview(path string, options PreviewOptions) (string, []Entry) {
	entries, err := ListDir(path, ListOptions{
		ShowHidden:  options.ShowHidden,
		DirsFirst:   options.DirsFirst,
		SortBy:      options.SortBy,
		SortReverse: options.SortReverse,
	})
	if err != nil {
		return fmt.Sprintf("Could not list directory:\n\n%s", err), nil
	}
	if len(entries) == 0 {
		return "Directory is empty.", nil
	}

	// Return all entries as-is for column-based rendering.
	// The text body is still generated for terminals that don't support
	// the rich rendering, and as a fallback.
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

	return strings.Join(lines, "\n"), entries
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

func findTool(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func buildPDFPreview(entry Entry, options PreviewOptions, base Preview) Preview {
	pdftotext := findTool("pdftotext")
	if pdftotext == "" {
		base.Kind = PreviewKindBinary
		base.Body = "PDF file detected.\nInstall poppler-utils (pdftotext) for text preview."
		base.PlainBody = base.Body
		return base
	}

	cmd := exec.Command(pdftotext, "-layout", "-nopgbrk", entry.Path, "-")
	out, err := cmd.Output()
	if err != nil {
		base.Kind = PreviewKindError
		base.Body = fmt.Sprintf("pdftotext error:\n\n%s", err)
		base.PlainBody = base.Body
		return base
	}

	text := string(out)
	if int64(len(text)) > options.MaxPreviewBytes {
		text = text[:options.MaxPreviewBytes]
	}

	// Get page count via pdfinfo if available
	pdfinfo := findTool("pdfinfo")
	if pdfinfo != "" {
		infoCmd := exec.Command(pdfinfo, entry.Path)
		if infoOut, err := infoCmd.Output(); err == nil {
			for _, line := range strings.Split(string(infoOut), "\n") {
				if strings.HasPrefix(strings.ToLower(line), "pages:") {
					base.Metadata.PageCount = strings.TrimSpace(strings.TrimPrefix(line[5:], ":"))
					break
				}
			}
		}
	}

	base.Kind = PreviewKindPDF
	base.PlainBody = text
	base.Body = highlightText(entry.Path, text, options.ThemeName)
	return base
}

func buildAudioPreview(entry Entry, options PreviewOptions, base Preview) Preview {
	ffprobe := findTool("ffprobe")
	if ffprobe == "" {
		base.Kind = PreviewKindBinary
		base.Body = "Audio file detected.\nInstall ffmpeg (ffprobe) for metadata preview."
		base.PlainBody = base.Body
		return base
	}

	cmd := exec.Command(ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		entry.Path,
	)
	out, err := cmd.Output()
	if err != nil {
		base.Kind = PreviewKindError
		base.Body = fmt.Sprintf("ffprobe error:\n\n%s", err)
		base.PlainBody = base.Body
		return base
	}

	var info struct {
		Format struct {
			Duration string `json:"duration"`
			Bitrate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		base.Kind = PreviewKindError
		base.Body = fmt.Sprintf("Could not parse ffprobe output:\n\n%s", err)
		base.PlainBody = base.Body
		return base
	}

	// Format duration
	if info.Format.Duration != "" {
		if secs, err := strconv.ParseFloat(info.Format.Duration, 64); err == nil {
			mins := int(secs) / 60
			secsRem := int(secs) % 60
			base.Metadata.Duration = fmt.Sprintf("%d:%02d", mins, secsRem)
		}
	}

	if info.Format.Bitrate != "" {
		if bps, err := strconv.ParseInt(info.Format.Bitrate, 10, 64); err == nil {
			base.Metadata.Bitrate = fmt.Sprintf("%d kbps", bps/1000)
		}
	}

	for _, stream := range info.Streams {
		if stream.CodecType == "audio" {
			base.Metadata.AudioCodec = stream.CodecName
			if stream.SampleRate != "" {
				base.Metadata.SampleRate = stream.SampleRate + " Hz"
			}
			switch stream.Channels {
			case 1:
				base.Metadata.Channels = "mono"
			case 2:
				base.Metadata.Channels = "stereo"
			case 6:
				base.Metadata.Channels = "5.1"
			case 8:
				base.Metadata.Channels = "7.1"
			default:
				base.Metadata.Channels = fmt.Sprintf("%d ch", stream.Channels)
			}
			break
		}
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("  Duration: %s", base.Metadata.Duration))
	if base.Metadata.Bitrate != "" {
		lines = append(lines, fmt.Sprintf("  Bitrate:  %s", base.Metadata.Bitrate))
	}
	if base.Metadata.AudioCodec != "" {
		lines = append(lines, fmt.Sprintf("  Codec:    %s", base.Metadata.AudioCodec))
	}
	if base.Metadata.SampleRate != "" {
		lines = append(lines, fmt.Sprintf("  Rate:     %s", base.Metadata.SampleRate))
	}
	if base.Metadata.Channels != "" {
		lines = append(lines, fmt.Sprintf("  Channels: %s", base.Metadata.Channels))
	}

	base.Kind = PreviewKindAudio
	base.Body = fmt.Sprintf("🎵 Audio File\n\n%s", strings.Join(lines, "\n"))
	base.PlainBody = fmt.Sprintf("Audio File\n\nDuration: %s\nBitrate: %s\nCodec: %s\nRate: %s\nChannels: %s",
		base.Metadata.Duration, base.Metadata.Bitrate, base.Metadata.AudioCodec,
		base.Metadata.SampleRate, base.Metadata.Channels)
	return base
}

func buildVideoPreview(entry Entry, options PreviewOptions, base Preview) Preview {
	ffprobe := findTool("ffprobe")
	if ffprobe == "" {
		base.Kind = PreviewKindBinary
		base.Body = "Video file detected.\nInstall ffmpeg (ffprobe) for metadata preview."
		base.PlainBody = base.Body
		return base
	}

	cmd := exec.Command(ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		entry.Path,
	)
	out, err := cmd.Output()
	if err != nil {
		base.Kind = PreviewKindError
		base.Body = fmt.Sprintf("ffprobe error:\n\n%s", err)
		base.PlainBody = base.Body
		return base
	}

	var info struct {
		Format struct {
			Duration string `json:"duration"`
			Bitrate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		base.Kind = PreviewKindError
		base.Body = fmt.Sprintf("Could not parse ffprobe output:\n\n%s", err)
		base.PlainBody = base.Body
		return base
	}

	// Format duration
	if info.Format.Duration != "" {
		if secs, err := strconv.ParseFloat(info.Format.Duration, 64); err == nil {
			hrs := int(secs) / 3600
			mins := (int(secs) % 3600) / 60
			secsRem := int(secs) % 60
			if hrs > 0 {
				base.Metadata.Duration = fmt.Sprintf("%d:%02d:%02d", hrs, mins, secsRem)
			} else {
				base.Metadata.Duration = fmt.Sprintf("%d:%02d", mins, secsRem)
			}
		}
	}

	if info.Format.Bitrate != "" {
		if bps, err := strconv.ParseInt(info.Format.Bitrate, 10, 64); err == nil {
			base.Metadata.Bitrate = fmt.Sprintf("%d kbps", bps/1000)
		}
	}

	for _, stream := range info.Streams {
		switch stream.CodecType {
		case "video":
			base.Metadata.VideoCodec = stream.CodecName
			if stream.Width > 0 && stream.Height > 0 {
				base.Metadata.Dimensions = fmt.Sprintf("%dx%d", stream.Width, stream.Height)
			}
		case "audio":
			if base.Metadata.AudioCodec == "" {
				base.Metadata.AudioCodec = stream.CodecName
			}
		}
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("  Duration:   %s", base.Metadata.Duration))
	if base.Metadata.Bitrate != "" {
		lines = append(lines, fmt.Sprintf("  Bitrate:    %s", base.Metadata.Bitrate))
	}
	if base.Metadata.VideoCodec != "" {
		lines = append(lines, fmt.Sprintf("  Video:      %s", base.Metadata.VideoCodec))
	}
	if base.Metadata.Dimensions != "" {
		lines = append(lines, fmt.Sprintf("  Resolution: %s", base.Metadata.Dimensions))
	}
	if base.Metadata.AudioCodec != "" {
		lines = append(lines, fmt.Sprintf("  Audio:      %s", base.Metadata.AudioCodec))
	}

	base.Kind = PreviewKindVideo
	base.Body = fmt.Sprintf("🎬 Video File\n\n%s", strings.Join(lines, "\n"))
	base.PlainBody = fmt.Sprintf("Video File\n\nDuration: %s\nBitrate: %s\nVideo: %s\nResolution: %s\nAudio: %s",
		base.Metadata.Duration, base.Metadata.Bitrate, base.Metadata.VideoCodec,
		base.Metadata.Dimensions, base.Metadata.AudioCodec)
	return base
}

func DetectImage(data []byte) (string, string, bool) {
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
