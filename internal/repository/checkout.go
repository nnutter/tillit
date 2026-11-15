package repository

import "path/filepath"

// CheckoutDir is spor's checkout for the worktree at gitDir.
// It is under the common git dir, outside .git/worktrees, which Git
// reserves for a worktree's administrative state.
func CheckoutDir(gitDir string) (string, error) {
	common, err := CommonDir(gitDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "spor", "checkouts", worktreeKey(gitDir)), nil
}
