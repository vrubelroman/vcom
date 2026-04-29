package remote

import (
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHClient wraps an SSH connection and SFTP client for remote filesystem access.
type SSHClient struct {
	// Host is the SSH host configuration used to establish the connection.
	Host SSHHost

	sshConn *ssh.Client
	sftpCli *sftp.Client

	keepaliveStop chan struct{}
	keepaliveWg   sync.WaitGroup
}

// Connect establishes an SSH connection to the remote host and opens an SFTP session.
// It uses key-based authentication if IdentityFile is set, otherwise falls back to password auth.
func Connect(host SSHHost) (*SSHClient, error) {
	authMethods, err := authMethodsForHost(host)
	if err != nil {
		return nil, fmt.Errorf("ssh auth: %w", err)
	}

	user := host.User
	if user == "" {
		user = os.Getenv("USER")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: support known_hosts verification
		Timeout:         15 * time.Second,
	}

	addr := host.Addr()
	sshConn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	sftpCli, err := sftp.NewClient(sshConn)
	if err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("sftp client: %w", err)
	}

	client := &SSHClient{
		Host:          host,
		sshConn:       sshConn,
		sftpCli:       sftpCli,
		keepaliveStop: make(chan struct{}),
	}

	// Start keepalive goroutine — sends keepalive@openssh.com every 30s
	// to prevent the SSH server from dropping the connection during inactivity.
	client.keepaliveWg.Add(1)
	go func() {
		defer client.keepaliveWg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _, err := sshConn.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					return
				}
			case <-client.keepaliveStop:
				return
			}
		}
	}()

	return client, nil
}

// authMethodsForHost returns the appropriate SSH auth methods for the given host.
// For SSH config hosts with IdentityFile, it uses public key authentication.
// For custom hosts with a password, it uses password authentication.
func authMethodsForHost(host SSHHost) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try key-based auth if identity file is specified
	if host.IdentityFile != "" {
		key, err := os.ReadFile(host.IdentityFile)
		if err == nil {
			signer, err := ssh.ParsePrivateKey(key)
			if err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			} else {
				// If the key is encrypted, try with empty passphrase or common ones
				// For simplicity, we try without passphrase first
				// In a real implementation, we might prompt for a passphrase
			}
		}
	}

	// Try password auth if password is set
	if host.Password != "" {
		methods = append(methods, ssh.Password(host.Password))
	}

	// Always include keyboard-interactive as a fallback (it wraps password)
	if host.Password != "" {
		methods = append(methods, ssh.KeyboardInteractive(
			func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = host.Password
				}
				return answers, nil
			},
		))
	}

	// Always try default SSH agent and default keys as a last resort
	// This covers the case where the user has an SSH agent running with loaded keys
	// but no IdentityFile is specified in the config.
	if host.IdentityFile == "" && host.Password == "" {
		// Add default key paths
		home, err := os.UserHomeDir()
		if err == nil {
			defaultKeys := []string{
				home + "/.ssh/id_rsa",
				home + "/.ssh/id_ed25519",
				home + "/.ssh/id_ecdsa",
				home + "/.ssh/id_ecdsa_sk",
				home + "/.ssh/id_ed25519_sk",
				home + "/.ssh/identity",
			}
			for _, keyPath := range defaultKeys {
				if key, err := os.ReadFile(keyPath); err == nil {
					if signer, err := ssh.ParsePrivateKey(key); err == nil {
						methods = append(methods, ssh.PublicKeys(signer))
					}
				}
			}
		}
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication methods available for host %q", host.Name)
	}

	return methods, nil
}

// ReadDir reads the contents of a remote directory and returns os.FileInfo entries.
func (c *SSHClient) ReadDir(dirPath string) ([]os.FileInfo, error) {
	if c.sftpCli == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.sftpCli.ReadDir(dirPath)
}

// Lstat returns file information without following symlinks.
func (c *SSHClient) Lstat(path string) (os.FileInfo, error) {
	if c.sftpCli == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.sftpCli.Lstat(path)
}

// Stat returns file information following symlinks.
func (c *SSHClient) Stat(path string) (os.FileInfo, error) {
	if c.sftpCli == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.sftpCli.Stat(path)
}

// ReadLink reads the target of a symbolic link.
func (c *SSHClient) ReadLink(linkPath string) (string, error) {
	if c.sftpCli == nil {
		return "", fmt.Errorf("not connected")
	}
	return c.sftpCli.ReadLink(linkPath)
}

// RealPath resolves a path to its absolute form on the remote server.
func (c *SSHClient) RealPath(p string) (string, error) {
	if c.sftpCli == nil {
		return "", fmt.Errorf("not connected")
	}
	return c.sftpCli.RealPath(p)
}

// ReadFile opens a remote file for reading.
func (c *SSHClient) ReadFile(filePath string) (io.ReadCloser, error) {
	if c.sftpCli == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.sftpCli.Open(filePath)
}

// CreateFile opens a remote file for writing, creating it if it doesn't exist.
func (c *SSHClient) CreateFile(filePath string) (io.WriteCloser, error) {
	if c.sftpCli == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.sftpCli.Create(filePath)
}

// MkdirAll creates a remote directory and any necessary parents.
// If the directory already exists, it returns nil (no error).
func (c *SSHClient) MkdirAll(dirPath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}
	// sftp doesn't have MkdirAll, so we implement it manually
	// First check if the path already exists
	_, err := c.sftpCli.Stat(dirPath)
	if err == nil {
		return nil // already exists
	}
	if !os.IsNotExist(err) {
		return err
	}
	// Ensure parent exists first
	parent := path.Dir(dirPath)
	if parent != dirPath && parent != "." {
		if err := c.MkdirAll(parent); err != nil {
			return err
		}
	}
	return c.sftpCli.Mkdir(dirPath)
}

// Mkdir creates a single remote directory.
func (c *SSHClient) Mkdir(dirPath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}
	return c.sftpCli.Mkdir(dirPath)
}

// Remove deletes a remote file.
func (c *SSHClient) Remove(filePath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}
	return c.sftpCli.Remove(filePath)
}

// RemoveDirectory removes a remote directory (must be empty).
func (c *SSHClient) RemoveDirectory(dirPath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}
	return c.sftpCli.RemoveDirectory(dirPath)
}

// Rename moves/renames a remote file or directory.
func (c *SSHClient) Rename(oldPath, newPath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}
	return c.sftpCli.Rename(oldPath, newPath)
}

// Close closes the SFTP session and SSH connection.
func (c *SSHClient) Close() error {
	// Stop the keepalive goroutine first
	if c.keepaliveStop != nil {
		select {
		case <-c.keepaliveStop:
			// already closed
		default:
			close(c.keepaliveStop)
		}
		c.keepaliveWg.Wait()
	}

	var firstErr error

	if c.sftpCli != nil {
		if err := c.sftpCli.Close(); err != nil {
			firstErr = err
		}
		c.sftpCli = nil
	}

	if c.sshConn != nil {
		if err := c.sshConn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		c.sshConn = nil
	}

	return firstErr
}

// IsConnected returns true if the client has an active connection.
func (c *SSHClient) IsConnected() bool {
	return c.sftpCli != nil && c.sshConn != nil
}

// RemoveRecursive recursively deletes a remote file or directory.
// For directories, it walks and removes all children first.
func (c *SSHClient) RemoveRecursive(path string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}

	info, err := c.sftpCli.Stat(path)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return c.sftpCli.Remove(path)
	}

	// Walk directory and collect all paths (files first, then dirs)
	var files []string
	var dirs []string
	err = c.Walk(path, func(walkPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if walkPath == path {
			return nil // skip root
		}
		if info.IsDir() {
			dirs = append(dirs, walkPath)
		} else {
			files = append(files, walkPath)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Remove files first, then directories (reverse order for deepest first)
	for _, f := range files {
		if err := c.sftpCli.Remove(f); err != nil {
			return err
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := c.sftpCli.RemoveDirectory(dirs[i]); err != nil {
			return err
		}
	}

	// Finally remove the root directory
	return c.sftpCli.RemoveDirectory(path)
}

// CopyFileToRemote copies a local file to a remote destination via SFTP.
// It creates parent directories as needed.
func (c *SSHClient) CopyFileToRemote(localPath, remotePath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local: %w", err)
	}
	defer localFile.Close()

	// Ensure parent directory exists
	parent := path.Dir(remotePath)
	if err := c.MkdirAll(parent); err != nil {
		return fmt.Errorf("mkdir remote: %w", err)
	}

	remoteFile, err := c.sftpCli.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	defer remoteFile.Close()

	_, err = io.Copy(remoteFile, localFile)
	if err != nil {
		return fmt.Errorf("copy to remote: %w", err)
	}

	return nil
}

// CopyFileFromRemote copies a remote file to a local destination via SFTP.
// It creates parent directories as needed.
func (c *SSHClient) CopyFileFromRemote(remotePath, localPath string) error {
	if c.sftpCli == nil {
		return fmt.Errorf("not connected")
	}

	remoteFile, err := c.sftpCli.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote: %w", err)
	}
	defer remoteFile.Close()

	// Ensure parent directory exists
	parent := filepath.Dir(localPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("mkdir local: %w", err)
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local: %w", err)
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, remoteFile)
	if err != nil {
		return fmt.Errorf("copy from remote: %w", err)
	}

	return nil
}

// CopyDirToRemote recursively copies a local directory to a remote path.
func (c *SSHClient) CopyDirToRemote(localDir, remoteDir string) error {
	return filepath.Walk(localDir, func(localPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(localDir, localPath)
		remotePath := path.Join(remoteDir, relPath)

		if info.IsDir() {
			return c.MkdirAll(remotePath)
		}

		return c.CopyFileToRemote(localPath, remotePath)
	})
}

// CopyDirFromRemote recursively copies a remote directory to a local path.
func (c *SSHClient) CopyDirFromRemote(remoteDir, localDir string) error {
	return c.Walk(remoteDir, func(remotePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(remoteDir, remotePath)
		localPath := filepath.Join(localDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(localPath, 0o755)
		}

		return c.CopyFileFromRemote(remotePath, localPath)
	})
}

// CopyFileBetweenRemotes copies a single file from one remote host to another
// by streaming the file contents through the local machine. Both SFTP connections
// must be active (connected).
func CopyFileBetweenRemotes(srcClient, dstClient *SSHClient, srcPath, dstPath string) error {
	if srcClient.sftpCli == nil {
		return fmt.Errorf("source client not connected")
	}
	if dstClient.sftpCli == nil {
		return fmt.Errorf("destination client not connected")
	}

	srcFile, err := srcClient.sftpCli.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open remote source %s: %w", srcPath, err)
	}
	defer srcFile.Close()

	// Ensure parent directory exists on the destination
	parent := path.Dir(dstPath)
	if err := dstClient.MkdirAll(parent); err != nil {
		return fmt.Errorf("mkdir remote dest %s: %w", parent, err)
	}

	dstFile, err := dstClient.sftpCli.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create remote dest %s: %w", dstPath, err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("copy remote to remote %s → %s: %w", srcPath, dstPath, err)
	}

	return nil
}

// CopyDirBetweenRemotes recursively copies a directory from one remote host to another.
// Directories are created on the destination first, then all files are streamed through
// the local machine.
func CopyDirBetweenRemotes(srcClient, dstClient *SSHClient, srcDir, dstDir string) error {
	return srcClient.Walk(srcDir, func(remotePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(srcDir, remotePath)
		dstPath := path.Join(dstDir, relPath)

		if info.IsDir() {
			return dstClient.MkdirAll(dstPath)
		}

		return CopyFileBetweenRemotes(srcClient, dstClient, remotePath, dstPath)
	})
}

// Walk walks the remote filesystem tree rooted at root, calling walkFn for each file/dir.
// This is a simplified version of filepath.Walk for SFTP.
type walkFunc func(path string, info os.FileInfo, err error) error

func (c *SSHClient) Walk(root string, walkFn walkFunc) error {
	return c.walk(root, walkFn, nil)
}

func (c *SSHClient) walk(dirPath string, walkFn walkFunc, info os.FileInfo) error {
	if info == nil {
		var err error
		info, err = c.sftpCli.Stat(dirPath)
		if err != nil {
			return walkFn(dirPath, nil, err)
		}
	}

	err := walkFn(dirPath, info, nil)
	if err != nil {
		if err == filepathSkipDir {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	entries, err := c.sftpCli.ReadDir(dirPath)
	if err != nil {
		return walkFn(dirPath, info, err)
	}

	for _, entry := range entries {
		childPath := path.Join(dirPath, entry.Name())
		if entry.IsDir() {
			err = c.walk(childPath, walkFn, entry)
		} else {
			err = walkFn(childPath, entry, nil)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// filepathSkipDir is used as a return value from Walk to skip a directory.
var filepathSkipDir = fmt.Errorf("skip this directory")

// SftpToFileInfo converts an os.FileInfo to a vfs-compatible file info.
// This is used for consistent file information handling across local and remote.
func SftpToFileInfo(name string, info os.FileInfo) (os.FileInfo, error) {
	return info, nil
}

// WalkDirEntry wraps os.FileInfo with the file name for directory listings.
type WalkDirEntry struct {
	os.FileInfo
	entryName string
}

func (e *WalkDirEntry) Name() string {
	return e.entryName
}

// NewWalkDirEntry creates a new WalkDirEntry with an overridden name.
func NewWalkDirEntry(info os.FileInfo, name string) *WalkDirEntry {
	return &WalkDirEntry{FileInfo: info, entryName: name}
}

// DialTimeout is the timeout for establishing SSH connections.
const DialTimeout = 15 * time.Second

// DefaultPort is the default SSH port.
const DefaultPort = "22"

// ResolveAddr returns the SSH address for the given host, applying the default port if needed.
func ResolveAddr(hostname, port string) string {
	host := strings.TrimSpace(hostname)
	if port == "" || port == "0" {
		port = DefaultPort
	}
	return net.JoinHostPort(host, port)
}
