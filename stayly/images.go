package main

// Wikimedia Commons image search — free, keyless. Used to enrich live results
// with real photos and power the carousel lightbox.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type imageRef struct {
	Thumb string `json:"thumb"`
	Full  string `json:"full"`
}

var (
	imgMu    sync.Mutex
	imgCache = map[string][]imageRef{}
	imgHTTP  = &http.Client{Timeout: 8 * time.Second}
)

func commonsImages(q string, limit int) []imageRef {
	if q == "" {
		return nil
	}
	key := q + "|" + strconv.Itoa(limit)
	imgMu.Lock()
	if c, ok := imgCache[key]; ok {
		imgMu.Unlock()
		return c
	}
	imgMu.Unlock()

	u := "https://commons.wikimedia.org/w/api.php?action=query&generator=search&gsrsearch=" +
		url.QueryEscape(q) + "&gsrnamespace=6&gsrlimit=" + strconv.Itoa(limit) +
		"&prop=imageinfo&iiprop=url&iiurlwidth=640&format=json&origin=*"
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "concierge-demo/1.0 (contact: demo@example.com)")
	resp, err := imgHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var data struct {
		Query struct {
			Pages map[string]struct {
				ImageInfo []struct {
					ThumbURL string `json:"thumburl"`
					URL      string `json:"url"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}
	out := []imageRef{}
	for _, p := range data.Query.Pages {
		if len(p.ImageInfo) > 0 {
			out = append(out, imageRef{Thumb: p.ImageInfo[0].ThumbURL, Full: p.ImageInfo[0].URL})
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	imgMu.Lock()
	imgCache[key] = out
	imgMu.Unlock()
	return out
}

func handleImages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := 6
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 20 {
			limit = n
		}
	}
	imgs := commonsImages(q, limit)
	if imgs == nil {
		imgs = []imageRef{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"images": imgs})
}
