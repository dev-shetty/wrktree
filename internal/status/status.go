// Package status collects the values of each worktree and prints them.
package status

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
)

// duLimit is the number of du processes that run at one time.
const duLimit = 4

// Row holds the values of one worktree. An OK flag that is false means
// that the value is not available.
type Row struct {
	WT            git.Worktree
	Current       bool
	Changed       int
	ChangedOK     bool
	Ahead, Behind int
	DistOK        bool
	LastCommit    time.Time
	CommitOK      bool
	Size          int64
	SizeState     disk.State
}

// Options controls LoadAll. An empty Base means that no base exists.
type Options struct {
	Base  string
	TTL   time.Duration
	Fresh bool
	Cwd   string
	Now   time.Time
}

// LoadAll reads the values of all worktrees at the same time. It waits for
// every value, including the du runs that disk.Decide asks for. Then it
// updates the cache and saves it. A failed save does not change the rows.
func LoadAll(ctx context.Context, r git.Runner, wts []git.Worktree, cache *disk.Cache, repo string, o Options) []Row {
	rows := make([]Row, len(wts))
	cur := git.Current(wts, o.Cwd)
	sem := make(chan struct{}, duLimit)
	var wg sync.WaitGroup
	paths := make([]string, len(wts))

	for i, w := range wts {
		paths[i] = w.Path
		rows[i] = Row{WT: w, Current: i == cur}
		if w.Prunable {
			continue
		}
		if _, err := os.Stat(w.Path); err != nil {
			continue
		}
		row := &rows[i]
		start := func(f func()) {
			wg.Add(1)
			go func() { defer wg.Done(); f() }()
		}
		start(func() {
			if n, err := r.ChangedFiles(ctx, w.Path); err == nil {
				row.Changed, row.ChangedOK = n, true
			}
		})
		if o.Base != "" {
			start(func() {
				if a, b, err := r.Distance(ctx, w.Path, o.Base); err == nil {
					row.Ahead, row.Behind, row.DistOK = a, b, true
				}
			})
		}
		start(func() {
			if t, err := r.LastCommit(ctx, w.Path); err == nil {
				row.LastCommit, row.CommitOK = t, true
			}
		})
		start(func() {
			e, ok := cache.Get(repo, w.Path)
			bytes, state, runDu := disk.Decide(e, ok, o.TTL, o.Now, o.Fresh)
			if runDu {
				sem <- struct{}{}
				n, err := disk.Size(ctx, w.Path)
				<-sem
				if err == nil {
					bytes, state = n, disk.SizeFresh
					cache.Set(repo, w.Path, n, o.Now)
				}
			}
			row.Size, row.SizeState = bytes, state
		})
	}
	wg.Wait()
	cache.Retain(repo, paths)
	_ = cache.Save()
	return rows
}

// Sort puts the main worktree first. The others follow, newest commit first.
// A row without a commit time goes last.
func Sort(rows []Row) {
	slices.SortStableFunc(rows, func(a, b Row) int {
		if a.WT.IsMain != b.WT.IsMain {
			if a.WT.IsMain {
				return -1
			}
			return 1
		}
		if a.CommitOK != b.CommitOK {
			if a.CommitOK {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.LastCommit.Unix(), a.LastCommit.Unix())
	})
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// Age describes how long before now t was, for example "3 weeks ago".
func Age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	days := int(d / (24 * time.Hour))
	switch {
	case days < 14:
		return plural(days, "day")
	case days < 60:
		return plural(days/7, "week")
	case days < 365:
		return plural(days/30, "month")
	}
	return plural(days/365, "year")
}

const (
	currentMark = "●"
	unavailable = "—"
)

func name(r Row) string {
	w := r.WT
	n := w.Branch
	folder := filepath.Base(w.Path)
	if w.Detached || n == "" {
		n = w.Head
		if len(n) > 7 {
			n = n[:7]
		}
	}
	if w.Locked {
		n += " [locked]"
	}
	if w.Prunable {
		n += " [missing]"
	}
	if !w.Detached && w.Branch != "" && folder != w.Branch {
		n += " (" + folder + ")"
	}
	return n
}

func changedText(r Row) string {
	switch {
	case !r.ChangedOK:
		return unavailable
	case r.Changed == 0:
		return "clean"
	case r.Changed == 1:
		return "1 file"
	}
	return fmt.Sprintf("%d files", r.Changed)
}

func sizeText(r Row) string {
	switch r.SizeState {
	case disk.SizeFresh:
		return disk.Human(r.Size)
	case disk.SizeCached, disk.SizeStale:
		return "~" + disk.Human(r.Size)
	}
	return unavailable
}

// Table writes the rows as plain text with aligned columns.
func Table(w io.Writer, rows []Row, base string, now time.Time) {
	distHead := base
	if distHead == "" {
		distHead = "BASE"
	}
	cells := [][]string{{" ", "BRANCH", "CHANGES", distHead, "SIZE", "LAST COMMIT"}}
	for _, r := range rows {
		mark, dist, age := " ", unavailable, unavailable
		if r.Current {
			mark = currentMark
		}
		if r.DistOK {
			dist = fmt.Sprintf("↑%d ↓%d", r.Ahead, r.Behind)
		}
		if r.CommitOK {
			age = Age(r.LastCommit, now)
		}
		cells = append(cells, []string{mark, name(r), changedText(r), dist, sizeText(r), age})
	}
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, c := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	for _, row := range cells {
		var b strings.Builder
		for i, c := range row {
			b.WriteString(c)
			if i == 0 {
				b.WriteString(" ")
			} else if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c)+2))
			}
		}
		fmt.Fprintln(w, b.String())
	}
}

type jsonRow struct {
	Path       string     `json:"path"`
	Branch     string     `json:"branch"`
	Head       string     `json:"head"`
	Main       bool       `json:"main"`
	Current    bool       `json:"current"`
	Detached   bool       `json:"detached"`
	Locked     bool       `json:"locked"`
	Prunable   bool       `json:"prunable"`
	Changed    *int       `json:"changed"`
	Ahead      *int       `json:"ahead"`
	Behind     *int       `json:"behind"`
	Base       string     `json:"base"`
	LastCommit *time.Time `json:"last_commit"`
	SizeBytes  *int64     `json:"size_bytes"`
	SizeApprox bool       `json:"size_approx"`
}

// JSON writes the rows as an array of objects.
func JSON(w io.Writer, rows []Row, base string) error {
	out := make([]jsonRow, 0, len(rows))
	for _, r := range rows {
		j := jsonRow{
			Path: r.WT.Path, Branch: r.WT.Branch, Head: r.WT.Head, Main: r.WT.IsMain,
			Current: r.Current, Detached: r.WT.Detached, Locked: r.WT.Locked,
			Prunable: r.WT.Prunable, Base: base,
		}
		if r.ChangedOK {
			j.Changed = &r.Changed
		}
		if r.DistOK {
			j.Ahead, j.Behind = &r.Ahead, &r.Behind
		}
		if r.CommitOK {
			t := r.LastCommit
			j.LastCommit = &t
		}
		if r.SizeState != disk.SizeNone {
			j.SizeBytes = &r.Size
			j.SizeApprox = r.SizeState != disk.SizeFresh
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
