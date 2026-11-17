package smith

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type fileBlob struct {
	path string
	mode filemode.FileMode
	hash plumbing.Hash
}

func loadFiles(repo *git.Repository, head plumbing.Hash) ([]fileBlob, []fileBlob, error) {
	commit, err := repo.CommitObject(head)
	if err != nil {
		return nil, nil, err
	}
	headFiles, err := treeFiles(repo.Storer, commit.TreeHash)
	if err != nil {
		return nil, nil, err
	}
	indexFiles, err := readIndex(repo)
	if err != nil {
		return nil, nil, err
	}
	return headFiles, indexFiles, nil
}

func treeFiles(source storer.EncodedObjectStorer, hash plumbing.Hash) ([]fileBlob, error) {
	tree, err := object.GetTree(source, hash)
	if err != nil {
		return nil, err
	}
	return walkTree(source, tree, "")
}

func walkTree(source storer.EncodedObjectStorer, tree *object.Tree, prefix string) ([]fileBlob, error) {
	var files []fileBlob
	for _, entry := range tree.Entries {
		path := joinPath(prefix, entry.Name)
		if entry.Mode == filemode.Dir {
			nested, err := treeFiles(source, entry.Hash)
			if err != nil {
				return nil, err
			}
			files = append(files, prefixFiles(path, nested)...)
			continue
		}
		files = append(files, fileBlob{path: path, mode: entry.Mode, hash: entry.Hash})
	}
	return files, nil
}

func prefixFiles(prefix string, files []fileBlob) []fileBlob {
	out := make([]fileBlob, len(files))
	for i, file := range files {
		out[i] = file
		out[i].path = joinPath(prefix, file.path)
	}
	return out
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func readIndex(repo *git.Repository) ([]fileBlob, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}
	return indexFiles(idx)
}

func indexFiles(idx *index.Index) ([]fileBlob, error) {
	if idx == nil {
		return nil, nil
	}
	var files []fileBlob
	for _, entry := range idx.Entries {
		file, skip, err := indexFile(entry)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

func indexFile(entry *index.Entry) (fileBlob, bool, error) {
	if entry.Stage != 0 {
		return fileBlob{}, false, fmt.Errorf("index has a conflict at %s", entry.Name)
	}
	if entry.IntentToAdd {
		return fileBlob{}, true, nil
	}
	if entry.Hash.IsZero() {
		return fileBlob{}, true, nil
	}
	return fileBlob{path: entry.Name, mode: entry.Mode, hash: entry.Hash}, false, nil
}

func changedPaths(head, index []fileBlob) []string {
	left := byPath(head)
	right := byPath(index)
	set := map[string]struct{}{}
	markChanged(set, left, right)
	markChanged(set, right, left)
	paths := pathKeys(set)
	slices.Sort(paths)
	return paths
}

func pathKeys(set map[string]struct{}) []string {
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	return paths
}

func markChanged(set map[string]struct{}, left, right map[string]fileBlob) {
	for path, file := range left {
		if fileDiffers(file, right) {
			set[path] = struct{}{}
		}
	}
}

func fileDiffers(file fileBlob, right map[string]fileBlob) bool {
	other, ok := right[file.path]
	if !ok {
		return true
	}
	if other.mode != file.mode {
		return true
	}
	return !other.hash.Equal(file.hash)
}

func byPath(files []fileBlob) map[string]fileBlob {
	out := make(map[string]fileBlob, len(files))
	for _, file := range files {
		out[file.path] = file
	}
	return out
}

func readBlob(source storer.EncodedObjectStorer, hash plumbing.Hash) ([]byte, error) {
	blob, err := object.GetBlob(source, hash)
	if err != nil {
		return nil, err
	}
	reader, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	cerr := reader.Close()
	if err != nil {
		return nil, err
	}
	if cerr != nil {
		return nil, cerr
	}
	return data, nil
}

func goFile(path string) bool {
	return strings.HasSuffix(path, ".go")
}
