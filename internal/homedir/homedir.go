// Package homedir resolves the real user's home directory, including when
// the process is running under sudo.
package homedir

import (
	"os"
	"os/user"
)

// Dir returns the real user's home directory. os.UserHomeDir() resolves to
// /root when the process is running as root via sudo, which points config,
// SSH, and trash paths at the wrong place. When the effective UID is 0 and
// SUDO_USER is set, it resolves the invoking user's home instead.
func Dir() string {
	if os.Geteuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if u, err := user.Lookup(sudoUser); err == nil {
				return u.HomeDir
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
