package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"

	"golang.org/x/image/draw"

	// Registered decoder: image.Decode reads what the sniffing accepted.
	_ "golang.org/x/image/webp"
)

const (
	maxImagePixels   = 40_000_000
	maxImageSide     = 12_000
	storedMaxSide    = 2560
	jpegQuality      = 88
	sniffLength      = 512
	rejectWrongType  = "the file's content does not match its declared type"
	rejectDamaged    = "the image could not be read"
	rejectDimensions = "the image dimensions are not acceptable"
	rejectPDFActive  = "the PDF contains scripts, launch actions or embedded files"
)

// inspection is what checking an uploaded file produced: the bytes to keep
// (re-encoded for images) or why the file is refused.
type inspection struct {
	data        []byte
	contentType string
	width       int
	height      int
	sha256      string
	rejection   string
}

// pdfActiveMarkers are PDF name objects that run code or carry other files.
// A document a driver uploads has no reason to contain any of them. This is a
// byte search (after undoing #xx escapes in names): names inside compressed
// object streams are not seen, so it catches the common cases, not a
// determined attacker. Staff open these files in a browser's PDF viewer,
// which runs no PDF JavaScript anyway.
var pdfActiveMarkers = [][]byte{
	[]byte("/JavaScript"),
	[]byte("/JS"),
	[]byte("/Launch"),
	[]byte("/EmbeddedFile"),
	[]byte("/EmbeddedFiles"),
	[]byte("/FileAttachment"),
	[]byte("/RichMedia"),
}

// inspect checks an upload against its declared type. The type is taken from
// the bytes (not from what the client said), and images are decoded and
// written again: that drops EXIF (camera, GPS position) and anything appended
// or hidden in the file. WebP is written back as JPEG.
func inspect(declared string, data []byte) inspection {
	sniffed := http.DetectContentType(data[:min(len(data), sniffLength)])

	if sniffed != declared {
		return inspection{rejection: rejectWrongType}
	}

	if declared == TypePDF {
		return inspectPDF(data)
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return inspection{rejection: rejectDamaged}
	}

	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxImageSide || config.Height > maxImageSide ||
		config.Width*config.Height > maxImagePixels {
		return inspection{rejection: rejectDimensions}
	}

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return inspection{rejection: rejectDamaged}
	}

	decoded = fitWithin(decoded, storedMaxSide)

	if declared == TypeJPEG {
		decoded = orient(decoded, jpegOrientation(data))
	}

	var out bytes.Buffer

	contentType := TypeJPEG

	if declared == TypePNG {
		contentType = TypePNG
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, decoded)
	} else {
		err = jpeg.Encode(&out, decoded, &jpeg.Options{Quality: jpegQuality})
	}

	if err != nil {
		return inspection{rejection: rejectDamaged}
	}

	bounds := decoded.Bounds()

	return inspection{
		data:        out.Bytes(),
		contentType: contentType,
		width:       bounds.Dx(),
		height:      bounds.Dy(),
		sha256:      hexSHA256(out.Bytes()),
	}
}

// fitWithin scales an image down so its longer side is at most maxSide. A
// document photo stays readable at that size and takes a fraction of the space.
func fitWithin(src image.Image, maxSide int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	if width <= maxSide && height <= maxSide {
		return src
	}

	if width >= height {
		height = height * maxSide / width
		width = maxSide
	} else {
		width = width * maxSide / height
		height = maxSide
	}

	dst := image.NewRGBA(image.Rect(0, 0, max(width, 1), max(height, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)

	return dst
}

func inspectPDF(data []byte) inspection {
	searchable := unescapeNames(data)

	for _, marker := range pdfActiveMarkers {
		if containsName(searchable, marker) {
			return inspection{rejection: rejectPDFActive}
		}
	}

	return inspection{data: data, contentType: TypePDF, sha256: hexSHA256(data)}
}

// containsName finds a PDF name that is not just the start of a longer one
// ("/JS" must not match "/JSomething").
func containsName(data []byte, name []byte) bool {
	for offset := 0; ; {
		index := bytes.Index(data[offset:], name)
		if index < 0 {
			return false
		}

		end := offset + index + len(name)
		if end >= len(data) || !isNameChar(data[end]) {
			return true
		}

		offset = end
	}
}

// unescapeNames undoes the #xx escapes PDF allows inside names, so
// "/J#61vaScript" is found as "/JavaScript".
func unescapeNames(data []byte) []byte {
	if bytes.IndexByte(data, '#') < 0 {
		return data
	}

	out := make([]byte, 0, len(data))

	for i := 0; i < len(data); i++ {
		if data[i] == '#' && i+2 < len(data) && isHex(data[i+1]) && isHex(data[i+2]) {
			out = append(out, hexValue(data[i+1])<<4|hexValue(data[i+2]))
			i += 2

			continue
		}

		out = append(out, data[i])
	}

	return out
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexValue(b byte) byte {
	switch {
	case b >= 'a':
		return b - 'a' + 10
	case b >= 'A':
		return b - 'A' + 10
	default:
		return b - '0'
	}
}

func isNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
