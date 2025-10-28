package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var v *viper.Viper

// supportedKeys lists all valid configuration keys
// This is used to validate config.yaml and warn about unsupported keys
var supportedKeys = map[string]bool{
	"json":              true,
	"no-daemon":         true,
	"no-auto-flush":     true,
	"no-auto-import":    true,
	"db":                true,
	"actor":             true,
	"issue-prefix":      true,
	"flush-debounce":    true,
	"auto-start-daemon": true,
}

// Initialize sets up the viper configuration singleton
// Should be called once at application startup
func Initialize() error {
	v = viper.New()

	// Set config file name and type
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Add config search paths (in order of precedence)
	// 1. Walk up from CWD to find project .beads/ directory
	//    This allows commands to work from subdirectories
	cwd, err := os.Getwd()
	if err == nil {
		// Walk up parent directories to find .beads/config.yaml
		for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			beadsDir := filepath.Join(dir, ".beads")
			configPath := filepath.Join(beadsDir, "config.yaml")
			if _, err := os.Stat(configPath); err == nil {
				// Found .beads/config.yaml - add this path
				v.AddConfigPath(beadsDir)
				break
			}
			// Also check if .beads directory exists (even without config.yaml)
			if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
				v.AddConfigPath(beadsDir)
				break
			}
		}
		
		// Also add CWD/.beads for backward compatibility
		v.AddConfigPath(filepath.Join(cwd, ".beads"))
	}

	// 2. User config directory (~/.config/bd/)
	if configDir, err := os.UserConfigDir(); err == nil {
		v.AddConfigPath(filepath.Join(configDir, "bd"))
	}

	// 3. Home directory (~/.beads/)
	if homeDir, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(homeDir, ".beads"))
	}

	// Automatic environment variable binding
	// Environment variables take precedence over config file
	// E.g., BD_JSON, BD_NO_DAEMON, BD_ACTOR, BD_DB
	v.SetEnvPrefix("BD")
	
	// Replace hyphens and dots with underscores for env var mapping
	// This allows BD_NO_DAEMON to map to "no-daemon" config key
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// Set defaults for all flags
	v.SetDefault("json", false)
	v.SetDefault("no-daemon", false)
	v.SetDefault("no-auto-flush", false)
	v.SetDefault("no-auto-import", false)
	v.SetDefault("no-db", false)
	v.SetDefault("db", "")
	v.SetDefault("actor", "")
	v.SetDefault("issue-prefix", "")
	
	// Additional environment variables (not prefixed with BD_)
	// These are bound explicitly for backward compatibility
	_ = v.BindEnv("flush-debounce", "BEADS_FLUSH_DEBOUNCE")
	_ = v.BindEnv("auto-start-daemon", "BEADS_AUTO_START_DAEMON")
	
	// Set defaults for additional settings
	v.SetDefault("flush-debounce", "30s")
	v.SetDefault("auto-start-daemon", true)

	// Read config file if it exists (don't error if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file found but another error occurred
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found - create it if .beads directory exists
		if err := createDefaultConfigIfNeeded(); err != nil {
			log.Printf("Warning: failed to create default config.yaml: %v\n", err)
		}
	} else {
		// Config file was found and read successfully - validate it
		validateConfig()
	}

	return nil
}

// defaultConfigTemplate contains the default config.yaml with helpful comments
const defaultConfigTemplate = `# Beads (bd) Configuration File
# This file controls settings for your beads issue tracking project.
#
# Configuration precedence (highest to lowest):
# 1. Command-line flags
# 2. Environment variables (BD_* prefix)
# 3. This config file
# 4. Built-in defaults

# Issue Prefix (REQUIRED)
# The prefix used for all issue IDs in this project (e.g., "bd-123")
# Set this during 'bd init', or manually edit it here.
# Once set, use 'bd rename-prefix' to change it.
issue-prefix: "issue"

# Output Format
# Set to true to output JSON instead of human-readable text
# Can be overridden with --json flag or BD_JSON env var
json: false

# Daemon Mode
# Set to true to disable background daemon for auto-export
# Can be overridden with --no-daemon flag or BD_NO_DAEMON env var
no-daemon: false

# Auto-flush
# Set to true to disable automatic flushing of changes
# Can be overridden with --no-auto-flush flag or BD_NO_AUTO_FLUSH env var
no-auto-flush: false

# Auto-import
# Set to true to disable automatic import on startup
# Can be overridden with --no-auto-import flag or BD_NO_AUTO_IMPORT env var
no-auto-import: false

# Flush Debounce
# How long to wait before flushing changes (prevents too-frequent writes)
# Can be overridden with BEADS_FLUSH_DEBOUNCE env var
flush-debounce: "30s"

# Auto-start Daemon
# Whether to automatically start the daemon if not running
# Can be overridden with BEADS_AUTO_START_DAEMON env var
auto-start-daemon: true

# Database Path (optional)
# Override the default database location (.beads/beads.db)
# Can be overridden with --db flag or BD_DB env var
# Leave empty to use default location
db: ""

# Actor (optional)
# Default username for issue operations
# Can be overridden with --actor flag or BD_ACTOR env var
# If empty, uses git config user.name or system username
actor: ""

# For more configuration options and integration examples, see:
# https://github.com/steveyegge/beads
`

// createDefaultConfigIfNeeded creates a default config.yaml in the .beads directory if one doesn't exist
func createDefaultConfigIfNeeded() error {
	// Find .beads directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	var beadsDir string
	for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, ".beads")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			beadsDir = candidate
			break
		}
	}

	if beadsDir == "" {
		// No .beads directory found - don't create config
		return nil
	}

	configPath := filepath.Join(beadsDir, "config.yaml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		// Config exists, don't overwrite
		return nil
	}

	// Create config file with default template
	if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	log.Printf("Created default configuration file: %s\n", configPath)
	return nil
}

// validateConfig checks for unsupported keys in the config file
// and issues warnings to help users identify typos or obsolete settings
func validateConfig() {
	if v == nil {
		return
	}

	configFile := v.ConfigFileUsed()
	if configFile == "" {
		// No config file was loaded
		return
	}

	// Get all keys from the config file
	allSettings := v.AllSettings()

	// Check each key against the supported list
	for key := range allSettings {
		if !supportedKeys[key] {
			log.Printf("Warning: unsupported configuration key '%s' in %s - this setting will be ignored\n", key, configFile)
		}
	}
}

// GetString retrieves a string configuration value
func GetString(key string) string {
	if v == nil {
		return ""
	}
	return v.GetString(key)
}

// GetBool retrieves a boolean configuration value
func GetBool(key string) bool {
	if v == nil {
		return false
	}
	return v.GetBool(key)
}

// GetInt retrieves an integer configuration value
func GetInt(key string) int {
	if v == nil {
		return 0
	}
	return v.GetInt(key)
}

// GetDuration retrieves a duration configuration value
func GetDuration(key string) time.Duration {
	if v == nil {
		return 0
	}
	return v.GetDuration(key)
}

// Set sets a configuration value
func Set(key string, value interface{}) {
	if v != nil {
		v.Set(key, value)
	}
}

// BindPFlag is reserved for future use if we want to bind Cobra flags directly to Viper
// For now, we handle flag precedence manually in PersistentPreRun
// Uncomment and implement if needed:
//
// func BindPFlag(key string, flag *pflag.Flag) error {
// 	if v == nil {
// 		return fmt.Errorf("viper not initialized")
// 	}
// 	return v.BindPFlag(key, flag)
// }

// AllSettings returns all configuration settings as a map
func AllSettings() map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v.AllSettings()
}

// SetIssuePrefix updates the issue-prefix in config.yaml
// This is the source of truth for the project's issue prefix
// In test environments without .beads directory, updates viper in-memory only
func SetIssuePrefix(prefix string) error {
	// Find the .beads directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	var beadsDir string
	for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, ".beads")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			beadsDir = candidate
			break
		}
	}

	if beadsDir == "" {
		// No .beads directory found - just update viper in-memory (for tests)
		if v != nil {
			v.Set("issue-prefix", prefix)
			return nil
		}
		return fmt.Errorf("no .beads directory found and viper not initialized")
	}

	configPath := filepath.Join(beadsDir, "config.yaml")

	// Read existing config or use empty map
	var configData map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, &configData); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	} else {
		configData = make(map[string]interface{})
	}

	// Update issue-prefix
	configData["issue-prefix"] = prefix

	// Write back to file
	data, err := yaml.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Update in-memory viper configuration
	if v != nil {
		v.Set("issue-prefix", prefix)
	}

	return nil
}

// GetIssuePrefix returns the issue-prefix from config.yaml
// This is the canonical source of truth for the project's issue prefix
func GetIssuePrefix() string {
	return GetString("issue-prefix")
}
