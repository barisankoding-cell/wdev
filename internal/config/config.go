package config

import (
	"os"
	"os/user"
	"path/filepath"
)

type Config struct {
	WdevEtc          string
	DBPath           string
	ScriptDir        string
	NginxSitesAvail  string
	NginxSitesEnable string
	HostsFile        string
	HTTPPort         string
	HTTPHost         string
}

func Load() *Config {
	wdevEtc := os.Getenv("WDEV_ETC")
	if wdevEtc == "" {
		if _, err := os.Stat("/etc/wdev"); err == nil {
			wdevEtc = "/etc/wdev"
		} else if os.Geteuid() == 0 {
			wdevEtc = "/etc/wdev"
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			wdevEtc = filepath.Join(home, ".config", "wdev")
		}
	}

	dbPath := os.Getenv("WDEV_DB")
	if dbPath == "" {
		// Test if wdevEtc is writable
		testFile := filepath.Join(wdevEtc, ".write_test")
		if err := os.WriteFile(testFile, []byte(""), 0644); err == nil {
			_ = os.Remove(testFile)
			dbPath = filepath.Join(wdevEtc, "wdev.db")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			userEtc := filepath.Join(home, ".config", "wdev")
			_ = os.MkdirAll(userEtc, 0755)
			dbPath = filepath.Join(userEtc, "wdev.db")
		}
	}

	scriptDir := os.Getenv("WDEV_SCRIPT_DIR")
	if scriptDir == "" {
		scriptDir = "/var/lib/wdev"
	}

	port := os.Getenv("WDEV_PORT")
	if port == "" {
		port = "3200"
	}

	host := os.Getenv("WDEV_HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	return &Config{
		WdevEtc:          wdevEtc,
		DBPath:           dbPath,
		ScriptDir:        scriptDir,
		NginxSitesAvail:  "/etc/nginx/sites-available",
		NginxSitesEnable: "/etc/nginx/sites-enabled",
		HostsFile:        "/etc/hosts",
		HTTPPort:         port,
		HTTPHost:         host,
	}
}

// IsRoot checks if running as root
func IsRoot() bool {
	return os.Geteuid() == 0
}

// CurrentUsername returns the non-root or sudo username
func CurrentUsername() string {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		return sudoUser
	}
	u, err := user.Current()
	if err == nil {
		return u.Username
	}
	return "root"
}
