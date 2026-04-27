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
	StatusBar     lipgloss.Color
	Footer        lipgloss.Color
	Border        lipgloss.Color
	BorderActive  lipgloss.Color
	Text          lipgloss.Color
	Muted         lipgloss.Color
	Accent        lipgloss.Color
	Info          lipgloss.Color
	Success       lipgloss.Color
	Selection     lipgloss.Color
	Hover         lipgloss.Color
	Marked        lipgloss.Color
	Warning       lipgloss.Color
	Danger        lipgloss.Color
	ActivePath    lipgloss.Color
	ConfirmButton lipgloss.Color
	CancelButton  lipgloss.Color
	ProgressFill  lipgloss.Color
	ProgressEmpty lipgloss.Color
	HelpNav       lipgloss.Color
	HelpPanels    lipgloss.Color
	HelpDialogs   lipgloss.Color
	HelpMouse     lipgloss.Color
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
	"catppuccin-lavender",
	"tokyo-night",
	"gruvbox-dark",
	"gruvbox",
	"nord-frost",
	"nord",
	"ayu-dark",
	"breeze",
	"cyberpunk",
	"dracula",
	"eldritch",
	"kanagawa",
	"kanagawa-paper",
	"rose-pine",
	"solarized-dark",
	"vesper",
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
			StatusBar:     lipgloss.Color("#1E1E2E"),
			Footer:        lipgloss.Color("#11111B"),
			Border:        lipgloss.Color("#45475A"),
			BorderActive:  lipgloss.Color("#89B4FA"),
			Text:          lipgloss.Color("#CDD6F4"),
			Muted:         lipgloss.Color("#A6ADC8"),
			Accent:        lipgloss.Color("#F5C2E7"),
			Info:          lipgloss.Color("#89DCEB"),
			Success:       lipgloss.Color("#A6E3A1"),
			Selection:     lipgloss.Color("#313244"),
			Hover:         lipgloss.Color("#2A2B3C"),
			Marked:        lipgloss.Color("#F38BA8"),
			Warning:       lipgloss.Color("#F9E2AF"),
			Danger:        lipgloss.Color("#F38BA8"),
			ActivePath:    lipgloss.Color("#89DCEB"),
			ConfirmButton: lipgloss.Color("#A6E3A1"),
			CancelButton:  lipgloss.Color("#F38BA8"),
			ProgressFill:  lipgloss.Color("#89B4FA"),
			ProgressEmpty: lipgloss.Color("#45475A"),
			HelpNav:       lipgloss.Color("#89B4FA"),
			HelpPanels:    lipgloss.Color("#F9E2AF"),
			HelpDialogs:   lipgloss.Color("#CBA6F7"),
			HelpMouse:     lipgloss.Color("#F38BA8"),
			Folder:        lipgloss.Color("#89B4FA"),
			TextFile:      lipgloss.Color("#A6E3A1"),
			ConfigFile:    lipgloss.Color("#F9E2AF"),
			ExecFile:      lipgloss.Color("#FAB387"),
			ImageFile:     lipgloss.Color("#94E2D5"),
			BinaryFile:    lipgloss.Color("#CBA6F7"),
			FooterKey:     lipgloss.Color("#89DCEB"),
		}, nil

	case "catppuccin-lavender":
		return Palette{
			Name:          "catppuccin-lavender",
			Background:    lipgloss.Color("#11111B"),
			Panel:         lipgloss.Color("#181825"),
			PanelInactive: lipgloss.Color("#1E1E2E"),
			PanelElevated: lipgloss.Color("#24273A"),
			StatusBar:     lipgloss.Color("#1E1E2E"),
			Footer:        lipgloss.Color("#11111B"),
			Border:        lipgloss.Color("#45475A"),
			BorderActive:  lipgloss.Color("#B4BEFE"),
			Text:          lipgloss.Color("#CDD6F4"),
			Muted:         lipgloss.Color("#A6ADC8"),
			Accent:        lipgloss.Color("#B4BEFE"),
			Info:          lipgloss.Color("#89DCEB"),
			Success:       lipgloss.Color("#A6E3A1"),
			Selection:     lipgloss.Color("#313244"),
			Hover:         lipgloss.Color("#2A2B3C"),
			Marked:        lipgloss.Color("#F38BA8"),
			Warning:       lipgloss.Color("#F9E2AF"),
			Danger:        lipgloss.Color("#F38BA8"),
			ActivePath:    lipgloss.Color("#B4BEFE"),
			ConfirmButton: lipgloss.Color("#A6E3A1"),
			CancelButton:  lipgloss.Color("#F38BA8"),
			ProgressFill:  lipgloss.Color("#B4BEFE"),
			ProgressEmpty: lipgloss.Color("#45475A"),
			HelpNav:       lipgloss.Color("#B4BEFE"),
			HelpPanels:    lipgloss.Color("#F9E2AF"),
			HelpDialogs:   lipgloss.Color("#CBA6F7"),
			HelpMouse:     lipgloss.Color("#F38BA8"),
			Folder:        lipgloss.Color("#B4BEFE"),
			TextFile:      lipgloss.Color("#A6E3A1"),
			ConfigFile:    lipgloss.Color("#F9E2AF"),
			ExecFile:      lipgloss.Color("#FAB387"),
			ImageFile:     lipgloss.Color("#89DCEB"),
			BinaryFile:    lipgloss.Color("#CBA6F7"),
			FooterKey:     lipgloss.Color("#B4BEFE"),
		}, nil

	case "tokyo-night":
		return Palette{
			Name:          "tokyo-night",
			Background:    lipgloss.Color("#16161E"),
			Panel:         lipgloss.Color("#1A1B26"),
			PanelInactive: lipgloss.Color("#24283B"),
			PanelElevated: lipgloss.Color("#2A2F44"),
			StatusBar:     lipgloss.Color("#24283B"),
			Footer:        lipgloss.Color("#16161E"),
			Border:        lipgloss.Color("#3B4261"),
			BorderActive:  lipgloss.Color("#7AA2F7"),
			Text:          lipgloss.Color("#C0CAF5"),
			Muted:         lipgloss.Color("#9AA5CE"),
			Accent:        lipgloss.Color("#BB9AF7"),
			Info:          lipgloss.Color("#73DACA"),
			Success:       lipgloss.Color("#9ECE6A"),
			Selection:     lipgloss.Color("#292E42"),
			Hover:         lipgloss.Color("#252A3D"),
			Marked:        lipgloss.Color("#F7768E"),
			Warning:       lipgloss.Color("#E0AF68"),
			Danger:        lipgloss.Color("#F7768E"),
			ActivePath:    lipgloss.Color("#73DACA"),
			ConfirmButton: lipgloss.Color("#9ECE6A"),
			CancelButton:  lipgloss.Color("#F7768E"),
			ProgressFill:  lipgloss.Color("#7AA2F7"),
			ProgressEmpty: lipgloss.Color("#3B4261"),
			HelpNav:       lipgloss.Color("#7AA2F7"),
			HelpPanels:    lipgloss.Color("#E0AF68"),
			HelpDialogs:   lipgloss.Color("#BB9AF7"),
			HelpMouse:     lipgloss.Color("#F7768E"),
			Folder:        lipgloss.Color("#7AA2F7"),
			TextFile:      lipgloss.Color("#9ECE6A"),
			ConfigFile:    lipgloss.Color("#E0AF68"),
			ExecFile:      lipgloss.Color("#FF9E64"),
			ImageFile:     lipgloss.Color("#73DACA"),
			BinaryFile:    lipgloss.Color("#BB9AF7"),
			FooterKey:     lipgloss.Color("#73DACA"),
		}, nil

	case "gruvbox-dark", "gruvbox":
		return Palette{
			Name:          name,
			Background:    lipgloss.Color("#1D2021"),
			Panel:         lipgloss.Color("#282828"),
			PanelInactive: lipgloss.Color("#32302F"),
			PanelElevated: lipgloss.Color("#3C3836"),
			StatusBar:     lipgloss.Color("#32302F"),
			Footer:        lipgloss.Color("#1D2021"),
			Border:        lipgloss.Color("#504945"),
			BorderActive:  lipgloss.Color("#FABD2F"),
			Text:          lipgloss.Color("#EBDBB2"),
			Muted:         lipgloss.Color("#BDAE93"),
			Accent:        lipgloss.Color("#83A598"),
			Info:          lipgloss.Color("#8EC07C"),
			Success:       lipgloss.Color("#B8BB26"),
			Selection:     lipgloss.Color("#3C3836"),
			Hover:         lipgloss.Color("#45403D"),
			Marked:        lipgloss.Color("#FB4934"),
			Warning:       lipgloss.Color("#FE8019"),
			Danger:        lipgloss.Color("#FB4934"),
			ActivePath:    lipgloss.Color("#8EC07C"),
			ConfirmButton: lipgloss.Color("#B8BB26"),
			CancelButton:  lipgloss.Color("#FB4934"),
			ProgressFill:  lipgloss.Color("#FABD2F"),
			ProgressEmpty: lipgloss.Color("#504945"),
			HelpNav:       lipgloss.Color("#83A598"),
			HelpPanels:    lipgloss.Color("#FABD2F"),
			HelpDialogs:   lipgloss.Color("#D3869B"),
			HelpMouse:     lipgloss.Color("#FB4934"),
			Folder:        lipgloss.Color("#83A598"),
			TextFile:      lipgloss.Color("#B8BB26"),
			ConfigFile:    lipgloss.Color("#FABD2F"),
			ExecFile:      lipgloss.Color("#FE8019"),
			ImageFile:     lipgloss.Color("#8EC07C"),
			BinaryFile:    lipgloss.Color("#D3869B"),
			FooterKey:     lipgloss.Color("#8EC07C"),
		}, nil

	case "nord-frost", "nord":
		return Palette{
			Name:          name,
			Background:    lipgloss.Color("#2E3440"),
			Panel:         lipgloss.Color("#3B4252"),
			PanelInactive: lipgloss.Color("#434C5E"),
			PanelElevated: lipgloss.Color("#4C566A"),
			StatusBar:     lipgloss.Color("#434C5E"),
			Footer:        lipgloss.Color("#2E3440"),
			Border:        lipgloss.Color("#4C566A"),
			BorderActive:  lipgloss.Color("#88C0D0"),
			Text:          lipgloss.Color("#ECEFF4"),
			Muted:         lipgloss.Color("#D8DEE9"),
			Accent:        lipgloss.Color("#81A1C1"),
			Info:          lipgloss.Color("#8FBCBB"),
			Success:       lipgloss.Color("#A3BE8C"),
			Selection:     lipgloss.Color("#434C5E"),
			Hover:         lipgloss.Color("#505A70"),
			Marked:        lipgloss.Color("#BF616A"),
			Warning:       lipgloss.Color("#EBCB8B"),
			Danger:        lipgloss.Color("#BF616A"),
			ActivePath:    lipgloss.Color("#8FBCBB"),
			ConfirmButton: lipgloss.Color("#A3BE8C"),
			CancelButton:  lipgloss.Color("#BF616A"),
			ProgressFill:  lipgloss.Color("#88C0D0"),
			ProgressEmpty: lipgloss.Color("#4C566A"),
			HelpNav:       lipgloss.Color("#81A1C1"),
			HelpPanels:    lipgloss.Color("#EBCB8B"),
			HelpDialogs:   lipgloss.Color("#B48EAD"),
			HelpMouse:     lipgloss.Color("#BF616A"),
			Folder:        lipgloss.Color("#81A1C1"),
			TextFile:      lipgloss.Color("#A3BE8C"),
			ConfigFile:    lipgloss.Color("#EBCB8B"),
			ExecFile:      lipgloss.Color("#D08770"),
			ImageFile:     lipgloss.Color("#8FBCBB"),
			BinaryFile:    lipgloss.Color("#B48EAD"),
			FooterKey:     lipgloss.Color("#8FBCBB"),
		}, nil

	case "ayu-dark":
		return Palette{
			Name:          "ayu-dark",
			Background:    lipgloss.Color("#0A0E14"),
			Panel:         lipgloss.Color("#0D1017"),
			PanelInactive: lipgloss.Color("#11151D"),
			PanelElevated: lipgloss.Color("#151A23"),
			StatusBar:     lipgloss.Color("#11151D"),
			Footer:        lipgloss.Color("#0A0E14"),
			Border:        lipgloss.Color("#1F2430"),
			BorderActive:  lipgloss.Color("#FFCC66"),
			Text:          lipgloss.Color("#B3B1AD"),
			Muted:         lipgloss.Color("#565B66"),
			Accent:        lipgloss.Color("#FF8F40"),
			Info:          lipgloss.Color("#95E6CB"),
			Success:       lipgloss.Color("#7FD962"),
			Selection:     lipgloss.Color("#1F2430"),
			Hover:         lipgloss.Color("#191E27"),
			Marked:        lipgloss.Color("#F26D78"),
			Warning:       lipgloss.Color("#FFCC66"),
			Danger:        lipgloss.Color("#F26D78"),
			ActivePath:    lipgloss.Color("#95E6CB"),
			ConfirmButton: lipgloss.Color("#7FD962"),
			CancelButton:  lipgloss.Color("#F26D78"),
			ProgressFill:  lipgloss.Color("#FFCC66"),
			ProgressEmpty: lipgloss.Color("#1F2430"),
			HelpNav:       lipgloss.Color("#FF8F40"),
			HelpPanels:    lipgloss.Color("#FFCC66"),
			HelpDialogs:   lipgloss.Color("#D4A0FF"),
			HelpMouse:     lipgloss.Color("#F26D78"),
			Folder:        lipgloss.Color("#FF8F40"),
			TextFile:      lipgloss.Color("#B3B1AD"),
			ConfigFile:    lipgloss.Color("#FFCC66"),
			ExecFile:      lipgloss.Color("#F29668"),
			ImageFile:     lipgloss.Color("#95E6CB"),
			BinaryFile:    lipgloss.Color("#D4A0FF"),
			FooterKey:     lipgloss.Color("#95E6CB"),
		}, nil

	case "breeze":
		return Palette{
			Name:          "breeze",
			Background:    lipgloss.Color("#232629"),
			Panel:         lipgloss.Color("#2A2D30"),
			PanelInactive: lipgloss.Color("#313437"),
			PanelElevated: lipgloss.Color("#383B3E"),
			StatusBar:     lipgloss.Color("#313437"),
			Footer:        lipgloss.Color("#232629"),
			Border:        lipgloss.Color("#494D51"),
			BorderActive:  lipgloss.Color("#3DAEE9"),
			Text:          lipgloss.Color("#EFF0F1"),
			Muted:         lipgloss.Color("#B0B5BA"),
			Accent:        lipgloss.Color("#3DAEE9"),
			Info:          lipgloss.Color("#27E6A6"),
			Success:       lipgloss.Color("#27AE60"),
			Selection:     lipgloss.Color("#313437"),
			Hover:         lipgloss.Color("#35383B"),
			Marked:        lipgloss.Color("#ED1515"),
			Warning:       lipgloss.Color("#F67400"),
			Danger:        lipgloss.Color("#ED1515"),
			ActivePath:    lipgloss.Color("#27E6A6"),
			ConfirmButton: lipgloss.Color("#27AE60"),
			CancelButton:  lipgloss.Color("#ED1515"),
			ProgressFill:  lipgloss.Color("#3DAEE9"),
			ProgressEmpty: lipgloss.Color("#494D51"),
			HelpNav:       lipgloss.Color("#3DAEE9"),
			HelpPanels:    lipgloss.Color("#F67400"),
			HelpDialogs:   lipgloss.Color("#9B59B6"),
			HelpMouse:     lipgloss.Color("#ED1515"),
			Folder:        lipgloss.Color("#3DAEE9"),
			TextFile:      lipgloss.Color("#27AE60"),
			ConfigFile:    lipgloss.Color("#F67400"),
			ExecFile:      lipgloss.Color("#E67E22"),
			ImageFile:     lipgloss.Color("#27E6A6"),
			BinaryFile:    lipgloss.Color("#9B59B6"),
			FooterKey:     lipgloss.Color("#27E6A6"),
		}, nil

	case "cyberpunk":
		return Palette{
			Name:          "cyberpunk",
			Background:    lipgloss.Color("#000B1A"),
			Panel:         lipgloss.Color("#0A1628"),
			PanelInactive: lipgloss.Color("#0F1D30"),
			PanelElevated: lipgloss.Color("#142338"),
			StatusBar:     lipgloss.Color("#0F1D30"),
			Footer:        lipgloss.Color("#000B1A"),
			Border:        lipgloss.Color("#1E3A5F"),
			BorderActive:  lipgloss.Color("#00FFF0"),
			Text:          lipgloss.Color("#E0E0E0"),
			Muted:         lipgloss.Color("#808080"),
			Accent:        lipgloss.Color("#FF00FF"),
			Info:          lipgloss.Color("#00FFF0"),
			Success:       lipgloss.Color("#00FF41"),
			Selection:     lipgloss.Color("#142338"),
			Hover:         lipgloss.Color("#192C42"),
			Marked:        lipgloss.Color("#FF0055"),
			Warning:       lipgloss.Color("#FFB000"),
			Danger:        lipgloss.Color("#FF0055"),
			ActivePath:    lipgloss.Color("#00FFF0"),
			ConfirmButton: lipgloss.Color("#00FF41"),
			CancelButton:  lipgloss.Color("#FF0055"),
			ProgressFill:  lipgloss.Color("#FF00FF"),
			ProgressEmpty: lipgloss.Color("#1E3A5F"),
			HelpNav:       lipgloss.Color("#FF00FF"),
			HelpPanels:    lipgloss.Color("#FFB000"),
			HelpDialogs:   lipgloss.Color("#FF00FF"),
			HelpMouse:     lipgloss.Color("#FF0055"),
			Folder:        lipgloss.Color("#00FFF0"),
			TextFile:      lipgloss.Color("#00FF41"),
			ConfigFile:    lipgloss.Color("#FFB000"),
			ExecFile:      lipgloss.Color("#FF6600"),
			ImageFile:     lipgloss.Color("#00FFF0"),
			BinaryFile:    lipgloss.Color("#FF00FF"),
			FooterKey:     lipgloss.Color("#00FFF0"),
		}, nil

	case "dracula":
		return Palette{
			Name:          "dracula",
			Background:    lipgloss.Color("#21222C"),
			Panel:         lipgloss.Color("#282A36"),
			PanelInactive: lipgloss.Color("#2F3242"),
			PanelElevated: lipgloss.Color("#363850"),
			StatusBar:     lipgloss.Color("#2F3242"),
			Footer:        lipgloss.Color("#21222C"),
			Border:        lipgloss.Color("#44475A"),
			BorderActive:  lipgloss.Color("#BD93F9"),
			Text:          lipgloss.Color("#F8F8F2"),
			Muted:         lipgloss.Color("#6272A4"),
			Accent:        lipgloss.Color("#FF79C6"),
			Info:          lipgloss.Color("#8BE9FD"),
			Success:       lipgloss.Color("#50FA7B"),
			Selection:     lipgloss.Color("#44475A"),
			Hover:         lipgloss.Color("#3A3D52"),
			Marked:        lipgloss.Color("#FF5555"),
			Warning:       lipgloss.Color("#F1FA8C"),
			Danger:        lipgloss.Color("#FF5555"),
			ActivePath:    lipgloss.Color("#8BE9FD"),
			ConfirmButton: lipgloss.Color("#50FA7B"),
			CancelButton:  lipgloss.Color("#FF5555"),
			ProgressFill:  lipgloss.Color("#FF79C6"),
			ProgressEmpty: lipgloss.Color("#44475A"),
			HelpNav:       lipgloss.Color("#BD93F9"),
			HelpPanels:    lipgloss.Color("#F1FA8C"),
			HelpDialogs:   lipgloss.Color("#FF79C6"),
			HelpMouse:     lipgloss.Color("#FF5555"),
			Folder:        lipgloss.Color("#BD93F9"),
			TextFile:      lipgloss.Color("#50FA7B"),
			ConfigFile:    lipgloss.Color("#F1FA8C"),
			ExecFile:      lipgloss.Color("#FFB86C"),
			ImageFile:     lipgloss.Color("#8BE9FD"),
			BinaryFile:    lipgloss.Color("#FF79C6"),
			FooterKey:     lipgloss.Color("#8BE9FD"),
		}, nil

	case "eldritch":
		return Palette{
			Name:          "eldritch",
			Background:    lipgloss.Color("#0B0D15"),
			Panel:         lipgloss.Color("#10121A"),
			PanelInactive: lipgloss.Color("#161822"),
			PanelElevated: lipgloss.Color("#1C1F2B"),
			StatusBar:     lipgloss.Color("#161822"),
			Footer:        lipgloss.Color("#0B0D15"),
			Border:        lipgloss.Color("#262A3B"),
			BorderActive:  lipgloss.Color("#67B0E8"),
			Text:          lipgloss.Color("#D3D7E0"),
			Muted:         lipgloss.Color("#8B8FA6"),
			Accent:        lipgloss.Color("#C278E8"),
			Info:          lipgloss.Color("#67B0E8"),
			Success:       lipgloss.Color("#74C287"),
			Selection:     lipgloss.Color("#1C1F2B"),
			Hover:         lipgloss.Color("#222638"),
			Marked:        lipgloss.Color("#E06868"),
			Warning:       lipgloss.Color("#E0A868"),
			Danger:        lipgloss.Color("#E06868"),
			ActivePath:    lipgloss.Color("#67B0E8"),
			ConfirmButton: lipgloss.Color("#74C287"),
			CancelButton:  lipgloss.Color("#E06868"),
			ProgressFill:  lipgloss.Color("#C278E8"),
			ProgressEmpty: lipgloss.Color("#262A3B"),
			HelpNav:       lipgloss.Color("#C278E8"),
			HelpPanels:    lipgloss.Color("#E0A868"),
			HelpDialogs:   lipgloss.Color("#C278E8"),
			HelpMouse:     lipgloss.Color("#E06868"),
			Folder:        lipgloss.Color("#67B0E8"),
			TextFile:      lipgloss.Color("#74C287"),
			ConfigFile:    lipgloss.Color("#E0A868"),
			ExecFile:      lipgloss.Color("#E08868"),
			ImageFile:     lipgloss.Color("#67B0E8"),
			BinaryFile:    lipgloss.Color("#C278E8"),
			FooterKey:     lipgloss.Color("#67B0E8"),
		}, nil

	case "kanagawa":
		return Palette{
			Name:          "kanagawa",
			Background:    lipgloss.Color("#1F1F28"),
			Panel:         lipgloss.Color("#252535"),
			PanelInactive: lipgloss.Color("#2A2A3C"),
			PanelElevated: lipgloss.Color("#363646"),
			StatusBar:     lipgloss.Color("#2A2A3C"),
			Footer:        lipgloss.Color("#1F1F28"),
			Border:        lipgloss.Color("#54546D"),
			BorderActive:  lipgloss.Color("#7FB4CA"),
			Text:          lipgloss.Color("#DCD7BA"),
			Muted:         lipgloss.Color("#938AA9"),
			Accent:        lipgloss.Color("#DCA561"),
			Info:          lipgloss.Color("#7FB4CA"),
			Success:       lipgloss.Color("#76946A"),
			Selection:     lipgloss.Color("#363646"),
			Hover:         lipgloss.Color("#30304A"),
			Marked:        lipgloss.Color("#C34043"),
			Warning:       lipgloss.Color("#DCA561"),
			Danger:        lipgloss.Color("#C34043"),
			ActivePath:    lipgloss.Color("#7FB4CA"),
			ConfirmButton: lipgloss.Color("#76946A"),
			CancelButton:  lipgloss.Color("#C34043"),
			ProgressFill:  lipgloss.Color("#DCA561"),
			ProgressEmpty: lipgloss.Color("#54546D"),
			HelpNav:       lipgloss.Color("#DCA561"),
			HelpPanels:    lipgloss.Color("#DCA561"),
			HelpDialogs:   lipgloss.Color("#957FB8"),
			HelpMouse:     lipgloss.Color("#C34043"),
			Folder:        lipgloss.Color("#7FB4CA"),
			TextFile:      lipgloss.Color("#76946A"),
			ConfigFile:    lipgloss.Color("#DCA561"),
			ExecFile:      lipgloss.Color("#E6C384"),
			ImageFile:     lipgloss.Color("#7FB4CA"),
			BinaryFile:    lipgloss.Color("#957FB8"),
			FooterKey:     lipgloss.Color("#7FB4CA"),
		}, nil

	case "kanagawa-paper":
		return Palette{
			Name:          "kanagawa-paper",
			Background:    lipgloss.Color("#1A1A22"),
			Panel:         lipgloss.Color("#222233"),
			PanelInactive: lipgloss.Color("#2A2A3E"),
			PanelElevated: lipgloss.Color("#323248"),
			StatusBar:     lipgloss.Color("#2A2A3E"),
			Footer:        lipgloss.Color("#1A1A22"),
			Border:        lipgloss.Color("#4A4A5E"),
			BorderActive:  lipgloss.Color("#9EC1C9"),
			Text:          lipgloss.Color("#C8C2B0"),
			Muted:         lipgloss.Color("#8B849E"),
			Accent:        lipgloss.Color("#C0A36E"),
			Info:          lipgloss.Color("#9EC1C9"),
			Success:       lipgloss.Color("#8EAA7A"),
			Selection:     lipgloss.Color("#323248"),
			Hover:         lipgloss.Color("#2C2C42"),
			Marked:        lipgloss.Color("#B5534E"),
			Warning:       lipgloss.Color("#C0A36E"),
			Danger:        lipgloss.Color("#B5534E"),
			ActivePath:    lipgloss.Color("#9EC1C9"),
			ConfirmButton: lipgloss.Color("#8EAA7A"),
			CancelButton:  lipgloss.Color("#B5534E"),
			ProgressFill:  lipgloss.Color("#C0A36E"),
			ProgressEmpty: lipgloss.Color("#4A4A5E"),
			HelpNav:       lipgloss.Color("#C0A36E"),
			HelpPanels:    lipgloss.Color("#C0A36E"),
			HelpDialogs:   lipgloss.Color("#A58DB8"),
			HelpMouse:     lipgloss.Color("#B5534E"),
			Folder:        lipgloss.Color("#9EC1C9"),
			TextFile:      lipgloss.Color("#8EAA7A"),
			ConfigFile:    lipgloss.Color("#C0A36E"),
			ExecFile:      lipgloss.Color("#D4BE8A"),
			ImageFile:     lipgloss.Color("#9EC1C9"),
			BinaryFile:    lipgloss.Color("#A58DB8"),
			FooterKey:     lipgloss.Color("#9EC1C9"),
		}, nil

	case "rose-pine":
		return Palette{
			Name:          "rose-pine",
			Background:    lipgloss.Color("#191724"),
			Panel:         lipgloss.Color("#1F1D2E"),
			PanelInactive: lipgloss.Color("#26233A"),
			PanelElevated: lipgloss.Color("#2A273F"),
			StatusBar:     lipgloss.Color("#26233A"),
			Footer:        lipgloss.Color("#191724"),
			Border:        lipgloss.Color("#3B355A"),
			BorderActive:  lipgloss.Color("#C4A7E7"),
			Text:          lipgloss.Color("#E0DEF4"),
			Muted:         lipgloss.Color("#908CAA"),
			Accent:        lipgloss.Color("#EB6F92"),
			Info:          lipgloss.Color("#9CCFD8"),
			Success:       lipgloss.Color("#3E8FB0"),
			Selection:     lipgloss.Color("#312F44"),
			Hover:         lipgloss.Color("#2A2740"),
			Marked:        lipgloss.Color("#EB6F92"),
			Warning:       lipgloss.Color("#F6C177"),
			Danger:        lipgloss.Color("#EB6F92"),
			ActivePath:    lipgloss.Color("#9CCFD8"),
			ConfirmButton: lipgloss.Color("#3E8FB0"),
			CancelButton:  lipgloss.Color("#EB6F92"),
			ProgressFill:  lipgloss.Color("#C4A7E7"),
			ProgressEmpty: lipgloss.Color("#3B355A"),
			HelpNav:       lipgloss.Color("#C4A7E7"),
			HelpPanels:    lipgloss.Color("#F6C177"),
			HelpDialogs:   lipgloss.Color("#C4A7E7"),
			HelpMouse:     lipgloss.Color("#EB6F92"),
			Folder:        lipgloss.Color("#C4A7E7"),
			TextFile:      lipgloss.Color("#3E8FB0"),
			ConfigFile:    lipgloss.Color("#F6C177"),
			ExecFile:      lipgloss.Color("#E0DEF4"),
			ImageFile:     lipgloss.Color("#9CCFD8"),
			BinaryFile:    lipgloss.Color("#C4A7E7"),
			FooterKey:     lipgloss.Color("#9CCFD8"),
		}, nil

	case "solarized-dark":
		return Palette{
			Name:          "solarized-dark",
			Background:    lipgloss.Color("#002B36"),
			Panel:         lipgloss.Color("#073642"),
			PanelInactive: lipgloss.Color("#0D4A56"),
			PanelElevated: lipgloss.Color("#125A68"),
			StatusBar:     lipgloss.Color("#0D4A56"),
			Footer:        lipgloss.Color("#002B36"),
			Border:        lipgloss.Color("#586E75"),
			BorderActive:  lipgloss.Color("#268BD2"),
			Text:          lipgloss.Color("#93A1A1"),
			Muted:         lipgloss.Color("#657B83"),
			Accent:        lipgloss.Color("#D33682"),
			Info:          lipgloss.Color("#2AA198"),
			Success:       lipgloss.Color("#859900"),
			Selection:     lipgloss.Color("#073642"),
			Hover:         lipgloss.Color("#0B4A56"),
			Marked:        lipgloss.Color("#DC322F"),
			Warning:       lipgloss.Color("#B58900"),
			Danger:        lipgloss.Color("#DC322F"),
			ActivePath:    lipgloss.Color("#2AA198"),
			ConfirmButton: lipgloss.Color("#859900"),
			CancelButton:  lipgloss.Color("#DC322F"),
			ProgressFill:  lipgloss.Color("#268BD2"),
			ProgressEmpty: lipgloss.Color("#586E75"),
			HelpNav:       lipgloss.Color("#268BD2"),
			HelpPanels:    lipgloss.Color("#B58900"),
			HelpDialogs:   lipgloss.Color("#D33682"),
			HelpMouse:     lipgloss.Color("#DC322F"),
			Folder:        lipgloss.Color("#268BD2"),
			TextFile:      lipgloss.Color("#859900"),
			ConfigFile:    lipgloss.Color("#B58900"),
			ExecFile:      lipgloss.Color("#CB4B16"),
			ImageFile:     lipgloss.Color("#2AA198"),
			BinaryFile:    lipgloss.Color("#D33682"),
			FooterKey:     lipgloss.Color("#2AA198"),
		}, nil

	case "vesper":
		return Palette{
			Name:          "vesper",
			Background:    lipgloss.Color("#101010"),
			Panel:         lipgloss.Color("#181820"),
			PanelInactive: lipgloss.Color("#1E1E30"),
			PanelElevated: lipgloss.Color("#252540"),
			StatusBar:     lipgloss.Color("#1E1E30"),
			Footer:        lipgloss.Color("#101010"),
			Border:        lipgloss.Color("#303050"),
			BorderActive:  lipgloss.Color("#A0A0FF"),
			Text:          lipgloss.Color("#E0E0F0"),
			Muted:         lipgloss.Color("#8888AA"),
			Accent:        lipgloss.Color("#C0C0FF"),
			Info:          lipgloss.Color("#8080FF"),
			Success:       lipgloss.Color("#80FF80"),
			Selection:     lipgloss.Color("#252540"),
			Hover:         lipgloss.Color("#2A2A48"),
			Marked:        lipgloss.Color("#FF6080"),
			Warning:       lipgloss.Color("#FFB040"),
			Danger:        lipgloss.Color("#FF6080"),
			ActivePath:    lipgloss.Color("#8080FF"),
			ConfirmButton: lipgloss.Color("#80FF80"),
			CancelButton:  lipgloss.Color("#FF6080"),
			ProgressFill:  lipgloss.Color("#C0C0FF"),
			ProgressEmpty: lipgloss.Color("#303050"),
			HelpNav:       lipgloss.Color("#A0A0FF"),
			HelpPanels:    lipgloss.Color("#FFB040"),
			HelpDialogs:   lipgloss.Color("#C0C0FF"),
			HelpMouse:     lipgloss.Color("#FF6080"),
			Folder:        lipgloss.Color("#8080FF"),
			TextFile:      lipgloss.Color("#80FF80"),
			ConfigFile:    lipgloss.Color("#FFB040"),
			ExecFile:      lipgloss.Color("#FF8040"),
			ImageFile:     lipgloss.Color("#8080FF"),
			BinaryFile:    lipgloss.Color("#C0C0FF"),
			FooterKey:     lipgloss.Color("#8080FF"),
		}, nil

	default:
		return Palette{}, fmt.Errorf("unknown theme %q", name)
	}
}
