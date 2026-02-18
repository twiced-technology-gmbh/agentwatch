# agentwatch

A terminal UI for watching AI agents work. AI tools (like Claude) create and move tasks on a Kanban board — you watch the progress in real-time and click a card to jump to the agent's terminal.

Tasks are stored as Markdown files with YAML frontmatter, making everything plaintext and git-friendly.

## Install

### Homebrew

```bash
brew install twiced-technology-gmbh/tap/agentwatch
```

### From source

```bash
go install github.com/twiced-technology-gmbh/agentwatch/cmd/agentwatch@latest
```

### Claude Code hook

Install the hook to have all Claude Code sessions automatically appear on the board:

```bash
npx @toolr/seedr add agentwatch --type hook
```

This works with all Claude instances out of the box — no per-project setup needed. The hook writes to both the global board and a per-project board at `<project-root>/.agents/agentwatch/` when inside a git repository. See [seedr.toolr.dev/hooks/agentwatch](https://seedr.toolr.dev/hooks/agentwatch) for details.

## Quick start

```bash
# Launch the TUI — auto-creates ~/.config/agentwatch on first run
agentwatch
```

## Configuration

The global board lives at `~/.config/agentwatch/config.yml` and is auto-created on first run with these columns:

```
Idle → In Progress → PermissionRequest → Waiting → Finished
```

Edit `~/.config/agentwatch/config.yml` to customize statuses.

### Project boards

You can run a separate board per project using `--dir`:

```bash
agentwatch --dir /path/to/project
```

This resolves to `/path/to/project/.agents/agentwatch/` and auto-creates the board if it doesn't exist. The hook automatically writes to both the global and project board.
