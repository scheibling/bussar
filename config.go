package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TransportType represents a supported means of transport.
type TransportType string

const (
	TransportBus   TransportType = "bus"
	TransportTram  TransportType = "tram"
	TransportTrain TransportType = "train"
	TransportMetro TransportType = "metro"
	TransportFerry TransportType = "ferry"
)

// StopConfig holds the configuration for a single stop.
type StopConfig struct {
	// ID is the ResRobot stop ID (required).
	ID int `yaml:"id"`
	// Name overrides the stop display name shown in the board header.
	// If empty the line name returned by the API is used.
	Name string `yaml:"name"`
	// MaxDepartures is the number of departure rows to display. Default: 10.
	MaxDepartures int `yaml:"max_departures"`
	// TransportTypes restricts which transport types are shown.
	// Leave empty to show all types.
	TransportTypes []TransportType `yaml:"transport_types"`
	// FilterLines restricts which line numbers are shown (e.g. ["55", "76"]).
	// Leave empty to show all lines.
	FilterLines []string `yaml:"filter_lines"`
	// FilterDirections restricts departures to those whose direction contains
	// one of the given substrings (case-insensitive). Leave empty for no filter.
	FilterDirections []string `yaml:"filter_directions"`
}

// PanelMode controls how multiple stops inside a panel are rendered.
type PanelMode string

const (
	// PanelModeCombined merges all stops into one list sorted by departure time.
	PanelModeCombined PanelMode = "combined"
	// PanelModeSeparate renders each stop as its own column side-by-side.
	PanelModeSeparate PanelMode = "separate"
)

// PanelConfig holds the configuration for a departure board panel.
type PanelConfig struct {
	// Name is the display title of the panel.
	Name string `yaml:"name"`
	// Mode controls how multiple stops are displayed. Default: "combined".
	Mode PanelMode `yaml:"mode"`
	// Stops is the list of stops to include in this panel.
	Stops []StopConfig `yaml:"stops"`
}

// Config is the top-level application configuration.
type Config struct {
	// APIKey is the Trafiklab / ResRobot access ID (required).
	APIKey string `yaml:"api_key"`
	// RefreshInterval is how often (in seconds) the board fetches fresh data. Default: 120.
	RefreshInterval int `yaml:"refresh_interval"`
	// ListenAddr is the TCP address the HTTP server listens on. Default: ":8080".
	ListenAddr string `yaml:"listen_addr"`
	// Panels is the ordered list of departure board panels to display.
	Panels []PanelConfig `yaml:"panels"`
}

var configPath = flag.String("config", "./config.yaml", "path to YAML config file")

// loadConfig parses the CLI flags, reads the config file, applies defaults,
// and validates the result. It terminates the process on any error.
func loadConfig() *Config {
	flag.Parse()

	data, err := os.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bussar: cannot read config file %q: %v\n", *configPath, err)
		os.Exit(1)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "bussar: cannot parse config file: %v\n", err)
		os.Exit(1)
	}

	applyDefaults(&cfg)

	if err := validateConfig(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "bussar: invalid configuration: %v\n", err)
		os.Exit(1)
	}

	return &cfg
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = 120
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	for pi := range cfg.Panels {
		if cfg.Panels[pi].Mode == "" {
			cfg.Panels[pi].Mode = PanelModeCombined
		}
		for si := range cfg.Panels[pi].Stops {
			if cfg.Panels[pi].Stops[si].MaxDepartures == 0 {
				cfg.Panels[pi].Stops[si].MaxDepartures = 10
			}
		}
	}
}

// validateConfig returns an error if any required field is missing or invalid.
func validateConfig(cfg *Config) error {
	if cfg.APIKey == "" {
		return errors.New("api_key is required")
	}
	if len(cfg.Panels) == 0 {
		return errors.New("at least one panel must be configured")
	}
	for i, panel := range cfg.Panels {
		label := fmt.Sprintf("panel[%d]", i)
		if panel.Name != "" {
			label = fmt.Sprintf("panel %q", panel.Name)
		}
		if len(panel.Stops) == 0 {
			return fmt.Errorf("%s: at least one stop is required", label)
		}
		if panel.Mode != PanelModeCombined && panel.Mode != PanelModeSeparate {
			return fmt.Errorf("%s: mode must be %q or %q, got %q", label, PanelModeCombined, PanelModeSeparate, panel.Mode)
		}
		for j, stop := range panel.Stops {
			stopLabel := fmt.Sprintf("%s stop[%d]", label, j)
			if stop.ID == 0 {
				return fmt.Errorf("%s: id is required", stopLabel)
			}
			for _, tt := range stop.TransportTypes {
				switch tt {
				case TransportBus, TransportTram, TransportTrain, TransportMetro, TransportFerry:
					// valid
				default:
					return fmt.Errorf("%s: unknown transport_type %q (valid: bus, tram, train, metro, ferry)", stopLabel, tt)
				}
			}
		}
	}
	return nil
}
