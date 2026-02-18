// Package cmd implements the agentwatch CLI commands.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/twiced-technology-gmbh/agentwatch/internal/board"
	"github.com/twiced-technology-gmbh/agentwatch/internal/clierr"
	"github.com/twiced-technology-gmbh/agentwatch/internal/config"
	"github.com/twiced-technology-gmbh/agentwatch/internal/output"
	"github.com/twiced-technology-gmbh/agentwatch/internal/task"
)

// version is set at build time via ldflags.
var version = "dev"

// Global flags.
var (
	flagJSON    bool
	flagTable   bool
	flagCompact bool
	flagDir     string
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:   "agentwatch",
	Short: "Terminal UI for watching AI agents work",
	Long: `agentwatch displays a live Kanban board showing what your AI agents are doing.
Just run agentwatch to open the TUI. AI tools create and move cards via hooks.`,
	Version:       version,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          runTUI,
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		if flagNoColor || os.Getenv("NO_COLOR") != "" {
			output.DisableColor()
		}
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "output as JSON")
	rootCmd.PersistentFlags().BoolVar(&flagTable, "table", false, "output as table")
	rootCmd.PersistentFlags().BoolVar(&flagCompact, "compact", false, "compact one-line-per-record output")
	rootCmd.PersistentFlags().BoolVar(&flagCompact, "oneline", false, "alias for --compact")
	rootCmd.PersistentFlags().StringVar(&flagDir, "dir", "", "path to kanban directory")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable color output")
}

// Execute runs the root command.
func Execute() {
	_, err := rootCmd.ExecuteC()
	if err == nil {
		return
	}

	// Handle SilentError — exit with code, no output.
	var silent *clierr.SilentError
	if errors.As(err, &silent) {
		os.Exit(silent.Code)
	}

	// Determine if JSON mode is active.
	jsonMode := flagJSON
	if !jsonMode {
		jsonMode = os.Getenv("KANBAN_OUTPUT") == "json"
	}

	if jsonMode {
		var cliErr *clierr.Error
		if errors.As(err, &cliErr) {
			output.JSONError(os.Stdout, cliErr.Code, cliErr.Message, cliErr.Details)
			os.Exit(cliErr.ExitCode())
		}
		// Unknown error — wrap as INTERNAL_ERROR.
		output.JSONError(os.Stdout, clierr.InternalError, err.Error(), nil)
		os.Exit(2) //nolint:mnd // exit code 2 for internal errors
	}

	// Non-JSON mode: print to stderr.
	fmt.Fprintln(os.Stderr, err)
	var cliErr *clierr.Error
	if errors.As(err, &cliErr) {
		os.Exit(cliErr.ExitCode())
	}
	os.Exit(1)
}

// defaultHomeDir returns the path to ~/.config/agentwatch.
func defaultHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config/agentwatch"), nil
}

// resolveDir returns the absolute path to the kanban directory.
// Falls back to ~/.config/agentwatch if no board is found in the current directory tree.
func resolveDir() (string, error) {
	if flagDir != "" {
		return flagDir, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	dir, err := config.FindDir(cwd)
	if err == nil {
		return dir, nil
	}

	// Fall back to ~/.config/agentwatch.
	return defaultHomeDir()
}

// loadConfig finds and loads the kanban config.
// If the resolved directory is ~/.config/agentwatch and it doesn't exist yet,
// it is auto-created with default agent statuses.
func loadConfig() (*config.Config, error) {
	dir, err := resolveDir()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(dir)
	if err == nil {
		return cfg, nil
	}

	// Auto-create ~/.config/agentwatch if it's the home default and doesn't exist.
	if !errors.Is(err, config.ErrNotFound) {
		return nil, err
	}
	homeDir, homeErr := defaultHomeDir()
	if homeErr != nil || dir != homeDir {
		return nil, err
	}

	return config.InitAgent(homeDir)
}

// outputFormat returns the detected output format from flags/env.
func outputFormat() output.Format {
	return output.Detect(flagJSON, flagTable, flagCompact)
}

// printWarnings writes task read warnings to stderr.
func printWarnings(warnings []task.ReadWarning) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: skipping malformed file %s: %v\n", w.File, w.Err)
	}
}

// validateDepIDs checks that all dependency IDs exist and none are self-referencing.
func validateDepIDs(tasksDir string, selfID int, ids []int) error {
	return task.ValidateDependencyIDs(tasksDir, selfID, ids)
}

// checkWIPLimit verifies that adding a task to targetStatus would not exceed
// the WIP limit. currentTaskStatus is the task's current status (empty for new tasks).
func checkWIPLimit(cfg *config.Config, statusCounts map[string]int, targetStatus, currentTaskStatus string) error {
	return board.CheckWIPLimit(cfg, statusCounts, targetStatus, currentTaskStatus)
}

// logActivity appends an entry to the activity log. Errors are silently
// discarded because logging should never fail a command.
func logActivity(cfg *config.Config, action string, taskID int, detail string) {
	board.LogMutation(cfg.Dir(), action, taskID, detail)
}

// checkClaim verifies that a mutating operation is allowed on a claimed task.
func checkClaim(t *task.Task, claimant string, timeout time.Duration) error {
	return task.CheckClaim(t, claimant, timeout)
}

// validateDeps validates parent and dependency references for a task.
func validateDeps(cfg *config.Config, t *task.Task) error {
	if t.Parent != nil {
		if err := validateDepIDs(cfg.TasksPath(), t.ID, []int{*t.Parent}); err != nil {
			return fmt.Errorf("invalid parent: %w", err)
		}
	}
	if len(t.DependsOn) > 0 {
		if err := validateDepIDs(cfg.TasksPath(), t.ID, t.DependsOn); err != nil {
			return err
		}
	}
	return nil
}

// parseIDs splits a comma-separated ID string into deduplicated int IDs.
func parseIDs(arg string) ([]int, error) {
	return board.ParseIDs(arg)
}

// runBatch executes fn for each ID and collects results. Returns a SilentError
// with exit code 1 if any operation failed (after outputting results).
func runBatch(ids []int, fn func(int) error) error {
	results := make([]output.BatchResult, 0, len(ids))
	anyFailed := false

	for _, id := range ids {
		err := fn(id)
		if err != nil {
			anyFailed = true
			var cliErr *clierr.Error
			if errors.As(err, &cliErr) {
				results = append(results, output.BatchResult{ID: id, OK: false, Error: cliErr.Message, Code: cliErr.Code})
			} else {
				results = append(results, output.BatchResult{ID: id, OK: false, Error: err.Error()})
			}
		} else {
			results = append(results, output.BatchResult{ID: id, OK: true})
		}
	}

	if outputFormat() == output.FormatJSON {
		if err := output.JSON(os.Stdout, results); err != nil {
			return err
		}
	} else {
		var succeeded int
		for _, r := range results {
			if r.OK {
				succeeded++
			} else {
				fmt.Fprintf(os.Stderr, "Error: task #%d: %s\n", r.ID, r.Error)
			}
		}
		output.Messagef(os.Stdout, "Completed %d/%d operations", succeeded, len(ids))
	}

	if anyFailed {
		return &clierr.SilentError{Code: 1}
	}
	return nil
}
