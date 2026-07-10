package grader

import "regexp"

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// stripANSI removes terminal escape sequences (color, cursor movement) so
// regex templates match what a human reads, not raw PTY control bytes.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

const ringCapacity = 10

// assetBuffer reassembles a per-asset byte stream into complete, ANSI-stripped
// lines and keeps the most recent ringCapacity of them for regex matching.
type assetBuffer struct {
	partial []byte
	lines   []string // ring, oldest first, len capped at ringCapacity
}

// Ingest appends chunk to the pending partial line, strips ANSI escapes,
// splits on '\n', and pushes every complete line into the ring buffer.
// The trailing incomplete segment (if any) is kept as the new partial.
// Returns true if at least one new complete line was added.
func (b *assetBuffer) Ingest(chunk []byte) bool {
	combined := stripANSI(string(append(b.partial, chunk...)))
	b.partial = nil

	segments := splitLines(combined)
	if len(segments) == 0 {
		return false
	}

	// splitLines always adds a trailing segment, which is:
	// - "" if combined ended in '\n' (complete line)
	// - non-empty if combined doesn't end in '\n' (incomplete)
	complete := segments[:len(segments)-1]
	if segments[len(segments)-1] != "" {
		// Last segment is non-empty, so it's incomplete
		b.partial = []byte(segments[len(segments)-1])
	}

	if len(complete) == 0 {
		return false
	}

	b.lines = append(b.lines, complete...)
	if overflow := len(b.lines) - ringCapacity; overflow > 0 {
		b.lines = b.lines[overflow:]
	}
	return true
}

// splitLines splits s on '\n', keeping the trailing empty segment if s ends
// in '\n' (so the caller can distinguish "ended cleanly" from "mid-line").
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// Joined returns the buffered lines newline-joined, for regex matching.
func (b *assetBuffer) Joined() string {
	return joinLines(b.lines)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
