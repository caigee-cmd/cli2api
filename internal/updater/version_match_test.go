package updater

import "testing"

func TestVersionsMatch(t *testing.T) {
	cases := []struct {
		reported string
		expected string
		want     bool
	}{
		{"v0.7.52", "0.7.52", true},
		{"0.7.52", "v0.7.52", true},
		{"v0.7.52", "v0.7.52", true},
		{" v0.7.52 ", "0.7.52", true},
		{"v0.7.51", "0.7.52", false},
		{"v0.7.520", "0.7.52", false},
		{"dev", "0.7.52", false},
		{"", "0.7.52", false},
	}
	for _, tc := range cases {
		if got := versionsMatch(tc.reported, tc.expected); got != tc.want {
			t.Fatalf("versionsMatch(%q, %q) = %v, want %v", tc.reported, tc.expected, got, tc.want)
		}
	}
}
