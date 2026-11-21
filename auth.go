package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/browser"
)

// loggedIn is who flop now acts as, and in which organization
type loggedIn struct {
	PrincipalID  string             `json:"principal_id"`
	User         *CurrentUser       `json:"user,omitempty"`
	Organization *organizationEntry `json:"organization,omitempty"`
	avatar       string             // a person's badge, drawn at a terminal
}

func (l loggedIn) human(w io.Writer) {
	fmt.Fprintln(w, "Authentication successful")
	if l.Organization == nil {
		fmt.Fprintln(w, "No organization selected: run `flop organization use <id>` before deploying (see `flop organization list`)")
	} else {
		fmt.Fprintf(w, "Using organization: %s\n", l.Organization.Name)
	}
	if l.avatar != "" {
		fmt.Fprintln(w, l.avatar)
	} else if l.User != nil {
		fmt.Fprintf(w, "Logged in as %s\n", l.User.Label())
	}
}

// verify is how a person approves this login, for whoever shows it to them
// (a JSON line on stderr with --json: an agent shows it to its person)
type verify struct {
	Event     string    `json:"event"` // "verify"
	URL       string    `json:"url"`
	UserCode  string    `json:"user_code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// showVerify tells whoever is watching how to approve a login: a JSON line
// for an agent, the code as a badge at a terminal, a plain line otherwise.
// At a terminal it opens the browser too, unless noBrowser; when no browser
// shows the address it is printed, and above the badge: the spinner redraws
// the line under the badge, and wipes whatever is printed there. It returns
// the badge and its spinner's line (-1 with no badge).
func showVerify(o *ui, start StartResponse, noBrowser bool, open func(url string) error) (string, int) {
	switch {
	case o.json:
		json.NewEncoder(o.err).Encode(verify{Event: "verify", URL: start.VerificationURIComplete, UserCode: start.UserCode, ExpiresAt: start.ExpiresAt})
	case o.tty && o.canAsk:
		if noBrowser {
			o.say("Approve this login at %s", start.VerificationURIComplete)
		} else if err := open(start.VerificationURIComplete); err != nil {
			o.say("Could not open a browser; approve the login at: %s", start.VerificationURIComplete)
		}
		pending, spinnerLine := renderPendingAuthBadge(start.UserCode, true)
		fmt.Fprintln(o.err) // set the badge apart from what is above it
		fmt.Fprintln(o.err, pending)
		return pending, spinnerLine
	default:
		o.say("Approve this login at %s (code %s); waiting...", start.VerificationURIComplete, start.UserCode)
	}
	return "", -1
}

// Login signs in through the panel: a person approves this device there.
// At a terminal it opens the browser and shows the code as a badge; else (or
// with noBrowser) it prints where to go, and waits.
func Login(ctx context.Context, o *ui, noBrowser bool) (loggedIn, error) {
	if os.Getenv("FLOP_TOKEN") != "" {
		return loggedIn{}, usage("FLOP_TOKEN is set: flop already signs in with that organization token", "unset FLOP_TOKEN to log in as yourself")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return loggedIn{}, err
	}
	if cfg.LocalConfig.ClientID == "" {
		cfg.LocalConfig.ClientID = uuid.New().String()
		if err := SaveLocalConfig(cfg.LocalConfig); err != nil {
			return loggedIn{}, fmt.Errorf("save local config: %w", err)
		}
	}

	client := NewClient(cfg)
	start, err := client.StartCLITokenFlow(ctx, cfg.LocalConfig.ClientID)
	if err != nil {
		return loggedIn{}, fmt.Errorf("start login: %w", err)
	}

	pending, spinnerLine := showVerify(o, start, noBrowser, browser.OpenURL)

	deadline, cancel := context.WithDeadline(ctx, start.ExpiresAt)
	defer cancel()
	var tok TokenResponse
	poll := func() error {
		var pollErr error
		tok, pollErr = client.PollForCLIToken(deadline, cfg.LocalConfig.ClientID, start.DeviceCode, time.Duration(start.Interval)*time.Second)
		return pollErr
	}
	// with no badge there is no spinner: this is poll alone
	if err := runWithLineSpinner(pending, spinnerLine, "Waiting for approval...", poll); err != nil {
		if errors.Is(err, TokenErrorExpiredToken) || errors.Is(err, context.DeadlineExceeded) {
			return loggedIn{}, fail("timeout", "the login was not approved in time", "run `flop auth login` again")
		}
		return loggedIn{}, fmt.Errorf("login: %w", err)
	}

	cfg.LocalConfig.APIKey = tok.Token
	cfg.LocalConfig.PrincipalID = tok.PrincipalID
	cfg.LocalConfig.LastLogin = time.Now()
	if err := SaveLocalConfig(cfg.LocalConfig); err != nil {
		return loggedIn{}, fmt.Errorf("save local config: %w", err)
	}
	client.SetToken(tok.Token)

	out := loggedIn{PrincipalID: tok.PrincipalID}
	if organizations, err := client.ListOrganizations(ctx); err == nil && len(organizations) > 0 {
		selected := selectOrganization(&cfg, organizations)
		if err := SaveLocalConfig(cfg.LocalConfig); err != nil {
			return loggedIn{}, fmt.Errorf("save local config: %w", err)
		}
		out.Organization = &organizationEntry{ID: selected.ID, Name: selected.Name, Current: true}
		if len(organizations) > 1 {
			o.say("Switch organizations with `flop organization use <id>`, see `flop organization list`")
		}
	}
	loggedInAs := tok.PrincipalID
	if user, err := client.CurrentUser(ctx); err == nil {
		out.User = &user
		loggedInAs = user.Label()
	}
	if o.tty {
		if avatar, err := renderAuthAvatar(tok.PrincipalID, loggedInAs, true); err == nil {
			out.avatar = avatar
		}
	}
	return out, nil
}

// loggedOut: flop forgot its login
type loggedOut struct {
	LoggedOut bool `json:"logged_out"`
}

func (loggedOut) human(w io.Writer) { fmt.Fprintln(w, "Logout successful") }

func Logout() (loggedOut, error) {
	cfg, err := LoadLocalConfig()
	if err != nil {
		return loggedOut{}, err
	}
	cfg.APIKey = ""
	cfg.PrincipalID = ""
	cfg.LastLogin = time.Time{}
	if err := SaveLocalConfig(cfg); err != nil {
		return loggedOut{}, fmt.Errorf("save local config: %w", err)
	}
	return loggedOut{LoggedOut: true}, nil
}

// whoami is who flop acts as: a person (a login) or an organization token
type whoami struct {
	Kind           string       `json:"kind"` // "login" or "organization_token"
	User           *CurrentUser `json:"user,omitempty"`
	OrganizationID string       `json:"organization_id,omitempty"`
	API            string       `json:"api"` // the spade flop talks to
}

func (w whoami) human(out io.Writer) {
	if w.Kind == "organization_token" {
		fmt.Fprintf(out, "An organization token (FLOP_TOKEN) for organization %s, at %s\n", w.OrganizationID, w.API)
		return
	}
	fmt.Fprintf(out, "Logged in as: %s\nEmail: %s\nID: %s\nOrganization: %s\nAPI: %s\n", w.User.Name, w.User.Email, w.User.ID, orDash(w.OrganizationID), w.API)
}

func Whoami(ctx context.Context) (whoami, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return whoami{}, err
	}
	if cfg.LocalConfig.APIKey == "" {
		return whoami{}, errNotLoggedIn
	}
	// an organization token is no person: spade's /me is people's
	if os.Getenv("FLOP_TOKEN") != "" {
		return whoami{Kind: "organization_token", OrganizationID: cfg.LocalConfig.OrganizationID, API: cfg.URLs.APIBaseURL}, nil
	}
	user, err := NewClient(cfg).CurrentUser(ctx)
	if err != nil {
		return whoami{}, fmt.Errorf("whoami: %w", err)
	}
	return whoami{Kind: "login", User: &user, OrganizationID: cfg.LocalConfig.OrganizationID, API: cfg.URLs.APIBaseURL}, nil
}
