/*
MIT License

Copyright (c) 2025 Mikael Schultz <mikael@conf-t.se>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Version: "v1.0.3",
	Use:     "ircpush",
	Short:   "Forward and colorize text messages to IRC channels",
	Long: `Forward and colorize text messages to IRC channels.

This tool listens for incoming text messages via a TCP listener and forwards
them to specified IRC channels, after applying regex powered syntax 
highlighting rules. It is configurable via a YAML configuration file.

Author: Mikael Schultz <mikael@conf-t.se>
GitHub: https://github.com/bitcanon/ircpush`,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// Set default config file path for the flag help text
	var defaultCfgPath string
	if runtime.GOOS == "windows" {
		defaultCfgPath = "C:\\ProgramData\\ircpush\\config.yaml"
	} else {
		// Show the search order in help text
		defaultCfgPath = "./config.yaml, ~/.ircpush, /etc/ircpush/config.yaml"
	}

	// Add flag for custom config file path
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", fmt.Sprintf("config file (search order: %s)", defaultCfgPath))

	// Add flag for debug mode
	rootCmd.PersistentFlags().Bool("debug", false, "show debug info")
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))

	// Set a custom version template
	rootCmd.SetVersionTemplate(`{{ printf "%s %s" .Name .Version }}`)
}

// initConfig reads in config file and ENV variables if set
func initConfig() {
	// Env overrides
	replacer := strings.NewReplacer("-", "_", ".", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.SetEnvPrefix("IRCPUSH")
	viper.AutomaticEnv()

	// 1) Explicit --config
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		if ext := filepath.Ext(cfgFile); ext == "" {
			// No extension; assume YAML
			viper.SetConfigType("yaml")
		}
		if err := viper.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config file %q: %v\n", cfgFile, err)
		} else {
			fmt.Fprintf(os.Stderr, "Using config file: %s\n", viper.ConfigFileUsed())
		}
		return
	}

	// Helper to try a specific file
	tryFile := func(path string, assumeYAML bool) bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		viper.SetConfigFile(path)
		if assumeYAML {
			viper.SetConfigType("yaml")
		}
		if err := viper.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config file %q: %v\n", path, err)
			return false
		}
		fmt.Fprintf(os.Stderr, "Using config file: %s\n", viper.ConfigFileUsed())
		return true
	}

	// 2) ./config.yaml next to the executable
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if tryFile(filepath.Join(exeDir, "config.yaml"), false) {
			return
		}
	}

	// 3) ~/.ircpush (file without extension, YAML content)
	if home, err := os.UserHomeDir(); err == nil {
		if tryFile(filepath.Join(home, ".ircpush"), true) {
			return
		}
	}

	// 4) /etc/ircpush/config.yaml
	_ = tryFile("/etc/ircpush/config.yaml", false)
}
