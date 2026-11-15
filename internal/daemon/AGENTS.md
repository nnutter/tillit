# Daemon

- The Spor daemon will run in the background and manage the Spor process for a given Git worktree.
- The Spor daemon will watch the SQLite database for new changes and process them as they are submitted (by the user having run `spor submit`).
- The Spor daemon will call the Smith to extract commits from the submitted changes and maintain an isolated Git Index to build commits.
- The Spor daemon will call the Auditor to verify commits and make fixup commits for any that fail verification.
- The Spor daemon will call the Mender to incorporate Spor commits back into the User's branch/worktree when the User runs `spor rebase`.
