# Daemon

- The daemon manages the watcher for one Git worktree.
- It processes captured snapshots and publishes validated refactorings without writing working files.
- It must preserve legacy state and refuse publication when an interrupted integration or pending manual commits require recovery.

## Skills

Load the skills named in ancestor `AGENTS.md` files before working here.
