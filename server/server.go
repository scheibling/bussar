// Package server provides the HTTP server that serves the departure board.
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/scheibling/bussar/api"
)

// StopSpec carries everything the server needs to know about one stop.
type StopSpec struct {
	ID               int
	Name             string
	MaxDepartures    int
	TransportTypes   []api.TransportType
	FilterLines      []string
	FilterDirections []string
}

// PanelSpec describes a single panel on the board.
type PanelSpec struct {
	Name  string
	Mode  string // "combined" | "separate"
	Stops []StopSpec
}

// Config holds all runtime settings for the server.
type Config struct {
	Addr            string
	RefreshInterval int
	APIKey          string
	RealtimeKey     string
	Panels          []PanelSpec
}

// Server runs the HTTP departure board.
type Server struct {
	cfg            Config
	client         *api.Client
	realtimeClient *api.RealtimeClient
}

// New creates a new Server.
func New(cfg Config) *Server {
	return &Server{
		cfg:            cfg,
		client:         api.NewClient(cfg.APIKey),
		realtimeClient: api.NewRealtimeClient(cfg.RealtimeKey),
	}
}

// Run starts the HTTP server and blocks until SIGINT/SIGTERM or an error.
func (s *Server) Run() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/departures", s.handleDepartures)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/flipper", s.handleFlipperAPI)
	mux.HandleFunc("/flipper", s.handleFlipperUI)
	mux.HandleFunc("/", handleStatic)

	srv := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", s.cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-quit:
		log.Println("shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// --- /api/config -------------------------------------------------------------

type configResponse struct {
	RefreshInterval int         `json:"refresh_interval"`
	Panels          []panelMeta `json:"panels"`
}

type panelMeta struct {
	Name  string     `json:"name"`
	Mode  string     `json:"mode"`
	Stops []stopMeta `json:"stops"`
}

type stopMeta struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := configResponse{
		RefreshInterval: s.cfg.RefreshInterval,
	}
	for _, p := range s.cfg.Panels {
		pm := panelMeta{Name: p.Name, Mode: p.Mode}
		for _, st := range p.Stops {
			pm.Stops = append(pm.Stops, stopMeta{ID: st.ID, Name: st.Name})
		}
		resp.Panels = append(resp.Panels, pm)
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- /api/departures ---------------------------------------------------------

type departuresResponse struct {
	Panels      []panelResponse `json:"panels"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type panelResponse struct {
	Name       string              `json:"name"`
	Mode       string              `json:"mode"`
	Columns    []columnResponse    `json:"columns,omitempty"`    // mode=separate
	Departures []departureResponse `json:"departures,omitempty"` // mode=combined
}

type columnResponse struct {
	StopName   string              `json:"stop_name"`
	Departures []departureResponse `json:"departures"`
}

type departureResponse struct {
	Stop          string `json:"stop"`
	Line          string `json:"line"`
	Direction     string `json:"direction"`
	Scheduled     string `json:"scheduled"`
	Realtime      string `json:"realtime,omitempty"`
	CountdownMin  int    `json:"countdown_min"`
	TransportType string `json:"transport_type"`
	Cancelled     bool   `json:"cancelled"`
}

func (s *Server) handleDepartures(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	ttl := time.Duration(s.cfg.RefreshInterval) * time.Second
	now := time.Now()

	resp := departuresResponse{GeneratedAt: now}

	for _, panel := range s.cfg.Panels {
		pr := panelResponse{Name: panel.Name, Mode: panel.Mode}

		switch strings.ToLower(panel.Mode) {
		case "separate":
			for _, stop := range panel.Stops {
				deps, err := s.fetchFiltered(ctx, stop, ttl)
				if err != nil {
					log.Printf("stop %d: fetch error: %v", stop.ID, err)
				}
				if len(deps) > stop.MaxDepartures {
					deps = deps[:stop.MaxDepartures]
				}
				name := stop.Name
				if name == "" && len(deps) > 0 {
					name = deps[0].StopName
				}
				col := columnResponse{StopName: name}
				for _, d := range deps {
					col.Departures = append(col.Departures, toResponse(d, now))
				}
				pr.Columns = append(pr.Columns, col)
			}

		default: // combined
			var all []api.Departure
			maxTotal := 0
			for _, stop := range panel.Stops {
				deps, err := s.fetchFiltered(ctx, stop, ttl)
				if err != nil {
					log.Printf("stop %d: fetch error: %v", stop.ID, err)
				}
				all = append(all, deps...)
				maxTotal += stop.MaxDepartures
			}
			sort.Slice(all, func(i, j int) bool {
				ti := effectiveTime(all[i])
				tj := effectiveTime(all[j])
				return ti.Before(tj)
			})
			if len(all) > maxTotal {
				all = all[:maxTotal]
			}
			for _, d := range all {
				pr.Departures = append(pr.Departures, toResponse(d, now))
			}
		}

		resp.Panels = append(resp.Panels, pr)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) fetchFiltered(ctx context.Context, stop StopSpec, ttl time.Duration) ([]api.Departure, error) {
	// Fetch enough from the API to have plenty after filtering.
	fetchMax := stop.MaxDepartures * 5
	if fetchMax < 50 {
		fetchMax = 50
	}

	deps, err := s.client.FetchDepartures(ctx, stop.ID, fetchMax, ttl)
	if err != nil {
		return nil, err
	}

	f := api.Filter{
		TransportTypes:   stop.TransportTypes,
		FilterLines:      stop.FilterLines,
		FilterDirections: stop.FilterDirections,
	}
	return f.Apply(deps), nil
}

func effectiveTime(d api.Departure) time.Time {
	if d.Realtime != nil {
		return *d.Realtime
	}
	return d.Scheduled
}

func toResponse(d api.Departure, now time.Time) departureResponse {
	dr := departureResponse{
		Stop:          d.StopName,
		Line:          d.Line,
		Direction:     d.Direction,
		Scheduled:     d.Scheduled.Format("15:04"),
		CountdownMin:  d.CountdownMinutes(now),
		TransportType: string(d.TransportType),
		Cancelled:     d.Cancelled,
	}
	if d.Realtime != nil {
		dr.Realtime = d.Realtime.Format("15:04")
	}
	return dr
}

// --- /flipper ----------------------------------------------------------------

type flipperResponse struct {
	RefreshInterval int          `json:"refresh_interval"`
	Departures      []flipperRow `json:"departures"`
}

type flipperRow struct {
	Time      string `json:"time"`
	Delay     int    `json:"delay"`    // seconds; negative=early, 0=on-time, positive=late
	Direction string `json:"direction"`
	Line      string `json:"line"`
	TimeLeft  int    `json:"timeLeft,omitempty"`
	Stop      string `json:"stop"`
}

func (s *Server) handleFlipperUI(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/flipper.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleFlipperAPI(w http.ResponseWriter, r *http.Request) {
	if len(s.cfg.Panels) == 0 {
		writeJSON(w, http.StatusOK, flipperResponse{
			RefreshInterval: s.cfg.RefreshInterval,
			Departures:      []flipperRow{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	ttl := time.Duration(s.cfg.RefreshInterval) * time.Second
	now := time.Now()
	panel := s.cfg.Panels[0]

	var all []api.Departure
	maxTotal := 0
	for _, stop := range panel.Stops {
		fetchMax := stop.MaxDepartures * 5
		if fetchMax < 50 {
			fetchMax = 50
		}
		raw, err := s.realtimeClient.FetchDepartures(ctx, stop.ID, fetchMax, ttl)
		if err != nil {
			log.Printf("flipper stop %d: realtime fetch error: %v", stop.ID, err)
		}
		f := api.Filter{
			TransportTypes:   stop.TransportTypes,
			FilterLines:      stop.FilterLines,
			FilterDirections: stop.FilterDirections,
		}
		deps := f.Apply(raw)
		if len(deps) > stop.MaxDepartures {
			deps = deps[:stop.MaxDepartures]
		}
		all = append(all, deps...)
		maxTotal += stop.MaxDepartures
	}
	sort.Slice(all, func(i, j int) bool {
		return effectiveTime(all[i]).Before(effectiveTime(all[j]))
	})
	if len(all) > maxTotal {
		all = all[:maxTotal]
	}

	rows := make([]flipperRow, 0, len(all))
	for _, d := range all {
		t := d.Scheduled
		if d.Realtime != nil {
			t = *d.Realtime
		}
		rows = append(rows, flipperRow{
			Time:      t.Format("15:04"),
			Delay:     d.DelaySeconds,
			Direction: d.Direction,
			Line:      d.Line,
			TimeLeft:  d.CountdownMinutes(now),
			Stop:      d.StopName,
		})
	}

	writeJSON(w, http.StatusOK, flipperResponse{
		RefreshInterval: s.cfg.RefreshInterval,
		Departures:      rows,
	})
}

// --- helpers -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
