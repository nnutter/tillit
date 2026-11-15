# Smith

- This can probably build on [intent](https://github.com/nnutter/intent) (private repo, checked out in repo root at intent/).
  However, I don't think its very robust.
  I'd rather duplicate the code over than call out to `intent`.
- The Smith receives a patch/diff with an assigned Spor-Id which propagates to commits it extracts from the path/diff.
- The Smith can maintain an isolated Git Index in order to build commits.
