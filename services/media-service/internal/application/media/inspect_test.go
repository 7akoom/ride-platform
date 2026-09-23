package media

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestInspectReencodesJPEGAndDropsExif(t *testing.T) {
	original := withExif(t, jpegSample(t, 80, 40), 1, "GPSLatitude 36.19 Canon EOS")

	result := inspect(TypeJPEG, original)
	if result.rejection != "" {
		t.Fatalf("rejected: %s", result.rejection)
	}

	if result.contentType != TypeJPEG || result.width != 80 || result.height != 40 {
		t.Fatalf("got %s %dx%d", result.contentType, result.width, result.height)
	}

	if bytes.Contains(result.data, []byte("Exif")) || bytes.Contains(result.data, []byte("GPSLatitude")) {
		t.Fatal("the stored image still carries the EXIF data")
	}

	if result.sha256 != hexSHA256(result.data) || len(result.sha256) != 64 {
		t.Fatalf("sha256 %q does not describe the stored bytes", result.sha256)
	}
}

func TestInspectAppliesExifOrientation(t *testing.T) {
	// 6 = the camera was turned right: shown turned 90 degrees clockwise, so
	// the red block moves from top-left to top-right and the sides swap.
	result := inspect(TypeJPEG, withExif(t, jpegSample(t, 80, 40), 6, ""))
	if result.rejection != "" {
		t.Fatalf("rejected: %s", result.rejection)
	}

	if result.width != 40 || result.height != 80 {
		t.Fatalf("got %dx%d, want 40x80", result.width, result.height)
	}

	img := decodeStored(t, result.data)

	if !isRed(img.At(35, 5)) || isRed(img.At(5, 5)) {
		t.Fatal("the image was not turned as its EXIF orientation says")
	}
}

func TestOrientationsMoveTheTopLeftCorner(t *testing.T) {
	src := marked(8, 4)

	// Where the source's top-left pixel ends up, for an 8x4 source.
	want := map[int]image.Point{
		2: {7, 0}, 3: {7, 3}, 4: {0, 3},
		5: {0, 0}, 6: {3, 0}, 7: {3, 7}, 8: {0, 7},
	}

	for orientation, corner := range want {
		out := orient(src, orientation)

		if !isRed(out.At(corner.X, corner.Y)) {
			t.Errorf("orientation %d: the top-left pixel is not at %v", orientation, corner)
		}
	}

	if orient(src, 1) != src || orient(src, 9) != src {
		t.Error("orientation 1 and unknown values must leave the image alone")
	}
}

func TestJPEGOrientationReadsBothByteOrdersAndIgnoresJunk(t *testing.T) {
	if got := jpegOrientation(withExif(t, jpegSample(t, 8, 8), 8, "")); got != 8 {
		t.Fatalf("got %d, want 8", got)
	}

	little := []byte("II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x03\x00\x00\x00\x00\x00\x00\x00")
	if got := tiffOrientation(little); got != 3 {
		t.Fatalf("little endian: got %d, want 3", got)
	}

	for _, junk := range [][]byte{nil, []byte("not a jpeg"), {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}} {
		if got := jpegOrientation(junk); got != 1 {
			t.Fatalf("junk %q: got %d, want 1", junk, got)
		}
	}
}

func TestInspectKeepsPNGAsPNG(t *testing.T) {
	result := inspect(TypePNG, pngSample(t, 30, 20))
	if result.rejection != "" || result.contentType != TypePNG || result.width != 30 || result.height != 20 {
		t.Fatalf("got %+v", result)
	}
}

func TestInspectStoresWebPAsJPEG(t *testing.T) {
	result := inspect(TypeWebP, webpSample(t))
	if result.rejection != "" {
		t.Fatalf("rejected: %s", result.rejection)
	}

	if result.contentType != TypeJPEG || result.width != 3 || result.height != 2 {
		t.Fatalf("got %s %dx%d", result.contentType, result.width, result.height)
	}
}

func TestInspectDropsBytesAppendedToAnImage(t *testing.T) {
	polyglot := append(jpegSample(t, 16, 16), []byte("PK\x03\x04 hidden archive <script>alert(1)</script>")...)

	result := inspect(TypeJPEG, polyglot)
	if result.rejection != "" {
		t.Fatalf("rejected: %s", result.rejection)
	}

	if bytes.Contains(result.data, []byte("hidden archive")) {
		t.Fatal("the appended bytes were kept")
	}
}

func TestInspectScalesLargeImagesDown(t *testing.T) {
	result := inspect(TypePNG, pngSample(t, 3000, 1000))
	if result.rejection != "" {
		t.Fatalf("rejected: %s", result.rejection)
	}

	if result.width != storedMaxSide || result.height != 853 {
		t.Fatalf("got %dx%d, want %dx853", result.width, result.height, storedMaxSide)
	}
}

func TestInspectRejects(t *testing.T) {
	tooWide := func() []byte {
		var out bytes.Buffer
		_ = png.Encode(&out, image.NewGray(image.Rect(0, 0, maxImageSide+1, 1)))

		return out.Bytes()
	}()

	damagedJPEG := append(jpegSample(t, 16, 16)[:40], bytes.Repeat([]byte{0x00}, 200)...)

	cases := []struct {
		name     string
		declared string
		data     []byte
		want     string
	}{
		{"a JPEG declared as PNG", TypePNG, jpegSample(t, 8, 8), rejectWrongType},
		{"a PNG declared as JPEG", TypeJPEG, pngSample(t, 8, 8), rejectWrongType},
		{"HTML declared as JPEG", TypeJPEG, []byte("<html><script>alert(1)</script></html>"), rejectWrongType},
		{"an image declared as PDF", TypePDF, pngSample(t, 8, 8), rejectWrongType},
		{"a damaged JPEG", TypeJPEG, damagedJPEG, rejectDamaged},
		{"a side over the limit", TypePNG, tooWide, rejectDimensions},
		{"a PDF with JavaScript", TypePDF, pdfSample("/OpenAction << /S /JavaScript /JS (app.alert(1)) >>"), rejectPDFActive},
		{"a PDF with a launch action", TypePDF, pdfSample("/OpenAction << /S /Launch /F (cmd.exe) >>"), rejectPDFActive},
		{"a PDF with an embedded file", TypePDF, pdfSample("/Names << /EmbeddedFiles 2 0 R >>"), rejectPDFActive},
		{"a PDF with /JS at the end", TypePDF, []byte("%PDF-1.4\n<< /JS"), rejectPDFActive},
		{"a PDF with an escaped name", TypePDF, pdfSample("/S /J#61va#53cript"), rejectPDFActive},
		{"a PDF with a file attachment", TypePDF, pdfSample("/Annots [<< /Subtype /FileAttachment >>]"), rejectPDFActive},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := inspect(tc.declared, tc.data)
			if result.rejection != tc.want {
				t.Fatalf("got rejection %q, want %q", result.rejection, tc.want)
			}

			if result.data != nil {
				t.Fatal("a rejected inspection must not return bytes to keep")
			}
		})
	}
}

func TestInspectAcceptsPlainPDFs(t *testing.T) {
	// "/JSomething" is another name, not /JS.
	original := pdfSample("/Pages 2 0 R /JSomething true")

	result := inspect(TypePDF, original)
	if result.rejection != "" {
		t.Fatalf("rejected: %s", result.rejection)
	}

	if result.contentType != TypePDF || !bytes.Equal(result.data, original) {
		t.Fatal("a PDF must be kept as uploaded")
	}
}
