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
					map[string]any{"name": "app.home.lab", "type": "A", "ttl": 3600, "comments": "managed-by:hl", "rData": map[string]any{"ipAddress": "192.168.1.50"}},
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
	if recs[0].Comments != "managed-by:hl" {
		t.Errorf("comments not parsed: %q", recs[0].Comments)
	}
	if recs[0].Value() != "192.168.1.50" {
		t.Errorf("Value() wrong: %q", recs[0].Value())
	}
	if recs[0].Zone != "home.lab" {
		t.Errorf("Zone not populated from query: %q", recs[0].Zone)
	}
}

func TestListPrimaryZones_FiltersToPrimary(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/list" {
			t.Errorf("path wrong: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"response": map[string]any{
				"zones": []any{
					map[string]any{"name": "home.lab", "type": "Primary"},
					map[string]any{"name": "fwd.example", "type": "Forwarder"},
					map[string]any{"name": "sec.example", "type": "Secondary"},
					map[string]any{"name": "synology.com", "type": "Primary"},
				},
			},
		})
	})
	zones, err := c.ListPrimaryZones(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(zones) != 2 || zones[0] != "home.lab" || zones[1] != "synology.com" {
		t.Fatalf("expected only Primary zones, got %v", zones)
	}
}

func TestDeleteRecord_QueryAndOK(t *testing.T) {
	var got url.Values
	var path string
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{}})
	})
	err := c.DeleteRecord(context.Background(), DeleteRecordRequest{
		Domain: "app.home.lab", Zone: "home.lab", Type: TypeCNAME, Value: "caddy.home.lab.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/api/zones/records/delete" {
		t.Errorf("path wrong: %s", path)
	}
	if got.Get("domain") != "app.home.lab" || got.Get("zone") != "home.lab" {
		t.Errorf("domain/zone wrong: %v", got)
	}
	if got.Get("type") != "CNAME" || got.Get("cname") != "caddy.home.lab." || got.Get("value") != "caddy.home.lab." {
		t.Errorf("type/value wrong: %v", got)
	}
}

func TestDeleteRecord_RequiresValue(t *testing.T) {
	c, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	})
	err := c.DeleteRecord(context.Background(), DeleteRecordRequest{Domain: "a", Zone: "z", Type: TypeA})
	if err == nil {
		t.Fatal("expected error for missing value")
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
