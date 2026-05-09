package main

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newUpstream(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

func newTestServer(t *testing.T, upstream *httptest.Server) *server {
	t.Helper()
	return &server{
		client:      upstream.Client(),
		upstreamURL: upstream.URL + "/codes?game=",
		gameInfo: map[string]gameConfig{
			"genshin": {
				title:       "Genshin Impact Codes",
				redeemURL:   "https://genshin.hoyoverse.com/en/gift?code=",
				description: "Active redemption codes for Genshin Impact",
			},
		},
	}
}

const validResponse = `{"codes":[{"id":1,"code":"TESTCODE1","status":"OK","game":"genshin","rewards":"60 primogems"},{"id":2,"code":"TESTCODE2","status":"OK","game":"genshin","rewards":""}],"game":"genshin"}`

func TestFeed(t *testing.T) {
	t.Parallel()
	upstream := newUpstream(t, http.StatusOK, validResponse)
	defer upstream.Close()
	srv := newTestServer(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/genshin", nil)
	req.SetPathValue("game", "genshin")
	rec := httptest.NewRecorder()
	srv.handleFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %s", ct)
	}

	var rss RSS
	if err := xml.Unmarshal(rec.Body.Bytes(), &rss); err != nil {
		t.Fatalf("invalid RSS XML: %v", err)
	}

	if rss.Version != "2.0" {
		t.Errorf("expected RSS version 2.0, got %s", rss.Version)
	}
	if len(rss.Channel.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(rss.Channel.Items))
	}

	item := rss.Channel.Items[0]
	if item.Title != "TESTCODE1 - 60 primogems" {
		t.Errorf("unexpected title: %s", item.Title)
	}
	if item.GUID != "genshin-1" {
		t.Errorf("unexpected GUID: %s", item.GUID)
	}

	item2 := rss.Channel.Items[1]
	if item2.Title != "TESTCODE2" {
		t.Errorf("expected bare code as title for empty rewards, got: %s", item2.Title)
	}
}

func TestInvalidGame(t *testing.T) {
	t.Parallel()
	srv := &server{gameInfo: defaultGameInfo}

	req := httptest.NewRequest(http.MethodGet, "/invalid", nil)
	req.SetPathValue("game", "invalid")
	rec := httptest.NewRecorder()
	srv.handleFeed(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := &server{gameInfo: defaultGameInfo}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{game}", srv.handleFeed)
	mux.HandleFunc("HEAD /{game}", srv.handleFeed)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(method, "/genshin", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

func TestHeadRequest(t *testing.T) {
	t.Parallel()
	srv := &server{gameInfo: defaultGameInfo}

	req := httptest.NewRequest(http.MethodHead, "/genshin", nil)
	req.SetPathValue("game", "genshin")
	rec := httptest.NewRecorder()
	srv.handleFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for HEAD, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %s", ct)
	}
}

func TestUpstreamError(t *testing.T) {
	t.Parallel()
	upstream := newUpstream(t, http.StatusInternalServerError, "")
	defer upstream.Close()
	srv := newTestServer(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/genshin", nil)
	req.SetPathValue("game", "genshin")
	rec := httptest.NewRecorder()
	srv.handleFeed(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestMalformedJSON(t *testing.T) {
	t.Parallel()
	upstream := newUpstream(t, http.StatusOK, `{invalid json}`)
	defer upstream.Close()
	srv := newTestServer(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/genshin", nil)
	req.SetPathValue("game", "genshin")
	rec := httptest.NewRecorder()
	srv.handleFeed(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for malformed JSON, got %d", rec.Code)
	}
}
