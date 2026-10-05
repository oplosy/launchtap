package apiserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
)

const secretMarker = "GPS-48.8584N-2.2945E"

func pngChunk(chunkType string, data []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(chunk, chunkType...)
	chunk = append(chunk, data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

// insertAfter splices extra bytes into content at offset.
func insertAfter(content []byte, offset int, extra ...[]byte) []byte {
	out := append([]byte(nil), content[:offset]...)
	for _, part := range extra {
		out = append(out, part...)
	}
	return append(out, content[offset:]...)
}

func noisyImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	seed := uint32(7)
	for index := range img.Pix {
		seed = seed*1664525 + 1013904223
		img.Pix[index] = byte(seed >> 24)
	}
	return img
}

func TestStripPNGMetadataKeepsRenderingChunksOnly(t *testing.T) {
	original := testPNG(t, 3, 2)
	const ihdrEnd = 8 + 12 + 13
	gamma := pngChunk("gAMA", []byte{0, 0, 0xb1, 0x8f})
	withMetadata := insertAfter(original, ihdrEnd,
		pngChunk("tEXt", []byte("Comment\x00"+secretMarker)),
		pngChunk("eXIf", []byte("MM\x00*"+secretMarker)),
		pngChunk("tIME", []byte{0x07, 0xea, 10, 5, 12, 0, 0}),
		gamma,
	)
	withMetadata = append(withMetadata, secretMarker...)

	stripped, err := stripImageMetadata("image/png", withMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if want := insertAfter(original, ihdrEnd, gamma); !bytes.Equal(stripped, want) {
		t.Fatalf("stripped PNG keeps unexpected chunks:\n got %x\nwant %x", stripped, want)
	}
	if _, err := png.Decode(bytes.NewReader(stripped)); err != nil {
		t.Fatal(err)
	}
}

func TestStripJPEGMetadataKeepsPixelsAndColorProfile(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, noisyImage(24, 16), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	original := encoded.Bytes()
	segment := func(marker byte, payload string) []byte {
		out := []byte{0xff, marker}
		out = binary.BigEndian.AppendUint16(out, uint16(len(payload)+2))
		return append(out, payload...)
	}
	icc := segment(jpegAPP2, "ICC_PROFILE\x00\x01\x01profile")
	withMetadata := insertAfter(original, 2,
		segment(0xe1, "Exif\x00\x00"+secretMarker),
		segment(0xe1, "http://ns.adobe.com/xap/1.0/\x00"+secretMarker),
		segment(jpegAPP2, "MPF\x00"+secretMarker),
		icc,
		segment(0xed, "Photoshop 3.0\x00"+secretMarker),
		segment(jpegCOM, secretMarker),
		[]byte{0xff},
	)
	withMetadata = append(withMetadata, secretMarker...)

	stripped, err := stripImageMetadata("image/jpeg", withMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if want := insertAfter(original, 2, icc); !bytes.Equal(stripped, want) {
		t.Fatalf("stripped JPEG differs from original plus ICC profile (len %d want %d)", len(stripped), len(want))
	}
	if bytes.Contains(stripped, []byte(secretMarker)) {
		t.Fatal("JPEG metadata survived")
	}
	if _, err := jpeg.Decode(bytes.NewReader(stripped)); err != nil {
		t.Fatal(err)
	}
}

func webpChunk(chunkType string, payload []byte) []byte {
	chunk := append([]byte(chunkType), binary.LittleEndian.AppendUint32(nil, uint32(len(payload)))...)
	chunk = append(chunk, payload...)
	if len(payload)%2 == 1 {
		chunk = append(chunk, 0)
	}
	return chunk
}

func riffWrap(chunks ...[]byte) []byte {
	body := []byte("WEBP")
	for _, chunk := range chunks {
		body = append(body, chunk...)
	}
	return append(append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...), body...)
}

func TestStripWebPMetadataDropsEXIFAndXMP(t *testing.T) {
	const iccFlag = 0x20
	vp8x := func(flags byte) []byte {
		return webpChunk("VP8X", []byte{flags, 0, 0, 0, 99, 0, 0, 49, 0, 0})
	}
	bits := uint32(99) | uint32(49)<<14
	lossless := webpChunk("VP8L", []byte{0x2f, byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24), 0, 0})
	icc := webpChunk("ICCP", []byte("icc"))
	withMetadata := riffWrap(
		vp8x(iccFlag|webpFlagEXIF|webpFlagXMP), icc, lossless,
		webpChunk("EXIF", []byte(secretMarker)), webpChunk("XMP ", []byte(secretMarker)),
	)

	stripped, err := stripImageMetadata("image/webp", withMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if want := riffWrap(vp8x(iccFlag), icc, lossless); !bytes.Equal(stripped, want) {
		t.Fatalf("stripped WebP:\n got %x\nwant %x", stripped, want)
	}
	if width, height, err := webpDimensions(stripped); err != nil || width != 100 || height != 50 {
		t.Fatalf("dimensions %dx%d err=%v", width, height, err)
	}
}

func TestStripImageMetadataRejectsMalformedContainers(t *testing.T) {
	corruptCRC := testPNG(t, 2, 2)
	corruptCRC[8+12+13-1] ^= 0xff
	var jpegImage bytes.Buffer
	if err := jpeg.Encode(&jpegImage, noisyImage(8, 8), nil); err != nil {
		t.Fatal(err)
	}
	truncatedJPEG := jpegImage.Bytes()[:jpegImage.Len()-20]
	oversizedChunk := riffWrap(webpChunk("VP8X", make([]byte, 10)))
	binary.LittleEndian.PutUint32(oversizedChunk[16:20], 1<<20)
	for name, test := range map[string]struct {
		contentType string
		content     []byte
	}{
		"png bad crc":        {"image/png", corruptCRC},
		"png without IEND":   {"image/png", testPNG(t, 2, 2)[:40]},
		"jpeg truncated":     {"image/jpeg", truncatedJPEG},
		"webp chunk overrun": {"image/webp", oversizedChunk},
		"webp riff overrun":  {"image/webp", riffWebP("VP8X", make([]byte, 10)...)[:16]},
	} {
		if _, err := stripImageMetadata(test.contentType, test.content); err == nil {
			t.Errorf("%s: malformed container accepted", name)
		}
	}
}

func TestImageUploadStoresStrippedContent(t *testing.T) {
	store := &fakeMetadataStore{}
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterMetadataRoutes(MetadataRoutes{Store: store, Verifier: fakeVerifier{principal: privyauth.Principal{PrivyDID: "did"}}, ChainID: 1})
	original := testPNG(t, 2, 2)
	upload := insertAfter(original, 8+12+13, pngChunk("eXIf", []byte(secretMarker)))
	request := httptest.NewRequest(http.MethodPut, "/v1/tokens/0x00000000000000000000000000000000000000bb/image", bytes.NewReader(upload))
	request.Header.Set("Content-Type", "image/png")
	authorize(request)
	request.Header.Set("If-Match", "0")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(store.image.Content, original) {
		t.Fatalf("status=%d stored %d bytes, want the %d-byte original", response.Code, len(store.image.Content), len(original))
	}
}
