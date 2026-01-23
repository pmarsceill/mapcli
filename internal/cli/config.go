package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// configCmd is the parent command for config management
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration settings",
	Long:  `View and modify MAP configuration settings stored in ~/.mapd/config.yaml.`,
}

var configListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all configuration values",
	Long:    `Display all configuration values including defaults and overrides.`,
	RunE:    runConfigList,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Long: `Get a specific configuration value by key.

Examples:
  map config get socket
  map config get agent.default-type
  map config get agent.default-count`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value and persist it to ~/.mapd/config.yaml.

Examples:
  map config set socket /custom/path.sock
  map config set agent.default-type codex
  map config set agent.default-count 3
  map config set agent.use-worktree false`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}

// initConfig reads in config file and ENV variables if set
func initConfig() error {
	// Set defaults
	viper.SetDefault("socket", "/tmp/mapd.sock")
	viper.SetDefault("data-dir", filepath.Join(os.Getenv("HOME"), ".mapd"))
	viper.SetDefault("agent.default-type", "claude")
	viper.SetDefault("agent.default-count", 1)
	viper.SetDefault("agent.default-branch", "")
	viper.SetDefault("agent.use-worktree", true)
	viper.SetDefault("agent.skip-permissions", true)

	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in ~/.mapd directory
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home directory: %w", err)
		}

		configDir := filepath.Join(home, ".mapd")
		viper.AddConfigPath(configDir)
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	// Environment variables with MAP_ prefix
	viper.SetEnvPrefix("MAP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	// Read config file (ignore if not found)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("read config: %w", err)
		}
	}

	// Bind the socket flag to viper
	if err := viper.BindPFlag("socket", rootCmd.PersistentFlags().Lookup("socket")); err != nil {
		return fmt.Errorf("bind socket flag: %w", err)
	}

	return nil
}

// writeConfig writes the current configuration to the config file
func writeConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".mapd")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if err := viper.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func runConfigList(cmd *cobra.Command, args []string) error {
	keys := viper.AllKeys()
	sort.Strings(keys)

	fmt.Printf("%-25s %s\n", "KEY", "VALUE")
	fmt.Println(strings.Repeat("-", 50))

	for _, key := range keys {
		value := viper.Get(key)
		fmt.Printf("%-25s %v\n", key, value)
	}

	// Show config file location if it exists
	if viper.ConfigFileUsed() != "" {
		fmt.Printf("\nConfig file: %s\n", viper.ConfigFileUsed())
	}

	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	if !viper.IsSet(key) {
		return fmt.Errorf("key %q not found", key)
	}

	fmt.Println(viper.Get(key))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	// Handle boolean values
	switch strings.ToLower(value) {
	case "true":
		viper.Set(key, true)
	case "false":
		viper.Set(key, false)
	default:
		// Try to parse as integer
		var intVal int
		if _, err := fmt.Sscanf(value, "%d", &intVal); err == nil {
			viper.Set(key, intVal)
		} else {
			viper.Set(key, value)
		}
	}

	if err := writeConfig(); err != nil {
		return err
	}

	fmt.Printf("set %s = %v\n", key, viper.Get(key))
	return nil
}
