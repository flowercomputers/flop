package main

import (
	"errors"
	"strings"
	"testing"
)

// Whoever logs in is told the address unless a browser shows it to them: at
// a terminal that is --no-browser, or a browser that cannot be opened. There
// the address goes above the badge and the badge is written last: the
// spinner redraws the line under it, and wipes whatever is printed there.
func TestLoginShowsTheAddressUnlessABrowserDoes(t *testing.T) {
	start := StartResponse{UserCode: "AB123", VerificationURIComplete: "https://panel.example/auth/cli/verify?user_code=AB123"}
	for _, c := range []struct {
		name                     string
		terminal, json           bool
		noBrowser, browserBroken bool
		opens                    int  // times a browser is opened
		address                  bool // the address is printed
	}{
		{name: "a terminal", terminal: true, opens: 1},
		{name: "a terminal, --no-browser", terminal: true, noBrowser: true, address: true},
		{name: "a terminal, no browser to open", terminal: true, browserBroken: true, opens: 1, address: true},
		{name: "no terminal", address: true},
		{name: "no terminal, --no-browser", noBrowser: true, address: true},
		{name: "--json", terminal: true, json: true, address: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			o, out, errs := testUI("", c.terminal, c.json)
			o.tty = c.terminal && !c.json
			opens := 0
			badge, spinnerLine := showVerify(o, start, c.noBrowser, func(string) error {
				opens++
				if c.browserBroken {
					return errors.New("no browser")
				}
				return nil
			})
			said := errs.String()
			if opens != c.opens || strings.Contains(said, start.VerificationURIComplete) != c.address {
				t.Fatalf("opened a browser %d times, and said:\n%s", opens, said)
			}
			if (spinnerLine >= 0) != o.tty || (o.tty && !strings.HasSuffix(said, badge+"\n")) {
				t.Fatalf("the badge (spinner line %d) is not the last thing said:\n%s", spinnerLine, said)
			}
			if out.Len() > 0 {
				t.Fatalf("stdout is the result's alone: %s", out.String())
			}
		})
	}
}
