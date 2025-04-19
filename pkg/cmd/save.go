package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ladzaretti/vlt-cli/pkg/clipboard"
	"github.com/ladzaretti/vlt-cli/pkg/genericclioptions"
	cmdutil "github.com/ladzaretti/vlt-cli/pkg/util"
	"github.com/ladzaretti/vlt-cli/pkg/util/input"
	"github.com/ladzaretti/vlt-cli/pkg/util/randstring"
	"github.com/ladzaretti/vlt-cli/pkg/vaulterrors"

	"github.com/ladzaretti/vlt-cli/pkg/vlt"

	"github.com/spf13/cobra"
)

var (
	ErrNoSecretUpdated  = errors.New("no secret was updated")
	ErrNoSecretInserted = errors.New("no secret was inserted")
)

type SaveError struct {
	Err error
}

func (e *SaveError) Error() string { return "save: " + e.Err.Error() }

func (e *SaveError) Unwrap() error { return e.Err }

// SaveOptions holds data required to run the command.
type SaveOptions struct {
	vault func() *vlt.Vault

	*genericclioptions.StdioOptions

	name     string   // name is the name of the secret to save in the vault.
	labels   []string // labels to associate with the a given secret.
	generate bool     // generate indicates whether to auto-generate a random secret.
	output   bool     // output controls whether to print the saved secret to stdout.
	copy     bool     // copy controls whether to copy the saved secret to the clipboard.
	paste    bool     // paste controls whether to read the secret to save from the clipboard.
}

var _ genericclioptions.CmdOptions = &SaveOptions{}

// NewSaveOptions initializes the options struct.
func NewSaveOptions(stdio *genericclioptions.StdioOptions, vault func() *vlt.Vault) *SaveOptions {
	return &SaveOptions{
		StdioOptions: stdio,
		vault:        vault,
	}
}

// NewCmdSave creates the save cobra command.
func NewCmdSave(stdio *genericclioptions.StdioOptions, vault func() *vlt.Vault) *cobra.Command {
	o := NewSaveOptions(stdio, vault)

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a new secret to the vault",
		Long: `Save a new key-value pair to the vault.

The name of the secret can be provided using the --name flag or entered interactively.
The secret value can be piped, redirected, or typed manually when prompted.

If input is piped or redirected, it is automatically used as the secret value,
taking precedence over interactive input.`,
		Run: func(cmd *cobra.Command, _ []string) {
			cmdutil.CheckErr(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	cmd.Flags().BoolVarP(&o.generate, "generate", "g", false, "generate a random secret")
	cmd.Flags().BoolVarP(&o.output, "output", "o", false, "output the saved secret to stdout (use with caution; intended primarily for piping)")
	cmd.Flags().BoolVarP(&o.copy, "copy-clipboard", "c", false, "copy the saved secret to clipboard")
	cmd.Flags().BoolVarP(&o.paste, "paste-clipboard", "p", false, "read the secret from clipboard")

	cmd.Flags().StringVarP(&o.name, "name", "", "", "the secret name (e.g., username)")
	cmd.Flags().StringSliceVarP(&o.labels, "label", "", nil, "label to associate with the secret (comma-separated or repeated)")

	return cmd
}

func (*SaveOptions) Complete() error {
	return nil
}

func (o *SaveOptions) Validate() error {
	if strings.HasPrefix(o.name, "-") {
		return fmt.Errorf("invalid --name value %q (must not start with '-')", o.name)
	}

	return o.validateInputSource()
}

func (o *SaveOptions) Run(ctx context.Context) (retErr error) {
	secret := ""

	// ensure error is wrapped and output is printed if everything succeeded
	defer func() {
		if retErr != nil {
			retErr = &SaveError{retErr}
			return
		}

		if len(secret) > 0 {
			if err := o.outputSecret(secret); err != nil {
				retErr = &SaveError{err}
				return
			}
		}
	}()

	s, err := o.readSecretNonInteractive()
	if err != nil {
		return fmt.Errorf("read secret non-interactive: %w", err)
	}

	secret = s

	if len(o.name) == 0 {
		k, err := o.readInteractive("Enter name: ")
		if err != nil {
			return fmt.Errorf("name read interactive: %w", err)
		}

		o.name = k
	}

	if len(o.name) == 0 {
		return vaulterrors.ErrEmptyName
	}

	if len(secret) == 0 {
		if err := o.readSecretInteractive(&secret); err != nil {
			return fmt.Errorf("read secret interactive: %w", err)
		}
	}

	if len(secret) == 0 {
		return vaulterrors.ErrEmptySecret
	}

	if len(o.labels) == 0 {
		labels, err := o.readInteractive("Enter label(s), comma-separated: ")
		if err != nil {
			return fmt.Errorf("label read interactive: %w", err)
		}

		o.labels = append(o.labels, cmdutil.ParseCommaSeparated(labels)...)
	}

	if len(o.labels) == 0 {
		return vaulterrors.ErrMissingLabels
	}

	return o.insertNewSecret(ctx, secret)
}

func (o *SaveOptions) readInteractive(prompt string, a ...any) (string, error) {
	return input.PromptRead(o.Out, o.In, prompt, a...)
}

func (o *SaveOptions) readSecretInteractive(secret *string) error {
	s, err := input.PromptReadSecure(o.Out, int(o.In.Fd()), "Enter secret for name %q: ", o.name)
	if err != nil {
		return err
	}

	*secret = s

	return nil
}

func (o *SaveOptions) readSecretNonInteractive() (string, error) {
	if o.generate {
		return randstring.New(20)
	}

	if o.paste {
		o.Debugf("Reading secret from clipboard")
		return clipboard.Paste()
	}

	if o.NonInteractive {
		o.Debugf("Reading non-interactive secret")
		return input.ReadTrim(o.In)
	}

	return "", nil
}

func (o *SaveOptions) insertNewSecret(ctx context.Context, s string) error {
	n, err := o.vault().InsertNewSecret(ctx, o.name, s, o.labels)
	if err != nil {
		return err
	}

	if n == 0 {
		return ErrNoSecretInserted
	}

	return nil
}

func (o *SaveOptions) outputSecret(s string) error {
	if o.output {
		o.Infof("%s\n", s)
		return nil
	}

	if o.copy {
		o.Debugf("Copying secret to clipboard")
		return clipboard.Copy(s)
	}

	return nil
}

func (o *SaveOptions) validateInputSource() error {
	used := 0
	if o.NonInteractive {
		used++
	}

	if o.generate {
		used++
	}

	if o.paste {
		used++
	}

	if used > 1 {
		return &SaveError{errors.New("only one of non-interactive input, --generate, or --paste-clipboard can be used at a time")}
	}

	return nil
}
