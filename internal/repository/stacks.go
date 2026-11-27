package repository

import (
	"context"

	"github.com/nnutter/commitstack"
)

// SnapshotStacks selects the commit stacks on the current branch between its
// upstream and its tip. An original commit and its fixup!, reword!, and amend!
// commits form one stack. start must be in the user's worktree, not Tillit's
// checkout.
func SnapshotStacks(ctx context.Context, start string) (commitstack.Snapshot, error) {
	return commitstack.New(start, nil).Snapshot(ctx)
}
