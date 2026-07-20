package ctxpack

import "testing"

func TestParseVersionOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want Version
		ok   bool
	}{
		{name: "upstream format", out: "ctxpack 0.4.0\n", want: Version{0, 4, 0}, ok: true},
		{name: "v prefix", out: "ctxpack v1.2.3\n", want: Version{1, 2, 3}, ok: true},
		{name: "pre-release suffix", out: "ctxpack 0.5.0-rc1\n", want: Version{0, 5, 0}, ok: true},
		{name: "build suffix", out: "ctxpack 0.5.0+dirty\n", want: Version{0, 5, 0}, ok: true},
		{name: "two components", out: "ctxpack 1.2\n", want: Version{1, 2, 0}, ok: true},
		{name: "no number", out: "ctxpack unknown\n"},
		{name: "empty", out: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersionOutput(tt.out)
			if tt.ok != (err == nil) {
				t.Fatalf("parseVersionOutput(%q) error = %v, want ok = %v", tt.out, err, tt.ok)
			}
			if tt.ok && got != tt.want {
				t.Errorf("parseVersionOutput(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}

func TestVersionLess(t *testing.T) {
	tests := []struct {
		a, b Version
		want bool
	}{
		{a: Version{0, 3, 9}, b: Version{0, 4, 0}, want: true},
		{a: Version{0, 4, 0}, b: Version{0, 4, 0}, want: false},
		{a: Version{0, 4, 1}, b: Version{0, 4, 0}, want: false},
		{a: Version{1, 0, 0}, b: Version{0, 99, 99}, want: false},
		{a: Version{0, 99, 99}, b: Version{1, 0, 0}, want: true},
	}

	for _, tt := range tests {
		if got := tt.a.Less(tt.b); got != tt.want {
			t.Errorf("%v.Less(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
