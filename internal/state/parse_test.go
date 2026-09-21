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
