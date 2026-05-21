package handlers

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSHub struct {
	Client *ent.Client
}

func NewWSHub(client *ent.Client) *WSHub {
	return &WSHub{Client: client}
}

func (h *WSHub) HandleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	settings, err := h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(c.Request.Context())
	if err != nil {
		log.Printf("Failed to load settings: %v", err)
		return
	}

	var sources []datasource.Datasource

	sonarr, _ := settings.Edges.SonarrOrErr()
	for _, s := range sonarr {
		sources = append(sources, &datasource.SonarrDS{Token: s.Token, URL: s.URL})
	}

	radarr, _ := settings.Edges.RadarrOrErr()
	for _, r := range radarr {
		sources = append(sources, &datasource.RadarrDS{Token: r.Token, URL: r.URL})
	}

	f1s, _ := settings.Edges.F1OrErr()
	for _, f := range f1s {
		sources = append(sources, &datasource.F1DS{Token: f.Token, URL: f.URL})
	}

	weather, _ := settings.Edges.WeatherOrErr()
	for _, w := range weather {
		sources = append(sources, &datasource.WeatherDS{Token: w.Token, URL: w.URL})
	}

	ha, _ := settings.Edges.HomeAssistantOrErr()
	for _, haItem := range ha {
		sources = append(sources, &datasource.HomeAssistantDS{Token: haItem.Token, URL: haItem.URL})
	}

	untappd, _ := settings.Edges.UntappdOrErr()
	for _, u := range untappd {
		sources = append(sources, &datasource.UntappdDS{Token: u.Token, URL: u.URL})
	}

	images, _ := settings.Edges.ImagesOrErr()
	for _, img := range images {
		sources = append(sources, &datasource.ImageDS{Path: img.Path})
	}

	videos, _ := settings.Edges.VideosOrErr()
	for _, vid := range videos {
		sources = append(sources, &datasource.VideoDS{Path: vid.Path})
	}

	crypto, _ := settings.Edges.CryptoOrErr()
	for _, c := range crypto {
		sources = append(sources, &datasource.CryptoDS{Token: c.Token, URL: c.URL})
	}

	if settings.Random {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(sources), func(i, j int) {
			sources[i], sources[j] = sources[j], sources[i]
		})
	}

	if len(sources) == 0 {
		msg, _ := json.Marshal(map[string]string{"error": "no datasources configured"})
		conn.WriteMessage(websocket.TextMessage, msg)
		return
	}

	timeout := time.Duration(settings.Timeout * float64(time.Second))

	for {
		for _, src := range sources {
			img, err := src.GetPNG()
			if err != nil {
				log.Printf("Error getting PNG from source: %v", err)
				continue
			}
			msg := map[string]string{
				"format": img.Format,
				"image":  string(img.Data),
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
			time.Sleep(timeout)
		}
	}
}
