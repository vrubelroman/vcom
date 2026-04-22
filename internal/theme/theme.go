package theme

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Palette struct {
	Name          string
	Background    lipgloss.Color
	Panel         lipgloss.Color
	PanelInactive lipgloss.Color
	PanelElevated lipgloss.Color
	Border        lipgloss.Color
	BorderActive  lipgloss.Color
	Text          lipgloss.Color
	Muted         lipgloss.Color
	Accent        lipgloss.Color
	Selection     lipgloss.Color
	Warning       lipgloss.Color
	Danger        lipgloss.Color
	Folder        lipgloss.Color
	TextFile      lipgloss.Color
	ConfigFile    lipgloss.Color
	ExecFile      lipgloss.Color
	ImageFile     lipgloss.Color
	BinaryFile    lipgloss.Color
	FooterKey     lipgloss.Color
}

var builtInThemes = []string{
	"catppuccin-mocha",
	"tokyo-night",
	"gruvbox-dark",
	"nord-frost",
}

func Names() []string {
	values := make([]string, len(builtInThemes))
	copy(values, builtInThemes)
	return values
}

func Next(current string) string {
	values := Names()
	if len(values) == 0 {
		return current
	}
	current = strings.ToLower(strings.TrimSpace(current))
	for idx, value := range values {
		if value == current {
			return values[(idx+1)%len(values)]
		}
	}
	return values[0]
}

func Resolve(name string) (Palette, error) {
	switch strings.ToLower(name) {
	case "catppuccin-mocha":
		return Palette{
			Name:          "catppuccin-mocha",
			Background:    lipgloss.Color("#11111B"),
			Panel:         lipgloss.Color("#181825"),
			PanelInactive: lipgloss.Color("#1E1E2E"),
			PanelElevated: lipgloss.Color("#24273A"),
			Border:        lipgloss.Color("#45475A"),
			BorderActive:  lipgloss.Color("#89B4FA"),
			Text:          lipgloss.Color("#CDD6F4"),
			Muted:         lipgloss.Color("#A6ADC8"),
			Accent:        lipgloss.Color("#F5C2E7"),
			Selection:     lipgloss.Color("#313244"),
			Warning:       lipgloss.Color("#F9E2AF"),
			Danger:        lipgloss.Color("#F38BA8"),
			Folder:        lipgloss.Color("#89B4FA"),
			TextFile:      lipgloss.Color("#A6E3A1"),
			ConfigFile:    lipgloss.Color("#F9E2AF"),
			ExecFile:      lipgloss.Color("#FAB387"),
			ImageFile:     lipgloss.Color("#94E2D5"),
			BinaryFile:    lipgloss.Color("#CBA6F7"),
			FooterKey:     lipgloss.Color("#89DCEB"),
		}, nil
	case "tokyo-night":
		return Palette{
			Name:          "tokyo-night",
			Background:    lipgloss.Color("#16161E"),
			Panel:         lipgloss.Color("#1A1B26"),
			PanelInactive: lipgloss.Color("#24283B"),
			PanelElevated: lipgloss.Color("#2A2F44"),
			Border:        lipgloss.Color("#3B4261"),
			BorderActive:  lipgloss.Color("#7AA2F7"),
			Text:          lipgloss.Color("#C0CAF5"),
			Muted:         lipgloss.Color("#9AA5CE"),
			Accent:        lipgloss.Color("#BB9AF7"),
			Selection:     lipgloss.Color("#292E42"),
			Warning:       lipgloss.Color("#E0AF68"),
			Danger:        lipgloss.Color("#F7768E"),
			Folder:        lipgloss.Color("#7AA2F7"),
			TextFile:      lipgloss.Color("#9ECE6A"),
			ConfigFile:    lipgloss.Color("#E0AF68"),
			ExecFile:      lipgloss.Color("#FF9E64"),
			ImageFile:     lipgloss.Color("#73DACA"),
			BinaryFile:    lipgloss.Color("#BB9AF7"),
			FooterKey:     lipgloss.Color("#73DACA"),
		}, nil
	case "gruvbox-dark":
		return Palette{
			Name:          "gruvbox-dark",
			Background:    lipgloss.Color("#1D2021"),
			Panel:         lipgloss.Color("#282828"),
			PanelInactive: lipgloss.Color("#32302F"),
			PanelElevated: lipgloss.Color("#3C3836"),
			Border:        lipgloss.Color("#504945"),
			BorderActive:  lipgloss.Color("#FABD2F"),
			Text:          lipgloss.Color("#EBDBB2"),
			Muted:         lipgloss.Color("#BDAE93"),
			Accent:        lipgloss.Color("#83A598"),
			Selection:     lipgloss.Color("#3C3836"),
			Warning:       lipgloss.Color("#FE8019"),
			Danger:        lipgloss.Color("#FB4934"),
			Folder:        lipgloss.Color("#83A598"),
			TextFile:      lipgloss.Color("#B8BB26"),
			ConfigFile:    lipgloss.Color("#FABD2F"),
			ExecFile:      lipgloss.Color("#FE8019"),
			ImageFile:     lipgloss.Color("#8EC07C"),
			BinaryFile:    lipgloss.Color("#D3869B"),
			FooterKey:     lipgloss.Color("#8EC07C"),
		}, nil
	case "nord-frost":
		return Palette{
			Name:          "nord-frost",
			Background:    lipgloss.Color("#2E3440"),
			Panel:         lipgloss.Color("#3B4252"),
			PanelInactive: lipgloss.Color("#434C5E"),
			PanelElevated: lipgloss.Color("#4C566A"),
			Border:        lipgloss.Color("#4C566A"),
			BorderActive:  lipgloss.Color("#88C0D0"),
			Text:          lipgloss.Color("#ECEFF4"),
			Muted:         lipgloss.Color("#D8DEE9"),
			Accent:        lipgloss.Color("#81A1C1"),
			Selection:     lipgloss.Color("#434C5E"),
			Warning:       lipgloss.Color("#EBCB8B"),
			Danger:        lipgloss.Color("#BF616A"),
			Folder:        lipgloss.Color("#81A1C1"),
			TextFile:      lipgloss.Color("#A3BE8C"),
			ConfigFile:    lipgloss.Color("#EBCB8B"),
			ExecFile:      lipgloss.Color("#D08770"),
			ImageFile:     lipgloss.Color("#8FBCBB"),
			BinaryFile:    lipgloss.Color("#B48EAD"),
			FooterKey:     lipgloss.Color("#8FBCBB"),
		}, nil
	default:
		return Palette{}, fmt.Errorf("unknown theme %q", name)
	}
}
