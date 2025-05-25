package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ladzaretti/vlt-cli/clierror"
	"github.com/ladzaretti/vlt-cli/genericclioptions"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

// ConfigOptions holds cli, file, and resolved global configuration.
type ConfigOptions struct {
	*genericclioptions.StdioOptions

	fileConfig *FileConfig
	cliFlags   *Flags

	resolved *ResolvedConfig
}

// Flags holds cli overrides for configuration.
type Flags struct {
	configPath string
	vaultPath  string
}

// ResolvedConfig contains the final merged configuration.
// cli flags take precedence over config file values.
//
//nolint:tagliatelle
type ResolvedConfig struct {
	CopyCmd         string   `json:"copy_cmd,omitempty"`
	PasteCmd        string   `json:"paste_cmd,omitempty"`
	SessionDuration Duration `json:"session_duration,omitempty"`
	VaultPath       string   `json:"vault_path,omitempty"`
	FindPipeCmd     []string `json:"find_pipe_cmd,omitempty"`
	PostLoginCmd    []string `json:"post_login_cmd,omitempty"`
	PostWriteCmd    []string `json:"post_write_cmd,omitempty"`
}

type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).String() }

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

var _ genericclioptions.CmdOptions = &ConfigOptions{}

// NewConfigOptions initializes ConfigOptions with default values.
func NewConfigOptions(stdio *genericclioptions.StdioOptions) *ConfigOptions {
	return &ConfigOptions{
		StdioOptions: stdio,
		fileConfig:   newFileConfig(),
		cliFlags:     &Flags{},
		resolved:     &ResolvedConfig{},
	}
}

func (o *ConfigOptions) Resolved() *ResolvedConfig { return o.resolved }

func (o *ConfigOptions) Complete() error {
	c, err := LoadFileConfig(o.cliFlags.configPath)
	if err != nil {
		return err
	}

	o.fileConfig = c

	return o.resolve()
}

func (o *ConfigOptions) resolve() error {
	o.resolved.CopyCmd = o.fileConfig.Clipboard.CopyCmd
	o.resolved.PasteCmd = o.fileConfig.Clipboard.PasteCmd
	o.resolved.FindPipeCmd = o.fileConfig.Pipeline.FindPipeCmd
	o.resolved.PostLoginCmd = o.fileConfig.Hooks.PostLoginCmd
	o.resolved.PostWriteCmd = o.fileConfig.Hooks.PostWriteCmd
	o.resolved.VaultPath = cmp.Or(o.cliFlags.vaultPath, o.fileConfig.Vault.Path)

	if len(o.resolved.VaultPath) == 0 {
		vaultPath, err := defaultVaultPath()
		if err != nil {
			return err
		}

		o.resolved.VaultPath = vaultPath
	}

	sessionDuration := cmp.Or(o.fileConfig.Vault.SessionDuration, defaultSessionDuration)

	t, err := time.ParseDuration(sessionDuration)
	if err != nil {
		return fmt.Errorf("invalid session duration: %w", err)
	}

	o.resolved.SessionDuration = Duration(t)

	return nil
}

func defaultVaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, defaultDatabaseFilename), nil
}
func (*ConfigOptions) Validate() error { return nil }

func (*ConfigOptions) Run(context.Context, ...string) error { return nil }

// NewCmdConfig creates the cobra config command tree.
func NewCmdConfig(defaults *DefaultVltOptions) *cobra.Command {
	hiddenFlags := []string{"config"}
	o := NewConfigOptions(defaults.StdioOptions)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Resolve and inspect the active vlt configuration (subcommands available)",
		Long: fmt.Sprintf(`Resolve and display the active vlt configuration.

If --file is not provided, the default config path (~/%s) is used.`, defaultConfigName),
		Run: func(cmd *cobra.Command, _ []string) {
			clierror.Check(genericclioptions.RejectDisallowedFlags(cmd, hiddenFlags...))
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))

			if len(o.fileConfig.path) == 0 {
				o.Infof("no config file found; using default values.\n")
				return
			}

			o.Infof("config loaded from: %q:\n\n%s\n\n", o.fileConfig.path, stringifyPretty(o.fileConfig))
			o.Infof("resolved runtime config:\n\n%+v\n", stringifyPretty(o.resolved))
		},
	}

	cmd.PersistentFlags().StringVarP(&o.cliFlags.configPath, "file", "f", "",
		fmt.Sprintf("path to the configuration file (default: ~/%s)", defaultConfigName))

	cmd.AddCommand(newGenerateConfigCmd(defaults))
	cmd.AddCommand(newValidateConfigCmd(defaults))

	genericclioptions.MarkFlagsHidden(cmd, hiddenFlags...)

	return cmd
}

// stringifyPretty returns the pretty-printed JSON representation of v.
// If marshalling fails, it returns the error message instead.
func stringifyPretty(v any) string {
	s, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("stringify error: %v", err)
	}

	return string(s)
}

type generateConfigOptions struct {
	*genericclioptions.StdioOptions
}

var _ genericclioptions.CmdOptions = &generateConfigOptions{}

// newGenerateConfigOptions initializes the options struct.
func newGenerateConfigOptions(stdio *genericclioptions.StdioOptions) *generateConfigOptions {
	return &generateConfigOptions{
		StdioOptions: stdio,
	}
}

func (*generateConfigOptions) Complete() error { return nil }

func (*generateConfigOptions) Validate() error { return nil }

func (o *generateConfigOptions) Run(context.Context, ...string) error {
	out, err := toml.Marshal(newFileConfig())
	clierror.Check(err)

	o.Infof("%s", string(out))

	return nil
}

// newGenerateConfigCmd creates the 'generate' subcommand for generating default config.
func newGenerateConfigCmd(defaults *DefaultVltOptions) *cobra.Command {
	hiddenFlags := []string{"file", "config"}
	o := newGenerateConfigOptions(defaults.StdioOptions)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Print a default config file",
		Long: `Outputs the default configuration in TOML format to stdout.

This command does not accept any arguments.`,
		Run: func(cmd *cobra.Command, _ []string) {
			clierror.Check(genericclioptions.RejectDisallowedFlags(cmd, hiddenFlags...))
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	genericclioptions.MarkFlagsHidden(cmd, hiddenFlags...)

	return cmd
}

type validateConfigOptions struct {
	*genericclioptions.StdioOptions

	configPath string
}

var _ genericclioptions.CmdOptions = &validateConfigOptions{}

// newValidateConfigOptions initializes the options struct.
func newValidateConfigOptions(stdio *genericclioptions.StdioOptions) *validateConfigOptions {
	return &validateConfigOptions{
		StdioOptions: stdio,
	}
}

func (*validateConfigOptions) Complete() error { return nil }

func (*validateConfigOptions) Validate() error { return nil }

func (o *validateConfigOptions) Run(context.Context, ...string) error {
	c, err := LoadFileConfig(o.configPath)
	clierror.Check(err)

	if len(c.path) == 0 {
		o.Infof("no config file found; Nothing to validate.\n")
		return nil
	}

	o.Infof("%s: OK\n", c.path)

	return nil
}

// newValidateConfigCmd creates the 'validate' subcommand for validating the config file.
func newValidateConfigCmd(defaults *DefaultVltOptions) *cobra.Command {
	hiddenFlags := []string{"config"}
	o := newValidateConfigOptions(defaults.StdioOptions)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check config validity",
		Long: fmt.Sprintf(`Loads the configuration file and checks for common errors.

If --file is not provided, the default config path (~/%s) is used.`, defaultConfigName),
		Run: func(cmd *cobra.Command, _ []string) {
			o.configPath, _ = cmd.InheritedFlags().GetString("file")

			clierror.Check(genericclioptions.RejectDisallowedFlags(cmd, hiddenFlags...))
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	genericclioptions.MarkFlagsHidden(cmd, hiddenFlags...)

	return cmd
}
