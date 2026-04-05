// Package api — Trafiklab Realtime API client.
// Endpoint: https://realtime-api.trafiklab.se/v1/departures/{stopID}?key={key}
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const realtimeEndpoint = "https://realtime-api.trafiklab.se/v1/departures"

// RealtimeClient fetches departure data from the Trafiklab Realtime API with
// an in-memory cache keyed by stop ID.
type RealtimeClient struct {
	apiKey     string
	httpClient *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewRealtimeClient creates a new RealtimeClient.
func NewRealtimeClient(apiKey string) *RealtimeClient {
	return &RealtimeClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		cache: make(map[string]cacheEntry),
	}
}

// FetchDepartures returns departures for stopID, using a cached response when
// the cache is younger than ttl.  At most maxJourneys departures are returned
// (pre-filter; the caller may apply additional filters).
func (c *RealtimeClient) FetchDepartures(ctx context.Context, stopID int, maxJourneys int, ttl time.Duration) ([]Departure, error) {
	key := fmt.Sprintf("rt-%d", stopID)

	c.mu.Lock()
	entry, hit := c.cache[key]
	c.mu.Unlock()

	if hit && time.Since(entry.fetchedAt) < ttl {
		return entry.departures, nil
	}

	deps, err := c.fetchRT(ctx, stopID)
	if err != nil {
		if hit {
			return entry.departures, nil
		}
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = cacheEntry{departures: deps, fetchedAt: time.Now()}
	c.mu.Unlock()

	if len(deps) > maxJourneys {
		deps = deps[:maxJourneys]
	}
	return deps, nil
}

// --- raw response structs ----------------------------------------------------

type rtResponse struct {
	Departures []rtDeparture `json:"departures"`
}

type rtDeparture struct {
	Scheduled string `json:"scheduled"`
	Realtime  string `json:"realtime"`
	Delay     int    `json:"delay"` // seconds; negative = early
	Canceled  bool   `json:"canceled"`
	Route     struct {
		Designation   string `json:"designation"`
		TransportMode string `json:"transport_mode"`
		Direction     string `json:"direction"`
	} `json:"route"`
	Stop struct {
		Name string `json:"name"`
	} `json:"stop"`
}

// --- fetch -------------------------------------------------------------------

func (c *RealtimeClient) fetchRT(ctx context.Context, stopID int) ([]Departure, error) {
	// rawURL := fmt.Sprintf("%s/%d/2026-04-06T08:30?key=%s", realtimeEndpoint, stopID, c.apiKey)
	rawURL := fmt.Sprintf("%s/%d?key=%s", realtimeEndpoint, stopID, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("realtime: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("realtime: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("realtime: API returned HTTP %d", resp.StatusCode)
	}

	var result rtResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("realtime: decode response: %w", err)
	}

	return parseRTDepartures(result.Departures), nil
}

func parseRTDepartures(raw []rtDeparture) []Departure {
	now := time.Now()
	out := make([]Departure, 0, len(raw))

	for _, r := range raw {
		sched, err := time.ParseInLocation("2006-01-02T15:04:05", r.Scheduled, time.Local)
		if err != nil {
			continue
		}
		if sched.Before(now) {
			continue
		}

		dep := Departure{
			StopName:      r.Stop.Name,
			Line:          r.Route.Designation,
			Direction:     r.Route.Direction,
			Scheduled:     sched,
			TransportType: rtModeToType(r.Route.TransportMode),
			Cancelled:     r.Canceled,
			DelaySeconds:  r.Delay,
		}

		if r.Realtime != "" {
			if rt, err := time.ParseInLocation("2006-01-02T15:04:05", r.Realtime, time.Local); err == nil {
				dep.Realtime = &rt
			}
		}

		out = append(out, dep)
	}

	return out
}

func rtModeToType(mode string) TransportType {
	switch strings.ToUpper(mode) {
	case "TRAM":
		return TypeTram
	case "TRAIN", "RAIL":
		return TypeTrain
	case "METRO", "SUBWAY":
		return TypeMetro
	case "FERRY", "BOAT":
		return TypeFerry
	default:
		return TypeBus
	}
}
