package config

import (
	"os"
	"path/filepath"
	"runtime"
)

type HostPaths struct {
	ConfigDir       string
	StateDir        string
	LogDir          string
	RunDir          string
	CredentialsFile string
	HostConfigFile  string
}

func resolveHomeDir() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return home
	}

	home = os.Getenv("HOME")
	if home != "" {
		return home
	}

	// Keep all generated paths absolute if HOME cannot be resolved.
	return os.TempDir()
}

func DefaultHostPaths() HostPaths {
	homeDir := resolveHomeDir()

	if runtime.GOOS == "darwin" {
		base := filepath.Join(homeDir, "Library", "Application Support", "Termix")
		return HostPaths{
			ConfigDir:       base,
			StateDir:        filepath.Join(base, "state"),
			LogDir:          filepath.Join(homeDir, "Library", "Logs", "Termix"),
			RunDir:          filepath.Join(base, "run"),
			CredentialsFile: filepath.Join(base, "credentials.json"),
			HostConfigFile:  filepath.Join(base, "host.json"),
		}
	}

	configDir := filepath.Join(homeDir, ".config", "termix")
	stateDir := filepath.Join(homeDir, ".local", "state", "termix")
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = filepath.Join(homeDir, ".termix", "run")
	} else {
		runDir = filepath.Join(runDir, "termix")
	}

	return HostPaths{
		ConfigDir:       configDir,
		StateDir:        stateDir,
		LogDir:          filepath.Join(stateDir, "logs"),
		RunDir:          runDir,
		CredentialsFile: filepath.Join(configDir, "credentials.json"),
		HostConfigFile:  filepath.Join(configDir, "host.json"),
	}
}
