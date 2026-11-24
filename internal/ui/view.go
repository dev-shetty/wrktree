package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/status"
)

const (
	defaultWidth = 80
	cursorMark   = "›"
	currentMark  = "●"
	pending      = "…"
	fetchFailed  = "fetch failed: distances can be stale"
	keyFooter    = " enter switch · o open · d delete · D delete + branch · p prune · f fetch · / filter · q quit"
	filterFooter = " enter keep filter · esc clear filter"
	typedLabel   = "Type yes to delete: "
	typedHint    = "type yes + enter · esc cancel"
	// Below these widths, the columns do not show.
	commitMinWidth = 60
	sizeMinWidth   = 45
	// chromeLines is the number of lines that are not table rows:
	// title, blank, header, blank, status, footer.
	chromeLines = 6
)

// renderer draws the screen. The screen is on stderr, so stderr decides
// the color profile.
var renderer = lipgloss.NewRenderer(os.Stderr)

var (
	dim   = renderer.NewStyle().Faint(true)
	green = renderer.NewStyle().Foreground(lipgloss.Color("2"))
)

// cell is one table cell. Text is plain. Dim draws it faint.
type cell struct {
	text string
	dim  bool
}

// View draws the screen. No line is wider than the terminal.
func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = defaultWidth
	}
	footer := m.footer(w)
	lines := []string{m.title(w), ""}
	switch {
	case !m.loaded:
		lines = append(lines, " loading"+pending)
	case m.loadErr != nil:
		lines = append(lines, " error: "+m.loadErr.Error())
	default:
		lines = append(lines, m.table(w, len(footer)-1)...)
	}
	lines = append(lines, "", m.statusLine())
	lines = append(lines, footer...)
	cut := renderer.NewStyle().MaxWidth(w)
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = cut.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// rightMargin is the count of columns that stay empty at the right end of the
// title. A terminal scroll bar can hide the last column.
const rightMargin = 2

func (m Model) title(w int) string {
	left := " wrktree · " + m.d.Name
	if !m.loaded || m.loadErr != nil {
		return left
	}
	right := fmt.Sprintf("%d worktrees", len(m.rows))
	if len(m.rows) == 1 {
		right = "1 worktree"
	}
	var total int64
	known, approx := false, false
	for _, r := range m.rows {
		if r.SizeState == disk.SizeNone {
			continue
		}
		known = true
		total += r.Size
		approx = approx || r.SizeState != disk.SizeFresh
	}
	if known {
		t := disk.Human(total)
		if approx {
			t = "~" + t
		}
		right += " · " + t
	}
	gap := w - rightMargin - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) statusLine() string {
	var parts []string
	switch {
	case m.mode == modeFilter:
		parts = append(parts, "/"+m.filter+"█")
	case m.filter != "":
		parts = append(parts, "/"+m.filter)
	}
	if m.busy != "" {
		parts = append(parts, m.busy+pending)
	}
	if m.note != "" {
		parts = append(parts, m.note)
	}
	if m.fetchFailed {
		parts = append(parts, fetchFailed)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " · ")
}

// cells returns the cells after BRANCH: CHANGES, distance, SIZE, LAST COMMIT.
func (m Model) cells(r status.Row) []cell {
	out := make([]cell, 4)

	switch {
	case r.ChangedOK:
		out[0] = cell{text: status.ChangedText(r)}
	case m.changedWait[r.WT.Path]:
		out[0] = cell{text: pending}
	default:
		out[0] = cell{text: status.Unavailable}
	}

	switch {
	case m.base == "":
		out[1] = cell{text: status.Unavailable}
	case r.DistOK:
		out[1] = cell{text: status.DistText(r), dim: m.fetching || m.fetchFailed}
	case m.distWait[r.WT.Path]:
		out[1] = cell{text: pending}
	default:
		out[1] = cell{text: status.Unavailable}
	}

	switch {
	case r.SizeState != disk.SizeNone:
		out[2] = cell{text: status.SizeText(r), dim: r.SizeState == disk.SizeStale}
	case m.sizePending(r.WT.Path):
		out[2] = cell{text: pending}
	default:
		out[2] = cell{text: status.Unavailable}
	}

	switch {
	case r.CommitOK:
		out[3] = cell{text: status.CommitText(r, m.d.Now())}
	case m.commitWait[r.WT.Path]:
		out[3] = cell{text: pending}
	default:
		out[3] = cell{text: status.Unavailable}
	}
	return out
}

func (m Model) sizePending(path string) bool {
	if m.sizeBusy[path] {
		return true
	}
	for _, p := range m.sizeWait {
		if p == path {
			return true
		}
	}
	return false
}

// footer returns the last lines of the screen: the key help, or a prompt.
// A prompt replaces the key help.
func (m Model) footer(w int) []string {
	switch m.mode {
	case modeFilter:
		return []string{filterFooter}
	case modeTyped:
		return m.deletePrompt(w)
	case modePrune:
		var lines []string
		for _, l := range m.pruneLines {
			lines = append(lines, wrap(l, w)...)
		}
		return append(lines, wrap(fmt.Sprintf("Prune %d entries? y/N", len(m.pruneLines)), w)...)
	}
	return []string{keyFooter}
}

// deletePrompt returns the lines of the typed delete prompt: the text of
// git.DeletePrompt, the branch line if D started the delete, the typed
// answer, and the key hint.
func (m Model) deletePrompt(w int) []string {
	lines := wrap(strings.TrimSpace(git.DeletePrompt(m.del.wt, m.del.plan)), w)
	if m.del.branch {
		lines = append(lines, wrap("The branch "+m.del.wt.Branch+" is deleted too.", w)...)
	}
	lines = append(lines, wrap(typedLabel+m.typed+"█", w)...)
	return append(lines, wrap(typedHint, w)...)
}

// wrap breaks s into lines of at most w columns. Each line starts with one space.
func wrap(s string, w int) []string {
	var out []string
	for _, l := range strings.Split(ansi.Wrap(s, max(w-1, 1), ""), "\n") {
		out = append(out, " "+l)
	}
	return out
}

// table draws the header and the rows that fit in the height. extra is the
// number of footer lines after the first. The branch column shrinks first.
// Narrow terminals hide LAST COMMIT, then SIZE.
func (m Model) table(w, extra int) []string {
	distHead := m.base
	if distHead == "" {
		distHead = "BASE"
	}
	heads := []string{"CHANGES", distHead, "SIZE", "LAST COMMIT"}
	show := []bool{true, true, w >= sizeMinWidth, w >= commitMinWidth}

	all := make([][]cell, len(m.rows))
	widths := make([]int, len(heads))
	for i, h := range heads {
		widths[i] = lipgloss.Width(h)
	}
	branchWidth := lipgloss.Width("BRANCH")
	for i, r := range m.rows {
		all[i] = m.cells(r)
		for j, c := range all[i] {
			widths[j] = max(widths[j], lipgloss.Width(c.text))
		}
		branchWidth = max(branchWidth, lipgloss.Width(status.Name(r)))
	}
	fixed := 3 // " " + mark + " "
	for j := range heads {
		if show[j] {
			fixed += 2 + widths[j]
		}
	}
	branchWidth = max(1, min(branchWidth, w-fixed))

	headCells := make([]cell, len(heads))
	for j, h := range heads {
		headCells[j] = cell{text: h, dim: true}
	}
	lines := []string{"   " + joinRow(dim.Render(pad("BRANCH", branchWidth)), headCells, widths, show)}

	cells := make(map[string][]cell, len(m.rows))
	for i, r := range m.rows {
		cells[r.WT.Path] = all[i]
	}
	vis := m.visible()
	if len(vis) == 0 {
		if m.filter != "" {
			return append(lines, " no worktree matches")
		}
		return append(lines, " no worktrees")
	}
	cur := m.cursorIndex()
	start, end := 0, len(vis)
	if body := m.height - chromeLines - extra; m.height > 0 && body < len(vis) {
		body = max(body, 1)
		if cur >= body {
			start = cur - body + 1
		}
		end = start + body
	}
	for i := start; i < end; i++ {
		r := vis[i]
		mark := " "
		switch {
		case i == cur:
			mark = cursorMark
		case r.Current:
			mark = currentMark
		}
		lines = append(lines, " "+mark+" "+joinRow(branchCell(r, branchWidth), cells[r.WT.Path], widths, show))
	}
	return lines
}

// branchCell draws the name in width columns. The folder note is dim and the
// merged mark is green. A name that does not fit ends with "…". The cut
// removes the folder note first, then shortens the branch and keeps the
// marks: " ✓ merged", "[locked]", and "[missing]".
func branchCell(r status.Row, width int) string {
	plain, note, marks := status.Plain(r), status.FolderNote(r), status.Marks(r)
	pw, mw := lipgloss.Width(plain), lipgloss.Width(marks)
	styled := styleMarks(marks)
	full := status.Name(r)
	switch {
	case lipgloss.Width(full) <= width:
		s := plain
		if note != "" {
			s += " " + dim.Render(note)
		}
		return s + styled + strings.Repeat(" ", width-lipgloss.Width(full))
	case pw+mw <= width:
		// Part of the folder note fits, if it has room for " (…".
		tail := ""
		if extra := width - pw - mw; note != "" && extra >= 3 {
			tail = ellipsize(" "+note, extra)
		}
		return pad(plain+tail, width-mw) + styled
	case width-mw >= 2:
		return pad(ellipsize(plain, width-mw), width-mw) + styled
	}
	return pad(ellipsize(plain+marks, width), width)
}

// styleMarks draws the merged mark green. The other marks stay plain.
func styleMarks(marks string) string {
	if rest, ok := strings.CutPrefix(marks, status.MergedMark); ok {
		return green.Render(status.MergedMark) + rest
	}
	return marks
}

// joinRow joins the branch cell and the shown cells with two spaces. The
// last cell has no padding.
func joinRow(branch string, cs []cell, widths []int, show []bool) string {
	var b strings.Builder
	b.WriteString(branch)
	last := -1
	for j := range cs {
		if show[j] {
			last = j
		}
	}
	for j, c := range cs {
		if !show[j] {
			continue
		}
		b.WriteString("  ")
		text := c.text
		if c.dim {
			text = dim.Render(text)
		}
		b.WriteString(text)
		if j != last {
			b.WriteString(strings.Repeat(" ", widths[j]-lipgloss.Width(c.text)))
		}
	}
	return b.String()
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

// ellipsize cuts s to width columns. The result ends with "…".
func ellipsize(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + pending
}
