package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type ListOptions struct {
	ShowHidden  bool
	DirsFirst   bool
	SortBy      string
	SortReverse bool
}

func ListDir(path string, options ListOptions) ([]Entry, error) {
	resolvedPath := path
	if resolvedPath == "" {
		resolvedPath = "."
	}

	dirEntries, err := os.ReadDir(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", resolvedPath, err)
	}

	entries := make([]Entry, 0, len(dirEntries)+1)
	if parent := filepath.Dir(resolvedPath); parent != resolvedPath {
		entries = append(entries, Entry{
			Name:     "..",
			Path:     parent,
			IsDir:    true,
			IsParent: true,
		})
	}

	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		hidden := strings.HasPrefix(name, ".")
		if hidden && !options.ShowHidden {
			continue
		}

		fullPath := filepath.Join(resolvedPath, name)
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}

		entry := Entry{
			Name:       name,
			Path:       fullPath,
			Extension:  ext(name),
			Mode:       info.Mode(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
			IsDir:      info.IsDir(),
			IsHidden:   hidden,
		}

		if createdAt, ok := statBirthTime(fullPath); ok {
			entry.CreatedAt = createdAt
			entry.CreatedKnown = true
		}

		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]

		if left.IsParent != right.IsParent {
			return left.IsParent
		}
		if options.DirsFirst && left.IsDir != right.IsDir {
			return left.IsDir
		}
		comparison := compareEntries(left, right, options.SortBy)
		if options.SortReverse {
			return comparison > 0
		}
		return comparison < 0
	})

	return entries, nil
}

func compareEntries(left Entry, right Entry, sortBy string) int {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "size":
		if left.Size != right.Size {
			return cmpInt64(left.Size, right.Size)
		}
	case "modified":
		if !left.ModifiedAt.Equal(right.ModifiedAt) {
			return cmpTimeDesc(left.ModifiedAt, right.ModifiedAt)
		}
	case "created":
		if left.CreatedKnown != right.CreatedKnown {
			if left.CreatedKnown {
				return -1
			}
			return 1
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return cmpTimeDesc(left.CreatedAt, right.CreatedAt)
		}
	case "extension":
		if left.Extension != right.Extension {
			return strings.Compare(left.Extension, right.Extension)
		}
	}

	return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
}

func cmpInt64(left int64, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func cmpTimeDesc(left time.Time, right time.Time) int {
	switch {
	case left.Equal(right):
		return 0
	case left.After(right):
		return -1
	default:
		return 1
	}
}

func statBirthTime(path string) (time.Time, bool) {
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, unix.AT_STATX_SYNC_AS_STAT, unix.STATX_BTIME, &stx); err == nil {
		if stx.Mask&unix.STATX_BTIME != 0 {
			return time.Unix(int64(stx.Btime.Sec), int64(stx.Btime.Nsec)), true
		}
	}

	info, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, false
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}

	seconds := int64(stat.Ctim.Sec)
	nanos := int64(stat.Ctim.Nsec)
	if seconds == 0 && nanos == 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, nanos), true
}

func DirectorySize(path string) (int64, error) {
	var total int64

	err := filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func FindSelected(entries []Entry, key string) int {
	for idx, entry := range entries {
		if entry.MatchKey() == key {
			return idx
		}
	}
	return 0
}

func HumanSize(size int64) string {
	if size < 0 {
		return "?"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PB", value/1024)
}

func ShortTime(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.Format("2006-01-02 15:04")
}

func CompactTime(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.Format("01-02 15:04")
}

func Permissions(mode fs.FileMode) string {
	return mode.String()
}

func IsBinarySample(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	var controls int
	for _, b := range data {
		if b == 0 {
			return true
		}
		if b < 9 || (b > 13 && b < 32) {
			controls++
		}
	}
	return controls > len(data)/10
}

func SafeBase(path string) string {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return path
	}
	return base
}

func JoinPath(path string, name string) string {
	return filepath.Join(path, name)
}

func ErrOverwrite(path string) error {
	return fmt.Errorf("target already exists: %s", path)
}

func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
