package repolink

import (
	"database/sql"
	"errors"
)

var errNoRepo = errors.New("no repository linked")

// ErrNoRepo reports whether err means the project has no linked repo.
func ErrNoRepo(err error) bool { return errors.Is(err, errNoRepo) }

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
