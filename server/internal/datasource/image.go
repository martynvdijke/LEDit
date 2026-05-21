package datasource

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/martynvdijke/ledit/internal/render"
)

type ImageDS struct {
	Path string
}

func (img *ImageDS) GetPNG() (*render.RenderedImage, error) {
	if !render.FileExists(img.Path) {
		return nil, fmt.Errorf("image file not found: %s", img.Path)
	}

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
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &render.RenderedImage{
		Format: ext,
		Data:   []byte(encoded),
	}, nil
}
