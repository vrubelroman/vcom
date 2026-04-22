package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	Startup  StartupConfig  `toml:"startup"`
	UI       UIConfig       `toml:"ui"`
	Browser  BrowserConfig  `toml:"browser"`
	Preview  PreviewConfig  `toml:"preview"`
	Behavior BehaviorConfig `toml:"behavior"`
}

type StartupConfig struct {
	LeftPath  string `toml:"left_path"`
	RightPath string `toml:"right_path"`
}

type UIConfig struct {
	AppTitle           string `toml:"app_title"`
	Theme              string `toml:"theme"`
	ShowTitleBar       bool   `toml:"show_title_bar"`
	ShowFooter         bool   `toml:"show_footer"`
	Border             string `toml:"border"`
	PathDisplay        string `toml:"path_display"`
	PaneGap            int    `toml:"pane_gap"`
	CenterWidthPercent int    `toml:"center_width_percent"`
}

type BrowserConfig struct {
	ShowHidden        bool                 `toml:"show_hidden"`
	DirsFirst         bool                 `toml:"dirs_first"`
	HumanReadableSize bool                 `toml:"human_readable_size"`
	Sort              SortConfig           `toml:"sort"`
	Columns           BrowserColumnsConfig `toml:"columns"`
}

type SortConfig struct {
	By      string `toml:"by"`
	Reverse bool   `toml:"reverse"`
}

type BrowserColumnsConfig struct {
	Name        bool `toml:"name"`
	Size        bool `toml:"size"`
	Modified    bool `toml:"modified"`
	Created     bool `toml:"created"`
	Permissions bool `toml:"permissions"`
	Extension   bool `toml:"extension"`
}

type PreviewConfig struct {
	ShowMetadata          bool  `toml:"show_metadata"`
	WrapText              bool  `toml:"wrap_text"`
	MaxPreviewBytes       int64 `toml:"max_preview_bytes"`
	DirectoryPreviewLimit int   `toml:"directory_preview_limit"`
}

type BehaviorConfig struct {
	ConfirmDelete           bool `toml:"confirm_delete"`
	ConfirmOverwrite        bool `toml:"confirm_overwrite"`
	CalculateDirSizeOnSpace bool `toml:"calculate_dir_size_on_space"`
	FollowSymlinks          bool `toml:"follow_symlinks"`
}

func Default() Config {
	return Config{
		Startup: StartupConfig{},
		UI: UIConfig{
			AppTitle:           "vcom",
			Theme:              "catppuccin-mocha",
			ShowTitleBar:       true,
			ShowFooter:         true,
			Border:             "rounded",
			PathDisplay:        "short",
			PaneGap:            1,
			CenterWidthPercent: 30,
		},
		Browser: BrowserConfig{
			ShowHidden:        true,
			DirsFirst:         true,
			HumanReadableSize: true,
			Sort: SortConfig{
				By:      "name",
				Reverse: false,
			},
			Columns: BrowserColumnsConfig{
				Name:     true,
				Size:     true,
				Modified: true,
			},
		},
		Preview: PreviewConfig{
			ShowMetadata:          true,
			WrapText:              false,
			MaxPreviewBytes:       64 * 1024,
			DirectoryPreviewLimit: 80,
		},
		Behavior: BehaviorConfig{
			ConfirmDelete:           true,
			ConfirmOverwrite:        true,
			CalculateDirSizeOnSpace: true,
			FollowSymlinks:          false,
		},
	}
}

func Load(explicitPath string) (Config, string, error) {
	cfg := Default()

	path, found, err := resolvePath(explicitPath)
	if err != nil {
		return Config{}, "", err
	}
	if !found {
		return cfg, "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, "", fmt.Errorf("parse %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, "", fmt.Errorf("validate %s: %w", path, err)
	}

	return cfg, path, nil
}

func (c *Config) Validate() error {
	if c.UI.Theme == "" {
		return errors.New("ui.theme must not be empty")
	}
	if strings.TrimSpace(c.UI.AppTitle) == "" {
		c.UI.AppTitle = "vcom"
	}
	if c.Preview.MaxPreviewBytes <= 0 {
		return errors.New("preview.max_preview_bytes must be > 0")
	}
	if c.Preview.DirectoryPreviewLimit <= 0 {
		return errors.New("preview.directory_preview_limit must be > 0")
	}
	if !c.Browser.Columns.Name {
		return errors.New("browser.columns.name must stay enabled")
	}
	if strings.TrimSpace(c.UI.PathDisplay) == "" {
		c.UI.PathDisplay = "short"
	}
	if c.UI.PaneGap < 0 || c.UI.PaneGap > 4 {
		return errors.New("ui.pane_gap must be between 0 and 4")
	}
	if c.UI.CenterWidthPercent < 20 || c.UI.CenterWidthPercent > 60 {
		return errors.New("ui.center_width_percent must be between 20 and 60")
	}
	switch strings.ToLower(strings.TrimSpace(c.Browser.Sort.By)) {
	case "", "name":
		c.Browser.Sort.By = "name"
	case "modified", "size", "created", "extension":
	default:
		return fmt.Errorf("browser.sort.by must be one of: name, modified, size, created, extension")
	}
	return nil
}

func resolvePath(explicitPath string) (string, bool, error) {
	var candidates []string

	if explicitPath != "" {
		candidates = append(candidates, explicitPath)
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", false, fmt.Errorf("resolve home dir: %w", err)
		}
		xdgDir := os.Getenv("XDG_CONFIG_HOME")
		if xdgDir == "" {
			xdgDir = filepath.Join(homeDir, ".config")
		}

		candidates = append(candidates,
			"vcom.toml",
			filepath.Join("config", "vcom.toml"),
			filepath.Join(xdgDir, "vcom", "vcom.toml"),
			filepath.Join(homeDir, ".config", "vcom", "vcom.toml"),
		)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absPath, err := filepath.Abs(candidate)
		if err != nil {
			return "", false, fmt.Errorf("resolve %s: %w", candidate, err)
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, fmt.Errorf("stat %s: %w", absPath, err)
		}
	}

	return "", false, nil
}
