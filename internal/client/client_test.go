package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
		if r.URL.Query().Get("environment") != "staging" || r.URL.Query().Get("slug") != "claims-review" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PathKeyResponse{
			Success:        true,
			AgentID:        "asserted|staging|claims-review",
			ShortID:        "e42ec80aebee",
			PathPrefix:     "a",
			GatewayBaseURL: "https://gw.example.com/a/e42ec80aebee",
		})
	})
	defer closeFn()

	resp, err := c.GetPathKey(context.Background(), "staging", "claims-review")
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
			ExistingAgentID: "asserted|staging|other-slug",
		})
	})
	defer closeFn()

	_, err := c.GetPathKey(context.Background(), "staging", "claims-review")
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
	if apiErr.ExistingAgentID != "asserted|staging|other-slug" {
		t.Errorf("unexpected existing agent id: %s", apiErr.ExistingAgentID)
	}
}

func TestGetPathKey_BadRequest400(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "invalid slug"})
	})
	defer closeFn()

	_, err := c.GetPathKey(context.Background(), "staging", "Bad Slug")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Message != "invalid slug" {
		t.Errorf("unexpected error: %+v", apiErr)
	}
}

func TestGetPathKey_Unauthorized401(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "invalid token"})
	})
	defer closeFn()

	_, err := c.GetPathKey(context.Background(), "staging", "claims-review")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 APIError, got %v", err)
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
		if body.Environment != "production" || body.Slug != "123456789012-us-east-1-support-bot" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CreateAgentResponse{
			Success:      true,
			AgentID:      "asserted|production|123456789012-us-east-1-support-bot",
			ShortID:      "75a30ffb2baa",
			Status:       "active",
			IsRegistered: true,
			Created:      true,
		})
	})
	defer closeFn()

	resp, err := c.CreateAgent(context.Background(), CreateAgentRequest{
		Environment: "production",
		Slug:        "123456789012-us-east-1-support-bot",
		DisplayName: "Support Bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Created || resp.ShortID != "75a30ffb2baa" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCreateAgent_Conflict409(t *testing.T) {
	c, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(errorBody{Success: false, Error: "owned by another principal"})
	})
	defer closeFn()

	_, err := c.CreateAgent(context.Background(), CreateAgentRequest{Environment: "production", Slug: "x", DisplayName: "X"})
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
			ArchivedAgentIDs: []string{"asserted|production|123456789012-us-east-1-support-bot"},
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
