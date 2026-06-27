package technitium

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func startServer(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-token"), srv
}

func TestAddRecord_A_QueryAndOK(t *testing.T) {
	var got url.Values
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"response": map[string]any{},
		})
	})
	err := c.AddRecord(context.Background(), AddRecordRequest{
		Domain: "app.home.lab", Zone: "home.lab", Type: TypeA,
		Value: "192.168.1.50", TTL: 300, Overwrite: true, Comments: "via cli",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Get("token") != "test-token" {
		t.Errorf("token not sent: %q", got.Get("token"))
	}
	if got.Get("domain") != "app.home.lab" || got.Get("zone") != "home.lab" {
		t.Errorf("domain/zone wrong: %v", got)
	}
	if got.Get("type") != "A" || got.Get("ipAddress") != "192.168.1.50" {
		t.Errorf("type/ipAddress wrong: %v", got)
	}
	if got.Get("cname") != "" {
		t.Errorf("cname should be empty for A: %q", got.Get("cname"))
	}
	if got.Get("ttl") != "300" || got.Get("overwrite") != "true" || got.Get("comments") != "via cli" {
		t.Errorf("optional params wrong: %v", got)
	}
}

func TestAddRecord_CNAME_UsesCnameParam(t *testing.T) {
	var got url.Values
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{}})
	})
	err := c.AddRecord(context.Background(), AddRecordRequest{
		Domain: "app.home.lab", Zone: "home.lab", Type: TypeCNAME, Value: "caddy.home.lab.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Get("type") != "CNAME" || got.Get("cname") != "caddy.home.lab." {
		t.Errorf("type/cname wrong: %v", got)
	}
	if got.Get("ipAddress") != "" {
		t.Errorf("ipAddress should be empty for CNAME: %q", got.Get("ipAddress"))
	}
}

func TestAddRecord_NonOKReturnsAPIError(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "error",
			"errorMessage": "zone not found",
			"errorCode":    404,
		})
	})
	err := c.AddRecord(context.Background(), AddRecordRequest{
		Domain: "app.home.lab", Zone: "nope.lab", Type: TypeA, Value: "1.2.3.4",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "zone not found") {
		t.Fatalf("error should wrap server message: %v", err)
	}
}

func TestAddRecord_Validation(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	cases := []AddRecordRequest{
		{Type: TypeA, Value: "1.2.3.4"},                   // missing domain
		{Domain: "a", Type: TypeA},                        // missing value
		{Domain: "a", Type: RecordType("MX"), Value: "x"}, // unsupported type
	}
	for i, r := range cases {
		if err := c.AddRecord(context.Background(), r); err == nil {
			t.Errorf("case %d: expected error for %+v", i, r)
		}
	}
}

func TestCreateToken_ParsesToken(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/createToken" {
			t.Errorf("path wrong: %s", r.URL.Path)
		}
		if r.URL.Query().Get("user") != "admin" || r.URL.Query().Get("tokenName") != "cli" {
			t.Errorf("params wrong: %v", r.URL.Query())
		}
		if r.URL.Query().Get("totp") != "123456" {
			t.Errorf("totp wrong: %v", r.URL.Query().Get("totp"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"response": map[string]any{"token": "abc123"},
		})
	})
	tok, err := c.CreateToken(context.Background(), "admin", "secret", "123456", "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "abc123" {
		t.Fatalf("got token %q want abc123", tok)
	}
}

func TestCreateToken_OmitsEmptyTOTP(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["totp"]; ok {
			t.Errorf("totp param should be absent for non-2FA login, got %v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"response": map[string]any{"token": "abc123"},
		})
	})
	tok, err := c.CreateToken(context.Background(), "admin", "secret", "", "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "abc123" {
		t.Fatalf("got token %q want abc123", tok)
	}
}

func TestListRecords_Decodes(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/records/get" {
			t.Errorf("path wrong: %s", r.URL.Path)
		}
		if r.URL.Query().Get("listZone") != "true" || r.URL.Query().Get("zone") != "home.lab" {
			t.Errorf("params wrong: %v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"response": map[string]any{
				"records": []any{
					map[string]any{"name": "app.home.lab", "type": "A", "ttl": 3600, "rData": map[string]any{"ipAddress": "192.168.1.50"}},
				},
			},
		})
	})
	recs, err := c.ListRecords(context.Background(), "home.lab", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 || recs[0].Name != "app.home.lab" || recs[0].Type != "A" {
		t.Fatalf("got %+v", recs)
	}
}

func TestClient_BaseURLTrimmedAndBearerHeader(t *testing.T) {
	var auth string
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{}})
	})
	c.AddRecord(context.Background(), AddRecordRequest{Domain: "a", Zone: "z", Type: TypeA, Value: "1.2.3.4"})
	if auth != "Bearer test-token" {
		t.Fatalf("auth header: %q", auth)
	}
}
