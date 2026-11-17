package smith

import (
	"cmp"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type dirNode struct {
	files []object.TreeEntry
	dirs  map[string]*dirNode
}

func newDir() *dirNode {
	return &dirNode{dirs: map[string]*dirNode{}}
}

func storeFiles(source storer.EncodedObjectStorer, files []fileBlob) (plumbing.Hash, error) {
	root := newDir()
	for _, file := range files {
		root.add(file)
	}
	return root.store(source)
}

func (d *dirNode) add(file fileBlob) {
	parent, name := d.place(strings.Split(file.path, "/"))
	parent.files = append(parent.files, object.TreeEntry{
		Name: name,
		Mode: file.mode,
		Hash: file.hash,
	})
}

func (d *dirNode) child(name string) *dirNode {
	next := d.dirs[name]
	if next == nil {
		next = newDir()
		d.dirs[name] = next
	}
	return next
}

func (d *dirNode) entries(source storer.EncodedObjectStorer) ([]object.TreeEntry, error) {
	entries := slices.Clone(d.files)
	for name, child := range d.dirs {
		hash, err := child.store(source)
		if err != nil {
			return nil, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: hash})
	}
	sortEntries(entries)
	return entries, nil
}

func (d *dirNode) place(parts []string) (*dirNode, string) {
	node := d
	for _, part := range parts[:len(parts)-1] {
		node = node.child(part)
	}
	return node, parts[len(parts)-1]
}

func (d *dirNode) store(source storer.EncodedObjectStorer) (plumbing.Hash, error) {
	entries, err := d.entries(source)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return storeTree(source, entries)
}

func sortEntries(entries []object.TreeEntry) {
	slices.SortFunc(entries, func(a, b object.TreeEntry) int {
		return cmp.Compare(entrySortName(a), entrySortName(b))
	})
}

func entrySortName(entry object.TreeEntry) string {
	if entry.Mode == filemode.Dir {
		return entry.Name + "/"
	}
	return entry.Name
}

func storeTree(source storer.EncodedObjectStorer, entries []object.TreeEntry) (plumbing.Hash, error) {
	obj := source.NewEncodedObject()
	tree := &object.Tree{Entries: entries}
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return source.SetEncodedObject(obj)
}

func storeBlob(source storer.EncodedObjectStorer, content []byte) (plumbing.Hash, error) {
	obj := source.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	writer, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	_, err = writer.Write(content)
	cerr := writer.Close()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if cerr != nil {
		return plumbing.ZeroHash, cerr
	}
	return source.SetEncodedObject(obj)
}

func replaceHash(files []fileBlob, path string, hash plumbing.Hash) []fileBlob {
	out := slices.Clone(files)
	for i := range out {
		if out[i].path != path {
			continue
		}
		out[i].hash = hash
		return out
	}
	return out
}
