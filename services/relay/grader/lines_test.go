package grader

import "testing"

func TestStripANSI_removes_color_codes(t *testing.T) {
	in := "\x1b[32mchown bob /tmp/labfile\x1b[0m\n"
	want := "chown bob /tmp/labfile\n"
	if got := stripANSI(in); got != want {
		t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
	}
}

func TestStripANSI_leaves_plain_text_untouched(t *testing.T) {
	in := "no escapes here"
	if got := stripANSI(in); got != in {
		t.Errorf("stripANSI(%q) = %q, want unchanged", in, got)
	}
}

func TestAssetBuffer_splits_complete_lines_and_keeps_partial(t *testing.T) {
	b := &assetBuffer{}

	newLines := b.Ingest([]byte("hello wor"))
	if newLines {
		t.Error("no newline yet: want newLines=false")
	}
	if got := b.Joined(); got != "" {
		t.Errorf("Joined() = %q, want empty (no complete line yet)", got)
	}

	newLines = b.Ingest([]byte("ld\nsecond line\nthird-partial"))
	if !newLines {
		t.Error("want newLines=true after chunk containing newlines")
	}
	want := "hello world\nsecond line"
	if got := b.Joined(); got != want {
		t.Errorf("Joined() = %q, want %q", got, want)
	}
}

func TestAssetBuffer_ring_caps_at_ten_lines(t *testing.T) {
	b := &assetBuffer{}
	for i := 0; i < 15; i++ {
		b.Ingest([]byte("line" + string(rune('0'+i%10)) + "\n"))
	}
	lines := b.Joined()
	count := 1
	for _, r := range lines {
		if r == '\n' {
			count++
		}
	}
	if count != 10 {
		t.Errorf("got %d lines, want 10 (ring cap)", count)
	}
	if got := b.Joined(); got[:5] != "line5" {
		t.Errorf("Joined() = %q, want to start with the 6th line (oldest 5 dropped)", got)
	}
}

func TestAssetBuffer_strips_ansi_before_splitting(t *testing.T) {
	b := &assetBuffer{}
	b.Ingest([]byte("\x1b[32mchown bob /tmp/labfile\x1b[0m\n"))
	want := "chown bob /tmp/labfile"
	if got := b.Joined(); got != want {
		t.Errorf("Joined() = %q, want %q", got, want)
	}
}
