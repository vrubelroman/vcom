package vfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCopyPathWithProgressContextRemovesPartialTargetOnCancel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	for idx := 0; idx < 64; idx++ {
		path := filepath.Join(srcDir, "file-"+strconv.Itoa(idx)+".txt")
		if err := os.WriteFile(path, []byte("payload-"+strconv.Itoa(idx)), 0o644); err != nil {
			t.Fatalf("write source file %d: %v", idx, err)
		}
	}

	stats, err := CopyStats(srcDir)
	if err != nil {
		t.Fatalf("copy stats: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err = CopyPathWithProgressContext(ctx, srcDir, dstDir, false, stats, func(progress CopyProgress) {
		if progress.FilesDone >= 1 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}

	targetPath := filepath.Join(dstDir, filepath.Base(srcDir))
	if _, statErr := os.Stat(targetPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected partial target to be removed, stat err=%v", statErr)
	}
}

func TestMovePathWithProgressContextCancelledBeforeStartKeepsSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcFile := filepath.Join(root, "source.txt")
	dstDir := filepath.Join(root, "dst")
	if err := os.WriteFile(srcFile, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	stats, err := CopyStats(srcFile)
	if err != nil {
		t.Fatalf("copy stats: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = MovePathWithProgressContext(ctx, srcFile, dstDir, false, stats, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}

	if _, statErr := os.Stat(srcFile); statErr != nil {
		t.Fatalf("expected source to remain in place, stat err=%v", statErr)
	}
	targetPath := filepath.Join(dstDir, filepath.Base(srcFile))
	if _, statErr := os.Stat(targetPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected destination file to be absent, stat err=%v", statErr)
	}
}

func TestRenamePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "old.txt")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	target, err := RenamePath(source, "new.txt")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	if filepath.Base(target) != "new.txt" {
		t.Fatalf("unexpected target path: %s", target)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("expected renamed file to exist, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(source); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected source to be absent, stat err=%v", statErr)
	}
}

func TestRenamePathRejectsExistingTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "old.txt")
	target := filepath.Join(root, "new.txt")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	_, err := RenamePath(source, "new.txt")
	if err == nil {
		t.Fatalf("expected overwrite error, got nil")
	}
	if got, want := err.Error(), ErrOverwrite(target).Error(); got != want {
		t.Fatalf("expected overwrite error %q, got %q", want, got)
	}
}
