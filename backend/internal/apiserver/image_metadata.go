package apiserver

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
)

var errMalformedImage = errors.New("malformed image container")

// stripImageMetadata drops camera and editor metadata (EXIF, XMP, IPTC, text comments,
// timestamps, embedded previews) without re-encoding pixels. Only the chunks a browser needs
// to render the image (pixel data, palette, transparency, color profile, animation) survive;
// anything after the end-of-image marker is discarded. contentType must come from detectImage.
func stripImageMetadata(contentType string, content []byte) ([]byte, error) {
	switch contentType {
	case "image/png":
		return stripPNGMetadata(content)
	case "image/jpeg":
		return stripJPEGMetadata(content)
	case "image/webp":
		return stripWebPMetadata(content)
	default:
		return nil, errMalformedImage
	}
}

// pngRenderingChunks are the ancillary PNG chunks that change how pixels render. Critical
// chunks (IHDR, PLTE, IDAT, IEND) are always kept; tEXt/zTXt/iTXt/eXIf/tIME and unknown
// private chunks are dropped.
var pngRenderingChunks = map[string]bool{
	"tRNS": true, "gAMA": true, "cHRM": true, "sRGB": true, "iCCP": true, "sBIT": true,
	"cICP": true, "mDCV": true, "cLLI": true, "bKGD": true, "pHYs": true,
	"acTL": true, "fcTL": true, "fdAT": true,
}

func stripPNGMetadata(content []byte) ([]byte, error) {
	const signatureLength = 8
	out := bytes.NewBuffer(make([]byte, 0, len(content)))
	out.Write(content[:signatureLength])
	for offset := signatureLength; ; {
		if len(content)-offset < 12 {
			return nil, errMalformedImage
		}
		length := binary.BigEndian.Uint32(content[offset : offset+4])
		if uint64(length) > uint64(len(content)-offset-12) {
			return nil, errMalformedImage
		}
		end := offset + 12 + int(length)
		chunkType := string(content[offset+4 : offset+8])
		if crc32.ChecksumIEEE(content[offset+4:end-4]) != binary.BigEndian.Uint32(content[end-4:end]) {
			return nil, errMalformedImage
		}
		// An uppercase first letter marks a critical chunk, which a decoder cannot skip.
		if (chunkType[0] >= 'A' && chunkType[0] <= 'Z') || pngRenderingChunks[chunkType] {
			out.Write(content[offset:end])
		}
		if chunkType == "IEND" {
			return out.Bytes(), nil
		}
		offset = end
	}
}

const (
	jpegSOS  = 0xda
	jpegEOI  = 0xd9
	jpegAPP0 = 0xe0
	jpegAPP2 = 0xe2
	// jpegAPP14 is the Adobe segment, which selects the color transform of the scan data.
	jpegAPP14 = 0xee
	jpegCOM   = 0xfe
)

func stripJPEGMetadata(content []byte) ([]byte, error) {
	out := bytes.NewBuffer(make([]byte, 0, len(content)))
	out.Write(content[:2])
	for offset := 2; ; {
		// Markers may be preceded by any number of 0xff fill bytes.
		for offset < len(content) && content[offset] == 0xff && offset+1 < len(content) && content[offset+1] == 0xff {
			offset++
		}
		if len(content)-offset < 2 || content[offset] != 0xff {
			return nil, errMalformedImage
		}
		marker := content[offset+1]
		if marker == jpegEOI {
			out.Write(content[offset : offset+2])
			return out.Bytes(), nil
		}
		if len(content)-offset < 4 {
			return nil, errMalformedImage
		}
		end := offset + 2 + int(binary.BigEndian.Uint16(content[offset+2:offset+4]))
		if end > len(content) || end < offset+4 {
			return nil, errMalformedImage
		}
		if keepJPEGSegment(marker, content[offset+4:end]) {
			out.Write(content[offset:end])
		}
		offset = end
		if marker == jpegSOS {
			// Entropy-coded data runs until the next marker that is neither a stuffed 0xff00
			// byte nor a restart marker; progressive files continue with more segments.
			scanEnd := offset
			for scanEnd+1 < len(content) {
				next := content[scanEnd+1]
				if content[scanEnd] == 0xff && next != 0 && next != 0xff && (next < 0xd0 || next > 0xd7) {
					break
				}
				scanEnd++
			}
			if scanEnd+1 >= len(content) {
				return nil, errMalformedImage
			}
			out.Write(content[offset:scanEnd])
			offset = scanEnd
		}
	}
}

// keepJPEGSegment keeps every non-application segment except comments, the JFIF header, the
// Adobe color-transform segment, and ICC profiles. Other APPn segments carry EXIF, XMP, IPTC,
// or multi-picture previews.
func keepJPEGSegment(marker byte, payload []byte) bool {
	switch {
	case marker == jpegCOM:
		return false
	case marker == jpegAPP0 || marker == jpegAPP14:
		return true
	case marker == jpegAPP2:
		return bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00"))
	case marker > jpegAPP0 && marker <= 0xef:
		return false
	default:
		return true
	}
}

// webpRenderingChunks are the WebP chunks needed to render the image; EXIF, XMP, and unknown
// chunks are dropped.
var webpRenderingChunks = map[string]bool{
	"VP8X": true, "VP8 ": true, "VP8L": true, "ALPH": true, "ICCP": true, "ANIM": true, "ANMF": true,
}

const (
	webpFlagEXIF = 0x08
	webpFlagXMP  = 0x04
)

func stripWebPMetadata(content []byte) ([]byte, error) {
	const headerLength = 12
	riffEnd := 8 + int(binary.LittleEndian.Uint32(content[4:8]))
	if riffEnd > len(content) || riffEnd < headerLength {
		return nil, errMalformedImage
	}
	out := bytes.NewBuffer(make([]byte, 0, riffEnd))
	out.Write(content[:headerLength])
	for offset := headerLength; offset < riffEnd; {
		if riffEnd-offset < 8 {
			return nil, errMalformedImage
		}
		size := int(binary.LittleEndian.Uint32(content[offset+4 : offset+8]))
		if size > riffEnd-offset-8 {
			return nil, errMalformedImage
		}
		chunkType := string(content[offset : offset+4])
		if webpRenderingChunks[chunkType] {
			start := out.Len()
			out.Write(content[offset : offset+8+size])
			if size%2 == 1 {
				// Chunks are padded to an even size; some writers omit the final pad byte.
				out.WriteByte(0)
			}
			if chunkType == "VP8X" && size > 0 {
				out.Bytes()[start+8] &^= webpFlagEXIF | webpFlagXMP
			}
		}
		offset = min(offset+8+size+size%2, riffEnd)
	}
	stripped := out.Bytes()
	binary.LittleEndian.PutUint32(stripped[4:8], uint32(len(stripped)-8))
	return stripped, nil
}
