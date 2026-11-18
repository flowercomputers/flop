package main

import "testing"

func TestBogSlug(t *testing.T) {
	for in, want := range map[string]string{"search-server": "search-server", "My_Bog": "my-bog", "-x-": "x"} {
		if got := bogSlug(in); got != want {
			t.Errorf("bogSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
