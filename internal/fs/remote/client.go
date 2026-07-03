package remote

import (
	"bufio"
	"context"
	"errors"
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
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"vcom/internal/homedir"
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

	hostKeyCB, err := tofuHostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("host key verification: %w", err)
	}

	user := host.User
	if user == "" {
		user = os.Getenv("USER")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCB,
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

// tofuHostKeyCallback returns a HostKeyCallback that verifies server host keys
// against ~/.ssh/known_hosts. Unknown hosts are trusted on first use (TOFU,
// matching `ssh -o StrictHostKeyChecking=accept-new`) and their key is
// recorded for future connections. A host whose key no longer matches a
// previously recorded entry is rejected outright, since that's the signature
// of a man-in-the-middle attack or a legitimately reissued key that the user
// must verify and update manually (e.g. via `ssh-keygen -R <host>`).
func tofuHostKeyCallback() (ssh.HostKeyCallback, error) {
	khPath := KnownHostsPath()
	if khPath == "" {
		return nil, fmt.Errorf("cannot determine known_hosts path")
	}

	if err := os.MkdirAll(filepath.Dir(khPath), 0o700); err != nil {
		return nil, fmt.Errorf("create ssh dir: %w", err)
	}
	if _, err := os.Stat(khPath); os.IsNotExist(err) {
		f, err := os.OpenFile(khPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create known_hosts: %w", err)
		}
		f.Close()
	}

	verify, err := knownhosts.New(khPath)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts: %w", err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verify(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
			return fmt.Errorf("REMOTE HOST IDENTIFICATION HAS CHANGED for %s! This may indicate a man-in-the-middle attack, or the server's key was legitimately reissued. Refusing to connect; run `ssh-keygen -R %s` to remove the stale entry once you've verified the new key: %w", hostname, hostname, err)
		}

		// Unknown host: trust on first use and remember the key.
		line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
		f, ferr := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if ferr != nil {
			return fmt.Errorf("cannot save host key for %s: %w", hostname, ferr)
		}
		defer f.Close()
		if _, werr := f.WriteString(line + "\n"); werr != nil {
			return fmt.Errorf("cannot save host key for %s: %w", hostname, werr)
		}
		return nil
	}, nil
}

// sshAgentAuth connects to the running ssh-agent (via SSH_AUTH_SOCK) and
// returns an auth method backed by its loaded keys, or false if no agent is
// reachable. The agent connection is intentionally kept open for the life of
// the process rather than closed here: ssh.PublicKeysCallback invokes the
// callback lazily during the handshake, well after this function returns.
func sshAgentAuth() (ssh.AuthMethod, bool) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, false
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, false
	}
	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), true
}

// authMethodsForHost returns the appropriate SSH auth methods for the given host.
// For SSH config hosts with IdentityFile, it uses public key authentication.
// For custom hosts with a password, it uses password authentication.
func authMethodsForHost(host SSHHost) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	var passphraseProtectedKey string

	// Try key-based auth if identity file is specified.
	if host.IdentityFile != "" {
		key, err := os.ReadFile(host.IdentityFile)
		if err == nil {
			signer, perr := ssh.ParsePrivateKey(key)
			var passphraseErr *ssh.PassphraseMissingError
			if perr == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			} else if errors.As(perr, &passphraseErr) {
				// The key is encrypted. We have no interactive prompt for a
				// passphrase yet, but the same key may already be unlocked
				// in a running ssh-agent, so fall through and try that too.
				passphraseProtectedKey = host.IdentityFile
				if authMethod, ok := sshAgentAuth(); ok {
					methods = append(methods, authMethod)
				}
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

	// When no explicit identity file or password is configured, try the
	// running ssh-agent first (it can supply already-decrypted keys that a
	// bare file scan cannot), then fall back to scanning default key paths.
	if host.IdentityFile == "" && host.Password == "" {
		if authMethod, ok := sshAgentAuth(); ok {
			methods = append(methods, authMethod)
		}

		home := homedir.Dir()
		if home != "" {
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
		if passphraseProtectedKey != "" {
			return nil, fmt.Errorf("identity file %q is passphrase-protected; load it into ssh-agent first (ssh-add %s)", passphraseProtectedKey, passphraseProtectedKey)
		}
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

// ShellQuote wraps s in single quotes so it's safe to interpolate into a
// shell command string passed to Exec/ExecWithProgress, regardless of
// spaces, `$`, backticks, or other shell metacharacters it contains. Any
// single quote in s is escaped by closing the quote, emitting an escaped
// literal quote, and reopening it (the standard POSIX shell idiom).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Exec runs a shell command on the remote server and returns combined output.
func (c *SSHClient) Exec(cmd string) ([]byte, error) {
	if c.sshConn == nil {
		return nil, fmt.Errorf("not connected")
	}
	session, err := c.sshConn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	defer session.Close()
	return session.CombinedOutput(cmd)
}

// ExecWithProgress runs a shell command on the remote server and calls onLine
// for each line of stdout output.
func (c *SSHClient) ExecWithProgress(cmd string, onLine func(line string)) error {
	if c.sshConn == nil {
		return fmt.Errorf("not connected")
	}
	session, err := c.sshConn.NewSession()
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		onLine(scanner.Text())
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return scanErr
	}

	return session.Wait()
}

// SameHostAs returns true if this client and other are connected to the same server.
func (c *SSHClient) SameHostAs(other *SSHClient) bool {
	if c == nil || other == nil {
		return false
	}
	return c.Host.SameAs(other.Host)
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

// DownloadFile downloads a remote file to a local path via SFTP.
func (c *SSHClient) DownloadFile(remotePath, localPath string) error {
	return c.CopyFileFromRemote(remotePath, localPath)
}

// CopyDirToRemote recursively copies a local directory to a remote path.
func (c *SSHClient) CopyDirToRemote(localDir, remoteDir string) error {
	return c.copyDirToRemote(localDir, remoteDir, nil, nil)
}

// CopyDirToRemoteProgress is like CopyDirToRemote but calls onFile after each copy.
func (c *SSHClient) CopyDirToRemoteProgress(localDir, remoteDir string, onFile func(path string, done, total int), ctx context.Context) error {
	return c.copyDirToRemote(localDir, remoteDir, onFile, ctx)
}

func (c *SSHClient) copyDirToRemote(localDir, remoteDir string, onFile func(path string, done, total int), ctx context.Context) error {
	done := 0
	return filepath.Walk(localDir, func(localPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		relPath, _ := filepath.Rel(localDir, localPath)
		remotePath := path.Join(remoteDir, relPath)
		if info.IsDir() {
			return c.MkdirAll(remotePath)
		}
		if err := c.CopyFileToRemote(localPath, remotePath); err != nil {
			return err
		}
		done++
		if onFile != nil {
			onFile(remotePath, done, 0)
		}
		return nil
	})
}

// CopyDirFromRemote recursively copies a remote directory to a local path.
func (c *SSHClient) CopyDirFromRemote(remoteDir, localDir string) error {
	return c.copyDirFromRemote(remoteDir, localDir, nil, nil)
}

// CopyDirFromRemoteProgress is like CopyDirFromRemote but calls onFile after each copy.
func (c *SSHClient) CopyDirFromRemoteProgress(remoteDir, localDir string, onFile func(path string, done, total int), ctx context.Context) error {
	return c.copyDirFromRemote(remoteDir, localDir, onFile, ctx)
}

func (c *SSHClient) copyDirFromRemote(remoteDir, localDir string, onFile func(path string, done, total int), ctx context.Context) error {
	done := 0
	return c.Walk(remoteDir, func(remotePath string, info os.FileInfo, err error) error {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(remoteDir, remotePath)
		localPath := filepath.Join(localDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(localPath, 0o755)
		}
		if isSymlinkToDir(c, remotePath, info) {
			// Copying/dereferencing a symlinked directory isn't supported;
			// skip it instead of failing the whole transfer trying to Open()
			// what the SFTP server sees as a directory.
			return nil
		}
		if err := c.CopyFileFromRemote(remotePath, localPath); err != nil {
			return err
		}
		done++
		if onFile != nil {
			onFile(localPath, done, 0)
		}
		return nil
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
	if srcClient.SameHostAs(dstClient) && path.Clean(srcPath) == path.Clean(dstPath) {
		// Same file on the same host: dstClient.Create() below would
		// truncate it before srcFile has finished being read, corrupting
		// the "copy" into an empty file.
		return fmt.Errorf("source and target are the same: %s", dstPath)
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
func CopyDirBetweenRemotes(srcClient, dstClient *SSHClient, srcDir, dstDir string) error {
	return copyDirBetweenRemotes(srcClient, dstClient, srcDir, dstDir, nil, nil)
}

func copyDirBetweenRemotes(srcClient, dstClient *SSHClient, srcDir, dstDir string, onFile func(path string, done, total int), ctx context.Context) error {
	if srcClient.SameHostAs(dstClient) && remotePathWithin(dstDir, srcDir) {
		// Same host, and the destination is srcDir itself or nested inside
		// it: MkdirAll below would create the destination inside the very
		// tree Walk is reading, so the walk would recurse into its own
		// output and never terminate on its own.
		return fmt.Errorf("cannot copy %q into itself", srcDir)
	}
	done := 0
	return srcClient.Walk(srcDir, func(remotePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		relPath, _ := filepath.Rel(srcDir, remotePath)
		dstPath := path.Join(dstDir, relPath)
		if info.IsDir() {
			return dstClient.MkdirAll(dstPath)
		}
		if isSymlinkToDir(srcClient, remotePath, info) {
			// See copyDirFromRemote: a symlink-to-directory can't be opened
			// as a regular file, so skip it instead of aborting the transfer.
			return nil
		}
		if err := CopyFileBetweenRemotes(srcClient, dstClient, remotePath, dstPath); err != nil {
			return err
		}
		done++
		if onFile != nil {
			onFile(remotePath, done, 0)
		}
		return nil
	})
}

// CopyDirBetweenRemotesProgress is like CopyDirBetweenRemotes with progress and context support.
func CopyDirBetweenRemotesProgress(srcClient, dstClient *SSHClient, srcDir, dstDir string, onFile func(path string, done, total int), ctx context.Context) error {
	return copyDirBetweenRemotes(srcClient, dstClient, srcDir, dstDir, onFile, ctx)
}

// Walk walks the remote filesystem tree rooted at root, calling walkFn for each file/dir.
// This is a simplified version of filepath.Walk for SFTP.
type walkFunc func(path string, info os.FileInfo, err error) error

func (c *SSHClient) Walk(root string, walkFn walkFunc) error {
	return c.walk(root, walkFn, nil)
}

func (c *SSHClient) walk(dirPath string, walkFn walkFunc, info os.FileInfo) error {
	if info == nil {
		var statErr error
		info, statErr = c.sftpCli.Stat(dirPath)
		if statErr != nil {
			if fnErr := walkFn(dirPath, nil, statErr); fnErr != nil && fnErr != filepathSkipDir {
				return fnErr
			}
			return nil
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

	entries, readErr := c.sftpCli.ReadDir(dirPath)
	if readErr != nil {
		// Report the error but allow walkFn to skip this directory
		// (returns nil, or filepathSkipDir → continue) or abort (returns
		// another error → stop). Either way this directory's subtree is done.
		if fnErr := walkFn(dirPath, info, readErr); fnErr != nil && fnErr != filepathSkipDir {
			return fnErr
		}
		return nil
	}

	// Note: entries for symlinks (including symlinks to directories) come
	// back from ReadDir with IsDir()==false, since SFTP reports directory
	// entries the way lstat would, without following links. They're handled
	// as leaves via walkFn below rather than recursed into, which also means
	// cyclic symlinks can't cause unbounded recursion here.
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

// remotePathWithin reports whether child is parent itself or a path nested
// inside it, using POSIX path semantics (remote paths are always "/"
// separated regardless of the local OS).
func remotePathWithin(child, parent string) bool {
	childClean := path.Clean(child)
	parentClean := path.Clean(parent)
	if childClean == parentClean {
		return true
	}
	return strings.HasPrefix(childClean, parentClean+"/")
}

// isSymlinkToDir reports whether the walk entry at remotePath is a symlink
// whose target is a directory. Such entries can't be copied with a plain
// Open()/Create() file copy (the server rejects opening a directory for
// reading), so callers use this to skip them instead of failing outright.
func isSymlinkToDir(c *SSHClient, remotePath string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := c.Stat(remotePath)
	return err == nil && target.IsDir()
}

// DirectorySize recursively walks a remote directory and sums up file sizes.
// Unreadable subdirectories are skipped gracefully instead of aborting the entire walk.
func (c *SSHClient) DirectorySize(dirPath string) (int64, error) {
	var total int64
	err := c.Walk(dirPath, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip unreadable directories instead of aborting the walk.
			// This handles permission-denied and other transient SFTP errors.
			return filepathSkipDir
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

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
