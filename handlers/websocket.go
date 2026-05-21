package handlers

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
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

type sourceWithName struct {
	Name   string
	Source datasource.Datasource
}

type WSHub struct {
	Client *ent.Client
}

func NewWSHub(client *ent.Client) *WSHub {
	return &WSHub{Client: client}
}

type feedState struct {
	mu         sync.Mutex
	paused     bool
	skip       bool
	currentIdx int
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

	var sources []sourceWithName

	sonarr, _ := settings.Edges.SonarrOrErr()
	for _, s := range sonarr {
		sources = append(sources, sourceWithName{Name: "Sonarr", Source: &datasource.SonarrDS{Token: s.Token, URL: s.URL}})
	}

	radarr, _ := settings.Edges.RadarrOrErr()
	for _, r := range radarr {
		sources = append(sources, sourceWithName{Name: "Radarr", Source: &datasource.RadarrDS{Token: r.Token, URL: r.URL}})
	}

	f1s, _ := settings.Edges.F1OrErr()
	for _, f := range f1s {
		sources = append(sources, sourceWithName{Name: "F1", Source: &datasource.F1DS{Token: f.Token, URL: f.URL}})
	}

	weather, _ := settings.Edges.WeatherOrErr()
	for _, w := range weather {
		sources = append(sources, sourceWithName{Name: "Weather", Source: &datasource.WeatherDS{Token: w.Token, URL: w.URL}})
	}

	ha, _ := settings.Edges.HomeAssistantOrErr()
	for _, haItem := range ha {
		sources = append(sources, sourceWithName{Name: "HomeAssistant", Source: &datasource.HomeAssistantDS{Token: haItem.Token, URL: haItem.URL}})
	}

	untappd, _ := settings.Edges.UntappdOrErr()
	for _, u := range untappd {
		sources = append(sources, sourceWithName{Name: "Untappd", Source: &datasource.UntappdDS{Token: u.Token, URL: u.URL}})
	}

	images, _ := settings.Edges.ImagesOrErr()
	for _, img := range images {
		sources = append(sources, sourceWithName{Name: "Image", Source: &datasource.ImageDS{Path: img.Path}})
	}

	videos, _ := settings.Edges.VideosOrErr()
	for _, vid := range videos {
		sources = append(sources, sourceWithName{Name: "Video", Source: &datasource.VideoDS{Path: vid.Path}})
	}

	crypto, _ := settings.Edges.CryptoOrErr()
	for _, cr := range crypto {
		sources = append(sources, sourceWithName{Name: "Crypto", Source: &datasource.CryptoDS{Token: cr.Token, URL: cr.URL}})
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
	state := &feedState{}

	// Read control messages in a goroutine
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var cmd map[string]string
			if err := json.Unmarshal(msg, &cmd); err != nil {
				continue
			}
			state.mu.Lock()
			switch cmd["action"] {
			case "next":
				state.skip = true
			case "pause":
				state.paused = true
			case "resume":
				state.paused = false
			}
			state.mu.Unlock()
		}
	}()

	for {
		for i, sw := range sources {
			state.currentIdx = i

			// Compute next source name
			nextName := ""
			nextIdx := (i + 1) % len(sources)
			if settings.Random {
				nextName = sources[rand.Intn(len(sources))].Name
			} else {
				nextName = sources[nextIdx].Name
			}

			// Wait if paused
			for {
				state.mu.Lock()
				p := state.paused
				state.mu.Unlock()
				if !p {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			img, err := sw.Source.GetPNG()
			if err != nil {
				log.Printf("Error getting PNG from %s: %v", sw.Name, err)
				continue
			}

			msg := map[string]string{
				"format": img.Format,
				"image":  string(img.Data),
				"source": sw.Name,
				"next":   nextName,
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}

			// Wait for timeout or skip signal
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				state.mu.Lock()
				if state.skip {
					state.skip = false
					state.mu.Unlock()
					break
				}
				state.mu.Unlock()
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}
