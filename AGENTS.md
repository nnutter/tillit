# Spor

- Spor should track application state in a SQLite database in the Git worktree's git-dir, under a spor subfolder.
- Spor should use `pi` to call a coding agent for specified coding agent tasks using a configured provider/model.
- Spor configuration should follow a hierarchy:
  - Global configuration in `~/.config/spor/config.json`
  - Repository configuration in `spor/config.json` in the Git worktree's git-common-dir (that's the base worktree's git-dir, right?)
  - Worktree configuration in `spor/config.json` in the Git worktree's git-dir
