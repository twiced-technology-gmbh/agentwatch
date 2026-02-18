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
agentwatch --dir /path/to/project/.agentwatch
```

Create the board by adding a `config.yml` in that directory with the same format as the global one. Tasks go into a `tasks/` subdirectory next to it.
