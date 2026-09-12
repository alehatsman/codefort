# AGENTS.md

This repo's agent instructions live in **[`CLAUDE.md`](CLAUDE.md)** — commands,
docs map, and house rules. Read it before doing anything. Architecture overview:
[`docs/architecture.md`](docs/architecture.md).

Two rules you must not get wrong, even if you read nothing else:

1. **Work is tracked as codefort issues, and you claim before you code.**
   Survey with `cf issue list --state todo,in_progress`, create an issue for
   the unit of work, then `cf issue claim <n> --state in_progress`. Never
   work an issue already `in_progress` under another identity. No code without
   an owned issue.
2. **Never auto-push to `main`.** Branch, keep the work local, and ask before
   merging.
