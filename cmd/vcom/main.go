package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"vcom/internal/config"
	"vcom/internal/ui"
)

func main() {
	configPath := flag.String("config", "", "path to vcom TOML config")
	flag.Parse()

	cfg, resolvedPath, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// Set up debug logging to a file in /tmp
	logDir := filepath.Join(os.TempDir(), "vcom-logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create log dir %s: %v\n", logDir, err)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("vcom-%s.log", time.Now().Format("2006-01-02_15-04-05")))
	f, err := tea.LogToFile(logPath, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot set up log file %s: %v\n", logPath, err)
	} else {
		defer f.Close()
		log.Printf("[INIT] Logging to %s", logPath)
		log.Printf("[INIT] vcom starting — config: %s, resolved: %s", *configPath, resolvedPath)
	}

	model, err := ui.NewModel(cfg, resolvedPath)
	if err != nil {
		log.Printf("[FATAL] startup error: %v", err)
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(1)
	}

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	log.Printf("[INIT] Bubble Tea program started")
	if _, err := program.Run(); err != nil {
		log.Printf("[FATAL] runtime error: %v", err)
		fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
		os.Exit(1)
	}
	log.Printf("[INIT] vcom exited cleanly")
}
