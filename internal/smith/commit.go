package smith

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"

	"github.com/nnutter/spor/internal/refactor"
)

func refactorSubject(item refactor.Refactoring) string {
	text := item.Describe()
	if text == "" {
		return "Refactor:"
	}
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError {
		return "Refactor: " + text
	}
	return "Refactor: " + string(unicode.ToUpper(first)) + text[size:]
}

func commitMessage(subject, sporID, changeID string) string {
	var b strings.Builder
	b.WriteString(subject)
	b.WriteString("\n\n")
	b.WriteString("Spor-Id: ")
	b.WriteString(sporID)
	b.WriteByte('\n')
	b.WriteString("Change-Id: ")
	b.WriteString(changeID)
	b.WriteByte('\n')
	return b.String()
}

func newChangeID(tree, parent plumbing.Hash, sporID, subject string) string {
	hash := sha256.New()
	_, _ = hash.Write(tree.Bytes())
	_, _ = hash.Write(parent.Bytes())
	_, _ = hash.Write([]byte(sporID))
	_, _ = hash.Write([]byte(subject))
	sum := hash.Sum(nil)
	return "I" + hex.EncodeToString(sum)[:40]
}

func writeCommit(source storer.EncodedObjectStorer, tree, parent plumbing.Hash, who identity, message string, when time.Time) (plumbing.Hash, error) {
	sig := object.Signature{Name: who.name, Email: who.email, When: when}
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      message,
		TreeHash:     tree,
		ParentHashes: []plumbing.Hash{parent},
	}
	obj := source.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return source.SetEncodedObject(obj)
}
