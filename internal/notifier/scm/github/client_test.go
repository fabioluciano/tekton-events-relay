package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testClientToken  = "test-token"
	testClientNodeID = "D_kwDOABCDEF"
)

func strContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDoGraphQL_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" { //nolint:goconst // test string
			t.Errorf("expected POST, got %s", r.Method)
		}

		auth := r.Header.Get("Authorization")
		if auth != authBearerPrefix+testClientToken {
			t.Errorf("expected Bearer auth, got %s", auth)
		}

		resp := map[string]any{
			"data": map[string]any{ //nolint:goconst
				"repository": map[string]any{ //nolint:goconst
					"discussion": map[string]any{ //nolint:goconst
						"id": testClientNodeID,
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(testClientToken, server.URL, false, nil, false)
	query := `query { repository(owner: "test", name: "repo") { discussion(number: 1) { id } } }`
	variables := map[string]any{"owner": "test", "name": "repo", "number": 1}

	data, err := client.DoGraphQL(context.Background(), query, variables)
	if err != nil {
		t.Fatalf("DoGraphQL failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}

	repo, ok := result["repository"].(map[string]any)
	if !ok {
		t.Fatal("expected repository in data")
	}

	discussion, ok := repo["discussion"].(map[string]any)
	if !ok {
		t.Fatal("expected discussion in repository")
	}

	if discussion["id"] != testClientNodeID {
		t.Errorf("expected node ID %s, got %v", testClientNodeID, discussion["id"])
	}
}

func TestDoGraphQL_BearerAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token-123" {
			t.Errorf("expected Bearer my-token-123, got %s", auth)
		}

		resp := map[string]any{"data": map[string]any{}}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("my-token-123", server.URL, false, nil, false)
	_, err := client.DoGraphQL(context.Background(), "query { __typename }", nil)
	if err != nil {
		t.Fatalf("DoGraphQL failed: %v", err)
	}
}

func TestDoGraphQL_EndpointRouting_GitHubCom(t *testing.T) {
	// Test github.com endpoint logic
	client := NewClient("token", "https://api.github.com", false, nil, false)
	endpoint := client.graphqlEndpoint()
	expected := "https://api.github.com/graphql"
	if endpoint != expected {
		t.Errorf("github.com endpoint: expected %s, got %s", expected, endpoint)
	}
}

func TestDoGraphQL_EndpointRouting_GHES(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graphql" {
			t.Errorf("expected /api/graphql path for GHES, got %s", r.URL.Path)
		}
		resp := map[string]any{"data": map[string]any{}}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("token", server.URL, false, nil, false)
	_, err := client.DoGraphQL(context.Background(), "query { __typename }", nil)
	if err != nil {
		t.Fatalf("DoGraphQL failed: %v", err)
	}
}

func TestDoGraphQL_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"errors": []map[string]any{
				{
					"message": "Field 'discussion' doesn't exist on type 'Repository'",
					"type":    "NOT_FOUND",
					"path":    []any{"repository", "discussion"},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("token", server.URL, false, nil, false)
	_, err := client.DoGraphQL(context.Background(), "query { invalid }", nil)
	if err == nil {
		t.Fatal("expected error from GraphQL errors array")
	}

	errMsg := err.Error()
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}

	// Should contain message, type, and path
	expectedSubstrings := []string{"graphql error", "doesn't exist", "NOT_FOUND"}
	for _, sub := range expectedSubstrings {
		if !strContains(errMsg, sub) {
			t.Errorf("error message missing %q: %s", sub, errMsg)
		}
	}
}

func TestDoGraphQL_MultipleGraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"errors": []map[string]any{
				{"message": "Error 1"},
				{"message": "Error 2"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("token", server.URL, false, nil, false)
	_, err := client.DoGraphQL(context.Background(), "query { invalid }", nil)
	if err == nil {
		t.Fatal("expected error from GraphQL errors array")
	}

	errMsg := err.Error()
	if !strContains(errMsg, "2 errors") {
		t.Errorf("expected '2 errors' in message, got: %s", errMsg)
	}
	if !strContains(errMsg, "Error 1") || !strContains(errMsg, "Error 2") {
		t.Errorf("expected both error messages, got: %s", errMsg)
	}
}

func TestDoGraphQL_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("bad-token", server.URL, false, nil, false)
	_, err := client.DoGraphQL(context.Background(), "query {}", nil)
	if err == nil {
		t.Fatal("expected error from HTTP 401")
	}

	if !strContains(err.Error(), "401") {
		t.Errorf("expected '401' in error, got: %s", err.Error())
	}
}
