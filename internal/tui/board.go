// Package tui implements a terminal UI for agentwatch boards.
package tui

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/twiced-technology-gmbh/agentwatch/internal/board"
	"github.com/twiced-technology-gmbh/agentwatch/internal/config"
	"github.com/twiced-technology-gmbh/agentwatch/internal/task"
)

// view represents the current screen state.
type view int

const (
	viewBoard view = iota
	viewConfirmDelete
	viewConfirmClearAll
)

// Key and layout constants.
const (
	keyEsc = "esc"

	tagMaxFraction = 2 // tags get at most 1/N of card width
	boardChrome    = 2 // blank line + status bar below the column area
	errorChrome    = 1 // extra line when error toast is displayed
	tickInterval   = 30 * time.Second // how often durations refresh
)

// Board is the top-level bubbletea model.
type Board struct {
	cfg       *config.Config
	tasks     []*task.Task
	columns   []column
	activeCol int
	activeRow int
	view      view
	width     int
	height    int
	err       error
	now       func() time.Time // clock for duration display; defaults to time.Now

	// Delete confirmation.
	deleteID    int
	deleteTitle string

	// Clear all confirmation.
	clearAllCount int

	// Double-click tracking for iTerm2 focus.
	lastClickCol  int
	lastClickRow  int
	lastClickTime time.Time

	// Per-title sequence numbers for distinguishing duplicate branches.
	titleSeq map[int]int
}

// column groups tasks belonging to a single status.
type column struct {
	status    string
	tasks     []*task.Task
	scrollOff int // first visible row index
}

// NewBoard creates a new Board model from a config.
func NewBoard(cfg *config.Config) *Board {
	b := &Board{cfg: cfg, now: time.Now}
	b.loadTasks()
	return b
}

// SetNow overrides the clock function used for duration display (for testing).
func (b *Board) SetNow(fn func() time.Time) {
	b.now = fn
}

// Init implements tea.Model.
func (b *Board) Init() tea.Cmd {
	return tickCmd()
}

// Update implements tea.Model.
func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return b.handleKey(msg)
	case tea.MouseMsg:
		return b.handleMouse(msg)
	case tea.WindowSizeMsg:
		b.width = msg.Width
		b.height = msg.Height
		return b, nil
	case ReloadMsg:
		b.loadTasks()
		return b, nil
	case TickMsg:
		return b, tickCmd()
	case errMsg:
		b.err = msg.err
		return b, nil
	}
	return b, nil
}

// View implements tea.Model.
func (b *Board) View() string {
	if b.width == 0 {
		return "Loading..."
	}

	switch b.view {
	case viewConfirmDelete:
		return b.viewDeleteConfirm()
	case viewConfirmClearAll:
		return b.viewClearAllConfirm()
	default:
		return b.viewBoard()
	}
}

func (b *Board) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys.
	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
		return b, tea.Quit
	}

	switch b.view {
	case viewBoard:
		return b.handleBoardKey(msg)
	case viewConfirmDelete:
		return b.handleDeleteKey(msg)
	case viewConfirmClearAll:
		return b.handleClearAllKey(msg)
	}

	return b, nil
}

func (b *Board) handleBoardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", keyEsc:
		return b, tea.Quit
	case "h", "left":
		if b.activeCol > 0 {
			b.activeCol--
			b.clampRow()
		}
	case "l", "right":
		if b.activeCol < len(b.columns)-1 {
			b.activeCol++
			b.clampRow()
		}
	case "j", "down":
		col := b.currentColumn()
		if col != nil && b.activeRow < len(col.tasks)-1 {
			b.activeRow++
			b.ensureVisible()
		}
	case "k", "up":
		if b.activeRow > 0 {
			b.activeRow--
			b.ensureVisible()
		}
	case "C":
		b.handleClearAllStart()
	case "d", "D":
		b.handleDeleteStart()
	case "enter":
		b.focusITermPane()
	}
	return b, nil
}

func (b *Board) handleDeleteStart() {
	if t := b.selectedTask(); t != nil {
		b.deleteID = t.ID
		b.deleteTitle = t.Title
		b.view = viewConfirmDelete
	}
}

func (b *Board) handleClearAllStart() {
	b.clearAllCount = len(b.tasks)
	if b.clearAllCount > 0 {
		b.view = viewConfirmClearAll
	}
}

func (b *Board) handleClearAllKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return b.executeClearAll()
	case "n", "N", keyEsc, "q":
		b.view = viewBoard
	}
	return b, nil
}

func (b *Board) executeClearAll() (tea.Model, tea.Cmd) {
	tasks, _, err := task.ReadAllLenient(b.cfg.TasksPath())
	if err != nil {
		b.err = fmt.Errorf("reading tasks: %w", err)
		b.view = viewBoard
		return b, nil
	}
	for _, t := range tasks {
		if b.cfg.IsArchivedStatus(t.Status) {
			continue
		}
		t.Status = config.ArchivedStatus
		t.Updated = b.now()
		_ = task.Write(t.File, t)
	}
	board.LogMutation(b.cfg.Dir(), "clear-all", 0, "")
	b.view = viewBoard
	b.loadTasks()
	return b, nil
}

// handleMouse handles mouse click events for card selection.
func (b *Board) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return b, nil
	}
	if b.view != viewBoard {
		return b, nil
	}

	colWidth := b.columnWidth()
	clickedCol := msg.X / colWidth
	if clickedCol >= len(b.columns) {
		return b, nil
	}

	col := &b.columns[clickedCol]
	lineY := msg.Y - 1
	if lineY < 0 {
		b.activeCol = clickedCol
		b.clampRow()
		return b, nil
	}

	clickedRow := -1
	cardLine := 0
	for rowIdx := col.scrollOff; rowIdx < len(col.tasks); rowIdx++ {
		cardH := b.cardHeight(col.tasks[rowIdx], colWidth)
		if lineY < cardLine+cardH {
			clickedRow = rowIdx
			break
		}
		cardLine += cardH
	}

	if clickedRow < 0 {
		b.activeCol = clickedCol
		b.clampRow()
		return b, nil
	}

	// Detect double-click: same card within 500ms.
	now := b.now()
	isDoubleClick := clickedCol == b.lastClickCol &&
		clickedRow == b.lastClickRow &&
		now.Sub(b.lastClickTime) < 500*time.Millisecond

	b.activeCol = clickedCol
	b.activeRow = clickedRow
	b.lastClickCol = clickedCol
	b.lastClickRow = clickedRow
	b.lastClickTime = now
	b.ensureVisible()

	if isDoubleClick {
		b.focusITermPane()
	}

	return b, nil
}

// focusITermPane reads the iTerm2 session ID stored by the hook and activates
// the corresponding pane via AppleScript.
func (b *Board) focusITermPane() {
	t := b.selectedTask()
	if t == nil {
		return
	}

	itermFile := filepath.Join(b.cfg.Dir(), ".sessions", fmt.Sprintf("%d.iterm", t.ID))
	data, err := os.ReadFile(itermFile)
	if err != nil || len(data) == 0 {
		return
	}

	// ITERM_SESSION_ID format: "w0t3p0:XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
	// AppleScript uses just the UUID part.
	raw := strings.TrimSpace(string(data))
	sessionID := raw
	if idx := strings.Index(raw, ":"); idx >= 0 {
		sessionID = raw[idx+1:]
	}

	script := fmt.Sprintf(`
tell application "iTerm2"
  activate
  repeat with w in windows
    repeat with t in tabs of w
      repeat with s in sessions of t
        if id of s is "%s" then
          tell t to select
          tell s to select
          set index of w to 1
          return
        end if
      end repeat
    end repeat
  end repeat
end tell`, sessionID)

	_ = exec.Command("osascript", "-e", script).Start()
}

func (b *Board) handleDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return b.executeDelete()
	case "n", "N", keyEsc, "q":
		b.view = viewBoard
	}
	return b, nil
}

// loadTasks reads all tasks and organizes them into columns.
func (b *Board) loadTasks() {
	tasks, _, err := task.ReadAllLenient(b.cfg.TasksPath())
	if err != nil {
		b.err = err
		return
	}
	b.err = nil

	// Filter out archived tasks from TUI display.
	var visibleTasks []*task.Task
	for _, t := range tasks {
		if !b.cfg.IsArchivedStatus(t.Status) {
			visibleTasks = append(visibleTasks, t)
		}
	}
	b.tasks = visibleTasks

	// Sort tasks by priority (higher priority first).
	board.Sort(visibleTasks, "priority", true, b.cfg)

	// Build columns from board statuses (excludes archived).
	displayStatuses := b.cfg.BoardStatuses()
	b.columns = make([]column, len(displayStatuses))
	for i, status := range displayStatuses {
		b.columns[i] = column{status: status}
	}

	for _, t := range visibleTasks {
		for i := range b.columns {
			if b.columns[i].status == t.Status {
				b.columns[i].tasks = append(b.columns[i].tasks, t)
				break
			}
		}
	}

	// Compute per-title sequence numbers from column-assigned tasks only.
	titleCount := make(map[string]int)
	for i := range b.columns {
		for _, t := range b.columns[i].tasks {
			titleCount[t.Title]++
		}
	}
	b.titleSeq = make(map[int]int)
	titleNext := make(map[string]int)
	for i := range b.columns {
		for _, t := range b.columns[i].tasks {
			if titleCount[t.Title] > 1 {
				titleNext[t.Title]++
				b.titleSeq[t.ID] = titleNext[t.Title]
			}
		}
	}

	b.clampRow()
}

func (b *Board) currentColumn() *column {
	if b.activeCol >= 0 && b.activeCol < len(b.columns) {
		return &b.columns[b.activeCol]
	}
	return nil
}

func (b *Board) selectedTask() *task.Task {
	col := b.currentColumn()
	if col == nil || len(col.tasks) == 0 {
		return nil
	}
	if b.activeRow >= 0 && b.activeRow < len(col.tasks) {
		return col.tasks[b.activeRow]
	}
	return nil
}

func (b *Board) clampRow() {
	col := b.currentColumn()
	if col == nil || len(col.tasks) == 0 {
		b.activeRow = 0
		return
	}
	if b.activeRow >= len(col.tasks) {
		b.activeRow = len(col.tasks) - 1
	}
	b.ensureVisible()
}

// chromeHeight returns the number of lines consumed by non-card elements below
// the column area: blank line + status bar (+ error line when an error is shown).
func (b *Board) chromeHeight() int {
	h := boardChrome
	if b.err != nil {
		h += errorChrome
	}
	return h
}

// visibleCardsForColumn returns the number of cards that fit in the column,
// accounting for scroll indicator lines ("↑ N more" / "↓ N more") that
// consume vertical space.
func (b *Board) visibleCardsForColumn(col *column, width int) int {
	budget := b.height - b.chromeHeight()
	if budget < 1 {
		return 1
	}

	// Always need 1 line for column header.
	avail := budget - 1

	// Check if up indicator is needed.
	if col.scrollOff > 0 {
		avail--
	}

	// Compute cards assuming no down indicator.
	n := b.fitCardsInHeight(col, avail, width)

	// Check if down indicator is needed.
	if col.scrollOff+n < len(col.tasks) {
		// Re-compute with 1 fewer line for the down indicator.
		n = b.fitCardsInHeight(col, avail-1, width)
		if n < 1 {
			n = 1
		}
	}

	return n
}

// ensureVisible adjusts the active column's scroll offset so the
// selected row is within the visible window.
func (b *Board) ensureVisible() {
	col := b.currentColumn()
	if col == nil {
		return
	}
	w := b.columnWidth()

	for range len(col.tasks) + 1 {
		maxVis := b.visibleCardsForColumn(col, w)

		switch {
		case b.activeRow >= col.scrollOff+maxVis:
			// Scroll down: selected row is below visible window.
			col.scrollOff = b.activeRow - maxVis + 1
		case b.activeRow < col.scrollOff:
			// Scroll up: selected row is above visible window.
			col.scrollOff = b.activeRow
		default:
			return // selected row is visible
		}
	}
}

func (b *Board) fitCardsInHeight(col *column, avail, width int) int {
	if len(col.tasks) == 0 {
		return 1
	}
	if avail < 1 {
		return 1
	}

	used := 0
	count := 0
	for i := col.scrollOff; i < len(col.tasks); i++ {
		cardLines := b.cardHeight(col.tasks[i], width)
		if count > 0 && used+cardLines > avail {
			break
		}
		count++
		used += cardLines
		if used >= avail {
			break
		}
	}

	if count < 1 {
		return 1
	}
	return count
}

func (b *Board) executeDelete() (tea.Model, tea.Cmd) {
	path, err := task.FindByID(b.cfg.TasksPath(), b.deleteID)
	if err != nil {
		b.err = fmt.Errorf("finding task #%d: %w", b.deleteID, err)
		b.view = viewBoard
		return b, nil
	}

	t, err := task.Read(path)
	if err != nil {
		b.err = fmt.Errorf("reading task #%d: %w", b.deleteID, err)
		b.view = viewBoard
		return b, nil
	}

	if t.Status != config.ArchivedStatus {
		oldStatus := t.Status
		t.Status = config.ArchivedStatus
		task.UpdateTimestamps(t, oldStatus, t.Status, b.cfg)
		t.Updated = b.now()
	}

	if err := task.Write(path, t); err != nil {
		b.err = fmt.Errorf("archiving task #%d: %w", b.deleteID, err)
	} else {
		board.LogMutation(b.cfg.Dir(), "delete", b.deleteID, b.deleteTitle)
	}

	b.view = viewBoard
	b.loadTasks()
	return b, nil
}

// WatchPaths returns the paths that should be watched for file changes.
func (b *Board) WatchPaths() []string {
	paths := []string{b.cfg.TasksPath()}
	if b.cfg.Dir() != b.cfg.TasksPath() {
		paths = append(paths, b.cfg.Dir())
	}
	return paths
}

// --- Messages ---

// ReloadMsg is sent by the file watcher to trigger a board refresh.
type ReloadMsg struct{}

type errMsg struct{ err error }

// TickMsg is sent periodically to refresh duration displays.
type TickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return TickMsg{} })
}

// --- Styles ---

var (
	columnHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("236")).
				Padding(0, 1)

	activeColumnHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("62")).
				Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1).
			MarginBottom(0)

	activeCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("226")).
			Padding(0, 1).
			MarginBottom(0)

	blockedCardStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("196")).
				Padding(0, 1).
				MarginBottom(0)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// tagColorPalette is a set of distinct, readable terminal colors for auto-coloring tags.
	tagColorPalette = []lipgloss.Color{"33", "36", "35", "32", "91", "34", "93", "96"}

	// toolStyle is for the active tool line — subtler than full cyan.
	toolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("66"))

	dialogPadY = 1
	dialogPadX = 2

	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(dialogPadY, dialogPadX)
)

// tagStyle returns a consistent lipgloss style for a tag, derived by hashing
// the tag name into the tagColorPalette. Same tag always gets the same color.
func tagStyle(tag string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tag))
	color := tagColorPalette[h.Sum32()%uint32(len(tagColorPalette))]
	return lipgloss.NewStyle().Foreground(color)
}

// ageStyle returns a lipgloss style for the duration label based on the
// configured age thresholds. Thresholds are walked in reverse order (longest
// first) so the first match wins.
func (b *Board) ageStyle(d time.Duration) lipgloss.Style {
	thresholds := b.cfg.AgeThresholdsDuration()
	// Walk backwards: pick the highest threshold that the duration exceeds.
	for i := len(thresholds) - 1; i >= 0; i-- {
		if d >= thresholds[i].After {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(thresholds[i].Color))
		}
	}
	return dimStyle
}

// --- View rendering ---

func (b *Board) viewBoard() string {
	if len(b.columns) == 0 {
		return "No statuses configured."
	}

	// Calculate column width.
	colWidth := b.columnWidth()

	// Render columns.
	renderedCols := make([]string, len(b.columns))
	for i, col := range b.columns {
		renderedCols[i] = b.renderColumn(i, col, colWidth)
	}

	boardView := lipgloss.JoinHorizontal(lipgloss.Top, renderedCols...)

	// Ensure the board view fits within the available height. At very small
	// terminal sizes, a single card can exceed the budget. Clamp from the
	// bottom (keeping headers at the top) and pad if needed.
	targetHeight := b.height - b.chromeHeight()
	if targetHeight > 0 {
		actual := strings.Count(boardView, "\n") + 1
		if actual > targetHeight {
			viewLines := strings.SplitN(boardView, "\n", targetHeight+1)
			boardView = strings.Join(viewLines[:targetHeight], "\n")
		} else if actual < targetHeight {
			boardView += strings.Repeat("\n", targetHeight-actual)
		}
	}

	statusBar := b.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, boardView, "", statusBar)
}

func (b *Board) columnWidth() int {
	if b.width == 0 || len(b.columns) == 0 {
		return 30 //nolint:mnd // default column width
	}
	// Total rendered width = w * numColumns (JoinHorizontal adds no gaps).
	w := b.width / len(b.columns)
	const maxColWidth = 75
	if w > maxColWidth {
		w = maxColWidth
	}
	return w
}

func (b *Board) renderColumn(colIdx int, col column, width int) string {
	// Header.
	headerText := fmt.Sprintf("%s (%d)", col.status, len(col.tasks))
	wip := b.cfg.WIPLimit(col.status)
	if wip > 0 {
		headerText = fmt.Sprintf("%s (%d/%d)", col.status, len(col.tasks), wip)
	}
	// Truncate to fit within padding (1 left + 1 right).
	const headerPad = 2
	headerText = truncate(headerText, width-headerPad)

	var header string
	if colIdx == b.activeCol {
		header = activeColumnHeaderStyle.Width(width).Render(headerText)
	} else {
		header = columnHeaderStyle.Width(width).Render(headerText)
	}

	// Determine visible card range.
	maxVis := b.visibleCardsForColumn(&col, width)
	start := col.scrollOff
	end := start + maxVis
	if end > len(col.tasks) {
		end = len(col.tasks)
	}
	if start > len(col.tasks) {
		start = len(col.tasks)
	}

	parts := []string{header}

	// Show "↑ N more" indicator if scrolled down.
	if start > 0 {
		indicator := fmt.Sprintf("  ↑ %d more", start)
		parts = append(parts, dimStyle.Width(width).Render(truncate(indicator, width)))
	}

	// Render visible cards.
	if len(col.tasks) == 0 {
		parts = append(parts, dimStyle.Width(width).Render("  (empty)"))
	} else {
		for rowIdx := start; rowIdx < end; rowIdx++ {
			t := col.tasks[rowIdx]
			active := colIdx == b.activeCol && rowIdx == b.activeRow
			parts = append(parts, b.renderCard(t, active, width))
		}
	}

	// Show "↓ N more" indicator if more cards below.
	if end < len(col.tasks) {
		remaining := len(col.tasks) - end
		indicator := fmt.Sprintf("  ↓ %d more", remaining)
		parts = append(parts, dimStyle.Width(width).Render(truncate(indicator, width)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (b *Board) renderCard(t *task.Task, active bool, width int) string {
	contentLines := b.cardContentLines(t, width)
	content := strings.Join(contentLines, "\n")

	// Border color follows the tag color (project color for global, branch color for project).
	style := cardStyle
	if len(t.Tags) > 0 {
		h := fnv.New32a()
		_, _ = h.Write([]byte(t.Tags[0]))
		borderColor := tagColorPalette[h.Sum32()%uint32(len(tagColorPalette))]
		style = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)
	}
	if active {
		style = activeCardStyle
	}

	return style.Width(width - 2).Render(content) //nolint:mnd // border width
}

func (b *Board) cardHeight(t *task.Task, width int) int {
	contentLines := b.cardContentLines(t, width)
	return len(contentLines) + 2 //nolint:mnd // top and bottom borders
}

func (b *Board) cardContentLines(t *task.Task, width int) []string {
	// Card content.
	const cardChrome = 4 // border (2) + padding (2)
	cardWidth := width - cardChrome
	if cardWidth < 1 {
		cardWidth = 1
	}

	const maxBodyLines = 4

	assigneeSuffix := ""
	assigneeLen := 0
	if t.Assignee != "" {
		assigneeSuffix = "  " + dimStyle.Render(t.Assignee)
		assigneeLen = len(t.Assignee) + 2
	}

	titleStyle := dimStyle
	if len(t.Tags) > 0 {
		titleStyle = tagStyle(t.Tags[0])
	}

	var contentLines []string

	isGlobal := len(t.Tags) > 0 && t.Tags[0] != t.Title
	if isGlobal {
		// Global board: PROJECT colored by project hash, WT/BRANCH colored by branch hash
		projectStyle := tagStyle(t.Tags[0])
		contentLines = append(contentLines, projectStyle.Render("PROJECT: "+truncate(t.Tags[0], cardWidth)))

		branch := t.Title
		prefix := t.Tags[0] + "/"
		if strings.HasPrefix(branch, prefix) {
			branch = branch[len(prefix):]
		}
		branchStyle := tagStyle(branch)
		seqSuffix := ""
		if seq, ok := b.titleSeq[t.ID]; ok {
			seqSuffix = dimStyle.Render(fmt.Sprintf(" #%d", seq))
		}
		branchWidth := cardWidth - assigneeLen - lipgloss.Width(seqSuffix)
		if branchWidth < 1 {
			branchWidth = 1
		}
		contentLines = append(contentLines, branchStyle.Render("WT/BRANCH: "+truncate(branch, branchWidth))+seqSuffix+assigneeSuffix)
	} else {
		// Project board: just the title, no ID
		titleWidth := cardWidth - assigneeLen
		if titleWidth < 1 {
			titleWidth = 1
		}
		contentLines = append(contentLines, titleStyle.Render(truncate(t.Title, titleWidth))+assigneeSuffix)
	}

	// Claim line — current tool call, subtly colored.
	if t.ClaimedBy != "" {
		contentLines = append(contentLines, toolStyle.Render(t.ClaimedBy))
	}

	// Body lines — user's task/prompt, up to 3 lines, shown in dim.
	if t.Body != "" {
		body := strings.TrimSpace(unescapeBody(t.Body))
		wrapped := wrapTitle(body, cardWidth, maxBodyLines)
		for _, line := range wrapped {
			contentLines = append(contentLines, dimStyle.Render(line))
		}
	}

	return contentLines
}

// wrapTitle2 splits a title across maxLines lines with different widths:
// firstWidth for the first line (shares space with the ID prefix),
// restWidth for continuation lines (uses full card width).
func wrapTitle2(title string, firstWidth, restWidth, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	if lipgloss.Width(title) <= firstWidth || maxLines == 1 {
		return []string{truncate(title, firstWidth)}
	}

	words := strings.Fields(title)
	lines := make([]string, 0, maxLines)
	var current strings.Builder

	for i, word := range words {
		lineWidth := restWidth
		if len(lines) == 0 {
			lineWidth = firstWidth
		}

		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if lipgloss.Width(current.String())+1+lipgloss.Width(word) <= lineWidth {
			current.WriteByte(' ')
			current.WriteString(word)
		} else {
			lines = append(lines, truncate(current.String(), lineWidth))
			current.Reset()
			current.WriteString(word)
			if len(lines) == maxLines-1 {
				// Last line: append all remaining words.
				for _, w := range words[i+1:] {
					current.WriteByte(' ')
					current.WriteString(w)
				}
				break
			}
		}
	}
	if current.Len() > 0 {
		w := restWidth
		if len(lines) == 0 {
			w = firstWidth
		}
		lines = append(lines, truncate(current.String(), w))
	}
	return lines
}

// wrapTitle splits a title across maxLines lines, word-wrapping at word
// boundaries. Each line is at most maxWidth characters.
func wrapTitle(title string, maxWidth, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	if lipgloss.Width(title) <= maxWidth || maxLines == 1 {
		return []string{truncate(title, maxWidth)}
	}

	words := strings.Fields(title)
	lines := make([]string, 0, maxLines)
	var current strings.Builder

	for i, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if lipgloss.Width(current.String())+1+lipgloss.Width(word) <= maxWidth {
			current.WriteByte(' ')
			current.WriteString(word)
		} else {
			lines = append(lines, truncate(current.String(), maxWidth))
			current.Reset()
			current.WriteString(word)
			if len(lines) == maxLines-1 {
				// Last line: append all remaining words.
				for _, w := range words[i+1:] {
					current.WriteByte(' ')
					current.WriteString(w)
				}
				break
			}
		}
	}
	if current.Len() > 0 {
		lines = append(lines, truncate(current.String(), maxWidth))
	}
	return lines
}

func (b *Board) renderStatusBar() string {
	total := len(b.tasks)
	status := fmt.Sprintf(" %s | %d tasks | d:del C:clear-all q:quit",
		b.cfg.Board.Name, total)
	status = truncate(status, b.width)

	if b.err != nil {
		errStr := errorStyle.Render(truncate("Error: "+b.err.Error(), b.width))
		return errStr + "\n" + statusBarStyle.Render(status)
	}

	return statusBarStyle.Render(status)
}

func (b *Board) viewDeleteConfirm() string {
	content := errorStyle.Render("Delete task?") + "\n\n" +
		fmt.Sprintf("  #%d: %s", b.deleteID, b.deleteTitle) + "\n\n" +
		dimStyle.Render("y:yes  n:no")

	return dialogStyle.Render(content)
}

func (b *Board) viewClearAllConfirm() string {
	content := errorStyle.Render("Delete ALL tasks?") + "\n\n" +
		fmt.Sprintf("  %d tasks will be removed from the board.", b.clearAllCount) + "\n\n" +
		dimStyle.Render("y:yes  n:no")

	return dialogStyle.Render(content)
}

// unescapeBody replaces literal escape sequences in body text with their
// corresponding whitespace characters. This handles bodies set via CLI flags
// where \n and \t are passed as literal two-character sequences.
func unescapeBody(s string) string {
	r := strings.NewReplacer(
		`\n`, "\n",
		`\t`, "\t",
		`\r`, "",
		`\\`, `\`,
	)
	return r.Replace(s)
}

func truncate(s string, maxLen int) string {
	if maxLen < 4 { //nolint:mnd // minimum length for truncation
		maxLen = 4
	}
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	// Slice by runes to avoid breaking multi-byte UTF-8 characters.
	runes := []rune(s)
	target := maxLen - 3 //nolint:mnd // room for "..."
	if target > len(runes) {
		target = len(runes)
	}
	// Trim runes from the end until the display width fits.
	for target > 0 && lipgloss.Width(string(runes[:target])) > maxLen-3 {
		target--
	}
	return string(runes[:target]) + "..."
}

// humanDuration formats a duration as a compact human-readable string.
// Examples: "<1m", "5m", "2h", "3d", "2w", "3mo", "1y".
func humanDuration(d time.Duration) string {
	const (
		day   = 24 * time.Hour
		week  = 7 * day
		month = 30 * day
		year  = 365 * day
	)

	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < day:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < week:
		return strconv.Itoa(int(d/day)) + "d"
	case d < month:
		return strconv.Itoa(int(d/week)) + "w"
	case d < year:
		return strconv.Itoa(int(d/month)) + "mo"
	default:
		return strconv.Itoa(int(d/year)) + "y"
	}
}
