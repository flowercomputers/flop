package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// testUI is a command's ui for a test: in is what stdin holds, canAsk
// whether flop may prompt (a terminal, no --no-input)
func testUI(in string, canAsk, asJSON bool) (*ui, *bytes.Buffer, *bytes.Buffer) {
	var out, errs bytes.Buffer
	return &ui{json: asJSON, out: &out, err: &errs, in: strings.NewReader(in), canAsk: canAsk}, &out, &errs
}

// a result's JSON is exactly one document on stdout; its human form goes
// there too, and neither touches stderr
func TestOneResultTwoRenderings(t *testing.T) {
	r := keyRevoked{Bog: "search", ID: "key_1"}
	o, out, errs := testUI("", false, true)
	if err := o.emit(r); err != nil {
		t.Fatal(err)
	}
	var got keyRevoked
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got != r || strings.Count(out.String(), "\n") != 1 || errs.Len() != 0 {
		t.Fatalf("json %q (%v), stderr %q", out, err, errs)
	}
	o, out, errs = testUI("", false, false)
	o.emit(r)
	if !strings.Contains(out.String(), "key_1") || errs.Len() != 0 {
		t.Fatalf("human %q, stderr %q", out, errs)
	}
}

func TestErrorsAreClassified(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
		exit int
	}{
		{&APIError{Status: http.StatusNotFound, Body: "no such bog"}, "not_found", 4},
		{fmt.Errorf("rollout: %w", &APIError{Status: http.StatusConflict}), "conflict", 5},
		{&APIError{Status: http.StatusUnauthorized}, "unauthorized", 3},
		{&APIError{Status: http.StatusForbidden, Body: "this organization token lacks the bog:create scope"}, "forbidden", 3},
		{&APIError{Status: http.StatusPaymentRequired}, "payment_required", 9},
		{&APIError{Status: http.StatusBadRequest}, "invalid", 2},
		{&APIError{Status: http.StatusBadGateway}, "unavailable", 8},
		{&APIError{Status: http.StatusTooManyRequests, Body: "too many requests: try again in 30 seconds"}, "unavailable", 8},
		{errNotLoggedIn, "not_logged_in", 3},
		{fail("timeout", "gave up", ""), "timeout", 6},
		{errors.New(`Required flag "image" not set`), "usage", 2},
		{errors.New("something odd"), "failed", 1},
	} {
		e := classify(c.err)
		if e.Code != c.code || e.exit() != c.exit {
			t.Errorf("%v: %s (exit %d), want %s (exit %d)", c.err, e.Code, e.exit(), c.code, c.exit)
		}
	}
	// a missing scope names the scope to ask for
	if e := classify(&APIError{Status: http.StatusForbidden, Body: "this organization token lacks the bog:create scope"}); !strings.Contains(e.Hint, "bog:create") {
		t.Fatalf("hint %q", e.Hint)
	}
	// spade's words, with its status, not a Go error chain's
	if e := classify(fmt.Errorf("rollout: %w", &APIError{Status: http.StatusNotFound, Body: "deployment not found\n"})); e.Message != "rollout: deployment not found (404)" || e.Status != 404 {
		t.Fatalf("message %q status %d", e.Message, e.Status)
	}
}

func TestErrorsPrintAsJSONOrWithTheirHint(t *testing.T) {
	e := fail("not_found", "no bog x", "see `flop bog deployments`")
	var b bytes.Buffer
	printError(&b, true, e)
	var got struct{ Error cliError }
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || got.Error != *e {
		t.Fatalf("%q %v", b.String(), err)
	}
	b.Reset()
	printError(&b, false, e)
	if b.String() != "error: no bog x\nhint: see `flop bog deployments`\n" {
		t.Fatalf("%q", b.String())
	}
}

func TestJSONIsSeenBeforeParsing(t *testing.T) {
	if !jsonRequested([]string{"bog", "deploy", "--json"}) || jsonRequested([]string{"bog", "--", "--json"}) || jsonRequested([]string{"bog"}) {
		t.Fatal("jsonRequested")
	}
}
