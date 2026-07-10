// regexcheck validates exercise `template` regexes against Go's regexp
// package (RE2 syntax) — the same engine relay-grader compiles them with
// (see services/relay/grader/backend.go). Go's RE2 rejects constructs other
// regex flavors accept, most notably lookahead/lookbehind (?=...) (?!...)
// and backreferences, so a template that "looks fine" under Python's `re`
// can still fail to compile at grading time and silently never pass.
//
// Protocol: reads a JSON array of pattern strings from stdin (each pattern
// is a template body exactly as it appears in the lab YAML, not yet
// (?s)-prefixed), writes a JSON array of {ok, error} objects to stdout in
// the same order.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

type result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func main() {
	var patterns []string
	if err := json.NewDecoder(os.Stdin).Decode(&patterns); err != nil {
		fmt.Fprintln(os.Stderr, "regexcheck: failed to decode stdin as a JSON array of strings:", err)
		os.Exit(2)
	}

	results := make([]result, len(patterns))
	for i, p := range patterns {
		// Mirror relay-grader's exact compilation: services/relay/grader/backend.go
		// prepends "(?s)" so `.` spans the newlines that join buffered lines.
		if _, err := regexp.Compile("(?s)" + p); err != nil {
			results[i] = result{OK: false, Error: err.Error()}
		} else {
			results[i] = result{OK: true}
		}
	}

	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, "regexcheck: failed to encode results:", err)
		os.Exit(2)
	}
}
