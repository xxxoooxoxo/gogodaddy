package godaddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddRecordsSendsPatchWithAuthAndShopperHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/v1/domains/example.com/records" {
			t.Errorf("path = %s, want /v1/domains/example.com/records", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "sso-key key:secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Shopper-Id"); got != "shopper-123" {
			t.Errorf("X-Shopper-Id = %q", got)
		}

		var records []DNSRecord
		if err := json.NewDecoder(r.Body).Decode(&records); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(records) != 1 || records[0].Type != "TXT" || records[0].Name != "_acme-challenge" || records[0].Data != "token" {
			t.Errorf("records = %#v", records)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "secret", "shopper-123", server.Client())
	if err := client.AddRecords(context.Background(), "example.com", []DNSRecord{{
		Type: "TXT",
		Name: "_acme-challenge",
		Data: "token",
	}}); err != nil {
		t.Fatalf("AddRecords() error = %v", err)
	}
}

func TestGetRecordsDecodesResponseAndUsesQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.URL.Path; got != "/v1/domains/example.com/records/TXT/@" {
			t.Errorf("path = %s, want /v1/domains/example.com/records/TXT/@", got)
		}
		if got := r.URL.Query().Get("offset"); got != "5" {
			t.Errorf("offset = %q, want 5", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit = %q, want 10", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"type":"TXT","name":"@","data":"hello","ttl":600}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "secret", "", server.Client())
	records, err := client.GetRecords(context.Background(), "example.com", "TXT", "@", 5, 10)
	if err != nil {
		t.Fatalf("GetRecords() error = %v", err)
	}
	if len(records) != 1 || records[0].Data != "hello" || records[0].TTL == nil || *records[0].TTL != 600 {
		t.Fatalf("records = %#v", records)
	}
}

func TestReplaceRecordsByTypeNameOmitsTypeAndNameFromBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/v1/domains/example.com/records/A/www" {
			t.Errorf("path = %s, want /v1/domains/example.com/records/A/www", r.URL.Path)
		}

		var body []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(body) != 1 {
			t.Fatalf("body length = %d, want 1", len(body))
		}
		if _, ok := body[0]["type"]; ok {
			t.Errorf("replace type/name body included type: %#v", body[0])
		}
		if _, ok := body[0]["name"]; ok {
			t.Errorf("replace type/name body included name: %#v", body[0])
		}
		if body[0]["data"] != "192.0.2.2" {
			t.Errorf("data = %#v", body[0]["data"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "secret", "", server.Client())
	err := client.ReplaceRecordsByTypeName(context.Background(), "example.com", "A", "www", []DNSRecord{{
		Type: "A",
		Name: "www",
		Data: "192.0.2.2",
	}})
	if err != nil {
		t.Fatalf("ReplaceRecordsByTypeName() error = %v", err)
	}
}

func TestDeleteRecordsByTypeNameSendsDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/v1/domains/example.com/records/TXT/_acme-challenge" {
			t.Errorf("path = %s, want /v1/domains/example.com/records/TXT/_acme-challenge", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "secret", "", server.Client())
	if err := client.DeleteRecordsByTypeName(context.Background(), "example.com", "TXT", "_acme-challenge"); err != nil {
		t.Fatalf("DeleteRecordsByTypeName() error = %v", err)
	}
}
