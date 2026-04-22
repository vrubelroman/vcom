package vfs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func CopyPath(srcPath string, dstDir string, overwrite bool) (string, error) {
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

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(srcPath)
		if err != nil {
			return "", err
		}
		return targetPath, os.Symlink(target, targetPath)
	}

	if srcInfo.IsDir() {
		return targetPath, copyDir(srcPath, targetPath)
	}

	return targetPath, copyFile(srcPath, targetPath, srcInfo.Mode())
}

func MovePath(srcPath string, dstDir string, overwrite bool) (string, error) {
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

	if err := os.Rename(srcPath, targetPath); err == nil {
		return targetPath, nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return "", err
	}

	targetPath, err := CopyPath(srcPath, dstDir, overwrite)
	if err != nil {
		return "", err
	}
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

func copyDir(srcDir string, dstDir string) error {
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
		case info.IsDir():
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		default:
			if err := copyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(srcPath string, dstPath string, mode os.FileMode) error {
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

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
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
