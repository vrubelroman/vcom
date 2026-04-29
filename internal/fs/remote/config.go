package remote

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseSSHConfig parses ~/.ssh/config and returns a list of SSH hosts.
// It handles the most common SSH config directives: Host, HostName, Port, User, IdentityFile.
func ParseSSHConfig() []SSHHost {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	configPath := filepath.Join(home, ".ssh", "config")
	return parseSSHConfigFile(configPath)
}

func parseSSHConfigFile(path string) []SSHHost {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var hosts []SSHHost
	var current *SSHHost
	var currentNames []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Remove inline comments (everything after # that's not in quotes)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		keyword := strings.ToLower(parts[0])
		value := strings.Join(parts[1:], " ")

		switch keyword {
		case "host":
			// Save previous host block
			if current != nil && len(currentNames) > 0 {
				for _, name := range currentNames {
					if !isWildcardPattern(name) {
						host := *current
						host.Name = name
						hosts = append(hosts, host)
					}
				}
			}

			// Start new host block
			current = &SSHHost{
				Port:          "22",
				FromSSHConfig: true,
			}
			currentNames = strings.Fields(value)

		case "hostname":
			if current != nil {
				current.HostName = value
			}

		case "port":
			if current != nil {
				current.Port = value
			}

		case "user":
			if current != nil {
				current.User = value
			}

		case "identityfile":
			if current != nil {
				// Handle ~ expansion and relative paths
				resolved := resolveIdentityPath(value)
				if resolved != "" {
					current.IdentityFile = resolved
				}
			}
		}
	}

	// Save last host block
	if current != nil && len(currentNames) > 0 {
		for _, name := range currentNames {
			if !isWildcardPattern(name) {
				host := *current
				host.Name = name
				hosts = append(hosts, host)
			}
		}
	}

	return hosts
}

// isWildcardPattern returns true if the pattern contains wildcard characters.
func isWildcardPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?")
}

// resolveIdentityPath resolves a path from SSH config (handles ~ and relative paths).
func resolveIdentityPath(path string) string {
	if path == "" {
		return ""
	}

	// Handle ~/ or $HOME/
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		path = filepath.Join(home, path[2:])
	}

	// Handle relative paths (relative to ~/.ssh/)
	if !filepath.IsAbs(path) {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		path = filepath.Join(home, ".ssh", path)
	}

	return filepath.Clean(path)
}

// SSHConfigPath returns the path to the user's SSH config file.
func SSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// HostsFilePath returns the path to the custom hosts data file.
func HostsFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "vcom", "hosts.dat")
}

// GetSSHDir returns the path to the .ssh directory.
func GetSSHDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh")
}

// KnownHostsPath returns the path to known_hosts.
func KnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

// ConfigFileExists checks if the SSH config file exists.
func ConfigFileExists() bool {
	path := SSHConfigPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// ValidateHost checks if a host entry has the minimum required fields.
func ValidateHost(host SSHHost) error {
	if strings.TrimSpace(host.Name) == "" {
		return fmt.Errorf("host name is required")
	}
	if strings.TrimSpace(host.HostName) == "" {
		return fmt.Errorf("hostname/address is required")
	}
	if strings.TrimSpace(host.User) == "" {
		return fmt.Errorf("username is required")
	}
	return nil
}
