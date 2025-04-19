package cmd

import (
	"context"
	"fmt"

	"github.com/ladzaretti/vlt-cli/pkg/genericclioptions"
	cmdutil "github.com/ladzaretti/vlt-cli/pkg/util"
	"github.com/ladzaretti/vlt-cli/pkg/util/input"
	"github.com/ladzaretti/vlt-cli/pkg/vaulterrors"
	"github.com/ladzaretti/vlt-cli/pkg/vlt"

	"github.com/spf13/cobra"
)

// CreateOptions have the data required to perform the create operation.
type CreateOptions struct {
	vaultPath func() string

	*genericclioptions.StdioOptions
}

var _ genericclioptions.CmdOptions = &CreateOptions{}

// NewCreateOptions initializes the options struct.
func NewCreateOptions(stdio *genericclioptions.StdioOptions, vaultPath func() string) *CreateOptions {
	return &CreateOptions{
		StdioOptions: stdio,
		vaultPath:    vaultPath,
	}
}

// NewCmdCreate creates the create cobra command.
func NewCmdCreate(stdio *genericclioptions.StdioOptions, path func() string) *cobra.Command {
	o := NewCreateOptions(stdio, path)

	return &cobra.Command{
		Use:   "create",
		Short: "Initialize a new vault",
		Long:  "Create a new vault by specifying the SQLite database file where credentials will be stored.",
		Run: func(cmd *cobra.Command, _ []string) {
			cmdutil.CheckErr(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}
}

func (*CreateOptions) Complete() error {
	return nil
}

func (o *CreateOptions) Validate() error {
	if o.NonInteractive {
		return vaulterrors.ErrNonInteractiveUnsupported
	}

	return nil
}

func (o *CreateOptions) Run(ctx context.Context) error {
	mk, err := input.PromptNewPassword(o.Out, int(o.In.Fd()))
	if err != nil {
		return fmt.Errorf("read new master key: %w", err)
	}

	vault, err := vlt.New(o.vaultPath())
	if err != nil {
		return fmt.Errorf("create vault: %w", err)
	}

	if err := vault.SetMasterKey(ctx, mk); err != nil {
		return fmt.Errorf("set master key: %w", err)
	}

	o.Infof("New vault successfully created at %q\n", o.vaultPath())

	return nil
}
