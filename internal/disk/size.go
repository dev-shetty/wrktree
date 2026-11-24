// Package disk measures directory size and caches the results.
package disk

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Size returns the size of path in bytes, from `du -sk`.
// du can exit non-zero when it cannot read one file. If stdout holds a
// number, Size uses the number and ignores that error.
func Size(ctx context.Context, path string) (int64, error) {
	cmd := exec.CommandContext(ctx, "du", "-sk", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	runErr := cmd.Run()

	fields := strings.Fields(out.String())
	if len(fields) > 0 {
		if kb, err := strconv.ParseInt(fields[0], 10, 64); err == nil && kb >= 0 {
			return kb * 1024, nil
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return 0, ctxErr
	}
	if runErr != nil {
		return 0, fmt.Errorf("du %s: %w", path, runErr)
	}
	return 0, fmt.Errorf("du %s: unexpected output %q", path, out.String())
}

// Human formats a byte count with 1024 units: KB and MB have no
// decimals, GB has one. A value below 1 KiB that is not 0 shows as "1 KB".
func Human(b int64) string {
	const (
		kb = int64(1024)
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b <= 0:
		return "0 KB"
	case b < mb:
		return fmt.Sprintf("%d KB", max(1, (b+kb/2)/kb))
	case b < gb:
		return fmt.Sprintf("%d MB", (b+mb/2)/mb)
	default:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	}
}

// State tells how a shown size was obtained.
type State int

const (
	SizeNone State = iota
	SizeCached
	SizeStale
	SizeFresh
)

// Decide tells what to show now and if `du` must run. An entry whose age
// is exactly ttl is stale. A ttl of 0 makes every entry stale.
func Decide(e Entry, ok bool, ttl time.Duration, now time.Time, fresh bool) (bytes int64, state State, runDu bool) {
	if !ok {
		return 0, SizeNone, true
	}
	if fresh || now.Sub(e.At) >= ttl {
		return e.Bytes, SizeStale, true
	}
	return e.Bytes, SizeCached, false
}
