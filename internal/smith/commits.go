package smith

import (
	"encoding/hex"
	"uuid"
)

// Commit is one commit written to the spor ref.
type Commit struct {
	Hash    string
	Subject string
}

func newSporID() string {
	id := uuid.New()
	return "S" + hex.EncodeToString(id[:])
}

func toCommits(made []madeCommit) []Commit {
	out := make([]Commit, len(made))
	for i, commit := range made {
		out[i] = Commit{Hash: commit.hash.String(), Subject: commit.subject}
	}
	return out
}
