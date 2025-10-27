package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var v *viper.Viper

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
	v.SetDefault("json-output", false)
	v.SetDefault("no-daemon", false)
	v.SetDefault("no-auto-flush", false)
	v.SetDefault("no-auto-import", false)
	v.SetDefault("no-db", false)
	v.SetDefault("no-json", false) // Default to false: maintain JSONL sync for backward compatibility
	v.SetDefault("db", "")
	v.SetDefault("actor", "")
	v.SetDefault("backend", "sqlite")
	v.SetDefault("issue-prefix", "")
	
	// Additional environment variables (not prefixed with BD_)
	// These are bound explicitly for backward compatibility
	_ = v.BindEnv("flush-debounce", "BEADS_FLUSH_DEBOUNCE")
	_ = v.BindEnv("auto-start-daemon", "BEADS_AUTO_START_DAEMON")
	
	// Set defaults for additional settings
	v.SetDefault("flush-debounce", "5s")
	v.SetDefault("auto-start-daemon", true)

	// Read config file if it exists (don't error if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file found but another error occurred
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found - this is ok, we'll use defaults
	}

	// Backwards compatibility: migrate old config keys to new kebab-case keys
	// TODO: Remove this migration code in a future version (v2.0?)
	migrateOldConfigKeys()

	return nil
}

// migrateOldConfigKeys provides backwards compatibility for old config key names
// TODO: Remove this in a future major version
func migrateOldConfigKeys() {
	if v == nil {
		return
	}

	// Migrate "json" -> "json-output"
	if v.IsSet("json") && !v.IsSet("json-output") {
		v.Set("json-output", v.Get("json"))
	}

	// Migrate "issue_prefix" -> "issue-prefix" (underscore to hyphen)
	if v.IsSet("issue_prefix") && !v.IsSet("issue-prefix") {
		v.Set("issue-prefix", v.Get("issue_prefix"))
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

// FileUsed returns the path to the active configuration file.
func FileUsed() string {
	if v == nil {
		return ""
	}
	return v.ConfigFileUsed()
}

// AllSettings returns all configuration settings as a map
func AllSettings() map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v.AllSettings()
}

// IsSet checks if a configuration key is set (either in config file, env var, or flag)
func IsSet(key string) bool {
	if v == nil {
		return false
	}
	return v.IsSet(key)
}

// WriteConfig writes only explicitly set configuration values to the config file
// This prevents polluting the config file with all defaults
func WriteConfig() error {
	if v == nil {
		return fmt.Errorf("viper not initialized")
	}

	// Find the config file path
	configPath := v.ConfigFileUsed()
	if configPath == "" {
		// Config file doesn't exist yet, find .beads directory
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Walk up to find .beads directory
		for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			beadsDir := filepath.Join(dir, ".beads")
			if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
				configPath = filepath.Join(beadsDir, "config.yaml")
				break
			}
		}

		if configPath == "" {
			return fmt.Errorf("no .beads directory found")
		}
	}

	// Read existing config to preserve manually set values
	existingConfig := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(data, &existingConfig)
	}

	// Only write values that differ from defaults or were explicitly set
	// List of keys that should be persisted to config file
	persistKeys := []string{"backend", "issue-prefix", "no-db", "actor"}

	for _, key := range persistKeys {
		if v.IsSet(key) {
			val := v.Get(key)
			// Only write if value is different from default or already in config
			if _, exists := existingConfig[key]; exists || !isDefaultValue(key, val) {
				existingConfig[key] = val
			}
		}
	}

	// Marshal and write
	data, err := yaml.Marshal(existingConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(configPath, data, 0644)
}

// isDefaultValue checks if a value is the default for a given key
func isDefaultValue(key string, val interface{}) bool {
	defaults := map[string]interface{}{
		"json-output":        false,
		"no-daemon":          false,
		"no-auto-flush":      false,
		"no-auto-import":     false,
		"no-db":              false,
		"no-json":            false,
		"db":                 "",
		"actor":              "",
		"backend":            "sqlite",
		"issue-prefix":       "",
		"flush-debounce":     "5s",
		"auto-start-daemon":  true,
	}

	defaultVal, exists := defaults[key]
	if !exists {
		return false
	}

	return val == defaultVal
}
