package cli

import (
	"context"
	"fmt"

	"github.com/ladzaretti/vlt-cli/clierror"
	"github.com/ladzaretti/vlt-cli/clipboard"
	"github.com/ladzaretti/vlt-cli/genericclioptions"
	"github.com/ladzaretti/vlt-cli/randstring"

	"github.com/spf13/cobra"
)

type GenerateOptions struct {
	*genericclioptions.StdioOptions

	policy randstring.PasswordPolicy
	copy   bool
}

var _ genericclioptions.CmdOptions = &GenerateOptions{}

// NewGenerateOptions initializes the options struct.
func NewGenerateOptions(stdio *genericclioptions.StdioOptions) *GenerateOptions {
	return &GenerateOptions{
		StdioOptions: stdio,
	}
}

func (*GenerateOptions) Complete() error { return nil }

func (*GenerateOptions) Validate() error { return nil }

func (o *GenerateOptions) Run(context.Context, ...string) error {
	policy := o.policy

	zero := randstring.PasswordPolicy{}
	if policy == zero {
		policy = randstring.DefaultPasswordPolicy
	}

	s, err := randstring.NewWithPolicy(policy)
	if err != nil {
		return err
	}

	if o.copy {
		o.Debugf("copying secret to clipboard\n")
		return clipboard.Copy(s)
	}

	o.Infof("%s", s)

	return nil
}

// NewCmdGenerate creates the Generate cobra command.
func NewCmdGenerate(defaults *DefaultVltOptions) *cobra.Command {
	o := NewGenerateOptions(defaults.StdioOptions)

	cmd := &cobra.Command{
		Use:     "generate",
		Aliases: []string{"gen", "rand"},
		Short:   "Generate a random password",
		Long: fmt.Sprintf(`Generate a random password based on the provided character requirements and minimum length.

If no flags are provided, the default policy is:
  - At least %d uppercase letters
  - At least %d lowercase letters
  - At least %d numeric
  - At least %d special
  - Minimum total length: %d

If a specific requirement is provided (e.g., '--digits 4'), the generated password will 
contain at least that many characters of the specified type. Any remaining characters will 
be randomly chosen to meet the minimum total length.
`,
			randstring.DefaultPasswordPolicy.MinUppercase,
			randstring.DefaultPasswordPolicy.MinLowercase,
			randstring.DefaultPasswordPolicy.MinNumeric,
			randstring.DefaultPasswordPolicy.MinSpecial,
			randstring.DefaultPasswordPolicy.MinLength,
		),
		Run: func(cmd *cobra.Command, _ []string) {
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	cmd.Flags().IntVarP(&o.policy.MinUppercase, "upper-case", "u", 0, "minimum number of uppercase letters")
	cmd.Flags().IntVarP(&o.policy.MinLowercase, "lower-case", "l", 0, "minimum number of lowercase letters")
	cmd.Flags().IntVarP(&o.policy.MinSpecial, "special", "s", 0, "minimum number of special characters")
	cmd.Flags().IntVarP(&o.policy.MinNumeric, "numeric", "d", 0, "minimum number of numeric characters")
	cmd.Flags().IntVarP(&o.policy.MinLength, "min-length", "m", 0, "minimum total length of the password")
	cmd.Flags().BoolVarP(&o.copy, "copy-clipboard", "c", false, "copy the generated password to the clipboard")

	return cmd
}
