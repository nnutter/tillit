# Auditor

- The Auditor can maintain an alternate Git Worktree in order to run verification, etc.
- The Auditor tracks verification status based on Git commit hash, Git patch hash, and Change-Id which will enable the User to chose when to re-verify commits.
- The Auditor can be configured to re-verify at the Git commit hash level, Git patch hash level, or Change-Id level with the option to manually run.
- The Auditor will call a coding agent to create fixup commits for commits that fail verification.
- The Auditor can be configured to halt if the coding agent does not succeed at verification within a specified number of attempts, default to 3 attempts.

## Skills

Load the skills named in ancestor `AGENTS.md` files before working here.

This package adds no further skill.
