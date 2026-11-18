package photo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

const (
	imageSize    = 256
	maxDimension = 4096
)

var (
	ErrFileTooLarge      = errors.New("photo file is too large")
	ErrInvalidImage      = errors.New("photo must be a valid JPEG or PNG image")
	ErrInvalidDimensions = errors.New("photo dimensions must be between 1 and 4096 pixels")
)

type Processor struct {
	maxFileSize int64
}

func NewProcessor(maxFileSize int64) (*Processor, error) {
	if maxFileSize <= 0 {
		return nil, errors.New("maximum photo file size must be positive")
	}

	return &Processor{maxFileSize: maxFileSize}, nil
}

func (processor *Processor) Process(ctx context.Context, data []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if int64(len(data)) > processor.maxFileSize {
		return nil, ErrFileTooLarge
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, ErrInvalidImage
	}

	if config.Width < 1 || config.Height < 1 || config.Width > maxDimension || config.Height > maxDimension {
		return nil, ErrInvalidDimensions
	}

	var source image.Image

	switch format {
	case "jpeg":
		source, err = jpeg.Decode(bytes.NewReader(data))
	case "png":
		source, err = png.Decode(bytes.NewReader(data))
	}

	if err != nil {
		return nil, ErrInvalidImage
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	x := bounds.Min.X + (bounds.Dx()-side)/2
	y := bounds.Min.Y + (bounds.Dy()-side)/2
	crop := image.Rect(x, y, x+side, y+side)
	result := image.NewNRGBA(image.Rect(0, 0, imageSize, imageSize))
	draw.CatmullRom.Scale(result, result.Bounds(), source, crop, draw.Src, nil)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var output bytes.Buffer
	// Use PNG to preserve transparency and keep one output format for all profile photos.
	if err := png.Encode(&output, result); err != nil {
		return nil, fmt.Errorf("encode profile photo: %w", err)
	}

	return output.Bytes(), nil
}
