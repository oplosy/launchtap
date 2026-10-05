package apiserver

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/metadata"
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/ethereum/go-ethereum/common"
)

type fakeVerifier struct {
	principal privyauth.Principal
	err       error
}

func (v fakeVerifier) Verify(context.Context, string, string) (privyauth.Principal, error) {
	return v.principal, v.err
}

type fakeMetadataStore struct {
	metadata    metadata.Metadata
	image       metadata.Image
	metadataSet bool
	imageSet    bool
	err         error
}

func (s *fakeMetadataStore) ReplaceMetadata(_ context.Context, _ int64, _ common.Address, _ []common.Address, value metadata.Metadata) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.metadataSet {
		value.Revision = s.metadata.Revision + 1
	}
	s.metadata = value
	s.metadataSet = true
	return value.Revision, nil
}
func (s *fakeMetadataStore) ReplaceImage(_ context.Context, _ int64, _ common.Address, _ []common.Address, value metadata.Image) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.imageSet {
		value.Revision = s.image.Revision + 1
	}
	s.image = value
	s.imageSet = true
	return value.Revision, nil
}
func (s *fakeMetadataStore) GetImage(context.Context, int64, common.Address) (metadata.Image, error) {
	if s.err != nil {
		return metadata.Image{}, s.err
	}
	return s.image, nil
}
func (s *fakeMetadataStore) GetMetadata(context.Context, int64, common.Address) (metadata.Metadata, error) {
	if s.err != nil {
		return metadata.Metadata{}, s.err
	}
	return s.metadata, nil
}

func TestMetadataAndImageHTTPContracts(t *testing.T) {
	store := &fakeMetadataStore{}
	creator := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://web.example"}
	server := New(cfg, ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterMetadataRoutes(MetadataRoutes{Store: store, Verifier: fakeVerifier{principal: privyauth.Principal{PrivyDID: "did:privy:test", Wallets: []common.Address{creator}}}, ChainID: 46630})
	token := "0x00000000000000000000000000000000000000bb"

	request := httptest.NewRequest(http.MethodPut, "/v1/tokens/"+token+"/metadata", strings.NewReader(`{"description":"plain text","x_url":"https://x.com/test","telegram_url":"https://t.me/test"}`))
	request.Header.Set("Content-Type", "application/json")
	authorize(request)
	request.Header.Set("If-Match", `"0"`)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"0"` || store.metadata.Description != "plain text" || store.metadata.Revision != 0 {
		t.Fatalf("metadata status=%d headers=%v body=%s stored=%+v", response.Code, response.Header(), response.Body.String(), store.metadata)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/tokens/"+token+"/metadata", nil)
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"0"` || !strings.Contains(response.Body.String(), `"revision":0`) || !strings.Contains(response.Body.String(), "plain text") {
		t.Fatalf("metadata read status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}

	png := testPNG(t, 2, 2)
	request = httptest.NewRequest(http.MethodPut, "/v1/tokens/"+token+"/image", bytes.NewReader(png))
	request.Header.Set("Content-Type", "image/png")
	authorize(request)
	request.Header.Set("If-Match", "0")
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"0"` || store.image.ContentType != "image/png" || store.image.Revision != 0 || !bytes.Equal(store.image.Content, png) {
		t.Fatalf("image write status=%d body=%s stored=%+v", response.Code, response.Body.String(), store.image)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/tokens/"+token+"/image", nil)
	request.Header.Set("Origin", "https://web.example")
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Content-Length") != strconv.Itoa(len(png)) || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Revision") != "0" || response.Header().Get("Cache-Control") != imageCacheControl || !bytes.Equal(response.Body.Bytes(), png) {
		t.Fatalf("image read status=%d headers=%v body=%x", response.Code, response.Header(), response.Body.Bytes())
	}
	if response.Header().Get("Access-Control-Expose-Headers") != "ETag, X-Revision" {
		t.Fatalf("allowed-origin image response does not expose validators: %v", response.Header())
	}
	etag := response.Header().Get("ETag")
	request = httptest.NewRequest(http.MethodGet, "/v1/tokens/"+token+"/image", nil)
	request.Header.Set("If-None-Match", etag)
	request.Header.Set("Origin", "https://web.example")
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotModified || response.Body.Len() != 0 {
		t.Fatalf("conditional read status=%d body=%x", response.Code, response.Body.Bytes())
	}
	for _, validator := range []string{"*", "W/" + etag} {
		request = httptest.NewRequest(http.MethodGet, "/v1/tokens/"+token+"/image", nil)
		request.Header.Set("If-None-Match", validator)
		response = httptest.NewRecorder()
		server.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotModified {
			t.Fatalf("conditional validator=%q status=%d", validator, response.Code)
		}
	}

	// Metadata and image revisions are independent concurrency domains. A write
	// in one resource must not advance or invalidate the other resource.
	request = httptest.NewRequest(http.MethodPut, "/v1/tokens/"+token+"/metadata", strings.NewReader(`{"description":"second"}`))
	request.Header.Set("Content-Type", "application/json")
	authorize(request)
	request.Header.Set("If-Match", `"0"`)
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"1"` || store.metadata.Revision != 1 || store.image.Revision != 0 {
		t.Fatalf("independent metadata revision status=%d headers=%v metadata=%d image=%d", response.Code, response.Header(), store.metadata.Revision, store.image.Revision)
	}
	request = httptest.NewRequest(http.MethodPut, "/v1/tokens/"+token+"/image", bytes.NewReader(png))
	request.Header.Set("Content-Type", "image/png")
	authorize(request)
	request.Header.Set("If-Match", `"0"`)
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"1"` || store.metadata.Revision != 1 || store.image.Revision != 1 {
		t.Fatalf("independent image revision status=%d headers=%v metadata=%d image=%d", response.Code, response.Header(), store.metadata.Revision, store.image.Revision)
	}
}

func TestSubjectLimiterBoundsIdentityState(t *testing.T) {
	limiter := NewSubjectLimiter(1, time.Minute)
	now := time.Unix(1_000, 0)
	for index := 0; index < maxSubjectLimiterEntries; index++ {
		if !limiter.Allow(strconv.Itoa(index), now) {
			t.Fatalf("entry %d rejected before capacity", index)
		}
	}
	if limiter.Allow("overflow", now) {
		t.Fatal("limiter accepted an unbounded identity entry")
	}
	if !limiter.Allow("replacement", now.Add(time.Minute)) || len(limiter.entries) != 1 {
		t.Fatalf("expired entries were not pruned: entries=%d", len(limiter.entries))
	}
}

func TestMetadataAuthenticationAuthorizationAndValidationProblems(t *testing.T) {
	token := "0x00000000000000000000000000000000000000bb"
	tests := []struct {
		name     string
		verifier privyauth.Verifier
		storeErr error
		body     string
		want     int
	}{
		{"authentication", fakeVerifier{err: privyauth.ErrInvalidCredentials}, nil, `{}`, http.StatusUnauthorized},
		{"authorization", fakeVerifier{principal: privyauth.Principal{PrivyDID: "did", Wallets: []common.Address{common.HexToAddress("0x01")}}}, metadata.ErrUnauthorized, `{}`, http.StatusForbidden},
		{"revision", fakeVerifier{principal: privyauth.Principal{PrivyDID: "did"}}, metadata.ErrRevisionConflict, `{}`, http.StatusPreconditionFailed},
		{"description", fakeVerifier{principal: privyauth.Principal{PrivyDID: "did"}}, nil, `{"description":"` + strings.Repeat("x", 2001) + `"}`, http.StatusUnprocessableEntity},
		{"url", fakeVerifier{principal: privyauth.Principal{PrivyDID: "did"}}, nil, `{"x_url":"http://x.com/test"}`, http.StatusUnprocessableEntity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeMetadataStore{err: test.storeErr}
			server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
			server.RegisterMetadataRoutes(MetadataRoutes{Store: store, Verifier: test.verifier, ChainID: 1})
			request := httptest.NewRequest(http.MethodPut, "/v1/tokens/"+token+"/metadata", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			authorize(request)
			request.Header.Set("If-Match", "0")
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != test.want {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, body)
			}
		})
	}
}

func TestMetadataMissingCredentialsAreRejected(t *testing.T) {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterMetadataRoutes(MetadataRoutes{Store: &fakeMetadataStore{}, Verifier: fakeVerifier{}, ChainID: 1})
	request := httptest.NewRequest(http.MethodPut, "/v1/tokens/0x00000000000000000000000000000000000000bb/metadata", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", "0")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d want=%d body=%s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestImageRejectsActiveMismatchAndOversizeContent(t *testing.T) {
	store := &fakeMetadataStore{}
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterMetadataRoutes(MetadataRoutes{Store: store, Verifier: fakeVerifier{principal: privyauth.Principal{PrivyDID: "did"}}, ChainID: 1})
	for _, test := range []struct {
		name, contentType string
		body              []byte
		want              int
	}{
		{"svg", "image/svg+xml", []byte(`<svg></svg>`), http.StatusUnsupportedMediaType},
		{"mismatch", "image/jpeg", testPNG(t, 2, 2), http.StatusUnsupportedMediaType},
		{"huge canvas", "image/png", testPNG(t, maxImageDimension+1, 1), http.StatusUnprocessableEntity},
		{"truncated png", "image/png", testPNG(t, 2, 2)[:12], http.StatusUnprocessableEntity},
		{"huge webp canvas", "image/webp", webpVP8X(maxImageDimension+1, 1), http.StatusUnprocessableEntity},
		{"oversize", "image/png", bytes.Repeat([]byte{0}, maxImageBytes+1), http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/v1/tokens/0x00000000000000000000000000000000000000bb/image", bytes.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			authorize(request)
			request.Header.Set("If-Match", "0")
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer access.token.value")
	request.Header.Set("privy-id-token", "identity.token.value")
}

var _ metadata.Store = (*fakeMetadataStore)(nil)
var _ privyauth.Verifier = fakeVerifier{}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// riffWebP builds a WebP header: RIFF, a zero size, WEBP, the first chunk tag, a zero chunk
// size, then the chunk payload.
func riffWebP(chunk string, payload ...byte) []byte {
	content := append([]byte("RIFF"), 0, 0, 0, 0)
	content = append(content, "WEBP"+chunk...)
	content = append(content, 0, 0, 0, 0)
	return append(content, payload...)
}

func webpVP8X(width, height int) []byte {
	payload := []byte{0, 0, 0, 0}
	for _, value := range []int{width - 1, height - 1} {
		payload = append(payload, byte(value), byte(value>>8), byte(value>>16))
	}
	return riffWebP("VP8X", payload...)
}

func TestWebPDimensions(t *testing.T) {
	bits := uint32(99) | uint32(49)<<14
	lossless := riffWebP("VP8L", 0x2f, byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24), 0, 0, 0, 0, 0)
	for name, test := range map[string]struct {
		content       []byte
		width, height int
	}{
		"extended": {webpVP8X(640, 480), 640, 480},
		"lossless": {lossless, 100, 50},
	} {
		width, height, err := webpDimensions(test.content)
		if err != nil || width != test.width || height != test.height {
			t.Errorf("%s: %dx%d err=%v", name, width, height, err)
		}
	}
	if _, _, err := webpDimensions(riffWebP("XXXX", make([]byte, 16)...)); err == nil {
		t.Fatal("unknown WebP chunk accepted")
	}
}
