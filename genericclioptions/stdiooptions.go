package genericclioptions

import (
	"errors"
	"fmt"

	"github.com/ladzaretti/vlt-cli/clierror"
	"github.com/ladzaretti/vlt-cli/input"
)

// StdioOptions provides stdin-related CLI helpers,
// intended to be embedded in option structs.
type StdioOptions struct {
	NonInteractive bool

	*IOStreams
}

var _ BaseOptions = &StdioOptions{}

// Complete sets default values, e.g., enabling Stdin if piped input is detected.
func (o *StdioOptions) Complete() error {
	if !o.NonInteractive {
		fi, err := o.In.Stat()
		if err != nil {
			return fmt.Errorf("stat input: %v", err)
		}

		if input.IsPipedOrRedirected(fi) {
			o.Debugf("input is piped or redirected; Enabling non-interactive mode.\n")
			o.NonInteractive = true
		}
	}

	clierror.DebugMode(o.Verbose)

	return nil
}

// Validate ensures the input mode (Stdin or interactive) is used appropriately.
func (o *StdioOptions) Validate() error {
	fi, err := o.In.Stat()
	if err != nil {
		return fmt.Errorf("stat input: %v", err)
	}

	if o.NonInteractive && !input.IsPipedOrRedirected(fi) {
		return errors.New("non-interactive mode requires piped or redirected input")
	}

	return nil
}
