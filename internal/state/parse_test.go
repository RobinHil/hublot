package state

import "testing"

func TestParseBytes(t *testing.T) {
	cases := map[string]int64{
		"512":    512,
		"512b":   512,
		"512k":   512 << 10,
		"512kb":  512 << 10,
		"512m":   512 << 20,
		"1.5g":   1610612736,
		"2G":     2 << 30,
		" 256M ": 256 << 20,
		"0":      0,
	}
	for in, want := range cases {
		got, err := ParseBytes(in)
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseBytes(%q): got %d, want %d", in, got, want)
		}
	}

	for _, in := range []string{"", "lots", "-5m", "12x"} {
		if _, err := ParseBytes(in); err == nil {
			t.Errorf("ParseBytes(%q) should have failed", in)
		}
	}
}

func TestParseCPUs(t *testing.T) {
	cases := map[string]int64{
		"1":   1_000_000_000,
		"1.5": 1_500_000_000,
		"0.5": 500_000_000,
		"0":   0,
		" 2 ": 2_000_000_000,
	}
	for in, want := range cases {
		got, err := ParseCPUs(in)
		if err != nil {
			t.Errorf("ParseCPUs(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseCPUs(%q): got %d, want %d", in, got, want)
		}
	}

	for _, in := range []string{"", "half", "-1"} {
		if _, err := ParseCPUs(in); err == nil {
			t.Errorf("ParseCPUs(%q) should have failed", in)
		}
	}
}

func TestParseList(t *testing.T) {
	got := ParseList("8080:80, 9000:9000  ,,7000:70")
	want := []string{"8080:80", "9000:9000", "7000:70"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if len(ParseList("   ")) != 0 {
		t.Error("whitespace is not an entry")
	}
}

func TestParseEnv(t *testing.T) {
	got, err := ParseEnv("TZ=Europe/Paris, DEBUG=1, EMPTY=")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 3 || got[0] != "TZ=Europe/Paris" || got[2] != "EMPTY=" {
		t.Errorf("got %v", got)
	}

	// A name with no value at all is a typo, not a variable.
	if _, err := ParseEnv("JUSTANAME"); err == nil {
		t.Error("a pair without = must be refused")
	}
	if _, err := ParseEnv("=value"); err == nil {
		t.Error("a value without a name must be refused")
	}
}

func TestParseMounts(t *testing.T) {
	got, err := ParseMounts("data:/var/lib/data, ./conf:/etc/conf:ro")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}

	for _, bad := range []string{"data", "data:relative/path", "a:/b:rx", "a:/b:ro:extra", ":/b"} {
		if _, err := ParseMounts(bad); err == nil {
			t.Errorf("%q should have been refused", bad)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	got, err := SplitCommand(`sh -c "echo hello world"`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	want := []string{"sh", "-c", "echo hello world"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	if got, _ := SplitCommand("  "); len(got) != 0 {
		t.Errorf("an empty command is no command: %v", got)
	}
	if _, err := SplitCommand(`sh -c "unbalanced`); err == nil {
		t.Error("an unbalanced quote must be reported, not guessed at")
	}
}

func TestExpandPath(t *testing.T) {
	const home = "/home/someone"

	cases := map[string]string{
		"~":               home,
		"~/":              home,
		"~/stacks/blog":   home + "/stacks/blog",
		"/srv/blog":       "/srv/blog",
		"./stack":         "./stack",
		"  ~/spaced  ":    home + "/spaced",
		"~notauser/thing": "~notauser/thing",
		"a~b":             "a~b",
		"":                "",
	}
	for in, want := range cases {
		if got := ExpandPath(in, home); got != want {
			t.Errorf("ExpandPath(%q) = %q, want %q", in, got, want)
		}
	}

	// Without a home directory there is nothing to expand to, and inventing
	// one would be worse than leaving the path alone.
	if got := ExpandPath("~/x", ""); got != "~/x" {
		t.Errorf("no home: got %q", got)
	}
}

func TestExpandPathSubstitutesVariables(t *testing.T) {
	t.Setenv("HUBLOT_TEST_DIR", "/var/tmp/stacks")
	if got := ExpandPath("$HUBLOT_TEST_DIR/blog", "/home/someone"); got != "/var/tmp/stacks/blog" {
		t.Errorf("got %q", got)
	}
}
