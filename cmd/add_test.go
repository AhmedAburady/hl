package cmd

import "testing"

func TestShortName(t *testing.T) {
	cases := []struct{ host, zone, want string }{
		{"app.home.lab", "home.lab", "app"},
		{"a.b.home.lab", "home.lab", "a.b"},
		{"dsm.synology.com", "synology.com", "dsm"},
		{"home.lab", "home.lab", "@"},
		{"APP.HOME.LAB", "home.lab", "APP"},     // case-insensitive suffix match, original case kept
		{"dsm.synology.com", "home.lab", "dsm"}, // host not in zone -> first label
		{"singlelabel", "home.lab", "singlelabel"},
	}
	for _, c := range cases {
		if got := shortName(c.host, c.zone); got != c.want {
			t.Errorf("shortName(%q,%q) = %q, want %q", c.host, c.zone, got, c.want)
		}
	}
}
