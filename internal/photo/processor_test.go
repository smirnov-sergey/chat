package photo_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"chat/internal/photo"
)

func encodeImage(t *testing.T, source image.Image, format string) []byte {
	t.Helper()
	var output bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&output, source, &jpeg.Options{Quality: 100})
	case "png":
		err = png.Encode(&output, source)
	case "gif":
		err = gif.Encode(&output, source, nil)
	default:
		t.Fatalf("unexpected fixture format: %s", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func processImage(t *testing.T, data []byte) image.Image {
	t.Helper()
	processor, err := photo.NewProcessor(5 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	output, err := processor.Process(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("processed photo must be a valid PNG: %v", err)
	}
	if result.Bounds().Dx() != 256 || result.Bounds().Dy() != 256 {
		t.Fatalf("output bounds=%v, want 256 by 256 pixels", result.Bounds())
	}
	return result
}

func TestProcessorAcceptsJPEGAndPNG(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			source := image.NewNRGBA(image.Rect(0, 0, 512, 512))
			for y := range 512 {
				for x := range 512 {
					pixel := color.NRGBA{R: 255, A: 255}
					if x >= 256 {
						pixel = color.NRGBA{G: 255, A: 255}
					}
					source.SetNRGBA(x, y, pixel)
				}
			}
			result := processImage(t, encodeImage(t, source, format))
			left := color.NRGBAModel.Convert(result.At(64, 128)).(color.NRGBA)
			right := color.NRGBAModel.Convert(result.At(192, 128)).(color.NRGBA)
			if left.R < 240 || left.G > 15 || right.G < 240 || right.R > 15 {
				t.Fatal("scaling must preserve the two colored halves")
			}
		})
	}
}

func TestProcessorCropsCenter(t *testing.T) {
	for _, bounds := range []image.Rectangle{image.Rect(0, 0, 512, 256), image.Rect(0, 0, 256, 512)} {
		t.Run(bounds.String(), func(t *testing.T) {
			source := image.NewNRGBA(bounds)
			for y := range bounds.Dy() {
				for x := range bounds.Dx() {
					position := x
					if bounds.Dy() > bounds.Dx() {
						position = y
					}
					pixel := color.NRGBA{G: 255, A: 255}
					if position < 128 || position >= 384 {
						pixel = color.NRGBA{R: 255, A: 255}
					}
					source.SetNRGBA(x, y, pixel)
				}
			}
			result := processImage(t, encodeImage(t, source, "png"))
			for _, point := range []image.Point{{0, 0}, {255, 0}, {128, 128}, {0, 255}, {255, 255}} {
				if got := color.NRGBAModel.Convert(result.At(point.X, point.Y)).(color.NRGBA); got != (color.NRGBA{G: 255, A: 255}) {
					t.Fatalf("center crop at %v = %+v, want green without the red outer bands", point, got)
				}
			}
		})
	}
}

func TestProcessorPreservesTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, A: 128})
		}
	}
	result := processImage(t, encodeImage(t, source, "png"))
	if got := color.NRGBAModel.Convert(result.At(128, 128)).(color.NRGBA); got.A != 128 || got.R < 250 {
		t.Fatalf("transparent pixel = %+v, want half-transparent red", got)
	}
}

func TestProcessorFileSizeBoundary(t *testing.T) {
	data := encodeImage(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)), "png")
	for _, limit := range []int64{int64(len(data)), int64(len(data) - 1)} {
		processor, err := photo.NewProcessor(limit)
		if err != nil {
			t.Fatal(err)
		}
		output, err := processor.Process(t.Context(), data)
		if limit == int64(len(data)) {
			if err != nil || len(output) == 0 {
				t.Fatalf("file exactly at the limit must be accepted: %v", err)
			}
		} else if output != nil || !errors.Is(err, photo.ErrFileTooLarge) {
			t.Fatalf("file one byte over the limit: error=%v, want ErrFileTooLarge", err)
		}
	}
}

func TestProcessorDimensionBoundaries(t *testing.T) {
	processor, err := photo.NewProcessor(5 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, bounds := range []image.Rectangle{
		image.Rect(0, 0, 1, 1), image.Rect(0, 0, 4096, 1), image.Rect(0, 0, 1, 4096),
		image.Rect(0, 0, 4097, 1), image.Rect(0, 0, 1, 4097),
	} {
		t.Run(bounds.String(), func(t *testing.T) {
			data := encodeImage(t, image.NewNRGBA(bounds), "png")
			output, err := processor.Process(t.Context(), data)
			if bounds.Dx() <= 4096 && bounds.Dy() <= 4096 {
				if err != nil || len(output) == 0 {
					t.Fatalf("valid dimensions rejected: %v", err)
				}
			} else if output != nil || !errors.Is(err, photo.ErrInvalidDimensions) {
				t.Fatalf("oversized dimensions: error=%v, want ErrInvalidDimensions", err)
			}
		})
	}
}

func TestProcessorRejectsInvalidImages(t *testing.T) {
	processor, err := photo.NewProcessor(5 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	pngData := encodeImage(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)), "png")
	truncatedPNG := pngData[:33]
	if _, _, err := image.DecodeConfig(bytes.NewReader(truncatedPNG)); err != nil {
		t.Fatalf("truncated fixture must have a readable header: %v", err)
	}
	for _, data := range [][]byte{nil, []byte("not an image"), truncatedPNG, encodeImage(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)), "gif")} {
		if output, err := processor.Process(t.Context(), data); output != nil || !errors.Is(err, photo.ErrInvalidImage) {
			t.Fatalf("invalid input: error=%v, want ErrInvalidImage", err)
		}
	}
}

func TestProcessorReencodesImage(t *testing.T) {
	data := encodeImage(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)), "png")
	const trailer = "untrusted appended content"
	data = append(data, trailer...)
	processor, err := photo.NewProcessor(5 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	output, err := processor.Process(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte(trailer)) || bytes.Equal(output, data) {
		t.Fatal("output must be newly encoded rather than a copy of the uploaded file")
	}
}

func TestProcessorCanceledContext(t *testing.T) {
	processor, err := photo.NewProcessor(5 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if output, err := processor.Process(ctx, nil); output != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled processing: error=%v, want context.Canceled", err)
	}
}

func TestNewProcessorRejectsInvalidLimit(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		if processor, err := photo.NewProcessor(limit); processor != nil || err == nil {
			t.Fatal("nonpositive file limit must not create a processor")
		}
	}
}
