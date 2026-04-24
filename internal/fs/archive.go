package vfs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ExtractArchiveToTemp(sourcePath string) (string, error) {
	tempDir, err := os.MkdirTemp("", "vcom-archive-")
	if err != nil {
		return "", err
	}
	cleanupOnErr := func(extractErr error) (string, error) {
		_ = os.RemoveAll(tempDir)
		return "", extractErr
	}

	sourceLower := strings.ToLower(sourcePath)
	switch {
	case strings.HasSuffix(sourceLower, ".zip"):
		if err := extractZipArchive(sourcePath, tempDir); err != nil {
			return cleanupOnErr(err)
		}
	case strings.HasSuffix(sourceLower, ".tar"):
		if err := extractTarArchive(sourcePath, tempDir, false); err != nil {
			return cleanupOnErr(err)
		}
	case strings.HasSuffix(sourceLower, ".tar.gz"), strings.HasSuffix(sourceLower, ".tgz"):
		if err := extractTarArchive(sourcePath, tempDir, true); err != nil {
			return cleanupOnErr(err)
		}
	default:
		return cleanupOnErr(fmt.Errorf("archive format is not supported: %s", filepath.Ext(sourcePath)))
	}

	return tempDir, nil
}

func extractZipArchive(sourcePath string, targetDir string) error {
	reader, err := zip.OpenReader(sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		relPath, ok := safeArchivePath(file.Name)
		if !ok {
			continue
		}
		fullPath := filepath.Join(targetDir, relPath)

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		if err := writeArchiveFile(fullPath, src, file.Mode()); err != nil {
			src.Close()
			return err
		}
		src.Close()
	}
	return nil
}

func extractTarArchive(sourcePath string, targetDir string, gzipped bool) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var reader io.Reader = file
	if gzipped {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		relPath, ok := safeArchivePath(header.Name)
		if !ok {
			continue
		}
		fullPath := filepath.Join(targetDir, relPath)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return err
			}
			if err := writeArchiveFile(fullPath, tarReader, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeArchiveFile(path string, source io.Reader, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, source)
	return err
}

func safeArchivePath(name string) (string, bool) {
	clean := filepath.Clean(name)
	if clean == "." || clean == string(filepath.Separator) {
		return "", false
	}
	if filepath.IsAbs(clean) {
		return "", false
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}
