// Package client is a small HTTP client for the Orion web app's REST routes
// under /api/v1/agents/asserted/*. It knows nothing about Terraform.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version is the provider version reported in the User-Agent header. Set by
// main.go via -ldflags at release build time; "dev" otherwise.
var Version = "dev"

const defaultTimeout = 10 * time.Second

// APIError is returned for any non-2xx response. The body is always
// {"success": false, "error": "<message>"}, plus "existingAgentId" on a
// 409 short-id collision.
type APIError struct {
	Status          int
	Message         string
	ExistingAgentID string
}

func (e *APIError) Error() string {
	if e.ExistingAgentID != "" {
		return fmt.Sprintf("orion api error (status %d): %s (existing agent id: %s)", e.Status, e.Message, e.ExistingAgentID)
	}
	return fmt.Sprintf("orion api error (status %d): %s", e.Status, e.Message)
}

// Client talks to the Orion web app's asserted-agent REST routes.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New constructs a Client. baseURL is the Orion web app's base URL (e.g.
// https://orion.example.com); no trailing path segment required.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// PathKeyResponse is the body of a successful GET path-key call.
type PathKeyResponse struct {
	Success        bool   `json:"success"`
	AgentID        string `json:"agentId"`
	ShortID        string `json:"shortId"`
	PathPrefix     string `json:"pathPrefix"`
	GatewayBaseURL string `json:"gatewayBaseUrl"`
}

// WorkloadIdentity is a workload's cloud-agnostic identity: which cloud,
// which account/project/subscription, which region, and its own name. Sent
// to the API as structured fields, never joined into a single string.
type WorkloadIdentity struct {
	CloudProvider  string
	CloudAccountID string
	CloudRegion    string
	WorkloadName   string
}

// GetPathKey calls GET /api/v1/agents/asserted/path-key.
func (c *Client) GetPathKey(ctx context.Context, environment string, identity WorkloadIdentity) (*PathKeyResponse, error) {
	q := url.Values{}
	q.Set("environment", environment)
	q.Set("cloudProvider", identity.CloudProvider)
	q.Set("cloudAccountId", identity.CloudAccountID)
	q.Set("cloudRegion", identity.CloudRegion)
	q.Set("workloadName", identity.WorkloadName)
	path := "/api/v1/agents/asserted/path-key?" + q.Encode()

	var out PathKeyResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AgentResponse is the body of a successful GET by short id call.
type AgentResponse struct {
	Success      bool   `json:"success"`
	AgentID      string `json:"agentId"`
	ShortID      string `json:"shortId"`
	DisplayName  string `json:"displayName"`
	Status       string `json:"status"`
	IsRegistered bool   `json:"isRegistered"`
	RegisteredBy string `json:"registeredBy"`
}

// GetAgent calls GET /api/v1/agents/asserted/{shortId}. A 404 is returned
// as an *APIError so the caller can decide how to react (e.g. remove from
// state), not swallowed here.
func (c *Client) GetAgent(ctx context.Context, shortID string) (*AgentResponse, error) {
	path := "/api/v1/agents/asserted/" + url.PathEscape(shortID)

	var out AgentResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAgentRequest is the body of POST /api/v1/agents/asserted. The
// workload identity is sent as structured fields, never joined into a
// single string.
type CreateAgentRequest struct {
	Environment          string   `json:"environment"`
	CloudProvider        string   `json:"cloudProvider"`
	CloudAccountID       string   `json:"cloudAccountId"`
	CloudRegion          string   `json:"cloudRegion"`
	WorkloadName         string   `json:"workloadName"`
	WorkloadResourceID   string   `json:"workloadResourceId,omitempty"`
	WorkloadType         string   `json:"workloadType,omitempty"`
	DisplayName          string   `json:"displayName"`
	FunctionalBoundaries []string `json:"functionalBoundaries,omitempty"`
	Adopt                bool     `json:"adopt,omitempty"`
}

// CreateAgentResponse is the body of a successful POST response.
type CreateAgentResponse struct {
	Success      bool   `json:"success"`
	AgentID      string `json:"agentId"`
	ShortID      string `json:"shortId"`
	Status       string `json:"status"`
	IsRegistered bool   `json:"isRegistered"`
	Created      bool   `json:"created"`
}

// CreateAgent calls POST /api/v1/agents/asserted. Also used for updates:
// the server upserts on (environment, cloud_provider, cloud_account_id,
// cloud_region, workload_name).
func (c *Client) CreateAgent(ctx context.Context, req CreateAgentRequest) (*CreateAgentResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal create agent request: %w", err)
	}

	var out CreateAgentResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents/asserted", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgentResponse is the body of a successful DELETE response.
type DeleteAgentResponse struct {
	Success          bool     `json:"success"`
	ArchivedAgentIDs []string `json:"archivedAgentIds"`
}

// DeleteAgent calls DELETE /api/v1/agents/asserted/{shortId}. A 404 is not
// treated as success here — the resource layer decides that.
func (c *Client) DeleteAgent(ctx context.Context, shortID string, adopt bool) (*DeleteAgentResponse, error) {
	path := "/api/v1/agents/asserted/" + url.PathEscape(shortID)
	if adopt {
		path += "?adopt=true"
	}

	var out DeleteAgentResponse
	if err := c.do(ctx, http.MethodDelete, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// errorBody mirrors every error response from the API.
type errorBody struct {
	Success         bool   `json:"success"`
	Error           string `json:"error"`
	ExistingAgentID string `json:"existingAgentId"`
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytesReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "terraform-provider-alterion/"+Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("orion api request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read orion api response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var eb errorBody
		msg := strings.TrimSpace(string(respBody))
		if jsonErr := json.Unmarshal(respBody, &eb); jsonErr == nil && eb.Error != "" {
			msg = eb.Error
		}
		return &APIError{
			Status:          resp.StatusCode,
			Message:         msg,
			ExistingAgentID: eb.ExistingAgentID,
		}
	}

	if out == nil {
		return nil
	}
	if len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode orion api response: %w", err)
	}
	return nil
}

func bytesReader(b []byte) io.Reader {
	if b == nil {
		return nil
	}
	return bytes.NewReader(b)
}

// ExpectedShortID reproduces the server's short-id derivation: the first 12
// hex characters of sha256("asserted|<environment>|<identityKey>"). Used
// only for tests and a diagnostic warning — the server's response is
// always authoritative.
func ExpectedShortID(environment, identityKey string) string {
	sum := sha256.Sum256([]byte("asserted|" + environment + "|" + identityKey))
	return hex.EncodeToString(sum[:])[:12]
}

// IdentityKey reproduces, for ExpectedShortID only, the server's
// composition of a workload's identity into the string it hashes:
// "<cloudProvider>.<cloudAccountId>.<cloudRegion>.<workloadName>". This
// join never appears on the wire; workloadName is used verbatim and
// case-sensitive.
func IdentityKey(identity WorkloadIdentity) string {
	return fmt.Sprintf("%s.%s.%s.%s", identity.CloudProvider, identity.CloudAccountID, identity.CloudRegion, identity.WorkloadName)
}
