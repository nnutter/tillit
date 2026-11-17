package smith

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
)

// latestUndoable excludes submissions retired by an integration barrier.
func latestUndoable(ctx context.Context, gitDir string) (submission, bool, bool, error) {
	db, err := openState(ctx, gitDir)
	if err != nil {
		return submission{}, false, false, err
	}
	defer func() { _ = db.Close() }()
	var item submission
	var body string
	var previous sql.NullString
	err = db.QueryRowContext(ctx, `SELECT s.id, s.ref, s.outcome, s.commits, p.tip FROM submission s LEFT JOIN submission_previous p ON p.id=s.id WHERE NOT EXISTS (SELECT 1 FROM mender_retired_submission r WHERE r.id=s.id) ORDER BY s.created DESC, s.rowid DESC LIMIT 1`).Scan(&item.id, &item.ref, &item.outcome, &body, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return submission{}, false, false, nil
	}
	if err != nil {
		return submission{}, false, false, err
	}
	if err := json.Unmarshal([]byte(body), &item.commits); err != nil {
		return submission{}, false, false, err
	}
	item.previous = previous.String
	return item, true, previous.Valid, nil
}
