package config

import (
	"os"
	"os/user"
	"path/filepath"
)

type Config struct {
	WdevEtc          string
	DBPath           string
	NginxSitesAvail  string
	NginxSitesEnable string
	WebRootBase      string
	HostsFile        string
	HTTPPort         string
	HTTPHost         string
	TLSCert          string
	TLSKey           string
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
		testFile := filepath.Join(wdevEtc, ".write_test")
		if err := os.WriteFile(testFile, []byte(""), 0600); err == nil {
			_ = os.Remove(testFile)
			dbPath = filepath.Join(wdevEtc, "wdev.db")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			userEtc := filepath.Join(home, ".config", "wdev")
			_ = os.MkdirAll(userEtc, 0700)
			dbPath = filepath.Join(userEtc, "wdev.db")
		}
	}

	port := os.Getenv("WDEV_PORT")
	if port == "" {
		port = "3200"
	}

	host := os.Getenv("WDEV_HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	wwwRoot := os.Getenv("WDEV_WWW_ROOT")
	if wwwRoot == "" {
		wwwRoot = "/var/www"
	}

	return &Config{
		WdevEtc:          wdevEtc,
		DBPath:           dbPath,
		NginxSitesAvail:  "/etc/nginx/sites-available",
		NginxSitesEnable: "/etc/nginx/sites-enabled",
		WebRootBase:      wwwRoot,
		HostsFile:        "/etc/hosts",
		HTTPPort:         port,
		HTTPHost:         host,
		TLSCert:          os.Getenv("WDEV_TLS_CERT"),
		TLSKey:           os.Getenv("WDEV_TLS_KEY"),
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
