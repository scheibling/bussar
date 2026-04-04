// Package api provides a client for the ResRobot v2.1 departure board API.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	endpoint       = "https://api.resrobot.se/v2.1/departureBoard"
	requestTimeout = 5 * time.Second
)

// TransportType mirrors the config type so the api package stays self-contained.
type TransportType string

const (
	TypeBus   TransportType = "bus"
	TypeTram  TransportType = "tram"
	TypeTrain TransportType = "train"
	TypeMetro TransportType = "metro"
	TypeFerry TransportType = "ferry"
)

// catCodeToType maps ResRobot catCode values to TransportType.
func catCodeToType(code int) TransportType {
	switch code {
	case 1, 2, 4:
		return TypeTrain
	case 5:
		return TypeMetro
	case 6:
		return TypeTram
	case 8:
		return TypeFerry
	default: // 3, 7 and anything else → bus
		return TypeBus
	}
}

// Departure represents a single departure row.
// DelaySeconds is negative for early, positive for late, zero for on-time.
// Only populated when the source provides an explicit delay value (realtime API).
type Departure struct {
	StopName      string        `json:"stop"`
	Line          string        `json:"line"`
	Direction     string        `json:"direction"`
	Scheduled     time.Time     `json:"scheduled"`
	Realtime      *time.Time    `json:"realtime,omitempty"`
	TransportType TransportType `json:"transport_type"`
	Cancelled     bool          `json:"cancelled"`
	DelaySeconds  int           `json:"delay_seconds"`
}

// CountdownMinutes returns minutes until departure, using realtime if available.
// Returns 0 when the departure is now or in the past.
func (d *Departure) CountdownMinutes(now time.Time) int {
	t := d.Scheduled
	if d.Realtime != nil {
		t = *d.Realtime
	}
	diff := t.Sub(now)
	if diff <= 0 {
		return 0
	}
	return int(diff.Minutes())
}

// --- raw API response structs ------------------------------------------------

type apiResponse struct {
	Departure []apiDeparture `json:"Departure"`
}

type apiDeparture struct {
	Name          string       `json:"name"`
	Stop          string       `json:"stop"`
	Direction     string       `json:"direction"`
	Destination   string       `json:"destination"`
	Date          string       `json:"date"`
	Time          string       `json:"time"`
	RtDate        string       `json:"rtDate"`
	RtTime        string       `json:"rtTime"`
	Cancelled     string       `json:"cancelled"`
	Product       []apiProduct `json:"Product"`
	JourneyStatus string       `json:"JourneyStatus"`
	DirectionFlag string       `json:"directionFlag"`
}

type apiProduct struct {
	Num     string `json:"num"`
	CatCode string `json:"catCode"`
}

// --- cache -------------------------------------------------------------------

type cacheEntry struct {
	departures []Departure
	fetchedAt  time.Time
}

// Client fetches departure data from the ResRobot API with an in-memory cache.
type Client struct {
	apiKey     string
	httpClient *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewClient creates a new API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		cache: make(map[string]cacheEntry),
	}
}

// FetchDepartures returns departures for stopID, using a cached response if it
// is younger than ttl.
func (c *Client) FetchDepartures(ctx context.Context, stopID int, maxJourneys int, ttl time.Duration) ([]Departure, error) {
	key := fmt.Sprintf("%d", stopID)

	c.mu.Lock()
	entry, hit := c.cache[key]
	c.mu.Unlock()

	if hit && time.Since(entry.fetchedAt) < ttl {
		return entry.departures, nil
	}

	deps, err := c.fetch(ctx, stopID, maxJourneys)
	if err != nil {
		// Return stale data on error if we have any.
		if hit {
			return entry.departures, nil
		}
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = cacheEntry{departures: deps, fetchedAt: time.Now()}
	c.mu.Unlock()

	return deps, nil
}

func (c *Client) fetch(ctx context.Context, stopID, maxJourneys int) ([]Departure, error) {
	params := url.Values{
		"format":      {"json"},
		"accessId":    {c.apiKey},
		"id":          {fmt.Sprintf("%d", stopID)},
		"maxJourneys": {fmt.Sprintf("%d", maxJourneys)},
		"duration":    {"480"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("resrobot: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resrobot: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resrobot: API returned HTTP %d", resp.StatusCode)
	}

	debugData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("resrobot: read response body: %w", err)
	}
	fmt.Printf("DEBUG: API response: %s\n", string(debugData))

	os.WriteFile("debugresponse.json", debugData, 0o777)

	resp.Body = io.NopCloser(strings.NewReader(string(debugData)))

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("resrobot: decode response: %w", err)
	}

	return parseDepartures(result.Departure), nil
}

func parseDepartures(raw []apiDeparture) []Departure {
	now := time.Now()
	out := make([]Departure, 0, len(raw))

	for _, r := range raw {
		sched, err := parseDateTime(r.Date, r.Time)
		if err != nil {
			continue
		}
		// Skip already-departed services.
		if sched.Before(now) {
			continue
		}

		dep := Departure{
			StopName:  r.Stop,
			Direction: r.Direction,
			Scheduled: sched,
			Cancelled: strings.EqualFold(r.Cancelled, "true"),
		}

		// Line number from the first Product entry.
		if len(r.Product) > 0 {
			dep.Line = r.Product[0].Num
			if code := parseCatCode(r.Product[0].CatCode); code >= 0 {
				dep.TransportType = catCodeToType(code)
			}
		}
		if dep.Line == "" {
			dep.Line = r.Name
		}

		// Realtime if available.
		if r.RtDate != "" && r.RtTime != "" {
			if rt, err := parseDateTime(r.RtDate, r.RtTime); err == nil {
				dep.Realtime = &rt
			}
		}

		// Overrides
		if strings.ToLower(r.Direction) == "uppsala gränbystaden" && r.DirectionFlag == "2" {
			dep.Direction = "Science Park, Akademiska sjukhuset"
		}

		out = append(out, dep)
	}

	return out
}

func parseDateTime(date, t string) (time.Time, error) {
	// API returns "2006-01-02" and "15:04:05".
	combined := date + " " + t
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", combined, time.Local)
	if err != nil {
		// Fallback: try without seconds.
		parsed, err = time.ParseInLocation("2006-01-02 15:04", combined, time.Local)
	}
	return parsed, err
}

func parseCatCode(s string) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return -1
	}
	return n
}

// --- filtering ---------------------------------------------------------------

// Filter holds the per-stop filter settings from config.
type Filter struct {
	TransportTypes   []TransportType
	FilterLines      []string
	FilterDirections []string
}

// Apply filters a departure slice according to the filter rules.
// An empty slice in a filter field means "accept all".
func (f Filter) Apply(deps []Departure) []Departure {
	if len(f.TransportTypes) == 0 && len(f.FilterLines) == 0 && len(f.FilterDirections) == 0 {
		return deps
	}

	out := deps[:0:0]
	for _, d := range deps {
		if !f.matchTransport(d) || !f.matchLine(d) || !f.matchDirection(d) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (f Filter) matchTransport(d Departure) bool {
	if len(f.TransportTypes) == 0 {
		return true
	}
	for _, tt := range f.TransportTypes {
		if tt == d.TransportType {
			return true
		}
	}
	return false
}

func (f Filter) matchLine(d Departure) bool {
	if len(f.FilterLines) == 0 {
		return true
	}
	for _, line := range f.FilterLines {
		if strings.EqualFold(line, d.Line) {
			return true
		}
	}
	return false
}

func (f Filter) matchDirection(d Departure) bool {
	if len(f.FilterDirections) == 0 {
		return true
	}
	dir := strings.ToLower(d.Direction)
	for _, fd := range f.FilterDirections {
		if strings.Contains(dir, strings.ToLower(fd)) {
			return true
		}
	}
	return false
}
