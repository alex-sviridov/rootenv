package main

import "testing"

func TestParseMemory(t *testing.T) {
	cases := []struct {
		input string
		want  int64
		err   bool
	}{
		{"", 0, false},
		{"512MB", 512 << 20, false},
		{"1GB", 1 << 30, false},
		{"256KB", 256 << 10, false},
		{"1G", 1 << 30, false},
		{"128M", 128 << 20, false},
		{"1K", 1 << 10, false},
		{"1024", 1024, false},
		{"bad", 0, true},
	}
	for _, tc := range cases {
		got, err := parseMemory(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("parseMemory(%q): expected error", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseMemory(%q): unexpected error: %v", tc.input, err)
			} else if got != tc.want {
				t.Errorf("parseMemory(%q): want %d, got %d", tc.input, tc.want, got)
			}
		}
	}
}

func TestParseCPUMilli(t *testing.T) {
	cases := []struct {
		input string
		want  int64
		err   bool
	}{
		{"", 0, false},
		{"1", 1000, false},
		{"0.5", 500, false},
		{"500m", 500, false},
		{"1000m", 1000, false},
		{"bad", 0, true},
		{"badm", 0, true},
	}
	for _, tc := range cases {
		got, err := parseCPUMilli(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("parseCPUMilli(%q): expected error", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseCPUMilli(%q): unexpected error: %v", tc.input, err)
			} else if got != tc.want {
				t.Errorf("parseCPUMilli(%q): want %d, got %d", tc.input, tc.want, got)
			}
		}
	}
}
