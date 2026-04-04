package main

import (
	"fmt"
	"log"

	"github.com/scheibling/bussar/api"
	"github.com/scheibling/bussar/server"
)

func main() {
	cfg := loadConfig()

	fmt.Printf("bussar starting — listening on %s\n", cfg.ListenAddr)
	fmt.Printf("  refresh interval : %ds\n", cfg.RefreshInterval)
	fmt.Printf("  panels           : %d\n", len(cfg.Panels))
	for _, p := range cfg.Panels {
		fmt.Printf("    • %q (%s, %d stop(s))\n", p.Name, p.Mode, len(p.Stops))
	}

	srvCfg := server.Config{
		Addr:            cfg.ListenAddr,
		RefreshInterval: cfg.RefreshInterval,
		APIKey:          cfg.APIKey,
		RealtimeKey:     cfg.RealtimeKey,
	}
	for _, p := range cfg.Panels {
		panel := server.PanelSpec{
			Name: p.Name,
			Mode: string(p.Mode),
		}
		for _, s := range p.Stops {
			stop := server.StopSpec{
				ID:               s.ID,
				Name:             s.Name,
				MaxDepartures:    s.MaxDepartures,
				FilterLines:      s.FilterLines,
				FilterDirections: s.FilterDirections,
			}
			for _, tt := range s.TransportTypes {
				stop.TransportTypes = append(stop.TransportTypes, api.TransportType(tt))
			}
			panel.Stops = append(panel.Stops, stop)
		}
		srvCfg.Panels = append(srvCfg.Panels, panel)
	}

	srv := server.New(srvCfg)
	if err := srv.Run(); err != nil {
		log.Fatalf("bussar: server error: %v", err)
	}
}
