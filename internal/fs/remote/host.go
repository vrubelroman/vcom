package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SSHHost represents a single SSH host configuration.
type SSHHost struct {
	// Name is the host alias (e.g. "myserver").
	Name string `json:"name"`
	// HostName is the actual hostname or IP address.
	HostName string `json:"hostname"`
	// Port is the SSH port (default 22).
	Port string `json:"port,omitempty"`
	// User is the SSH username.
	User string `json:"user,omitempty"`
	// IdentityFile is the path to the private key file (for key-based auth).
	IdentityFile string `json:"identity_file,omitempty"`
	// Password is stored encrypted (for password-based auth, user-added hosts).
	Password string `json:"password,omitempty"`
	// FromSSHConfig indicates this host came from ~/.ssh/config.
	FromSSHConfig bool `json:"from_ssh_config"`
}

// DisplayName returns the host display name.
func (h SSHHost) DisplayName() string {
	addr := h.HostName
	if h.Port != "" && h.Port != "22" {
		addr = fmt.Sprintf("%s:%s", addr, h.Port)
	}
	if h.User != "" {
		return fmt.Sprintf("%s (%s@%s)", h.Name, h.User, addr)
	}
	return fmt.Sprintf("%s (%s)", h.Name, addr)
}

// Addr returns the SSH address string (host:port).
func (h SSHHost) Addr() string {
	if h.Port == "" || h.Port == "22" {
		return h.HostName + ":22"
	}
	return h.HostName + ":" + h.Port
}

// SameAs returns true if two hosts point to the same server.
func (h SSHHost) SameAs(other SSHHost) bool {
	return h.HostName == other.HostName &&
		(h.Port == other.Port || (h.Port == "" && other.Port == "22") || (h.Port == "22" && other.Port == ""))
}

// HostStore manages SSH hosts from both ~/.ssh/config and user-added hosts.
type HostStore struct {
	customHosts []SSHHost
	configPath  string
	cipherKey   []byte
}

// NewHostStore creates a new HostStore.
func NewHostStore() (*HostStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}

	store := &HostStore{
		configPath: filepath.Join(home, ".config", "vcom", "hosts.dat"),
	}

	// Load or create encryption key
	keyPath := filepath.Join(home, ".config", "vcom", ".hosts-key")
	store.cipherKey, err = loadOrCreateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("encryption key: %w", err)
	}

	// Load custom hosts
	if err := store.load(); err != nil {
		// Ignore load errors for missing file
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	return store, nil
}

// loadOrCreateKey loads an existing AES key or creates a new one.
func loadOrCreateKey(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, err
		}
		if len(key) == 32 {
			return key, nil
		}
	}

	// Generate new 32-byte key for AES-256
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}

	return key, nil
}

type storedHosts struct {
	Hosts []storedHost `json:"hosts"`
}

type storedHost struct {
	Name         string `json:"name"`
	HostName     string `json:"hostname"`
	Port         string `json:"port,omitempty"`
	User         string `json:"user,omitempty"`
	Password     string `json:"password,omitempty"` // encrypted
	IdentityFile string `json:"identity_file,omitempty"`
}

func (s *HostStore) load() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	// Decrypt
	decrypted, err := decrypt(data, s.cipherKey)
	if err != nil {
		return fmt.Errorf("decrypt hosts: %w", err)
	}

	var stored storedHosts
	if err := json.Unmarshal(decrypted, &stored); err != nil {
		return fmt.Errorf("parse hosts: %w", err)
	}

	s.customHosts = make([]SSHHost, len(stored.Hosts))
	for i, h := range stored.Hosts {
		password := ""
		if h.Password != "" {
			pwd, err := decrypt([]byte(h.Password), s.cipherKey)
			if err == nil {
				password = string(pwd)
			}
		}
		s.customHosts[i] = SSHHost{
			Name:          h.Name,
			HostName:      h.HostName,
			Port:          h.Port,
			User:          h.User,
			Password:      password,
			IdentityFile:  h.IdentityFile,
			FromSSHConfig: false,
		}
	}

	return nil
}

// Save persists custom hosts to disk (encrypted).
func (s *HostStore) Save() error {
	stored := storedHosts{
		Hosts: make([]storedHost, len(s.customHosts)),
	}
	for i, h := range s.customHosts {
		password := ""
		if h.Password != "" {
			enc, err := encrypt([]byte(h.Password), s.cipherKey)
			if err == nil {
				password = string(enc)
			}
		}
		stored.Hosts[i] = storedHost{
			Name:         h.Name,
			HostName:     h.HostName,
			Port:         h.Port,
			User:         h.User,
			Password:     password,
			IdentityFile: h.IdentityFile,
		}
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("marshal hosts: %w", err)
	}

	encrypted, err := encrypt(data, s.cipherKey)
	if err != nil {
		return fmt.Errorf("encrypt hosts: %w", err)
	}

	dir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	return os.WriteFile(s.configPath, encrypted, 0o600)
}

// AddHost adds a custom host and saves.
func (s *HostStore) AddHost(host SSHHost) error {
	host.FromSSHConfig = false
	s.customHosts = append(s.customHosts, host)
	return s.Save()
}

// RemoveHost removes a custom host by name.
func (s *HostStore) RemoveHost(name string) error {
	for i, h := range s.customHosts {
		if h.Name == name {
			s.customHosts = append(s.customHosts[:i], s.customHosts[i+1:]...)
			return s.Save()
		}
	}
	return fmt.Errorf("host %q not found", name)
}

// AllHosts returns all hosts (from ssh config + custom).
func (s *HostStore) AllHosts() []SSHHost {
	sshConfigHosts := ParseSSHConfig()
	result := make([]SSHHost, 0, len(sshConfigHosts)+len(s.customHosts))

	// Build a set of names from ssh config to avoid duplicates
	seen := make(map[string]bool)
	for _, h := range sshConfigHosts {
		lower := strings.ToLower(h.Name)
		seen[lower] = true
		result = append(result, h)
	}

	for _, h := range s.customHosts {
		lower := strings.ToLower(h.Name)
		if !seen[lower] {
			result = append(result, h)
			seen[lower] = true
		}
	}

	return result
}

// FindByName looks up a host by its Name field. Returns nil if not found.
func (s *HostStore) FindByName(name string) *SSHHost {
	all := s.AllHosts()
	for i := range all {
		if strings.EqualFold(all[i].Name, name) {
			return &all[i]
		}
	}
	return nil
}

func encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
