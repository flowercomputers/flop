package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flowercomputers/flop/api"
)

func (c *Client) CreateBogKey(ctx context.Context, bog string, req api.CreateBogKeyRequest) (api.CreateBogKeyResponse, error) {
	var out api.CreateBogKeyResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/bogs/"+url.PathEscape(bog)+"/keys", req, &out)
	return out, err
}

func (c *Client) ListBogKeys(ctx context.Context, bog string) ([]api.BogKey, error) {
	var out api.ListBogKeysResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/"+url.PathEscape(bog)+"/keys", nil, &out)
	return out.Keys, err
}

func (c *Client) RevokeBogKey(ctx context.Context, bog, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/bogs/"+url.PathEscape(bog)+"/keys/"+url.PathEscape(id), nil, nil)
}

func (c *Client) GetBogAccess(ctx context.Context, bog string) (api.BogAccess, error) {
	var out api.BogAccess
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/"+url.PathEscape(bog)+"/access", nil, &out)
	return out, err
}

func (c *Client) SetBogAccess(ctx context.Context, bog string, access api.BogAccess) (api.BogAccess, error) {
	var out api.BogAccess
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/bogs/"+url.PathEscape(bog)+"/access", access, &out)
	return out, err
}

// keyCreated is a new key, with its secret: the only time it is shown
type keyCreated struct {
	Bog string `json:"bog"`
	api.CreateBogKeyResponse
}

func (k keyCreated) human(w io.Writer) {
	fmt.Fprintf(w, "Created key %s (%s) for %s. It works within a few seconds, once the routers have it.\n", k.Key.ID, k.Key.Name, k.Bog)
	if k.Key.ExpiresAt != nil {
		fmt.Fprintf(w, "It expires %s.\n", k.Key.ExpiresAt.Local().Format(time.RFC1123))
	}
	fmt.Fprintf(w, "\n  %s\n\nThis is the only time the key is shown; store it now. Use it as:\n\n  curl -H 'Authorization: Bearer %s' %s/...\n", k.Secret, k.Secret, k.URL)
}

// KeysCreate makes an API key and returns its secret, the only time it is
// ever shown.
func KeysCreate(ctx context.Context, bog, name, expires string) (keyCreated, error) {
	client, err := loggedInClient()
	if err != nil {
		return keyCreated{}, err
	}
	if bog == "" {
		return keyCreated{}, usage("keys create needs a bog", "flop bog keys create <bog> [--name NAME] [--expires 720h]")
	}
	out, err := client.CreateBogKey(ctx, bog, api.CreateBogKeyRequest{Name: name, ExpiresIn: expires})
	if err != nil {
		return keyCreated{}, fmt.Errorf("create key: %w", err)
	}
	return keyCreated{Bog: bog, CreateBogKeyResponse: out}, nil
}

// keyList is a bog's keys (never their secrets)
type keyList struct {
	Bog  string       `json:"bog"`
	Keys []api.BogKey `json:"keys"`
	now  time.Time
}

func KeysList(ctx context.Context, bog string) (keyList, error) {
	client, err := loggedInClient()
	if err != nil {
		return keyList{}, err
	}
	if bog == "" {
		return keyList{}, usage("keys list needs a bog", "flop bog keys list <bog>")
	}
	keys, err := client.ListBogKeys(ctx, bog)
	if err != nil {
		return keyList{}, fmt.Errorf("list keys: %w", err)
	}
	return keyList{Bog: bog, Keys: keys, now: time.Now()}, nil
}

func (l keyList) human(out io.Writer) {
	if len(l.Keys) == 0 {
		fmt.Fprintf(out, "No keys. Make one with `flop bog keys create %s`.\n", l.Bog)
		return
	}
	now := l.now
	if now.IsZero() {
		now = time.Now()
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCREATED\tEXPIRES\tLAST USED\tREQUESTS")
	for _, k := range l.Keys {
		expires := "never"
		if k.ExpiresAt != nil {
			expires = k.ExpiresAt.Local().Format("2006-01-02 15:04")
			if !k.ExpiresAt.After(now) {
				expires += " (expired)"
			}
		}
		lastUsed := "never"
		if k.LastUsedAt != nil {
			lastUsed = ago(*k.LastUsedAt)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\n", k.ID, k.Name, ago(k.CreatedAt), expires, lastUsed, k.Requests)
	}
	tw.Flush()
}

// keyRevoked is a key revoked
type keyRevoked struct {
	Bog string `json:"bog"`
	ID  string `json:"id"`
}

func (k keyRevoked) human(w io.Writer) {
	fmt.Fprintf(w, "Revoked %s; routers stop accepting it within seconds.\n", k.ID)
}

func KeysRevoke(ctx context.Context, bog, id string) (keyRevoked, error) {
	client, err := loggedInClient()
	if err != nil {
		return keyRevoked{}, err
	}
	if bog == "" || id == "" {
		return keyRevoked{}, usage("keys revoke needs a bog and a key id", "flop bog keys revoke <bog> <key-id> (see `flop bog keys list <bog>`)")
	}
	if err := client.RevokeBogKey(ctx, bog, id); err != nil {
		return keyRevoked{}, fmt.Errorf("revoke: %w", err)
	}
	return keyRevoked{Bog: bog, ID: id}, nil
}

// bogAccess is who may call a bog without a key
type bogAccess struct {
	Bog  string         `json:"bog"`
	Mode api.AccessMode `json:"mode"`
}

func (a bogAccess) human(w io.Writer) {
	if a.Mode == api.AccessPublic {
		fmt.Fprintf(w, "%s is public: anyone can call it, any method, without a key. Your bog decides what each caller may do (X-Bog-Auth, X-Bog-Key-Id).\n", a.Bog)
	} else {
		fmt.Fprintf(w, "%s is private: every request needs a key.\n", a.Bog)
	}
}

// Access shows a bog's access mode, or sets it when mode is given.
func Access(ctx context.Context, bog, mode string) (bogAccess, error) {
	client, err := loggedInClient()
	if err != nil {
		return bogAccess{}, err
	}
	if bog == "" || (mode != "" && mode != string(api.AccessPrivate) && mode != string(api.AccessPublic)) {
		return bogAccess{}, usage("access needs a bog, and optionally private or public", "flop bog access <bog> [private|public]")
	}
	var got api.BogAccess
	if mode == "" {
		got, err = client.GetBogAccess(ctx, bog)
	} else {
		got, err = client.SetBogAccess(ctx, bog, api.BogAccess{Mode: api.AccessMode(mode)})
	}
	if err != nil {
		return bogAccess{}, fmt.Errorf("access: %w", err)
	}
	return bogAccess{Bog: bog, Mode: got.Mode}, nil
}

// curled is a bog's answer to `flop bog curl`. People see the body as it
// came (its status on stderr when it is not a success); with --json, the
// status, content type and body (JSON as JSON, anything else as a string).
type curled struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	Body        any    `json:"body"`
	raw         []byte
}

func (c curled) human(w io.Writer) { w.Write(c.raw) }

// Curl calls an owner's own bog with a key made for this one request,
// expiring in 5 minutes, revoked as soon as the request ends.
func Curl(ctx context.Context, o *ui, bog, path, method, data string) (curled, error) {
	client, err := loggedInClient()
	if err != nil {
		return curled{}, err
	}
	if bog == "" {
		return curled{}, usage("curl needs a bog", "flop bog curl <bog> [path] [-X METHOD] [-d DATA]")
	}
	c, err := curlBog(ctx, client, http.DefaultClient, bog, path, method, data)
	if err == nil && c.Status >= 300 {
		o.say("%d %s", c.Status, http.StatusText(c.Status))
	}
	return c, err
}

// how long curl keeps asking while the routers don't have its key yet
var curlKeyWait, curlKeyPoll = 15 * time.Second, 300 * time.Millisecond

// routerRefusedKey: a router's own refusal of a key, which names itself so
func routerRefusedKey(resp *http.Response) bool {
	return resp.StatusCode == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") == `Bearer realm="bog"`
}

func curlBog(ctx context.Context, client *Client, web *http.Client, bog, path, method, data string) (curled, error) {
	key, err := client.CreateBogKey(ctx, bog, api.CreateBogKeyRequest{Name: "flop curl", ExpiresIn: "5m"})
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests {
		// every curl makes a key, and spade limits how many one caller makes
		return curled{}, &cliError{Code: "unavailable", Status: apiErr.Status, Message: "make a temporary key: " + apiErr.Error(),
			Hint: fmt.Sprintf("each curl makes a key, and spade limits how many are made in an hour: wait, or make one key (`flop bog keys create %s`) and call the bog with it", bog)}
	}
	if err != nil {
		return curled{}, fmt.Errorf("make a temporary key: %w", err)
	}
	defer client.RevokeBogKey(context.WithoutCancel(ctx), bog, key.Key.ID)

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if method == "" {
		method = http.MethodGet
		if data != "" {
			method = http.MethodPost
		}
	}
	// the key reaches the routers a second or two after spade makes it; a
	// router that doesn't have it yet refuses it before the bog sees
	// anything, so the request is sent again until one does
	var resp *http.Response
	for made := time.Now(); ; {
		req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), key.URL+path, bytes.NewReader([]byte(data)))
		if err != nil {
			return curled{}, err
		}
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		if data != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if resp, err = web.Do(req); err != nil {
			return curled{}, fail("unavailable", fmt.Sprintf("calling %s: %v", bog, err), fmt.Sprintf("see `flop bog status %s`", bog))
		}
		if !routerRefusedKey(resp) || time.Since(made) > curlKeyWait {
			break
		}
		resp.Body.Close()
		select {
		case <-ctx.Done():
			return curled{}, ctx.Err()
		case <-time.After(curlKeyPoll):
		}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return curled{}, err
	}
	c := curled{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), raw: raw, Body: string(raw)}
	var parsed any
	if json.Unmarshal(raw, &parsed) == nil {
		c.Body = parsed
	}
	return c, nil
}
