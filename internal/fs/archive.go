package vfs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ExtractArchiveToTemp(sourcePath string) (string, error) {
	// Count total files for progress reporting
	totalFiles, totalBytes := countArchiveEntries(sourcePath)

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
		if err := extractZipArchive(sourcePath, tempDir, nil, totalFiles, totalBytes); err != nil {
			return cleanupOnErr(err)
		}
	case strings.HasSuffix(sourceLower, ".tar"):
		if err := extractTarArchive(sourcePath, tempDir, false, nil, totalFiles, totalBytes); err != nil {
			return cleanupOnErr(err)
		}
	case strings.HasSuffix(sourceLower, ".tar.gz"), strings.HasSuffix(sourceLower, ".tgz"):
		if err := extractTarArchive(sourcePath, tempDir, true, nil, totalFiles, totalBytes); err != nil {
			return cleanupOnErr(err)
		}
	default:
		return cleanupOnErr(fmt.Errorf("archive format is not supported: %s", filepath.Ext(sourcePath)))
	}

	return tempDir, nil
}

// ExtractArchiveToDir extracts an archive to the specified target directory.
// Unlike ExtractArchiveToTemp, it extracts directly to targetDir without
// creating a temporary directory. The progress callback is called after each
// file is extracted; it may be nil.
func ExtractArchiveToDir(sourcePath, targetDir string, progress func(CopyProgress)) error {
	totalFiles, totalBytes := countArchiveEntries(sourcePath)
	sourceLower := strings.ToLower(sourcePath)
	switch {
	case strings.HasSuffix(sourceLower, ".zip"):
		return extractZipArchive(sourcePath, targetDir, progress, totalFiles, totalBytes)
	case strings.HasSuffix(sourceLower, ".tar"):
		return extractTarArchive(sourcePath, targetDir, false, progress, totalFiles, totalBytes)
	case strings.HasSuffix(sourceLower, ".tar.gz"), strings.HasSuffix(sourceLower, ".tgz"):
		return extractTarArchive(sourcePath, targetDir, true, progress, totalFiles, totalBytes)
	default:
		return fmt.Errorf("archive format is not supported: %s", filepath.Ext(sourcePath))
	}
}

// countArchiveEntries counts the total number of files and total uncompressed
// bytes in an archive without extracting. Used for progress reporting.
func countArchiveEntries(sourcePath string) (int64, int64) {
	sourceLower := strings.ToLower(sourcePath)
	switch {
	case strings.HasSuffix(sourceLower, ".zip"):
		return countZipEntries(sourcePath)
	case strings.HasSuffix(sourceLower, ".tar"), strings.HasSuffix(sourceLower, ".tar.gz"), strings.HasSuffix(sourceLower, ".tgz"):
		return countTarEntries(sourcePath)
	default:
		return 0, 0
	}
}

func countZipEntries(sourcePath string) (int64, int64) {
	r, err := zip.OpenReader(sourcePath)
	if err != nil {
		return 0, 0
	}
	defer r.Close()
	var files, bytes int64
	for _, f := range r.File {
		if !f.FileInfo().IsDir() {
			files++
			bytes += int64(f.UncompressedSize64)
		}
	}
	return files, bytes
}

func countTarEntries(sourcePath string) (int64, int64) {
	f, err := os.Open(sourcePath)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(strings.ToLower(sourcePath), ".tar.gz") || strings.HasSuffix(strings.ToLower(sourcePath), ".tgz") {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return 0, 0
		}
		defer gr.Close()
		reader = gr
	}

	tarReader := tar.NewReader(reader)
	var files, bytes int64
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			files++
			bytes += hdr.Size
		}
	}
	return files, bytes
}

func extractZipArchive(sourcePath string, targetDir string, progress func(CopyProgress), totalFiles, totalBytes int64) error {
	reader, err := zip.OpenReader(sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var filesDone int64
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

		filesDone++
		if progress != nil {
			progress(CopyProgress{
				FilesDone:  int(filesDone),
				FilesTotal: int(totalFiles),
				BytesDone:  0,
				BytesTotal: totalBytes,
				Stage:      "Extracting data",
			})
		}
	}
	return nil
}

func extractTarArchive(sourcePath string, targetDir string, gzipped bool, progress func(CopyProgress), totalFiles, totalBytes int64) error {
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
	var filesDone int64
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
			filesDone++
			if progress != nil {
				progress(CopyProgress{
					FilesDone:  int(filesDone),
					FilesTotal: int(totalFiles),
					BytesDone:  0,
					BytesTotal: totalBytes,
					Stage:      "Extracting data",
				})
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

// ArchiveFormat returns the file extension for a given archive format name.
func ArchiveFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		return ".zip"
	case "tar":
		return ".tar"
	case "targz", "tar.gz", "tgz":
		return ".tar.gz"
	default:
		return ".zip"
	}
}

// ArchiveName generates an archive filename from source paths.
func ArchiveName(sources []string, format string) string {
	ext := ArchiveFormat(format)
	if len(sources) == 1 {
		base := strings.TrimSuffix(filepath.Base(sources[0]), filepath.Ext(sources[0]))
		return base + ext
	}
	base := filepath.Base(filepath.Dir(sources[0]))
	if base == "." || base == "" || base == string(filepath.Separator) {
		base = "archive"
	}
	return base + ext
}

// CreateArchive creates an archive from source paths using the given format.
// Supported formats: "zip", "tar", "tar.gz" (or "targz", "tgz").
// Progress is reported via the callback function.
func CreateArchive(ctx context.Context, sources []string, archivePath string, progress func(CopyProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}

	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return createZipArchive(ctx, sources, archivePath, progress)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return createTarGzArchive(ctx, sources, archivePath, progress)
	case strings.HasSuffix(lower, ".tar"):
		return createTarArchive(ctx, sources, archivePath, progress)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Ext(archivePath))
	}
}

func createZipArchive(ctx context.Context, sources []string, archivePath string, progress func(CopyProgress)) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create %s: %w", archivePath, err)
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	var totalFiles int
	var totalBytes int64
	for _, source := range sources {
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("stat %s: %w", source, err)
		}
		if info.IsDir() {
			err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				totalFiles++
				if !info.IsDir() {
					totalBytes += info.Size()
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			totalFiles++
			totalBytes += info.Size()
		}
	}

	state := &copyProgressState{
		ctx:      ctx,
		stats:    TransferStats{FilesTotal: totalFiles, BytesTotal: totalBytes},
		callback: progress,
		lastEmit: time.Now(),
	}

	baseDir := commonBaseDir(sources)
	for _, source := range sources {
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("stat %s: %w", source, err)
		}
		relRoot := source
		if baseDir != "" {
			relRoot, _ = filepath.Rel(baseDir, source)
		}
		if info.IsDir() {
			err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				relPath, _ := filepath.Rel(baseDir, path)
				relPath = filepath.ToSlash(relPath)
				header, zipErr := zip.FileInfoHeader(info)
				if zipErr != nil {
					return zipErr
				}
				header.Name = relPath
				if info.IsDir() {
					header.Name += "/"
				} else {
					header.Method = zip.Deflate
				}
				writer, zipErr := zipWriter.CreateHeader(header)
				if zipErr != nil {
					return zipErr
				}
				if !info.IsDir() {
					f, openErr := os.Open(path)
					if openErr != nil {
						return openErr
					}
					written, copyErr := io.Copy(writer, f)
					f.Close()
					if copyErr != nil {
						return copyErr
					}
					state.filesDone++
					state.bytesDone += written
				} else {
					state.filesDone++
				}
				emitArchiveProgress(state, path)
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			relPath := filepath.ToSlash(relRoot)
			header, zipErr := zip.FileInfoHeader(info)
			if zipErr != nil {
				return zipErr
			}
			header.Name = relPath
			header.Method = zip.Deflate
			writer, zipErr := zipWriter.CreateHeader(header)
			if zipErr != nil {
				return zipErr
			}
			f, openErr := os.Open(source)
			if openErr != nil {
				return openErr
			}
			written, copyErr := io.Copy(writer, f)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
			state.filesDone++
			state.bytesDone += written
			emitArchiveProgress(state, source)
		}
	}
	return nil
}

func createTarArchive(ctx context.Context, sources []string, archivePath string, progress func(CopyProgress)) error {
	return createTarArchiveWithGzip(ctx, sources, archivePath, false, progress)
}

func createTarGzArchive(ctx context.Context, sources []string, archivePath string, progress func(CopyProgress)) error {
	return createTarArchiveWithGzip(ctx, sources, archivePath, true, progress)
}

func createTarArchiveWithGzip(ctx context.Context, sources []string, archivePath string, gzipped bool, progress func(CopyProgress)) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create %s: %w", archivePath, err)
	}
	defer file.Close()

	var writer io.WriteCloser = file
	if gzipped {
		gzipWriter := gzip.NewWriter(file)
		defer gzipWriter.Close()
		writer = gzipWriter
	}

	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	var totalFiles int
	var totalBytes int64
	for _, source := range sources {
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("stat %s: %w", source, err)
		}
		if info.IsDir() {
			err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				totalFiles++
				if !info.IsDir() {
					totalBytes += info.Size()
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			totalFiles++
			totalBytes += info.Size()
		}
	}

	state := &copyProgressState{
		ctx:      ctx,
		stats:    TransferStats{FilesTotal: totalFiles, BytesTotal: totalBytes},
		callback: progress,
		lastEmit: time.Now(),
	}

	baseDir := commonBaseDir(sources)
	for _, source := range sources {
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("stat %s: %w", source, err)
		}
		if info.IsDir() {
			err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				relPath, _ := filepath.Rel(baseDir, path)
				relPath = filepath.ToSlash(relPath)
				header, tarErr := tar.FileInfoHeader(info, path)
				if tarErr != nil {
					return tarErr
				}
				header.Name = relPath
				if info.IsDir() {
					header.Name += "/"
				}
				if err := tarWriter.WriteHeader(header); err != nil {
					return err
				}
				if !info.IsDir() {
					f, openErr := os.Open(path)
					if openErr != nil {
						return openErr
					}
					written, copyErr := io.Copy(tarWriter, f)
					f.Close()
					if copyErr != nil {
						return copyErr
					}
					state.filesDone++
					state.bytesDone += written
				} else {
					state.filesDone++
				}
				emitArchiveProgress(state, path)
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			relPath, _ := filepath.Rel(baseDir, source)
			relPath = filepath.ToSlash(relPath)
			header, tarErr := tar.FileInfoHeader(info, source)
			if tarErr != nil {
				return tarErr
			}
			header.Name = relPath
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}
			f, openErr := os.Open(source)
			if openErr != nil {
				return openErr
			}
			written, copyErr := io.Copy(tarWriter, f)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
			state.filesDone++
			state.bytesDone += written
			emitArchiveProgress(state, source)
		}
	}
	return nil
}

func emitArchiveProgress(state *copyProgressState, currentPath string) {
	if state.callback == nil {
		return
	}
	now := time.Now()
	if now.Sub(state.lastEmit) < 50*time.Millisecond {
		return
	}
	state.lastEmit = now
	state.callback(CopyProgress{
		FilesDone:   state.filesDone,
		FilesTotal:  state.stats.FilesTotal,
		BytesDone:   state.bytesDone,
		BytesTotal:  state.stats.BytesTotal,
		CurrentPath: currentPath,
		Stage:       "Archiving",
	})
}

// commonBaseDir returns the longest common directory prefix for the given paths.
func commonBaseDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		if info, err := os.Lstat(paths[0]); err == nil && info.IsDir() {
			return filepath.Dir(paths[0])
		}
		return filepath.Dir(paths[0])
	}

	base := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		dir := filepath.Dir(p)
		for !strings.HasPrefix(dir, base) && base != "" {
			parent := filepath.Dir(base)
			if parent == base {
				return ""
			}
			base = parent
		}
	}
	return base
}
