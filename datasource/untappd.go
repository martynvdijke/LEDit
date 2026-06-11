package datasource

import (
	"log/slog"

	"ledit/render"
	"ledit/render/themes"
)

type UntappdDS struct {
	Token string
	URL   string
}

func (u *UntappdDS) GetPNG() (*render.RenderedImage, error) {
	slog.Info("using mock untappd data", "source", "untappd")
	data := map[string]string{
		"brewery": "Local Brew Co.",
		"beer":    "IPA",
		"abv":     "6.5%",
		"rating":  "4.2",
	}
	return render.RenderDict(data, 400, 400, themes.UntappdTheme, "fonts/PixelifySans.ttf")
}
