package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// cliError is a failure as flop reports it: a stable code a program can
// branch on, a message, and the next step when there is one. Each code has
// its exit status (AGENTS.md).
type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Status  int    `json:"status,omitempty"` // spade's HTTP status, when spade said no
}

func (e *cliError) Error() string { return e.Message }

// exit statuses, by code
var exitCodes = map[string]int{
	"failed":           1,
	"usage":            2,
	"invalid":          2,
	"not_logged_in":    3,
	"unauthorized":     3,
	"forbidden":        3,
	"not_found":        4,
	"conflict":         5,
	"timeout":          6,
	"bog_failed":       7,
	"build_failed":     7,
	"unavailable":      8,
	"payment_required": 9,
}

func (e *cliError) exit() int {
	if n, ok := exitCodes[e.Code]; ok {
		return n
	}
	return 1
}

func fail(code, message, hint string) *cliError {
	return &cliError{Code: code, Message: message, Hint: hint}
}

func usage(message, hint string) *cliError { return fail("usage", message, hint) }

var scopeMissing = regexp.MustCompile(`lacks the (bog:[a-z]+) scope`)

// classify turns any error into a cliError: flop's own as they are, spade's
// answers by status, the network's as unavailable.
func classify(err error) *cliError {
	var ce *cliError
	if errors.As(err, &ce) {
		return ce
	}
	msg := err.Error()
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		e := &cliError{Status: apiErr.Status, Message: msg}
		switch s := apiErr.Status; {
		case s == http.StatusBadRequest:
			e.Code = "invalid"
		case s == http.StatusUnauthorized:
			e.Code = "unauthorized"
			e.Hint = "sign in again with `flop auth login`; in automation, FLOP_TOKEN may have expired or been revoked (make a new organization token in the panel)"
		case s == http.StatusForbidden:
			e.Code = "forbidden"
			if m := scopeMissing.FindStringSubmatch(apiErr.Body); m != nil {
				e.Hint = "make an organization token with the " + m[1] + " scope in the panel (API keys)"
			}
		case s == http.StatusNotFound:
			e.Code = "not_found"
			e.Hint = "see what there is: `flop bog deployments`, `flop bog volumes`, `flop bog keys list <bog>`"
		case s == http.StatusConflict:
			e.Code = "conflict"
		case s == http.StatusPaymentRequired:
			e.Code = "payment_required"
			e.Hint = "add credits or a card to the organization in the panel (Billing)"
		case s == http.StatusNotImplemented:
			e.Code = "invalid" // this spade does not do that (a feature turned off)
		case s == http.StatusTooManyRequests:
			e.Code = "unavailable" // spade's limit on one caller's requests
			e.Hint = "wait as long as it says, then try again"
		case s >= 500:
			e.Code = "unavailable"
			e.Hint = "spade did not manage it; try again in a minute"
		default:
			e.Code = "failed"
		}
		return e
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fail("timeout", msg, "")
	case strings.HasPrefix(msg, "Required flag") || strings.HasPrefix(msg, "flag provided but not defined") ||
		strings.HasPrefix(msg, "invalid value") || strings.Contains(msg, "No help topic"):
		return usage(msg, "see `flop <command> --help`")
	}
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) {
		return fail("unavailable", msg, "spade could not be reached; check your connection, or FLOP_API_BASE_URL")
	}
	return fail("failed", msg, "")
}

// errNotLoggedIn: no login and no FLOP_TOKEN
var errNotLoggedIn = fail("not_logged_in", "not logged in",
	"run `flop auth login`; in automation, set FLOP_TOKEN and FLOP_ORGANIZATION_ID (an organization token from the panel)")

var errNoOrganization = fail("not_logged_in", "no organization selected",
	"run `flop organization use <id>` (see `flop organization list`), or re-run `flop auth login`")
