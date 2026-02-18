# agentwatch

A terminal UI for watching AI agents work. AI tools (like Claude) create and move tasks on a Kanban board — you watch the progress in real-time and click a card to jump to the agent's terminal.

Tasks are stored as Markdown files with YAML frontmatter, making everything plaintext and git-friendly.

## Install

```bash
go install github.com/twiced-technology-gmbh/agentwatch/cmd/agentwatch@latest
```

## Quick start

```bash
# Initialize a board in the current directory
agentwatch init

# Launch the TUI
agentwatch tui
```

## CLI commands

```
agentwatch init          Create a new board
agentwatch tui           Launch the interactive terminal UI
agentwatch list          List tasks (with filters, sorting, grouping)
agentwatch board         Show board summary
agentwatch create TITLE  Create a task
agentwatch move ID       Move a task to a status
agentwatch show ID       Show task details
agentwatch edit ID       Edit a task in $EDITOR
agentwatch delete ID     Delete a task
```

Use `--json` on any command for machine-readable output.

## Requirements

Go 1.25+
