package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type TransferStats struct {
	FilesTotal int
	BytesTotal int64
}

type CopyProgress struct {
	FilesDone   int
	FilesTotal  int
	BytesDone   int64
	BytesTotal  int64
	CurrentPath string
	Stage       string
}

type copyProgressState struct {
	ctx       context.Context
	filesDone int
	bytesDone int64
	stats     TransferStats
	callback  func(CopyProgress)
	lastEmit  time.Time
}

func CopyPath(srcPath string, dstDir string, overwrite bool) (string, error) {
	return CopyPathWithProgress(srcPath, dstDir, overwrite, TransferStats{}, nil)
}

func CopyPathWithProgressContext(ctx context.Context, srcPath string, dstDir string, overwrite bool, stats TransferStats, progress func(CopyProgress)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	srcInfo, err := os.Lstat(srcPath)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", srcPath, err)
	}

	targetPath := filepath.Join(dstDir, filepath.Base(srcPath))
	if same, err := samePath(srcPath, targetPath); err != nil {
		return "", err
	} else if same {
		return "", fmt.Errorf("source and target are the same: %s", targetPath)
	}

	if exists, err := PathExists(targetPath); err != nil {
		return "", err
	} else if exists {
		if !overwrite {
			return "", ErrOverwrite(targetPath)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return "", err
		}
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	if progress == nil {
		progress = func(CopyProgress) {}
	}
	if stats.FilesTotal == 0 && stats.BytesTotal == 0 {
		resolved, err := CopyStats(srcPath)
		if err != nil {
			return "", err
		}
		stats = resolved
	}
	tracker := copyProgressState{ctx: ctx, stats: stats, callback: progress}
	tracker.emit(srcPath, true)

	cleanupOnErr := func(copyErr error) (string, error) {
		if copyErr != nil {
			_ = os.RemoveAll(targetPath)
		}
		return "", copyErr
	}

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(srcPath)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := os.Symlink(target, targetPath); err != nil {
			return "", err
		}
		tracker.finishFile(srcPath)
		return targetPath, nil
	}

	if srcInfo.IsDir() {
		if err := copyDir(srcPath, targetPath, &tracker); err != nil {
			return cleanupOnErr(err)
		}
		if err := ctx.Err(); err != nil {
			return cleanupOnErr(err)
		}
		tracker.emit(srcPath, true)
		return targetPath, nil
	}

	if err := copyFile(srcPath, targetPath, srcInfo.Mode(), &tracker); err != nil {
		return cleanupOnErr(err)
	}
	tracker.emit(srcPath, true)
	return targetPath, nil
}

func CopyStats(srcPath string) (TransferStats, error) {
	srcInfo, err := os.Lstat(srcPath)
	if err != nil {
		return TransferStats{}, fmt.Errorf("stat %s: %w", srcPath, err)
	}

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return TransferStats{FilesTotal: 1, BytesTotal: 0}, nil
	}
	if !srcInfo.IsDir() {
		return TransferStats{FilesTotal: 1, BytesTotal: srcInfo.Size()}, nil
	}

	stats := TransferStats{}
	err = filepath.WalkDir(srcPath, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		info, err := os.Lstat(current)
		if err != nil {
			return err
		}

		stats.FilesTotal++
		if info.Mode()&os.ModeSymlink == 0 {
			stats.BytesTotal += info.Size()
		}
		return nil
	})
	if err != nil {
		return TransferStats{}, err
	}

	return stats, nil
}

func CopyPathWithProgress(srcPath string, dstDir string, overwrite bool, stats TransferStats, progress func(CopyProgress)) (string, error) {
	return CopyPathWithProgressContext(context.Background(), srcPath, dstDir, overwrite, stats, progress)
}

func MovePath(srcPath string, dstDir string, overwrite bool) (string, error) {
	return MovePathWithProgress(srcPath, dstDir, overwrite, TransferStats{}, nil)
}

func MovePathWithProgress(srcPath string, dstDir string, overwrite bool, stats TransferStats, progress func(CopyProgress)) (string, error) {
	return MovePathWithProgressContext(context.Background(), srcPath, dstDir, overwrite, stats, progress)
}

func MovePathWithProgressContext(ctx context.Context, srcPath string, dstDir string, overwrite bool, stats TransferStats, progress func(CopyProgress)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	targetPath := filepath.Join(dstDir, filepath.Base(srcPath))
	if same, err := samePath(srcPath, targetPath); err != nil {
		return "", err
	} else if same {
		return "", fmt.Errorf("source and target are the same: %s", targetPath)
	}

	if exists, err := PathExists(targetPath); err != nil {
		return "", err
	} else if exists {
		if !overwrite {
			return "", ErrOverwrite(targetPath)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return "", err
		}
	}

	if progress == nil {
		progress = func(CopyProgress) {}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if stats.FilesTotal == 0 && stats.BytesTotal == 0 {
		resolved, err := CopyStats(srcPath)
		if err != nil {
			return "", err
		}
		stats = resolved
	}

	if err := os.Rename(srcPath, targetPath); err == nil {
		progress(CopyProgress{
			FilesDone:   stats.FilesTotal,
			FilesTotal:  stats.FilesTotal,
			BytesDone:   stats.BytesTotal,
			BytesTotal:  stats.BytesTotal,
			CurrentPath: srcPath,
			Stage:       "Move completed",
		})
		return targetPath, nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return "", err
	}

	targetPath, err := CopyPathWithProgressContext(ctx, srcPath, dstDir, overwrite, stats, progress)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		_ = os.RemoveAll(targetPath)
		return "", err
	}
	progress(CopyProgress{
		FilesDone:   stats.FilesTotal,
		FilesTotal:  stats.FilesTotal,
		BytesDone:   stats.BytesTotal,
		BytesTotal:  stats.BytesTotal,
		CurrentPath: srcPath,
		Stage:       "Finalizing move",
	})
	if err := DeletePath(srcPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func PathExists(path string) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, err
	}
}

func DeletePath(path string) error {
	return os.RemoveAll(path)
}

func MakeDir(parent string, name string) (string, error) {
	target := filepath.Join(parent, name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	return target, nil
}

func RenamePath(sourcePath string, newName string) (string, error) {
	newName = filepath.Base(filepath.Clean(newName))
	if newName == "." || newName == "" {
		return "", fmt.Errorf("invalid target name")
	}

	targetPath := filepath.Join(filepath.Dir(sourcePath), newName)
	if same, err := samePath(sourcePath, targetPath); err != nil {
		return "", err
	} else if same {
		return "", fmt.Errorf("source and target are the same: %s", targetPath)
	}

	if exists, err := PathExists(targetPath); err != nil {
		return "", err
	} else if exists {
		return "", ErrOverwrite(targetPath)
	}

	if err := os.Rename(sourcePath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func copyDir(srcDir string, dstDir string, tracker *copyProgressState) error {
	if tracker != nil && tracker.ctx != nil {
		if err := tracker.ctx.Err(); err != nil {
			return err
		}
	}
	info, err := os.Lstat(srcDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, info.Mode().Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if tracker != nil && tracker.ctx != nil {
			if err := tracker.ctx.Err(); err != nil {
				return err
			}
		}
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		info, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return err
			}
			if tracker != nil {
				tracker.finishFile(srcPath)
			}
		case info.IsDir():
			if err := copyDir(srcPath, dstPath, tracker); err != nil {
				return err
			}
		default:
			if err := copyFile(srcPath, dstPath, info.Mode(), tracker); err != nil {
				return err
			}
		}
	}

	if tracker != nil && tracker.ctx != nil {
		if err := tracker.ctx.Err(); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(srcPath string, dstPath string, mode os.FileMode, tracker *copyProgressState) error {
	if tracker != nil && tracker.ctx != nil {
		if err := tracker.ctx.Err(); err != nil {
			return err
		}
	}
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	writer := io.Writer(dstFile)
	if tracker != nil {
		writer = &progressWriter{base: dstFile, tracker: tracker, path: srcPath}
	}
	if _, err := io.Copy(writer, srcFile); err != nil {
		_ = dstFile.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if tracker != nil && tracker.ctx != nil {
		if err := tracker.ctx.Err(); err != nil {
			_ = dstFile.Close()
			_ = os.Remove(dstPath)
			return err
		}
	}
	if tracker != nil {
		tracker.finishFile(srcPath)
	}
	return nil
}

type progressWriter struct {
	base    io.Writer
	tracker *copyProgressState
	path    string
}

func (w *progressWriter) Write(data []byte) (int, error) {
	if w.tracker != nil && w.tracker.ctx != nil {
		if err := w.tracker.ctx.Err(); err != nil {
			return 0, err
		}
	}
	n, err := w.base.Write(data)
	if n > 0 {
		w.tracker.addBytes(int64(n), w.path)
	}
	return n, err
}

func (s *copyProgressState) addBytes(delta int64, currentPath string) {
	s.bytesDone += delta
	s.emit(currentPath, false)
}

func (s *copyProgressState) finishFile(currentPath string) {
	s.filesDone++
	s.emit(currentPath, true)
}

func (s *copyProgressState) emit(currentPath string, force bool) {
	if s.callback == nil {
		return
	}
	if !force && time.Since(s.lastEmit) < 75*time.Millisecond {
		return
	}
	s.lastEmit = time.Now()
	s.callback(CopyProgress{
		FilesDone:   s.filesDone,
		FilesTotal:  s.stats.FilesTotal,
		BytesDone:   s.bytesDone,
		BytesTotal:  s.stats.BytesTotal,
		CurrentPath: currentPath,
		Stage:       "Transferring data",
	})
}

func samePath(left string, right string) (bool, error) {
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false, err
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false, err
	}
	return leftAbs == rightAbs, nil
}
