# Daemon

- The Spor daemon will run in the background and manage the Spor process for a given Git worktree.
- The Spor daemon will watch the SQLite database for new changes and process them as they are added (by the user having run `spor add`).
- The Spor daemon will call the Smith to extract commits from the added changes and maintain an isolated Git Index to build commits.
- The Spor daemon will call the Auditor to verify commits and make fixup commits for any that fail verification.
- The Spor daemon will call the Mender to incorporate Spor commits back into the User's branch/worktree when the User runs `spor rebase`.

## Skills

Load the skills named in ancestor `AGENTS.md` files before working here.

When changing how this package calls the Smith, the Auditor, or the Mender, also load the skills named in that package's `AGENTS.md`.

This package adds no further skill.
