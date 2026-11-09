package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHealthAndReadiness(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	for _, path := range []string{"/healthz", "/v1/healthz", "/readyz", "/v1/readyz"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, w.Code)
		}
		if w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s missing request id", path)
		}
	}
}

func TestServerBoundaries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://app.example"}
	var logs bytes.Buffer
	s := New(cfg, ReadyFunc(func(context.Context) error { return nil }), slog.New(slog.NewJSONHandler(&logs, nil)))
	if s.HTTP.ReadHeaderTimeout != 5*time.Second || s.HTTP.ReadTimeout != 15*time.Second || s.HTTP.WriteTimeout != 15*time.Second || s.HTTP.IdleTimeout != 60*time.Second || s.HTTP.MaxHeaderBytes != 32<<10 {
		t.Fatal("server timeout or header limits changed")
	}
	req := httptest.NewRequest(http.MethodOptions, "/v1/healthz", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("preflight status=%d headers=%v", w.Code, w.Header())
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET,POST,PUT,DELETE,OPTIONS" {
		t.Fatalf("preflight methods=%q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Authorization,Content-Type,privy-id-token,If-Match,If-None-Match" {
		t.Fatalf("preflight headers=%q", got)
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); got != "ETag, X-Revision, Retry-After" {
		t.Fatalf("preflight exposed headers=%q", got)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Fatal("access log leaked authorization")
	}
	bad := httptest.NewRequest(http.MethodOptions, "/v1/healthz", nil)
	bad.Header.Set("Origin", "https://evil.example")
	badW := httptest.NewRecorder()
	s.Handler.ServeHTTP(badW, bad)
	if badW.Code != http.StatusForbidden {
		t.Fatalf("disallowed origin status=%d", badW.Code)
	}
	deniedRead := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	deniedRead.Header.Set("Origin", "https://evil.example")
	deniedReadW := httptest.NewRecorder()
	s.Handler.ServeHTTP(deniedReadW, deniedRead)
	if deniedReadW.Header().Get("Access-Control-Allow-Origin") != "" || deniedReadW.Header().Get("Access-Control-Expose-Headers") != "" {
		t.Fatalf("disallowed origin received CORS read headers: %v", deniedReadW.Header())
	}
}

func TestReadinessFailureIsProblem(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return context.Canceled }), nil)
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type=%q", got)
	}
}

func TestCanonicalUint256(t *testing.T) {
	for _, value := range []string{"", "01", "-1", "1.0", "1e3", "+1"} {
		if _, err := parseCanonicalUint256(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if n, err := parseCanonicalUint256("0"); err != nil || n.Sign() != 0 {
		t.Fatalf("zero: %v", err)
	}
}

func TestOpenAPIContractIncludesPlan3Endpoints(t *testing.T) {
	generated, err := GenerateOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{`"/events"`, `"/tokens/{token}/metadata"`, `"/tokens/{token}/image"`} {
		if !bytes.Contains(generated, []byte(path)) {
			t.Fatalf("generated OpenAPI missing %s", path)
		}
	}
	var document struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatal(err)
	}
	priceChangeSchemaFound := false
	for _, schema := range document.Components.Schemas {
		if rawProperty, ok := schema.Properties["price_change_24h_bps"]; ok {
			priceChangeSchemaFound = true
			var property struct {
				Type    string `json:"type"`
				Format  string `json:"format"`
				Minimum int64  `json:"minimum"`
				Maximum int64  `json:"maximum"`
			}
			if err := json.Unmarshal(rawProperty, &property); err != nil {
				t.Fatalf("decode price_change_24h_bps OpenAPI schema: %v", err)
			}
			if property.Type != "integer" || property.Format != "int64" || property.Minimum != -9007199254740991 || property.Maximum != 9007199254740991 {
				t.Fatalf("price_change_24h_bps OpenAPI schema = %s/%s [%d, %d], want integer/int64 [%d, %d]", property.Type, property.Format, property.Minimum, property.Maximum, -9007199254740991, 9007199254740991)
			}
		}
	}
	if !priceChangeSchemaFound {
		t.Fatal("generated OpenAPI has no price_change_24h_bps property")
	}
	type parameter struct {
		Name     string `json:"name"`
		In       string `json:"in"`
		Required bool   `json:"required"`
	}
	type operation struct {
		Parameters  []parameter `json:"parameters"`
		RequestBody struct {
			Content map[string]json.RawMessage `json:"content"`
		} `json:"requestBody"`
		Responses map[string]json.RawMessage `json:"responses"`
	}
	decodeOperation := func(path, method string) operation {
		t.Helper()
		raw, ok := document.Paths[path][method]
		if !ok {
			t.Fatalf("OpenAPI operation missing: %s %s", method, path)
		}
		var decoded operation
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode OpenAPI operation %s %s: %v", method, path, err)
		}
		return decoded
	}
	assertRequiredHeaders := func(path, method string, want ...string) {
		t.Helper()
		got := make(map[string]bool, len(want))
		for _, item := range decodeOperation(path, method).Parameters {
			if item.In == "header" {
				got[item.Name] = item.Required
			}
		}
		for _, name := range want {
			if !got[name] {
				t.Errorf("%s %s header %q is not required", method, path, name)
			}
		}
	}
	assertRequiredHeaders("/profile", "get", "Authorization", "privy-id-token")
	assertRequiredHeaders("/tokens/{token}/metadata", "put", "Authorization", "privy-id-token", "If-Match")
	assertRequiredHeaders("/tokens/{token}/image", "put", "Authorization", "privy-id-token", "If-Match", "Content-Type")
	imagePut := decodeOperation("/tokens/{token}/image", "put")
	wantMedia := []string{"image/jpeg", "image/png", "image/webp"}
	gotMedia := make([]string, 0, len(imagePut.RequestBody.Content))
	for media := range imagePut.RequestBody.Content {
		gotMedia = append(gotMedia, media)
	}
	slices.Sort(gotMedia)
	if !slices.Equal(gotMedia, wantMedia) {
		t.Fatalf("image PUT media types=%v want=%v", gotMedia, wantMedia)
	}
	var notModified struct {
		Headers map[string]json.RawMessage `json:"headers"`
		Content map[string]json.RawMessage `json:"content"`
	}
	imageGet := decodeOperation("/tokens/{token}/image", "get")
	if response, ok := imageGet.Responses["304"]; !ok {
		t.Fatal("image GET does not document conditional 304 response")
	} else if err := json.Unmarshal(response, &notModified); err != nil {
		t.Fatal(err)
	}
	if notModified.Content != nil {
		t.Fatalf("304 response unexpectedly documents a body: %v", notModified.Content)
	}
	for _, header := range []string{"ETag", "X-Revision"} {
		if _, ok := notModified.Headers[header]; !ok {
			t.Errorf("304 response does not document %s", header)
		}
	}
	var defaultError struct {
		Description string `json:"description"`
		Content     map[string]struct {
			Schema struct {
				Ref string `json:"$ref"`
			} `json:"schema"`
		} `json:"content"`
	}
	if response, ok := imageGet.Responses["default"]; !ok {
		t.Fatal("image GET does not document its RFC problem error response")
	} else if err := json.Unmarshal(response, &defaultError); err != nil {
		t.Fatal(err)
	}
	if defaultError.Description != "Error" {
		t.Errorf("default image GET error description=%q want %q", defaultError.Description, "Error")
	}
	if got := defaultError.Content["application/problem+json"].Schema.Ref; got != "#/components/schemas/ErrorModel" {
		t.Errorf("default image GET error schema ref=%q want RFC problem ErrorModel", got)
	}
}

func TestOpenAPIBurstTradersIsNotNullable(t *testing.T) {
	generated, err := GenerateOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type json.RawMessage `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatal(err)
	}
	if got := string(document.Components.Schemas["BurstItem"].Properties["traders"].Type); got != `"array"` {
		t.Fatalf("BurstItem.traders type = %s, want \"array\"", got)
	}
}
