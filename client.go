package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/flowercomputers/flop/api"
)

// Client is the flop-side client for the spade API.
type Client struct {
	baseURL        string
	token          string
	organizationID string
	http           *http.Client
	// stream has no timeout: build streams stay open for minutes.
	stream *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		baseURL:        cfg.URLs.APIBaseURL,
		token:          cfg.LocalConfig.APIKey,
		organizationID: cfg.LocalConfig.OrganizationID,
		http:           &http.Client{Timeout: 30 * time.Second},
		stream:         &http.Client{},
	}
}

// SetToken swaps the bearer token, e.g. right after login.
func (c *Client) SetToken(token string) {
	c.token = token
}

// APIError is a non-200 response from the spade API.
type APIError struct {
	Status int
	Body   string
}

// Error is spade's own words (its status travels in Status, and becomes a
// code in classify)
func (e *APIError) Error() string {
	if body := strings.TrimSpace(e.Body); body != "" {
		return fmt.Sprintf("%s (%d)", body, e.Status)
	}
	return fmt.Sprintf("%s (%d)", strings.ToLower(http.StatusText(e.Status)), e.Status)
}

// Code returns the "error" field of a JSON error body, if present.
func (e *APIError) Code() string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(e.Body), &body) == nil {
		return body.Error
	}
	return ""
}

func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent())
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.organizationID != "" {
		req.Header.Set(api.HeaderOrganizationID, c.organizationID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Body: string(buf)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type StartResponse struct {
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
}

type TokenResponse struct {
	Token       string `json:"token"`
	PrincipalID string `json:"principal_id"`
}

type CurrentUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (u CurrentUser) Label() string {
	if u.Name != "" && u.Email != "" {
		return fmt.Sprintf("%s <%s>", u.Name, u.Email)
	}
	if u.Name != "" {
		return u.Name
	}
	if u.Email != "" {
		return u.Email
	}
	return u.ID
}

func (c *Client) StartCLITokenFlow(ctx context.Context, clientID string) (StartResponse, error) {
	var out StartResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/token", map[string]string{
		"client_id": clientID,
	}, &out)
	return out, err
}

var TokenErrorAuthorizationPending = errors.New("authorization_pending")
var TokenErrorExpiredToken = errors.New("expired_token")

func (c *Client) PollCLITokenOnce(ctx context.Context, clientID string, deviceCode string) (TokenResponse, error) {
	var out TokenResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/token/status", map[string]string{
		"client_id":   clientID,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
	}, &out)
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code() {
		case "authorization_pending":
			return TokenResponse{}, TokenErrorAuthorizationPending
		case "expired_token":
			return TokenResponse{}, TokenErrorExpiredToken
		}
	}
	return out, err
}

// Polls until the CLI token is available, or the context deadline is reached,
// or an error we don't handle is returned.
// "authorization_pending" is expected, and we should retry
// "expired_token" is expected, and we should return an error
// anything else (with an HTTP status code other than 200) is unexpected, and we should return an error
func (c *Client) PollForCLIToken(ctx context.Context, clientID string, deviceCode string, interval time.Duration) (TokenResponse, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return TokenResponse{}, ctx.Err()
		case <-ticker.C:
		}

		tok, err := c.PollCLITokenOnce(ctx, clientID, deviceCode)
		if err == nil {
			return tok, nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return TokenResponse{}, TokenErrorExpiredToken
		}
		switch {
		case errors.Is(err, TokenErrorAuthorizationPending):
			continue
		case errors.Is(err, TokenErrorExpiredToken):
			return TokenResponse{}, TokenErrorExpiredToken
		default:
			return TokenResponse{}, fmt.Errorf("unexpected error: %w", err)
		}
	}
}

func (c *Client) CurrentUser(ctx context.Context) (CurrentUser, error) {
	var out CurrentUser
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/me", nil, &out)
	return out, err
}

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var out []Organization
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/organization", nil, &out)
	return out, err
}

func (c *Client) ListBuilds(ctx context.Context, pkg string) ([]api.Build, error) {
	path := "/api/v1/builds"
	if pkg != "" {
		path += "?package=" + url.QueryEscape(pkg)
	}
	var out api.ListBuildsResponse
	err := c.doJSON(ctx, http.MethodGet, path, nil, &out)
	return out.Builds, err
}

type StartBuildRequest struct {
	Package string
	GitSHA  string
	Dirty   bool
	Fresh   bool
	Tarball []byte
	Arch    string // api.ArchARM64 ("" means it) or api.ArchAMD64
}

// BuildStream is an open NDJSON build stream from the server.
type BuildStream struct {
	BuildID string
	body    io.ReadCloser
}

// StartBuild uploads the source tarball and returns once the server has
// accepted the build and assigned it an ID; consume the stream with Wait.
func (c *Client) StartBuild(ctx context.Context, req StartBuildRequest) (*BuildStream, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/builds", bytes.NewReader(req.Tarball))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", userAgent())
	httpReq.Header.Set("Content-Type", "application/gzip")
	httpReq.Header.Set(api.HeaderPackage, req.Package)
	if req.GitSHA != "" {
		httpReq.Header.Set(api.HeaderGitSHA, req.GitSHA)
	}
	httpReq.Header.Set(api.HeaderDirty, strconv.FormatBool(req.Dirty))
	httpReq.Header.Set(api.HeaderFresh, strconv.FormatBool(req.Fresh))
	if req.Arch != "" {
		httpReq.Header.Set(api.HeaderArch, req.Arch)
	}
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.organizationID != "" {
		httpReq.Header.Set(api.HeaderOrganizationID, c.organizationID)
	}
	resp, err := c.stream.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, &APIError{Status: resp.StatusCode, Body: string(buf)}
	}
	return &BuildStream{
		BuildID: resp.Header.Get(api.HeaderBuildID),
		body:    resp.Body,
	}, nil
}

// Wait consumes the stream, invoking onEvent for each out/err chunk, until
// the terminal done or error line.
func (s *BuildStream) Wait(onEvent func(api.BuildEvent)) (*api.BuildDone, error) {
	sc := bufio.NewScanner(s.body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev api.BuildEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("parse build event: %w", err)
		}
		switch {
		case ev.Error != "":
			return nil, fmt.Errorf("build error: %s", ev.Error)
		case ev.Done != nil:
			return ev.Done, nil
		case ev.HB:
			// keepalive only
		default:
			onEvent(ev)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("build stream ended without a done event")
}

func (s *BuildStream) Close() error {
	return s.body.Close()
}

func (c *Client) CreateDeployment(ctx context.Context, req api.CreateDeploymentRequest) (api.Deployment, error) {
	var out api.Deployment
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/deployments", req, &out)
	return out, err
}

func (c *Client) ListDeployments(ctx context.Context) ([]api.Deployment, error) {
	var out api.ListDeploymentsResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/deployments", nil, &out)
	return out.Deployments, err
}

func (c *Client) UpdateDeployment(ctx context.Context, id string, req api.UpdateDeploymentRequest) (api.Deployment, error) {
	var out api.Deployment
	err := c.doJSON(ctx, http.MethodPatch, "/api/v1/deployments/"+url.PathEscape(id), req, &out)
	return out, err
}

// DestroyBogData permanently deletes a bog's kept data volume. The bog name is
// sent back as the confirmation spade requires.
func (c *Client) DestroyBogData(ctx context.Context, bog string) (api.BogVolume, error) {
	var out api.BogVolume
	path := "/api/v1/bogs/" + url.PathEscape(bog) + "/data?confirm=" + url.QueryEscape(bog)
	err := c.doJSON(ctx, http.MethodDelete, path, nil, &out)
	return out, err
}

func (c *Client) ListBogData(ctx context.Context) ([]api.BogVolume, error) {
	var out api.ListBogDataResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/data", nil, &out)
	return out.Volumes, err
}

func (c *Client) RestoreBogData(ctx context.Context, bog, snapshotID string) (api.RestoreBogDataResponse, error) {
	var out api.RestoreBogDataResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/bogs/"+url.PathEscape(bog)+"/data/restore",
		api.RestoreBogDataRequest{SnapshotID: snapshotID}, &out)
	return out, err
}

func (c *Client) ReboxDeployment(ctx context.Context, id string) (api.Deployment, error) {
	var out api.Deployment
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/deployments/"+url.PathEscape(id)+"/rebox", nil, &out)
	return out, err
}

func (c *Client) GetSizes(ctx context.Context) (api.SizesResponse, error) {
	var out api.SizesResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/sizes", nil, &out)
	return out, err
}

func (c *Client) ResizeDeployment(ctx context.Context, id string, req api.ResizeDeploymentRequest) (api.Deployment, error) {
	var out api.Deployment
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/deployments/"+url.PathEscape(id)+"/resize", req, &out)
	return out, err
}

func (c *Client) GrowBogData(ctx context.Context, bog string, sizeGB int) (api.BogVolume, error) {
	var out api.BogVolume
	err := c.doJSON(ctx, http.MethodPatch, "/api/v1/bogs/"+url.PathEscape(bog)+"/data", api.GrowBogDataRequest{SizeGB: sizeGB}, &out)
	return out, err
}

func (c *Client) GetBog(ctx context.Context, name string) (api.Bog, error) {
	var out api.Bog
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/"+url.PathEscape(name), nil, &out)
	return out, err
}

func (c *Client) DeleteDeployment(ctx context.Context, id string) (api.Deployment, error) {
	var out api.Deployment
	err := c.doJSON(ctx, http.MethodDelete, "/api/v1/deployments/"+url.PathEscape(id), nil, &out)
	return out, err
}
