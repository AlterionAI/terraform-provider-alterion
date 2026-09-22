package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := New(srv.URL, "orion_at_testtoken")
	return c, srv.Close
}

func TestGetPathKey_Success(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/agents/asserted/path-key" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer orion_at_testtoken" {
			t.Errorf("unexpected auth header: %s", got)
		}
		q := r.URL.Query()
		if q.Get("environment") != "staging" || q.Get("cloudProvider") != "aws" || q.Get("cloudAccountId") != "123456789012" ||
			q.Get("cloudRegion") != "us-west-2" || q.Get("workloadName") != "claims-review" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PathKeyResponse{
			Success:        true,
			AgentID:        "asserted|staging|aws.123456789012.us-west-2.claims-review",
			ShortID:        "e42ec80aebee",
			PathPrefix:     "a",
			GatewayBaseURL: "https://gw.example.com/a/e42ec80aebee",
		})
	})
	defer closeFn()

	identity := WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-west-2", WorkloadName: "claims-review"}
	resp, err := c.GetPathKey(context.Background(), "staging", identity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ShortID != "e42ec80aebee" {
		t.Errorf("unexpected short id: %s", resp.ShortID)
	}
	if resp.GatewayBaseURL != "https://gw.example.com/a/e42ec80aebee" {
		t.Errorf("unexpected gateway base url: %s", resp.GatewayBaseURL)
	}
}

func TestGetPathKey_Collision409(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(errorBody{
			Success:         false,
			Error:           "short id collision",
			ExistingAgentID: "asserted|staging|aws.123456789012.us-west-2.other-workload",
		})
	})
	defer closeFn()

	identity := WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-west-2", WorkloadName: "claims-review"}
	_, err := c.GetPathKey(context.Background(), "staging", identity)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Status != http.StatusConflict {
		t.Errorf("expected status 409, got %d", apiErr.Status)
	}
	if apiErr.ExistingAgentID != "asserted|staging|aws.123456789012.us-west-2.other-workload" {
		t.Errorf("unexpected existing agent id: %s", apiErr.ExistingAgentID)
	}
}

func TestGetPathKey_BadRequest400(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "invalid workload name"})
	})
	defer closeFn()

	identity := WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-west-2", WorkloadName: "Bad Name"}
	_, err := c.GetPathKey(context.Background(), "staging", identity)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Message != "invalid workload name" {
		t.Errorf("unexpected error: %+v", apiErr)
	}
}

func TestGetPathKey_Unauthorized401(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "invalid token"})
	})
	defer closeFn()

	identity := WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-west-2", WorkloadName: "claims-review"}
	_, err := c.GetPathKey(context.Background(), "staging", identity)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 APIError, got %v", err)
	}
}

func TestGetAgent_Success(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/agents/asserted/75a30ffb2baa" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AgentResponse{
			Success:      true,
			AgentID:      "asserted|production|aws.123456789012.us-east-1.support-bot",
			ShortID:      "75a30ffb2baa",
			DisplayName:  "Support Bot",
			Status:       "active",
			IsRegistered: true,
			RegisteredBy: "orion_at_testtoken",
		})
	})
	defer closeFn()

	resp, err := c.GetAgent(context.Background(), "75a30ffb2baa")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DisplayName != "Support Bot" || resp.Status != "active" || !resp.IsRegistered {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGetAgent_NotFound404(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "agent not found"})
	})
	defer closeFn()

	_, err := c.GetAgent(context.Background(), "gone")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusNotFound {
		t.Fatalf("expected 404 APIError, got %v", err)
	}
	if apiErr.Message != "agent not found" {
		t.Errorf("unexpected message: %s", apiErr.Message)
	}
}

func TestCreateAgent_Success(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/agents/asserted" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body CreateAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.Environment != "production" || body.CloudProvider != "aws" || body.CloudAccountID != "123456789012" ||
			body.CloudRegion != "us-east-1" || body.WorkloadName != "support-bot" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CreateAgentResponse{
			Success:      true,
			AgentID:      "asserted|production|aws.123456789012.us-east-1.support-bot",
			ShortID:      "75a30ffb2baa",
			Status:       "active",
			IsRegistered: true,
			Created:      true,
		})
	})
	defer closeFn()

	resp, err := c.CreateAgent(context.Background(), CreateAgentRequest{
		Environment:    "production",
		CloudProvider:  "aws",
		CloudAccountID: "123456789012",
		CloudRegion:    "us-east-1",
		WorkloadName:   "support-bot",
		DisplayName:    "Support Bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Created || resp.ShortID != "75a30ffb2baa" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCreateAgent_FunctionalBoundaries(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body CreateAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		want := []string{"production-support", "finance"}
		if len(body.FunctionalBoundaries) != len(want) || body.FunctionalBoundaries[0] != want[0] || body.FunctionalBoundaries[1] != want[1] {
			t.Errorf("unexpected functionalBoundaries: %+v", body.FunctionalBoundaries)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CreateAgentResponse{Success: true, ShortID: "75a30ffb2baa", Created: true})
	})
	defer closeFn()

	_, err := c.CreateAgent(context.Background(), CreateAgentRequest{
		Environment:          "production",
		CloudProvider:        "aws",
		CloudAccountID:       "123456789012",
		CloudRegion:          "us-east-1",
		WorkloadName:         "support-bot",
		DisplayName:          "Support Bot",
		FunctionalBoundaries: []string{"production-support", "finance"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateAgent_Conflict409(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "owned by another principal"})
	})
	defer closeFn()

	_, err := c.CreateAgent(context.Background(), CreateAgentRequest{Environment: "production", CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "x", DisplayName: "X"})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusConflict {
		t.Fatalf("expected 409 APIError, got %v", err)
	}
}

func TestDeleteAgent_Success(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/agents/asserted/75a30ffb2baa" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(DeleteAgentResponse{
			Success:          true,
			ArchivedAgentIDs: []string{"asserted|production|aws.123456789012.us-east-1.support-bot"},
		})
	})
	defer closeFn()

	resp, err := c.DeleteAgent(context.Background(), "75a30ffb2baa", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ArchivedAgentIDs) != 1 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestDeleteAgent_AdoptQueryParam(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "adopt=true" {
			t.Errorf("expected adopt=true query, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(DeleteAgentResponse{Success: true})
	})
	defer closeFn()

	if _, err := c.DeleteAgent(context.Background(), "shortid123", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteAgent_NotFound404(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "not found"})
	})
	defer closeFn()

	_, err := c.DeleteAgent(context.Background(), "gone", false)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusNotFound {
		t.Fatalf("expected 404 APIError, got %v", err)
	}
}

// TestRequestShape asserts the outbound request headers/method/path for
// every route: Authorization: Bearer, User-Agent, JSON body field names
// (including functionalBoundaries) on POST.
func TestRequestShape(t *testing.T) {
	Version = "1.2.3"
	defer func() { Version = "dev" }()

	var gotMethod, gotPath, gotAuth, gotUA, gotAccept, gotContentType string
	var gotBody map[string]interface{}

	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CreateAgentResponse{Success: true, ShortID: "abc123"})
	})
	defer closeFn()

	_, err := c.CreateAgent(context.Background(), CreateAgentRequest{
		Environment:          "production",
		CloudProvider:        "aws",
		CloudAccountID:       "123456789012",
		CloudRegion:          "us-east-1",
		WorkloadName:         "support-bot",
		WorkloadResourceID:   "arn:aws:bedrock-agentcore:us-east-1:123456789012:runtime/support-bot",
		WorkloadType:         "bedrock-agentcore-runtime",
		DisplayName:          "Support Bot",
		FunctionalBoundaries: []string{"production-support"},
		Adopt:                true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/api/v1/agents/asserted" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer orion_at_testtoken" {
		t.Errorf("unexpected Authorization header: %s", gotAuth)
	}
	if gotUA != "terraform-provider-alterion/1.2.3" {
		t.Errorf("unexpected User-Agent header: %s", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("unexpected Accept header: %s", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Errorf("unexpected Content-Type header: %s", gotContentType)
	}

	wantFields := map[string]interface{}{
		"environment":        "production",
		"cloudProvider":      "aws",
		"cloudAccountId":     "123456789012",
		"cloudRegion":        "us-east-1",
		"workloadName":       "support-bot",
		"workloadResourceId": "arn:aws:bedrock-agentcore:us-east-1:123456789012:runtime/support-bot",
		"workloadType":       "bedrock-agentcore-runtime",
		"displayName":        "Support Bot",
		"adopt":              true,
	}
	for field, want := range wantFields {
		if got := gotBody[field]; got != want {
			t.Errorf("body field %q = %v, want %v", field, got, want)
		}
	}
	fb, ok := gotBody["functionalBoundaries"].([]interface{})
	if !ok || len(fb) != 1 || fb[0] != "production-support" {
		t.Errorf("unexpected functionalBoundaries field: %+v", gotBody["functionalBoundaries"])
	}
}

// TestGetPathKey_NoAuthHeaderLeak just re-confirms GET requests carry no
// body/Content-Type header (GET never sends a JSON body).
func TestGetPathKey_NoContentTypeOnGET(t *testing.T) {
	var gotContentType string
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PathKeyResponse{Success: true})
	})
	defer closeFn()

	identity := WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "x"}
	if _, err := c.GetPathKey(context.Background(), "staging", identity); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "" {
		t.Errorf("expected no Content-Type on GET, got %q", gotContentType)
	}
}

// TestTypedAPIError_CodeAndBoundaryName proves the client parses "code"
// and "boundaryName" off the error body into the typed *APIError.
func TestTypedAPIError_CodeAndBoundaryName(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorBody{
			Success:      false,
			Error:        "functional boundary not found",
			Code:         "ASSERTED_WORKLOAD_BOUNDARY_NOT_FOUND",
			BoundaryName: "finance",
		})
	})
	defer closeFn()

	_, err := c.CreateAgent(context.Background(), CreateAgentRequest{Environment: "production", CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "x", DisplayName: "X"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != "ASSERTED_WORKLOAD_BOUNDARY_NOT_FOUND" {
		t.Errorf("unexpected code: %s", apiErr.Code)
	}
	if apiErr.BoundaryName != "finance" {
		t.Errorf("unexpected boundary name: %s", apiErr.BoundaryName)
	}
	if !strings.Contains(apiErr.Error(), "finance") {
		t.Errorf("expected error message to mention boundary, got: %s", apiErr.Error())
	}
}

// TestTypedAPIError_Unauthorized401Message proves the 401 message tells
// the caller to mint a new token, per the API contract (token invalid,
// expired, revoked, or owner lost the approver role).
func TestTypedAPIError_Unauthorized401Message(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "token expired"})
	})
	defer closeFn()

	_, err := c.GetAgent(context.Background(), "shortid")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 APIError, got %v", err)
	}
	if !strings.Contains(apiErr.Error(), "mint a new token") {
		t.Errorf("expected 401 error message to say to mint a new token, got: %s", apiErr.Error())
	}
}

// TestErrorCodes_AllListedCodes table-drives every error code/status pair
// from the API contract, proving each round-trips through the typed
// *APIError with the right status and code.
func TestErrorCodes_AllListedCodes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
	}{
		{"invalid asserted workload", http.StatusBadRequest, "INVALID_ASSERTED_WORKLOAD"},
		{"owned by other", http.StatusConflict, "ASSERTED_WORKLOAD_OWNED_BY_OTHER"},
		{"unowned", http.StatusConflict, "ASSERTED_WORKLOAD_UNOWNED"},
		{"short id collision", http.StatusConflict, "ASSERTED_WORKLOAD_SHORT_ID_COLLISION"},
		{"boundary not found", http.StatusBadRequest, "ASSERTED_WORKLOAD_BOUNDARY_NOT_FOUND"},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: tc.name, Code: tc.code})
			})
			defer closeFn()

			_, err := c.GetAgent(context.Background(), "shortid")
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T", err)
			}
			if apiErr.Status != tc.status || apiErr.Code != tc.code {
				t.Errorf("got status=%d code=%s, want status=%d code=%s", apiErr.Status, apiErr.Code, tc.status, tc.code)
			}
		})
	}
}

// TestRouteLevelStatuses_401_403_404_500 covers the route-level statuses
// that apply regardless of endpoint-specific error codes.
func TestRouteLevelStatuses_401_403_404_500(t *testing.T) {
	statuses := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "route level error"})
			})
			defer closeFn()

			_, err := c.GetAgent(context.Background(), "shortid")
			apiErr, ok := err.(*APIError)
			if !ok || apiErr.Status != status {
				t.Fatalf("expected %d APIError, got %v", status, err)
			}
		})
	}
}

// TestGetAgent_5xxGeneric covers a bare 500 with a plain-text (non-JSON)
// body, which must still surface as a typed *APIError with the raw body
// as the message.
func TestGetAgent_5xxGeneric(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	})
	defer closeFn()

	_, err := c.GetAgent(context.Background(), "shortid")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 APIError, got %v", err)
	}
	if apiErr.Message != "internal server error" {
		t.Errorf("unexpected message: %q", apiErr.Message)
	}
}

// TestGetAgent_MalformedJSONOnSuccess proves a 2xx response with a body
// that isn't valid JSON surfaces as a plain (non-APIError) decode error.
func TestGetAgent_MalformedJSONOnSuccess(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not valid json"))
	})
	defer closeFn()

	_, err := c.GetAgent(context.Background(), "shortid")
	if err == nil {
		t.Fatal("expected a decode error, got nil")
	}
	if _, ok := err.(*APIError); ok {
		t.Fatalf("expected a plain decode error, got *APIError: %v", err)
	}
	if !strings.Contains(err.Error(), "decode orion api response") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestGetAgent_MalformedJSONOnError proves a non-2xx response with a body
// that isn't valid JSON still surfaces as a typed *APIError, falling back
// to the raw body text as the message.
func TestGetAgent_MalformedJSONOnError(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	})
	defer closeFn()

	_, err := c.GetAgent(context.Background(), "shortid")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusBadGateway {
		t.Fatalf("expected 502 APIError, got %v", err)
	}
	if apiErr.Message != "<html>bad gateway</html>" {
		t.Errorf("unexpected fallback message: %q", apiErr.Message)
	}
}

// TestGetAgent_ConnectionRefused proves a connection failure (server
// closed) surfaces as a plain error, not an *APIError (there was no HTTP
// response to type).
func TestGetAgent_ConnectionRefused(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	closeFn() // close immediately so the connection is refused

	_, err := c.GetAgent(context.Background(), "shortid")
	if err == nil {
		t.Fatal("expected a connection error, got nil")
	}
	if _, ok := err.(*APIError); ok {
		t.Fatalf("expected a plain connection error, got *APIError: %v", err)
	}
	if !strings.Contains(err.Error(), "orion api request failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestGetAgent_Timeout proves a server that never responds within the
// client's timeout surfaces as a plain (non-APIError) timeout error.
func TestGetAgent_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "orion_at_testtoken")
	c.httpClient.Timeout = 20 * time.Millisecond

	_, err := c.GetAgent(context.Background(), "shortid")
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if _, ok := err.(*APIError); ok {
		t.Fatalf("expected a plain timeout error, got *APIError: %v", err)
	}
}

// TestGetAgent_ContextCanceled proves a canceled context surfaces as a
// plain error too, distinct from a server-side timeout.
func TestGetAgent_ContextCanceled(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	defer closeFn()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetAgent(ctx, "shortid")
	if err == nil {
		t.Fatal("expected a context-canceled error, got nil")
	}
}

func TestDeleteAgent_Forbidden403(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "owned by another principal"})
	})
	defer closeFn()

	_, err := c.DeleteAgent(context.Background(), "shortid123", false)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusForbidden {
		t.Fatalf("expected 403 APIError, got %v", err)
	}
}
