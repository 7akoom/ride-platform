package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// A 3x2 lossless WebP (the standard library cannot write WebP).
const webpSampleBase64 = "UklGRh4AAABXRUJQVlA4TBEAAAAvAkAAAAdQjyIXpf+BiOh/AAA="

func webpSample(t *testing.T) []byte {
	t.Helper()

	data, err := base64.StdEncoding.DecodeString(webpSampleBase64)
	if err != nil {
		t.Fatalf("decode sample: %v", err)
	}

	return data
}

// marked is a blue image with a red block in its top-left quarter, so a
// rotation can be seen in the output (and survives JPEG compression).
func marked(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/4 && y < height/4 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}

	return img
}

func jpegSample(t *testing.T, width, height int) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := jpeg.Encode(&out, marked(width, height), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	return out.Bytes()
}

func pngSample(t *testing.T, width, height int) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := png.Encode(&out, marked(width, height)); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	return out.Bytes()
}

// withExif puts an APP1 Exif segment (an Orientation tag, then extra bytes
// standing in for camera data and GPS position) right after the JPEG's SOI.
func withExif(t *testing.T, jpegData []byte, orientation uint16, extra string) []byte {
	t.Helper()

	var tiff bytes.Buffer

	tiff.WriteString("MM")
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8)) // IFD right after the header
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1)) // one entry
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3)) // SHORT
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, orientation)
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0)) // padding of the value field
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0)) // no next IFD
	tiff.WriteString(extra)

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)

	var out bytes.Buffer

	out.Write(jpegData[:2])
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(jpegData[2:])

	return out.Bytes()
}

func pdfSample(body string) []byte {
	return []byte("%PDF-1.4\n1 0 obj << /Type /Catalog " + body + " >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")
}

func decodeStored(t *testing.T, data []byte) image.Image {
	t.Helper()

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode stored image: %v", err)
	}

	return img
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()

	return r > 0xB000 && g < 0x5000 && b < 0x5000
}
