package engine

import "github.com/downmygo/downmygo/internal/model"

// MinSplit is the smallest piece worth giving to another connection. A
// segment smaller than this is left to the worker that has it: splitting
// it costs a request and saves nothing.
const MinSplit int64 = 1 << 20 // 1 MiB

// InitialPlan divides size bytes into at most n segments. The planner
// starts with one segment and splits on demand, so this only seeds the
// table with a sensible first cut: a single segment for small files, and
// n equal parts for large ones.
func InitialPlan(size int64, n int) []model.Segment {
	if size <= 0 {
		return nil
	}
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	// No point in more pieces than MinSplit each.
	maxBySize := int(size / MinSplit)
	if maxBySize < 1 {
		maxBySize = 1
	}
	if n > maxBySize {
		n = maxBySize
	}
	if n == 1 {
		return []model.Segment{{Start: 0, End: size - 1}}
	}
	segs := make([]model.Segment, 0, n)
	part := size / int64(n)
	for i := 0; i < n; i++ {
		start := int64(i) * part
		end := start + part - 1
		if i == n-1 {
			end = size - 1
		}
		segs = append(segs, model.Segment{Start: start, End: end})
	}
	return segs
}

// ClaimInto is Claim with the active table owned by the caller, so a split
// can grow both together.
func ClaimInto(segs *[]model.Segment, active *[]bool, maxActive int, minSplit int64) int {
	if maxActive < 1 {
		maxActive = 1
	}
	activeCount := 0
	for _, a := range *active {
		if a {
			activeCount++
		}
	}
	best := -1
	var bestRem int64
	for i := range *segs {
		if (*active)[i] {
			continue
		}
		rem := (*segs)[i].Remaining()
		if rem <= 0 {
			continue
		}
		if rem > bestRem {
			best, bestRem = i, rem
		}
	}
	if best < 0 {
		if activeCount == 0 {
			return -1
		}
		return -2
	}
	seg := (*segs)[best]
	if bestRem > minSplit && activeCount < maxActive {
		mid := seg.Start + seg.Done + bestRem/2 - 1
		if mid >= seg.Start+seg.Done && mid < seg.End {
			first := model.Segment{Start: seg.Start, End: mid, Done: seg.Done}
			second := model.Segment{Start: mid + 1, End: seg.End}
			(*segs)[best] = first
			*segs = append(*segs, second)
			*active = append(*active, false)
			(*active)[best] = true
			return best
		}
	}
	(*active)[best] = true
	return best
}

// TotalDone sums the progress of every segment.
func TotalDone(segs []model.Segment) int64 {
	var n int64
	for _, s := range segs {
		n += s.Done
	}
	return n
}

// Complete reports whether every segment is fully downloaded.
func Complete(segs []model.Segment) bool {
	for _, s := range segs {
		if s.Remaining() > 0 {
			return false
		}
	}
	return len(segs) > 0
}
