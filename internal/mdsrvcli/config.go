package mdsrvcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type cliConfig struct {
	Profiles map[string]cliProfile `json:"profiles" yaml:"profiles"`
}

type cliProfile struct {
	Store           string        `json:"store,omitempty" yaml:"store,omitempty"`
	Backend         string        `json:"backend,omitempty" yaml:"backend,omitempty"`
	GMXCommand      string        `json:"gmx_command,omitempty" yaml:"gmx_command,omitempty"`
	AuthToken       string        `json:"auth_token,omitempty" yaml:"auth_token,omitempty"`
	Cache           string        `json:"cache,omitempty" yaml:"cache,omitempty"`
	Timeout         time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	JobTTL          time.Duration `json:"job_ttl,omitempty" yaml:"job_ttl,omitempty"`
	JobPruneOnStart bool          `json:"job_prune_on_start,omitempty" yaml:"job_prune_on_start,omitempty"`
}

type configFlags struct {
	path            string
	profile         string
	store           string
	backend         string
	gmxCommand      string
	authToken       string
	cache           string
	timeout         time.Duration
	jobTTL          time.Duration
	jobPruneOnStart bool
	force           bool
	jsonReport      bool
}

func (a app) configCommand() *cobra.Command {
	flags := &configFlags{profile: "local", jobTTL: 7 * 24 * time.Hour}
	cmd := &cobra.Command{Use: "config", Short: "Manage MDsrv headless CLI profiles"}
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create or update a CLI profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath(flags.path)
			if err != nil {
				return err
			}
			cfg, err := loadConfigIfExists(path)
			if err != nil {
				return err
			}
			if cfg.Profiles == nil {
				cfg.Profiles = map[string]cliProfile{}
			}
			if _, exists := cfg.Profiles[flags.profile]; exists && !flags.force {
				return fmt.Errorf("profile %q already exists; pass --force to overwrite", flags.profile)
			}
			cfg.Profiles[flags.profile] = cliProfile{
				Store:           firstNonEmpty(flags.store, "./mdsrv-data"),
				Backend:         firstNonEmpty(flags.backend, "auto"),
				GMXCommand:      flags.gmxCommand,
				AuthToken:       flags.authToken,
				Cache:           flags.cache,
				Timeout:         flags.timeout,
				JobTTL:          flags.jobTTL,
				JobPruneOnStart: flags.jobPruneOnStart,
			}
			if err := writeConfig(path, cfg); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, map[string]any{"path": path, "profile": flags.profile, "config": cfg.Profiles[flags.profile]})
			}
			fmt.Fprintln(a.stdout, path)
			return nil
		},
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath(flags.path)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(path)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, cfg.Profiles)
			}
			for name, profile := range cfg.Profiles {
				fmt.Fprintf(a.stdout, "%s\tstore=%s\tbackend=%s\n", name, profile.Store, profile.Backend)
			}
			return nil
		},
	}
	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the active config file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath(flags.path)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, map[string]any{"path": path})
			}
			fmt.Fprintln(a.stdout, path)
			return nil
		},
	}
	for _, sub := range []*cobra.Command{initCmd, listCmd, pathCmd} {
		sub.Flags().StringVar(&flags.path, "config", "", "config file path; defaults to $XDG_CONFIG_HOME/hlmdsrv/config.yaml")
		sub.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	}
	initCmd.Flags().StringVar(&flags.profile, "profile", "local", "profile name")
	initCmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "default store root")
	initCmd.Flags().StringVar(&flags.backend, "backend", "auto", "default backend: auto, python, or gromacs")
	initCmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "default GROMACS command")
	initCmd.Flags().StringVar(&flags.authToken, "auth-token", "", "default server auth token")
	initCmd.Flags().StringVar(&flags.cache, "cache", "", "default download cache")
	initCmd.Flags().DurationVar(&flags.timeout, "timeout", 0, "default command timeout")
	initCmd.Flags().DurationVar(&flags.jobTTL, "job-ttl", 7*24*time.Hour, "default serve job TTL for startup pruning")
	initCmd.Flags().BoolVar(&flags.jobPruneOnStart, "job-prune-on-start", false, "default serve startup pruning behavior")
	initCmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing profile")
	cmd.AddCommand(initCmd, listCmd, pathCmd)
	return cmd
}

func configPath(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return filepath.Abs(override)
	}
	if value := strings.TrimSpace(os.Getenv("MDSRV_CONFIG")); value != "" {
		return filepath.Abs(value)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "hlmdsrv", "config.yaml"), nil
}

func loadConfig(path string) (cliConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cliConfig{}, err
	}
	var cfg cliConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cliConfig{}, err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]cliProfile{}
	}
	return cfg, nil
}

func loadConfigIfExists(path string) (cliConfig, error) {
	cfg, err := loadConfig(path)
	if err == nil {
		return cfg, nil
	}
	if os.IsNotExist(err) {
		return cliConfig{Profiles: map[string]cliProfile{}}, nil
	}
	return cliConfig{}, err
}

func writeConfig(path string, cfg cliConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadNamedProfile(name string, configFile string) (cliProfile, bool, error) {
	name = firstNonEmpty(name, os.Getenv("MDSRV_PROFILE"))
	if strings.TrimSpace(name) == "" {
		return cliProfile{}, false, nil
	}
	path, err := configPath(configFile)
	if err != nil {
		return cliProfile{}, false, err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return cliProfile{}, false, fmt.Errorf("load profile %q: %w", name, err)
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return cliProfile{}, false, fmt.Errorf("profile %q not found in %s", name, path)
	}
	return profile, true, nil
}

func applyProfileValues(cmd *cobra.Command, profile cliProfile) {
	applyStringProfileFlag(cmd, "store", profile.Store)
	applyStringProfileFlag(cmd, "backend", profile.Backend)
	applyStringProfileFlag(cmd, "gmx-command", profile.GMXCommand)
	applyStringProfileFlag(cmd, "command", profile.GMXCommand)
	applyStringProfileFlag(cmd, "auth-token", profile.AuthToken)
	applyStringProfileFlag(cmd, "cache", profile.Cache)
	applyDurationProfileFlag(cmd, "job-ttl", profile.JobTTL)
	applyBoolProfileFlag(cmd, "job-prune-on-start", profile.JobPruneOnStart)
}

func applyStringProfileFlag(cmd *cobra.Command, name string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	flagSet := cmd.Flags()
	flag := flagSet.Lookup(name)
	if flag == nil {
		flagSet = cmd.InheritedFlags()
		flag = flagSet.Lookup(name)
	}
	for parent := cmd.Parent(); flag == nil && parent != nil; parent = parent.Parent() {
		flagSet = parent.Flags()
		flag = flagSet.Lookup(name)
	}
	if flag == nil || flag.Changed {
		return
	}
	_ = flagSet.Set(name, value)
}

func applyDurationProfileFlag(cmd *cobra.Command, name string, value time.Duration) {
	if value == 0 {
		return
	}
	applyStringProfileFlag(cmd, name, value.String())
}

func applyBoolProfileFlag(cmd *cobra.Command, name string, value bool) {
	if !value {
		return
	}
	applyStringProfileFlag(cmd, name, "true")
}
