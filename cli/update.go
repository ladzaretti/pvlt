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
	"github.com/ladzaretti/vlt-cli/vaulterrors"

	"github.com/spf13/cobra"
)

var (
	ErrNoUpdateArgs    = errors.New("no update arguments provided; specify at least one of --set-name, --add-label, or --remove-label")
	ErrNoSecretUpdated = errors.New("no secret was updated")
)

type UpdateError struct {
	Err error
}

func (e *UpdateError) Error() string { return "update: " + e.Err.Error() }

func (e *UpdateError) Unwrap() error { return e.Err }

type UpdateOptions struct {
	*genericclioptions.StdioOptions
	*VaultOptions

	search       *SearchableOptions
	newName      string
	addLabels    []string
	removeLabels []string
}

var _ genericclioptions.CmdOptions = &UpdateOptions{}

// NewUpdateOptions initializes the options struct.
func NewUpdateOptions(stdio *genericclioptions.StdioOptions, vaultOptions *VaultOptions) *UpdateOptions {
	return &UpdateOptions{
		StdioOptions: stdio,
		VaultOptions: vaultOptions,
		search:       NewSearchableOptions(),
	}
}

func (*UpdateOptions) Complete() error { return nil }

func (o *UpdateOptions) Validate() error {
	if err := o.search.Validate(); err != nil {
		return &UpdateError{err}
	}

	return o.validateUpdateArgs()
}

func (o *UpdateOptions) validateUpdateArgs() error {
	args := 0

	if len(o.newName) > 0 {
		args++
	}

	if len(o.addLabels) > 0 {
		args++
	}

	if len(o.removeLabels) > 0 {
		args++
	}

	if args == 0 {
		return &UpdateError{ErrNoUpdateArgs}
	}

	return nil
}

func (o *UpdateOptions) Run(ctx context.Context, args ...string) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = &UpdateError{retErr}
			return
		}
	}()

	o.search.WildcardFrom(args)

	matchingSecrets, err := o.search.search(ctx, o.vault)
	if err != nil {
		return err
	}

	count := len(matchingSecrets)

	switch count {
	case 1:
		o.Infof("found one match.\n")
	case 0:
		o.Warnf("No match found.\n")
		return vaulterrors.ErrSearchNoMatch
	default:
		o.Warnf("Expecting exactly one match, but found %d.\n\n", count)
		printTable(o.ErrOut, matchingSecrets)

		return vaulterrors.ErrAmbiguousSecretMatch
	}

	return o.vault.UpdateSecretMetadata(ctx, matchingSecrets[0].id, o.newName, o.removeLabels, o.addLabels)
}

// NewCmdUpdate creates the update cobra command.
func NewCmdUpdate(defaults *DefaultVltOptions) *cobra.Command {
	o := NewUpdateOptions(defaults.StdioOptions, defaults.vaultOptions)

	cmd := &cobra.Command{
		Use:   "update [glob]",
		Short: "Update secret data or metadata (subcommands available)",
		Long: `Update metadata for an existing secret.

This command updates metadata such as the name or labels of a secret.
The update will proceed only if exactly one secret matches the given search criteria.

To update the secret value, use the 'vlt update secret' subcommand.`,
		Example: `  # Rename a secret by ID
  vlt update --id 123 --set-name new-name

  # Add a label to a secret whose name or label matches the given glob pattern
  vlt update "*foo*" --add-label bar

  # Add a label to a secret by name
  vlt update --name github --add-label dev

  # Remove a label from a secret
  vlt update --id 456 --remove-label old-label`,
		Run: func(cmd *cobra.Command, args []string) {
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o, args...))
		},
	}

	cmd.Flags().IntVarP(&o.search.ID, "id", "", 0, FilterByID.Help())
	cmd.Flags().StringVarP(&o.search.Name, "name", "", "", FilterByName.Help())
	cmd.Flags().StringSliceVarP(&o.search.Labels, "label", "", nil, FilterByLabels.Help())

	cmd.Flags().StringVarP(&o.newName, "set-name", "", "", "new name for the secret")
	cmd.Flags().StringSliceVarP(&o.addLabels, "add-label", "", nil, "label to add to the secret")
	cmd.Flags().StringSliceVarP(&o.removeLabels, "remove-label", "", nil, "label to remove from the secret")

	cmd.AddCommand(NewCmdUpdateSecretValue(defaults))

	return cmd
}

type UpdateSecretValueOptions struct {
	*genericclioptions.StdioOptions
	*VaultOptions

	search *SearchableOptions

	generate bool // generate indicates whether to auto-generate a random secret.
	output   bool // output controls whether to print the saved secret to stdout.
	copy     bool // copy controls whether to copy the saved secret to the clipboard.
	paste    bool // paste controls whether to read the secret to save from the clipboard.
}

var _ genericclioptions.CmdOptions = &UpdateSecretValueOptions{}

// NewUpdateSecretValueOptions initializes the options struct.
func NewUpdateSecretValueOptions(stdio *genericclioptions.StdioOptions, vaultOptions *VaultOptions) *UpdateSecretValueOptions {
	return &UpdateSecretValueOptions{
		StdioOptions: stdio,
		VaultOptions: vaultOptions,
		search:       NewSearchableOptions(),
	}
}

func (*UpdateSecretValueOptions) Complete() error {
	return nil
}

func (o *UpdateSecretValueOptions) Validate() error {
	if err := o.search.Validate(); err != nil {
		return &UpdateError{err}
	}

	return o.validateUpdateSecretArgs()
}

func (o *UpdateSecretValueOptions) validateUpdateSecretArgs() error {
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
		return &UpdateError{errors.New("only one of non-interactive input, --generate, or --paste can be used at a time")}
	}

	return nil
}

func (o *UpdateSecretValueOptions) Run(ctx context.Context, args ...string) (retErr error) {
	o.search.WildcardFrom(args)

	matchingSecrets, err := o.search.search(ctx, o.vault)
	if err != nil {
		return &UpdateError{err}
	}

	count := len(matchingSecrets)

	switch count {
	case 1:
		o.Debugf("found one match.\n")
	case 0:
		o.Warnf("No match found.\n")
		return &UpdateError{vaulterrors.ErrSearchNoMatch}
	default:
		o.Warnf("Expecting exactly one match, but found %d.\n\n", count)
		printTable(o.ErrOut, matchingSecrets)

		return &UpdateError{vaulterrors.ErrAmbiguousSecretMatch}
	}

	id, secret := matchingSecrets[0].id, ""

	// ensure error is wrapped and output is printed if everything succeeded
	defer func() {
		if retErr != nil {
			retErr = &UpdateError{retErr}
			return
		}

		if len(secret) > 0 {
			if err := o.outputSecret(secret); err != nil {
				retErr = &UpdateError{err}
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
		s, err := o.promptReadSecure("Enter new secret value: ")
		if err != nil {
			return err
		}

		secret = s
	}

	if len(secret) == 0 {
		return vaulterrors.ErrEmptySecret
	}

	if err := o.UpdateSecretValue(ctx, id, secret); err != nil {
		return err
	}

	if err := genericclioptions.RunHook(ctx, o.StdioOptions, o.hooks.postWrite); err != nil {
		o.Warnf("Post-write hook failed: %v", err)
	}

	return nil
}

func (o *UpdateSecretValueOptions) readSecretNonInteractive() (string, error) {
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

func (o *UpdateSecretValueOptions) outputSecret(s string) error {
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

func (o *UpdateSecretValueOptions) promptReadSecure(prompt string, a ...any) (string, error) {
	return input.PromptReadSecure(o.Out, int(o.In.Fd()), prompt, a...)
}

func (o *UpdateSecretValueOptions) UpdateSecretValue(ctx context.Context, id int, secret string) error {
	n, err := o.vault.UpdateSecret(ctx, id, secret)
	if err != nil {
		return err
	}

	if n == 0 {
		return ErrNoSecretInserted
	}

	o.Infof("OK\n")

	return nil
}

// NewCmdUpdateSecretValue creates the cobra command.
func NewCmdUpdateSecretValue(defaults *DefaultVltOptions) *cobra.Command {
	o := NewUpdateSecretValueOptions(
		defaults.StdioOptions,
		defaults.vaultOptions,
	)

	cmd := &cobra.Command{
		Use:   "secret [glob]",
		Short: "Update the value of an existing secret",
		Long: `Update the value of an existing secret.

The update is performed only if exactly one secret matches the provided criteria.

You can provide the new value via prompt, clipboard, or by generating a random value.`,
		Example: ` # Update value using prompt (interactive)
  vlt update secret --id 123

  # Update value that matches a wildcard with a generated secret
  vlt update secret "*suffix" --generate

  # Update value with a generated secret
  vlt update secret --name api-key --generate

  # Update value using the clipboard as input
  vlt update secret --label env=prod --paste`,
		Run: func(cmd *cobra.Command, args []string) {
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o, args...))
		},
	}

	cmd.Flags().IntVarP(&o.search.ID, "id", "", 0, FilterByID.Help())
	cmd.Flags().StringVarP(&o.search.Name, "name", "", "", FilterByName.Help())
	cmd.Flags().StringSliceVarP(&o.search.Labels, "label", "", nil, FilterByLabels.Help())

	cmd.Flags().BoolVarP(&o.generate, "generate", "g", false, "generate a random secret")
	cmd.Flags().BoolVarP(&o.output, "output", "o", false, "output the saved secret to stdout (unsafe)")
	cmd.Flags().BoolVarP(&o.copy, "copy-clipboard", "c", false, "copy the saved secret to the clipboard")
	cmd.Flags().BoolVarP(&o.paste, "paste-clipboard", "p", false, "read the secret from the clipboard")

	return cmd
}
