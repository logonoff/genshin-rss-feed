package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type HoyoCodesResponse struct {
	Codes []HoyoCode `json:"codes"`
	Game  string     `json:"game"`
}

type HoyoCode struct {
	ID      int    `json:"id"`
	Code    string `json:"code"`
	Status  string `json:"status"`
	Game    string `json:"game"`
	Rewards string `json:"rewards"`
}

type RSS struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel RSSChannel `xml:"channel"`
}

type RSSChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []RSSItem `xml:"item"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	GUID        string `xml:"guid"`
}

type gameConfig struct {
	title       string
	redeemURL   string
	description string
}

var defaultGameInfo = map[string]gameConfig{
	"genshin": {
		title:       "Genshin Impact Codes",
		redeemURL:   "https://genshin.hoyoverse.com/en/gift?code=",
		description: "Active redemption codes for Genshin Impact",
	},
	"hkrpg": {
		title:       "Honkai: Star Rail Codes",
		redeemURL:   "https://hsr.hoyoverse.com/gift?code=",
		description: "Active redemption codes for Honkai: Star Rail",
	},
	"nap": {
		title:       "Zenless Zone Zero Codes",
		redeemURL:   "https://zenless.hoyoverse.com/redemption?code=",
		description: "Active redemption codes for Zenless Zone Zero",
	},
}

const maxResponseBytes = 1 << 20 // 1 MB

type server struct {
	client      *http.Client
	upstreamURL string
	gameInfo    map[string]gameConfig
}

func (s *server) handleFeed(w http.ResponseWriter, r *http.Request) {
	game := r.PathValue("game")
	info, ok := s.gameInfo[game]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown game %q, use one of: genshin, hkrpg, nap", game), http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=900")
		return
	}

	apiURL := s.upstreamURL + game
	resp, err := s.client.Get(apiURL)
	if err != nil {
		log.Printf("failed to fetch codes: %v", err)
		http.Error(w, "failed to fetch codes", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("upstream API returned %d for game %s", resp.StatusCode, game)
		http.Error(w, "upstream API error", http.StatusBadGateway)
		return
	}

	var codesResp HoyoCodesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&codesResp); err != nil {
		log.Printf("failed to decode API response: %v", err)
		http.Error(w, "failed to decode API response", http.StatusBadGateway)
		return
	}

	rss := RSS{
		Version: "2.0",
		Channel: RSSChannel{
			Title:         info.title,
			Link:          apiURL,
			Description:   info.description,
			LastBuildDate: time.Now().UTC().Format(time.RFC1123Z),
		},
	}

	for _, code := range codesResp.Codes {
		title := code.Code
		if code.Rewards != "" {
			title = code.Code + " - " + code.Rewards
		}
		rss.Channel.Items = append(rss.Channel.Items, RSSItem{
			Title:       title,
			Link:        info.redeemURL + code.Code,
			Description: code.Rewards,
			GUID:        fmt.Sprintf("%s-%d", code.Game, code.ID),
		})
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=900")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		log.Printf("error writing XML header: %v", err)
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(rss); err != nil {
		log.Printf("error encoding RSS: %v", err)
	}
}

func main() {
	srv := &server{
		client:      &http.Client{Timeout: 10 * time.Second},
		upstreamURL: "https://hoyo-codes.seria.moe/codes?game=",
		gameInfo:    defaultGameInfo,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{game}", srv.handleFeed)
	mux.HandleFunc("HEAD /{game}", srv.handleFeed)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	httpSrv := &http.Server{Addr: ":" + port, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("listening on :%s", port)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
