package cli

import (
	"context"

	"github.com/ladzaretti/vlt-cli/clierror"
	"github.com/ladzaretti/vlt-cli/genericclioptions"
	"github.com/ladzaretti/vlt-cli/vaultdaemon"

	"github.com/spf13/cobra"
)

type LogoutOptions struct {
	*genericclioptions.StdioOptions
	*VaultOptions

	sessionClient *vaultdaemon.SessionClient
}

var _ genericclioptions.CmdOptions = &LogoutOptions{}

// NewLogoutOptions initializes the options struct.
func NewLogoutOptions(stdio *genericclioptions.StdioOptions, vaultOptions *VaultOptions) *LogoutOptions {
	return &LogoutOptions{
		StdioOptions: stdio,
		VaultOptions: vaultOptions,
	}
}

func (o *LogoutOptions) Complete() error {
	s, err := vaultdaemon.NewSessionClient()
	if err != nil {
		return err
	}

	o.sessionClient = s

	return nil
}

func (*LogoutOptions) Validate() error { return nil }

func (o *LogoutOptions) Run(ctx context.Context, _ ...string) error {
	defer func() { _ = o.Close() }()

	if err := o.sessionClient.Logout(ctx, o.path); err != nil {
		return err
	}

	o.Infof("logout successful")

	return nil
}

func (o *LogoutOptions) Close() error {
	return o.sessionClient.Close()
}

// NewCmdLogout creates the logout cobra command.
func NewCmdLogout(defaults *DefaultVltOptions) *cobra.Command {
	o := NewLogoutOptions(
		defaults.StdioOptions,
		defaults.vaultOptions,
	)

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Logs the user out of the current session",
		Long:  "Logs the user out of the current session.",
		Run: func(cmd *cobra.Command, _ []string) {
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	return cmd
}
