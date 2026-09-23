package media

import (
	"encoding/binary"
	"image"
)

// Phones store a photo as the sensor saw it and put the way to hold it in the
// EXIF Orientation tag. Re-encoding drops EXIF, so the rotation is applied to
// the pixels first; otherwise a portrait document photo would come out on its
// side.

// jpegOrientation returns the EXIF Orientation (1-8) of a JPEG, or 1.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}

	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xFF {
			return 1
		}

		marker := data[offset+1]
		if marker == 0xD9 || marker == 0xDA { // end of image, start of scan
			return 1
		}

		length := int(binary.BigEndian.Uint16(data[offset+2:]))
		if length < 2 || offset+2+length > len(data) {
			return 1
		}

		segment := data[offset+4 : offset+2+length]

		if marker == 0xE1 && len(segment) > 6 && string(segment[:6]) == "Exif\x00\x00" {
			return tiffOrientation(segment[6:])
		}

		offset += 2 + length
	}

	return 1
}

func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}

	var order binary.ByteOrder

	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}

	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 1
	}

	entries := int(order.Uint16(tiff[ifd:]))

	for i := 0; i < entries; i++ {
		entry := ifd + 2 + i*12
		if entry+12 > len(tiff) {
			return 1
		}

		if order.Uint16(tiff[entry:]) == 0x0112 { // Orientation, a SHORT
			value := int(order.Uint16(tiff[entry+8:]))
			if value >= 1 && value <= 8 {
				return value
			}

			return 1
		}
	}

	return 1
}

// orient returns src turned the way the EXIF orientation says to show it.
func orient(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}

	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// Orientations 5-8 swap the sides.
	dstWidth, dstHeight := width, height
	if orientation >= 5 {
		dstWidth, dstHeight = height, width
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var dx, dy int

			switch orientation {
			case 2: // mirrored
				dx, dy = width-1-x, y
			case 3: // upside down
				dx, dy = width-1-x, height-1-y
			case 4: // mirrored, upside down
				dx, dy = x, height-1-y
			case 5: // mirrored, turned left
				dx, dy = y, x
			case 6: // turned right
				dx, dy = height-1-y, x
			case 7: // mirrored, turned right
				dx, dy = height-1-y, width-1-x
			case 8: // turned left
				dx, dy = y, width-1-x
			}

			dst.Set(dx, dy, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}

	return dst
}
