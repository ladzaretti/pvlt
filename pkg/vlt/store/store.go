package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	cmdutil "github.com/ladzaretti/vlt-cli/pkg/util"
)

var (
	// ErrNoLabelsProvided indicates that no labels were provided as an argument.
	ErrNoLabelsProvided = errors.New("no labels provided")

	// ErrNoIDsProvided indicates that no ids were provided as an argument.
	ErrNoIDsProvided = errors.New("no IDs provided")
)

// DBTX defines the subset of database operations used by [Store].
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db DBTX
}

func New(db DBTX) *Store {
	return &Store{
		db: db,
	}
}

// WithTx returns a new Store using the given transaction.
func (*Store) WithTx(tx *sql.Tx) *Store {
	return &Store{
		db: tx,
	}
}

const insertMasterKey = `
	INSERT INTO
		master_key (id, key)
	VALUES
		(0, $1) ON CONFLICT (id) DO NOTHING
`

func (s *Store) InsertMasterKey(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, insertMasterKey, key); err != nil {
		return err
	}

	return nil
}

const selectMasterKey = `
	SELECT
		key
	FROM
		master_key
	WHERE
		id = 0
`

func (s *Store) QueryMasterKey(ctx context.Context) (string, error) {
	var masterKey string

	err := s.db.QueryRowContext(ctx, selectMasterKey).Scan(&masterKey)
	if err != nil {
		return "", err
	}

	return masterKey, nil
}

//nolint:gosec
const insertSecret = `
	INSERT INTO
		secrets (name, secret)
	VALUES
		($1, $2)
`

func (s *Store) InsertNewSecret(ctx context.Context, name string, secret string) (int64, error) {
	res, err := s.db.ExecContext(ctx, insertSecret, name, secret)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

//nolint:gosec
const upsertNewSecret = `
	INSERT INTO
		secrets (id, secret)
	VALUES
		($1, $2) ON CONFLICT (id) DO
	UPDATE
	SET
		secret = EXCLUDED.secret;
`

func (s *Store) UpsertSecret(ctx context.Context, id string, secret string) (n int64, retErr error) {
	res, err := s.db.ExecContext(ctx, upsertNewSecret, id, secret)
	if err != nil {
		return 0, err
	}

	n, err = res.RowsAffected()
	if err != nil {
		return 0, err
	}

	return n, nil
}

//nolint:gosec
const selectSecret = `
	SELECT
		secret
	FROM
		secrets
	WHERE
		id = $1
`

// secret returns the secret associated with the given secret id.
func (s *Store) Secret(ctx context.Context, id int) (string, error) {
	var secret string

	err := s.db.QueryRowContext(ctx, selectSecret, id).Scan(&secret)
	if err != nil {
		return "", err
	}

	return secret, nil
}

const insertLabel = `
	INSERT INTO
		labels (name, secret_id)
	VALUES
		($1, $2) ON CONFLICT (name, secret_id) DO NOTHING
`

func (s *Store) InsertLabel(ctx context.Context, name string, secretID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, insertLabel, name, secretID)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

// secretLabelRow represents a row resulting from a join
// between the secrets and labels tables.
type secretLabelRow struct {
	id    int
	name  string
	label string
}

// LabeledSecret represents a secret with some of its associated labels.
type LabeledSecret struct {
	Name   string
	Labels []string
}

// SecretsByColumn returns secrets with labels that
// match all glob patterns for the given col.
//
// If no patterns are provided, it returns all secrets along with all their labels.
func (s *Store) SecretsByColumn(ctx context.Context, col string, patterns ...string) (map[int]LabeledSecret, error) {
	query := `
	SELECT
		s.id,
		s.name AS secret_name,
		l.name AS label
	FROM
		secrets s
		JOIN labels l ON s.id = l.secret_id
	` + whereGlobOrClause(col, patterns...)

	return s.secretsJoinLabels(ctx, query, cmdutil.ToAnySlice(patterns)...)
}

// SecretsByIDs returns a map of secrets and their labels for the given IDs.
//
// If the IDs slice is empty, the function returns [ErrNoIDsProvided].
func (s *Store) SecretsByIDs(ctx context.Context, ids []int) (map[int]LabeledSecret, error) {
	if len(ids) == 0 {
		return nil, ErrNoIDsProvided
	}

	placeholders := make([]string, len(ids))
	for i := range ids {
		placeholders[i] = "?"
	}

	query := `
	SELECT
		s.id,
		s.name,
		l.name AS label
	FROM
		secrets s
		JOIN labels l ON s.id = l.secret_id
	WHERE
		s.id IN (` + strings.Join(placeholders, ",") + ")"

	return s.secretsJoinLabels(ctx, query, cmdutil.ToAnySlice(ids)...)
}

// SecretsByLabelsAndName returns secrets with labels that match any of the
// provided label and name glob patterns.
//
// If no label patterns are provided, it returns [ErrNoLabelsProvided].
func (s *Store) SecretsByLabelsAndName(ctx context.Context, name string, labels ...string) (map[int]LabeledSecret, error) {
	if len(labels) == 0 {
		return nil, ErrNoLabelsProvided
	}

	query := `
	SELECT
		s.id,
		s.name AS secret_name,
		l.name AS label
	FROM
		secrets s
		JOIN labels l ON s.id = l.secret_id
	` + whereGlobOrClause("label", labels...) +
		"AND secret_name GLOB ?"

	args := append(cmdutil.ToAnySlice(labels), name)

	return s.secretsJoinLabels(ctx, query, args...)
}

// secretsJoinLabels executes a query to join secrets with their labels.
func (s *Store) secretsJoinLabels(ctx context.Context, query string, args ...any) (map[int]LabeledSecret, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:wsl

	var secrets []secretLabelRow
	for rows.Next() {
		var secret secretLabelRow
		if err := rows.Scan(&secret.id, &secret.name, &secret.label); err != nil {
			return nil, err
		}

		secrets = append(secrets, secret)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return reduce(secrets), nil
}

// whereGlobOrClause generates a WHERE GLOB OR clause
// for the given column and patterns.
func whereGlobOrClause(col string, patterns ...string) string {
	if len(patterns) == 0 {
		return ""
	}

	clauses := make([]string, len(patterns))
	for i := range clauses {
		clauses[i] = col + " GLOB ?"
	}

	return "WHERE " + strings.Join(clauses, " OR ")
}

func reduce(secrets []secretLabelRow) map[int]LabeledSecret {
	m := make(map[int]LabeledSecret)

	for _, s := range secrets {
		v, ok := m[s.id]
		if !ok {
			v = LabeledSecret{
				Name:   s.name,
				Labels: []string{s.label},
			}
		} else {
			v.Labels = append(v.Labels, s.label)
		}

		m[s.id] = v
	}

	return m
}
