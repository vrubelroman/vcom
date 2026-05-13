package ui

import (
	"os/exec"
	"runtime"
	"strings"
)

func resolveIconMode(mode string) (bool, string) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ascii":
		return false, "Icon mode: ASCII"
	case "nerd":
		return true, ""
	case "", "auto":
	default:
		return true, ""
	}

	if runtime.GOOS != "linux" {
		return true, ""
	}
	if _, err := exec.LookPath("fc-list"); err != nil {
		return true, ""
	}

	out, err := exec.Command("fc-list", ":", "family").Output()
	if err != nil {
		return true, ""
	}
	text := strings.ToLower(string(out))
	if strings.Contains(text, "nerd font") {
		return true, ""
	}

	return false, "Nerd Font not found: using ASCII icons"
}
