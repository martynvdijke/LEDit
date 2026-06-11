package datasource

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	"ledit/render"
)

type VideoDS struct {
	Path string
}

func (v *VideoDS) GetPNG() (*render.RenderedImage, error) {
	if !render.FileExists(v.Path) {
		slog.Error("video file not found", "source", "video", "path", v.Path)
		return nil, fmt.Errorf("video file not found: %s", v.Path)
	}

	slog.Info("reading video file", "source", "video", "path", v.Path)
	data, err := os.ReadFile(v.Path)
	if err != nil {
		slog.Error("video file read failed", "source", "video", "path", v.Path, "error", err)
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &render.RenderedImage{
		Format: "MP4",
		Data:   []byte(encoded),
	}, nil
}
