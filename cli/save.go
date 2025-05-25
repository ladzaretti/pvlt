package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ladzaretti/vlt-cli/clierror"
	"github.com/ladzaretti/vlt-cli/clipboard"
	"github.com/ladzaretti/vlt-cli/genericclioptions"
	"github.com/ladzaretti/vlt-cli/input"
	"github.com/ladzaretti/vlt-cli/randstring"
	cmdutil "github.com/ladzaretti/vlt-cli/util"
	"github.com/ladzaretti/vlt-cli/vaulterrors"

	"github.com/spf13/cobra"
)

var ErrNoSecretInserted = errors.New("no secret was inserted")

type SaveError struct {
	Err error
}

func (e *SaveError) Error() string { return "save: " + e.Err.Error() }

func (e *SaveError) Unwrap() error { return e.Err }

// SaveOptions holds data required to run the command.
type SaveOptions struct {
	*genericclioptions.StdioOptions
	*VaultOptions

	name     string   // name is the name of the secret to save in the vault.
	labels   []string // labels to associate with the a given secret.
	generate bool     // generate indicates whether to auto-generate a random secret.
	output   bool     // output controls whether to print the saved secret to stdout.
	copy     bool     // copy controls whether to copy the saved secret to the clipboard.
	paste    bool     // paste controls whether to read the secret to save from the clipboard.
}

var _ genericclioptions.CmdOptions = &SaveOptions{}

// NewSaveOptions initializes the options struct.
func NewSaveOptions(stdio *genericclioptions.StdioOptions, vaultOptions *VaultOptions) *SaveOptions {
	return &SaveOptions{
		StdioOptions: stdio,
		VaultOptions: vaultOptions,
	}
}

func (*SaveOptions) Complete() error { return nil }

func (o *SaveOptions) Validate() error {
	if strings.HasPrefix(o.name, "-") {
		return fmt.Errorf("invalid --name value %q (must not start with '-')", o.name)
	}

	return o.validateInputSource()
}

func (o *SaveOptions) Run(ctx context.Context, _ ...string) (retErr error) {
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

	interactive := len(s) == 0
	secret = strings.TrimSpace(s)

	if interactive {
		err := o.readInteractive(&secret)
		if err != nil {
			return err
		}
	}

	if len(o.name) == 0 {
		return vaulterrors.ErrEmptyName
	}

	if len(secret) == 0 {
		return vaulterrors.ErrEmptySecret
	}

	if len(o.labels) == 0 && !interactive {
		o.Warnf(`No labels were provided for the secret in non-interactive mode. 
You may want to add labels using the '--label' flag or interactively.\n`)
	}

	if err := o.insertNewSecret(ctx, secret); err != nil {
		return err
	}

	if err := genericclioptions.RunHook(ctx, o.StdioOptions, o.hooks.postWrite); err != nil {
		o.Warnf("Post-write hook failed: %v", err)
	}

	return nil
}

func (o *SaveOptions) readSecretNonInteractive() (string, error) {
	if o.generate {
		return randstring.NewWithPolicy(randstring.DefaultPasswordPolicy)
	}

	if o.paste {
		o.Debugf("reading secret from clipboard")
		return clipboard.Paste()
	}

	if o.NonInteractive {
		o.Debugf("reading non-interactive secret")
		return input.ReadTrim(o.In)
	}

	return "", nil
}

func (o *SaveOptions) readInteractive(secret *string) error {
	if len(o.name) == 0 {
		k, err := o.promptRead("Enter name: ")
		if err != nil {
			return fmt.Errorf("name read interactive: %w", err)
		}

		o.name = k
	}

	if len(*secret) == 0 {
		s, err := o.promptReadSecure("Enter secret for name %q: ", o.name)
		if err != nil {
			return err
		}

		*secret = s
	}

	if len(o.labels) == 0 {
		labels, err := o.promptRead("Enter labels (comma-separated), or press Enter to skip: ")
		if err != nil {
			return fmt.Errorf("label read interactive: %w", err)
		}

		o.labels = append(o.labels, cmdutil.ParseCommaSeparated(labels)...)
	}

	return nil
}

func (o *SaveOptions) promptRead(prompt string, a ...any) (string, error) {
	return input.PromptRead(o.Out, o.In, prompt, a...)
}

func (o *SaveOptions) promptReadSecure(prompt string, a ...any) (string, error) {
	return input.PromptReadSecure(o.Out, int(o.In.Fd()), prompt, a...)
}

func (o *SaveOptions) insertNewSecret(ctx context.Context, s string) error {
	n, err := o.vault.InsertNewSecret(ctx, o.name, s, o.labels)
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
		o.Infof("%s", s)
		return nil
	}

	if o.copy {
		o.Debugf("copying secret to clipboard\n")
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

// NewCmdSave creates the save cobra command.
func NewCmdSave(defaults *DefaultVltOptions) *cobra.Command {
	o := NewSaveOptions(
		defaults.StdioOptions,
		defaults.vaultOptions,
	)

	cmd := &cobra.Command{
		Use:     "save",
		Aliases: []string{"put"},
		Short:   "Save a new secret to the vault",
		Long: `Save a new key-value pair to the vault.

The secret value can be piped, redirected, or prompted.
Metadata (e.g., name and labels) can be provided via command-line arguments or prompted.

Note 1:
	If input is piped or redirected, it will be automatically used as the secret value.

Note 2:
	If data is piped or redirected into the command (i.e., stdin is not a TTY),
	metadata must be provided as command-line arguments. Interactive prompts will be skipped in this case.`,
		Run: func(cmd *cobra.Command, _ []string) {
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	cmd.Flags().BoolVarP(&o.generate, "generate", "g", false, "generate a random secret")
	cmd.Flags().BoolVarP(&o.output, "output", "o", false, "output the saved secret to stdout (unsafe)")
	cmd.Flags().BoolVarP(&o.copy, "copy-clipboard", "c", false, "copy the saved secret to the clipboard")
	cmd.Flags().BoolVarP(&o.paste, "paste-clipboard", "p", false, "read the secret from the clipboard")

	cmd.Flags().StringVarP(&o.name, "name", "", "", "the secret name (e.g., username)")
	cmd.Flags().StringSliceVarP(&o.labels, "label", "", nil, "optional label to associate with the secret (comma-separated or repeated)")

	return cmd
}
