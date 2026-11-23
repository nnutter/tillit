package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// EnsureNoPendingIntegration prevents mutation while an interrupted integration needs recovery.
func EnsureNoPendingIntegration(gitDir string) error {
	path := filepath.Join(gitDir, "spor", "state.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var exists int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='mender_state'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	var branch string
	err = db.QueryRow(`SELECT branch FROM mender_state WHERE id=1`).Scan(&branch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if branch != "" {
		return fmt.Errorf("pending integration needs recovery; use a previous Spor version or resolve it safely before continuing")
	}
	return nil
}

// Boundary returns the last integrated Spor commit, if one is recorded.
func Boundary(gitDir string) (string, error) {
	path := filepath.Join(gitDir, "spor", "state.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	var exists int
	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'mender_state'`).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists == 0 {
		return "", nil
	}
	var boundary string
	err = db.QueryRow(`SELECT boundary FROM mender_state WHERE id = 1`).Scan(&boundary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return boundary, err
}
