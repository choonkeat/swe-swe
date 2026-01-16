# ADR-001: Metadata storage location

**Status**: Accepted
**Date**: 2025-12-24
**Research**: [research/2025-12-24-domain-file-elimination.md](../../research/2025-12-24-domain-file-elimination.md)

## Context
swe-swe needs to store Docker Compose files, Dockerfiles, and configuration outside the user's project to avoid polluting their git repository.

## Decision
Store metadata in `$HOME/.swe-swe/projects/{sanitized-path-hash}/` where:
- `{sanitized-path-hash}` = path with non-alphanumeric chars replaced by hyphens + 8-char MD5 suffix
- A `.path` file records the original absolute path for reverse lookup

## Consequences
Good: Project directory stays clean, multiple projects supported, easy to list/prune stale projects.
Bad: Metadata not versioned with project, `swe-swe list` needed to find projects.
