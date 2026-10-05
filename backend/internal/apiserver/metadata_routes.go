package apiserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Contictus/launchtap/backend/internal/metadata"
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/danielgtaylor/huma/v2"
)

const (
	maxImageBytes            = 5 << 20
	maxImageDimension        = 4096
	maxSubjectLimiterEntries = 10_000
	// Image URLs carry no content hash, so browsers revalidate after a short window and the
	// ETag turns repeat loads into bodiless 304 responses.
	imageCacheControl = "public, max-age=60, stale-while-revalidate=600"
)

type MetadataRoutes struct {
	Store    metadata.Store
	Verifier privyauth.Verifier
	ChainID  int64
	Limiter  *SubjectLimiter
}

type metadataBody struct {
	Description string `json:"description,omitempty"`
	XURL        string `json:"x_url,omitempty"`
	TelegramURL string `json:"telegram_url,omitempty"`
}

type metadataWriteInput struct {
	Token         string       `path:"token"`
	Authorization string       `header:"Authorization" required:"true"`
	IdentityToken string       `header:"privy-id-token" required:"true"`
	IfMatch       string       `header:"If-Match" required:"true"`
	Body          metadataBody `json:"body"`
}

type metadataReadInput struct {
	Token string `path:"token"`
}

type imageWriteInput struct {
	Token         string `path:"token"`
	Authorization string `header:"Authorization" required:"true"`
	IdentityToken string `header:"privy-id-token" required:"true"`
	IfMatch       string `header:"If-Match" required:"true"`
	ContentType   string `header:"Content-Type" required:"true"`
	RawBody       []byte `contentType:"image/png"`
}

type imageReadInput struct {
	Token       string `path:"token"`
	IfNoneMatch string `header:"If-None-Match"`
}

type revisionBody struct {
	Revision int64  `json:"revision"`
	ImageURL string `json:"image_url,omitempty"`
}

type metadataReadBody struct {
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	XURL        string `json:"x_url,omitempty"`
	TelegramURL string `json:"telegram_url,omitempty"`
	Revision    int64  `json:"revision"`
}

type revisionOutput struct {
	ETag string `header:"ETag"`
	Body revisionBody
}

type imageOutput struct {
	Status        int    `status:"200"`
	ContentType   string `header:"Content-Type"`
	ContentLength int    `header:"Content-Length"`
	ETag          string `header:"ETag"`
	Revision      int64  `header:"X-Revision"`
	NoSniff       string `header:"X-Content-Type-Options"`
	CacheControl  string `header:"Cache-Control"`
	Body          []byte
}

func (r MetadataRoutes) Register(api huma.API) {
	if r.Limiter == nil {
		r.Limiter = NewSubjectLimiter(30, time.Minute)
	}
	huma.Register(api, huma.Operation{OperationID: "replaceTokenMetadata", Method: http.MethodPut, Path: "/tokens/{token}/metadata", Tags: []string{"metadata"}}, r.replaceMetadata)
	huma.Register(api, huma.Operation{OperationID: "getTokenMetadata", Method: http.MethodGet, Path: "/tokens/{token}/metadata", Tags: []string{"metadata"}}, r.getMetadata)
	huma.Register(api, huma.Operation{
		OperationID: "replaceTokenImage", Method: http.MethodPut, Path: "/tokens/{token}/image", Tags: []string{"metadata"},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
			"image/png": {Schema: &huma.Schema{Type: "string", Format: "binary"}}, "image/jpeg": {Schema: &huma.Schema{Type: "string", Format: "binary"}}, "image/webp": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
		}},
	}, r.replaceImage)
	huma.Register(api, huma.Operation{
		OperationID: "getTokenImage", Method: http.MethodGet, Path: "/tokens/{token}/image", Tags: []string{"metadata"},
		Responses: map[string]*huma.Response{
			"200": {Description: "Token image", Content: map[string]*huma.MediaType{
				"image/png": {Schema: &huma.Schema{Type: "string", Format: "binary"}}, "image/jpeg": {Schema: &huma.Schema{Type: "string", Format: "binary"}}, "image/webp": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
			}},
			"304": {
				Description: "Not modified when If-None-Match matches; the response has no body.",
				Headers: map[string]*huma.Param{
					"ETag":                   {Description: "Image content hash validator.", Schema: &huma.Schema{Type: "string"}},
					"X-Revision":             {Description: "Image revision.", Schema: &huma.Schema{Type: "integer", Format: "int64"}},
					"X-Content-Type-Options": {Description: "Image response security policy.", Schema: &huma.Schema{Type: "string"}},
					"Cache-Control":          {Description: "Image cache policy.", Schema: &huma.Schema{Type: "string"}},
				},
			},
			"default": {
				Description: "Error",
				Content: map[string]*huma.MediaType{
					"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/ErrorModel"}},
				},
			},
		},
	}, r.getImage)
}

func (r MetadataRoutes) getMetadata(ctx context.Context, input *metadataReadInput) (*struct {
	ETag string `header:"ETag"`
	Body metadataReadBody
}, error) {
	tokenAddress, err := address(input.Token)
	if err != nil {
		return nil, err
	}
	if r.Store == nil {
		return nil, apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
	value, err := r.Store.GetMetadata(ctx, r.ChainID, tokenAddress)
	if err != nil {
		return nil, mapMetadataError(ctx, err)
	}
	return &struct {
		ETag string `header:"ETag"`
		Body metadataReadBody
	}{ETag: revisionETag(value.Revision), Body: metadataReadBody{Description: value.Description, ImageURL: value.ImageURL, XURL: value.XURL, TelegramURL: value.TelegramURL, Revision: value.Revision}}, nil
}

func (r MetadataRoutes) replaceMetadata(ctx context.Context, input *metadataWriteInput) (*revisionOutput, error) {
	tokenAddress, err := address(input.Token)
	if err != nil {
		return nil, err
	}
	principal, err := r.authenticate(ctx, input.Authorization, input.IdentityToken)
	if err != nil {
		return nil, err
	}
	revision, err := parseRevision(input.IfMatch)
	if err != nil {
		return nil, err
	}
	if err := validateMetadata(input.Body); err != nil {
		return nil, err
	}
	imageURL := "/v1/tokens/" + tokenAddress.Hex() + "/image"
	next, err := r.Store.ReplaceMetadata(ctx, r.ChainID, tokenAddress, principal.Wallets, metadata.Metadata{
		Description: input.Body.Description, ImageURL: imageURL, XURL: input.Body.XURL, TelegramURL: input.Body.TelegramURL, Revision: revision, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, mapMetadataError(ctx, err)
	}
	return &revisionOutput{ETag: revisionETag(next), Body: revisionBody{Revision: next, ImageURL: imageURL}}, nil
}

func (r MetadataRoutes) replaceImage(ctx context.Context, input *imageWriteInput) (*revisionOutput, error) {
	tokenAddress, err := address(input.Token)
	if err != nil {
		return nil, err
	}
	principal, err := r.authenticate(ctx, input.Authorization, input.IdentityToken)
	if err != nil {
		return nil, err
	}
	revision, err := parseRevision(input.IfMatch)
	if err != nil {
		return nil, err
	}
	detected, err := detectImage(input.RawBody)
	if err != nil {
		return nil, err
	}
	declared := strings.ToLower(strings.TrimSpace(strings.Split(input.ContentType, ";")[0]))
	if declared != detected {
		return nil, apiProblem(http.StatusUnsupportedMediaType, "image_type_mismatch", "Declared and detected image types differ")
	}
	hash := sha256.Sum256(input.RawBody)
	next, err := r.Store.ReplaceImage(ctx, r.ChainID, tokenAddress, principal.Wallets, metadata.Image{
		ContentType: detected, Content: append([]byte(nil), input.RawBody...), SHA256: hash, Revision: revision, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, mapMetadataError(ctx, err)
	}
	return &revisionOutput{ETag: revisionETag(next), Body: revisionBody{Revision: next, ImageURL: "/v1/tokens/" + tokenAddress.Hex() + "/image"}}, nil
}

func (r MetadataRoutes) getImage(ctx context.Context, input *imageReadInput) (*imageOutput, error) {
	tokenAddress, err := address(input.Token)
	if err != nil {
		return nil, err
	}
	image, err := r.Store.GetImage(ctx, r.ChainID, tokenAddress)
	if err != nil {
		return nil, mapMetadataError(ctx, err)
	}
	etag := `"sha256-` + hex.EncodeToString(image.SHA256[:]) + `"`
	if matchesETag(input.IfNoneMatch, etag) {
		return &imageOutput{Status: http.StatusNotModified, ETag: etag, Revision: image.Revision, NoSniff: "nosniff", CacheControl: imageCacheControl}, nil
	}
	out := &imageOutput{Status: http.StatusOK, ContentType: image.ContentType, ContentLength: len(image.Content), ETag: etag, Revision: image.Revision, NoSniff: "nosniff", CacheControl: imageCacheControl, Body: image.Content}
	return out, nil
}

func (r MetadataRoutes) authenticate(ctx context.Context, authorization, identity string) (privyauth.Principal, error) {
	if r.Verifier == nil || r.Store == nil {
		return privyauth.Principal{}, apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
	access, err := privyauth.ParseBearer(authorization)
	if err != nil {
		return privyauth.Principal{}, apiProblem(http.StatusUnauthorized, "authentication_failed", "Authentication failed")
	}
	principal, err := r.Verifier.Verify(ctx, access, identity)
	if err != nil {
		return privyauth.Principal{}, apiProblem(http.StatusUnauthorized, "authentication_failed", "Authentication failed")
	}
	if !r.Limiter.Allow(principal.PrivyDID, time.Now()) {
		return privyauth.Principal{}, apiProblem(http.StatusTooManyRequests, "rate_limited", "Write rate limit exceeded")
	}
	return principal, nil
}

func mapMetadataError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, metadata.ErrNotFound):
		return apiProblem(http.StatusNotFound, "token_not_found", "Token or image not found")
	case errors.Is(err, metadata.ErrUnauthorized):
		return apiProblem(http.StatusForbidden, "creator_required", "A verified creator wallet is required")
	case errors.Is(err, metadata.ErrRevisionConflict):
		return apiProblem(http.StatusPreconditionFailed, "revision_conflict", "Resource revision changed")
	default:
		return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
}

func validateMetadata(value metadataBody) error {
	if !utf8.ValidString(value.Description) || len([]byte(value.Description)) > 2000 {
		return apiProblem(http.StatusUnprocessableEntity, "invalid_metadata", "Description must be valid UTF-8 and at most 2000 bytes")
	}
	for name, value := range map[string]string{"x_url": value.XURL, "telegram_url": value.TelegramURL} {
		if value == "" {
			continue
		}
		if len([]byte(value)) > 2048 {
			return apiProblem(http.StatusUnprocessableEntity, "invalid_metadata", name+" must be at most 2048 bytes")
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return apiProblem(http.StatusUnprocessableEntity, "invalid_metadata", name+" must be an HTTPS URL")
		}
	}
	return nil
}

func detectImage(content []byte) (string, error) {
	if len(content) == 0 || len(content) > maxImageBytes {
		return "", apiProblem(http.StatusRequestEntityTooLarge, "invalid_image", "Image must be between 1 byte and 5 MiB")
	}
	var contentType string
	var config image.Config
	var err error
	switch {
	case len(content) >= 8 && string(content[:8]) == "\x89PNG\r\n\x1a\n":
		contentType = "image/png"
		config, err = png.DecodeConfig(bytes.NewReader(content))
	case len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff:
		contentType = "image/jpeg"
		config, err = jpeg.DecodeConfig(bytes.NewReader(content))
	case len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP":
		contentType = "image/webp"
		config.Width, config.Height, err = webpDimensions(content)
	default:
		return "", apiProblem(http.StatusUnsupportedMediaType, "invalid_image", "Only PNG, JPEG, and WebP images are accepted")
	}
	// Browsers decode the full bitmap; a small file can declare a huge canvas (a decode bomb).
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxImageDimension || config.Height > maxImageDimension {
		return "", apiProblem(http.StatusUnprocessableEntity, "invalid_image", "Image must be a valid file of at most 4096x4096 pixels")
	}
	return contentType, nil
}

// webpDimensions reads the canvas size from the first chunk of a RIFF WebP file.
func webpDimensions(content []byte) (int, int, error) {
	invalid := errors.New("invalid WebP header")
	if len(content) < 30 {
		return 0, 0, invalid
	}
	switch string(content[12:16]) {
	case "VP8 ":
		if content[23] != 0x9d || content[24] != 0x01 || content[25] != 0x2a {
			return 0, 0, invalid
		}
		return int(binary.LittleEndian.Uint16(content[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(content[28:30]) & 0x3fff), nil
	case "VP8L":
		if content[20] != 0x2f {
			return 0, 0, invalid
		}
		bits := binary.LittleEndian.Uint32(content[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, nil
	case "VP8X":
		width := int(content[24]) | int(content[25])<<8 | int(content[26])<<16
		height := int(content[27]) | int(content[28])<<8 | int(content[29])<<16
		return width + 1, height + 1, nil
	default:
		return 0, 0, invalid
	}
}

func parseRevision(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, apiProblem(http.StatusBadRequest, "invalid_revision", "If-Match must contain a non-negative integer revision")
	}
	return revision, nil
}

func revisionETag(revision int64) string { return `"` + strconv.FormatInt(revision, 10) + `"` }

func matchesETag(header, wanted string) bool {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || value == wanted || strings.TrimPrefix(value, "W/") == wanted {
			return true
		}
	}
	return false
}

type subjectWindow struct {
	start time.Time
	count int
}
type SubjectLimiter struct {
	mu      sync.Mutex
	entries map[string]subjectWindow
	maximum int
	window  time.Duration
}

func NewSubjectLimiter(maximum int, window time.Duration) *SubjectLimiter {
	return &SubjectLimiter{entries: make(map[string]subjectWindow), maximum: maximum, window: window}
}

func (l *SubjectLimiter) Allow(subject string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.entries[subject]
	if !exists && len(l.entries) >= maxSubjectLimiterEntries {
		for candidate, candidateEntry := range l.entries {
			if now.Sub(candidateEntry.start) >= l.window {
				delete(l.entries, candidate)
			}
		}
		if len(l.entries) >= maxSubjectLimiterEntries {
			return false
		}
	}
	if entry.start.IsZero() || now.Sub(entry.start) >= l.window {
		l.entries[subject] = subjectWindow{start: now, count: 1}
		return true
	}
	if entry.count >= l.maximum {
		return false
	}
	entry.count++
	l.entries[subject] = entry
	return true
}
