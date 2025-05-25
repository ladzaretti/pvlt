package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ladzaretti/vlt-cli/clierror"
	"github.com/ladzaretti/vlt-cli/genericclioptions"

	"github.com/spf13/cobra"
)

type ImportError struct {
	Err error
}

func (e *ImportError) Error() string { return "import: " + e.Err.Error() }

func (e *ImportError) Unwrap() error { return e.Err }

const (
	// firefoxHeader defines the expected CSV header for exported Firefox passwords.
	firefoxHeader = "url,username,password,httpRealm,formActionOrigin,guid,timeCreated,timeLastUsed,timePasswordChanged"

	// chromiumHeader defines the expected CSV header for exported Chromium passwords.
	chromiumHeader = "name,url,username,password,note"
)

var (
	// firefoxImporter is a custom password importer for exported Firefox password data.
	firefoxImporter = CustomImporter{
		NameIndex:    ptr(1),
		SecretIndex:  ptr(2),
		LabelIndexes: []int{0, 3, 4},
	}

	// chromiumImporter is a custom password importer for exported Chromium password data.
	chromiumImporter = CustomImporter{
		NameIndex:    ptr(2),
		SecretIndex:  ptr(3),
		LabelIndexes: []int{0, 1, 4},
	}

	// vltImporter is a password importer for exported vlt password data.
	vltImporter = VltImporter{}
)

type VltImporter struct{}

var _ Importer = VltImporter{}

func (VltImporter) validate(record []string) error {
	if len(record) != 3 {
		return &ImportError{errors.New("expected 3 fields per record for vlt csv record")}
	}

	return nil
}

func (VltImporter) convert(record []string) secret {
	// assumes validate has run.
	// panicking on out-of-bounds access is acceptable in this context.
	return secret{
		name:   record[0],
		secret: record[1],
		labels: strings.Split(record[2], ","),
	}
}

type secret struct {
	name   string
	secret string
	labels []string
}

type Importer interface {
	convert(record []string) secret
	validate(record []string) error
}

// CustomImporter defines custom column indexes used to extract fields from a CSV row.
type CustomImporter struct {
	NameIndex    *int  `json:"name,omitempty"`   // NameIndex is the index of the name column.
	SecretIndex  *int  `json:"secret,omitempty"` // SecretIndex is the index of the secret column.
	LabelIndexes []int `json:"labels,omitempty"` // LabelIndexes are the indexes of the label columns.
}

var _ Importer = CustomImporter{}

func (ic CustomImporter) validate(record []string) error {
	if ic.NameIndex == nil {
		return errors.New("name index is not set")
	}

	if ic.SecretIndex == nil {
		return errors.New("secret index is not set")
	}

	if *ic.NameIndex >= len(record) {
		return fmt.Errorf("name index %d is out of range (record has %d columns)", *ic.NameIndex, len(record))
	}

	if *ic.SecretIndex >= len(record) {
		return fmt.Errorf("secret index %d is out of range (record has %d columns)", *ic.SecretIndex, len(record))
	}

	for _, index := range ic.LabelIndexes {
		if index >= len(record) {
			return fmt.Errorf("label index %d is out of range (record has %d columns)", index, len(record))
		}
	}

	return nil
}

func (ic CustomImporter) convert(record []string) secret {
	// safe to dereference since validate is expected to run first.
	s := secret{
		name:   record[*ic.NameIndex],
		secret: record[*ic.SecretIndex],
		labels: make([]string, 0, len(ic.LabelIndexes)),
	}

	for _, labelIndex := range ic.LabelIndexes {
		label := record[labelIndex]
		if len(label) > 0 {
			s.labels = append(s.labels, label)
		}
	}

	return s
}

func (ic CustomImporter) String() string {
	name := "nil"
	if ic.NameIndex != nil {
		name = strconv.Itoa(*ic.NameIndex)
	}

	secret := "nil"
	if ic.SecretIndex != nil {
		secret = strconv.Itoa(*ic.SecretIndex)
	}

	return fmt.Sprintf(`{"name": %s, "secret": %s, "labels": %v}`, name, secret, ic.LabelIndexes)
}

func ptr[T any](v T) *T {
	return &v
}

type ImportOptions struct {
	*genericclioptions.StdioOptions
	*VaultOptions

	CSVPath string
	indexes string

	importConfig CustomImporter
}

var _ genericclioptions.CmdOptions = &ImportOptions{}

// NewImportOptions initializes the options struct.
func NewImportOptions(stdio *genericclioptions.StdioOptions, vaultOptions *VaultOptions) *ImportOptions {
	return &ImportOptions{
		StdioOptions: stdio,
		VaultOptions: vaultOptions,
	}
}

func (o *ImportOptions) Complete() error {
	if len(o.indexes) > 0 {
		if err := json.Unmarshal([]byte(o.indexes), &o.importConfig); err != nil {
			return &ImportError{err}
		}
	}

	return nil
}

func (o *ImportOptions) Validate() error {
	if !o.NonInteractive && len(o.CSVPath) == 0 {
		return &ImportError{errors.New("no path provided; use --path to specify the input file")}
	}

	return nil
}

func (o *ImportOptions) Run(ctx context.Context, _ ...string) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = &ImportError{retErr}
			return
		}
	}()

	var in io.Reader

	if o.NonInteractive {
		in = o.In
	}

	if len(o.CSVPath) > 0 {
		f, err := os.Open(o.CSVPath)
		if err != nil {
			return err
		}
		defer func() { //nolint:wsl
			_ = f.Close()
		}()

		in = f
	}

	r := csv.NewReader(in)

	header, err := r.Read()
	if err != nil {
		return err
	}

	importer := o.importerForHeader(strings.Join(header, ","))
	if err := importer.validate(header); err != nil {
		return err
	}

	i := 0
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		s := importer.convert(record)

		if _, err := o.vault.InsertNewSecret(ctx, s.name, s.secret, s.labels); err != nil {
			return err
		}

		i++
	}

	o.Infof("successfully imported %d records.\n", i)

	if err := genericclioptions.RunHook(ctx, o.StdioOptions, o.hooks.postWrite); err != nil {
		o.Warnf("Post-write hook failed: %v", err)
	}

	return nil
}

//nolint:ireturn
func (o *ImportOptions) importerForHeader(header string) Importer {
	switch header {
	case firefoxHeader:
		o.Infof("firefox export file detected.\n")
		return firefoxImporter

	case chromiumHeader:
		o.Infof("chromium export file detected.\n")
		return chromiumImporter

	case vltExportHeader:
		o.Infof("vlt export file detected.\n")
		return vltImporter

	default:
		o.Debugf("using custom import config: %s\n", o.importConfig)
		return o.importConfig
	}
}

// NewCmdImport creates the import cobra command.
func NewCmdImport(defaults *DefaultVltOptions) *cobra.Command {
	o := NewImportOptions(
		defaults.StdioOptions,
		defaults.vaultOptions,
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import secrets from a CSV file",
		Long: `Import secrets into the vault from a CSV file.

The input must be a CSV file with at least two columns: one for the secret's name and one for its value (e.g., password). 
Additional columns can be used for optional labels.

Use the --indexes flag to specify how to extract each field. 
Indexes are zero-based and refer to column positions in the header row.

Firefox and Chromium-based CSV files are auto-detected for import and do not require manual index specification.
`,
		Example: `
# Import using Firefox-compatible format (auto-detected)
vlt import --path passwords.csv

# Import from custom CSV data using a column mapping
echo -e "password,username,label_1,label_2\npass,some_username,meta1,meta2" | \
  vlt import \
      --indexes '{"name":1,"secret":0,"labels":[2,3]}'`,
		Run: func(cmd *cobra.Command, _ []string) {
			clierror.Check(genericclioptions.ExecuteCommand(cmd.Context(), o))
		},
	}

	cmd.Flags().StringVarP(&o.indexes, "indexes", "i", "", "json with column indexes (e.g., '{\"name\":0,\"secret\":1,\"labels\":[2]}')")
	cmd.Flags().StringVarP(&o.CSVPath, "path", "p", "", "path to the input CSV file")

	return cmd
}
