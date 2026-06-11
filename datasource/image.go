package datasource

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"ledit/render"
)

type ImageDS struct {
	Path string
}

func (img *ImageDS) GetPNG() (*render.RenderedImage, error) {
	if !render.FileExists(img.Path) {
		slog.Error("image file not found", "source", "image", "path", img.Path)
		return nil, fmt.Errorf("image file not found: %s", img.Path)
	}
	slog.Info("reading image file", "source", "image", "path", img.Path)

	ext := strings.ToUpper(render.GetExtension(img.Path))
	switch ext {
	case "JPG":
		ext = "JPEG"
	case "JPEG", "PNG", "GIF":
		// valid
	default:
		ext = "PNG"
	}

	data, err := os.ReadFile(img.Path)
	if err != nil {
		slog.Error("image file read failed", "source", "image", "path", img.Path, "error", err)
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &render.RenderedImage{
		Format: ext,
		Data:   []byte(encoded),
	}, nil
}
