package datasource

import (
	"encoding/base64"
	"fmt"
	"os"

	"ledit/render"
)

type VideoDS struct {
	Path string
}

func (v *VideoDS) GetPNG() (*render.RenderedImage, error) {
	if !render.FileExists(v.Path) {
		return nil, fmt.Errorf("video file not found: %s", v.Path)
	}

	data, err := os.ReadFile(v.Path)
	if err != nil {
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &render.RenderedImage{
		Format: "MP4",
		Data:   []byte(encoded),
	}, nil
}
