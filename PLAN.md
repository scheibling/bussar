# Bussar — Bus Departure Board

A Go web application that renders a train-platform-style departure board, fetching live data from the ResRobot API (Trafiklab).

---

## Design Decisions

| Concern | Decision |
|---|---|
| Language | Go (single binary) |
| Frontend | HTML/CSS/JS embedded into binary via `embed.FS` |
| Config | YAML file loaded at startup |
| API | ResRobot v2.1 `/departureBoard` endpoint |
| Visual style | Modern dark LED board (Stockholm Centralstation style) |
| Time display | Scheduled departure time **+** live countdown (e.g. "14:32 · 3 min") |
| Auto-refresh | Every 2 minutes (client-side + server push via polling) |
| Multi-stop | Configurable: stops can be grouped into **panels** or merged into a **combined view** |
| Transport filter | Per-stop filter in config (bus, tram, train, metro, ferry, or all) |

---

## Milestones

### Milestone 1 — Project Skeleton
- [ ] Initialize Go module (`go mod init`)
- [ ] Directory structure:
  ```
  bussar/
  ├── main.go
  ├── config.go
  ├── api/
  │   └── resrobot.go
  ├── server/
  │   └── server.go
  ├── web/
  │   ├── index.html
  │   ├── style.css
  │   └── app.js
  ├── config.example.yaml
  └── PLAN.md
  ```
- [ ] `go.mod` and initial dependencies (`gopkg.in/yaml.v3`)
- [ ] `config.example.yaml` with documented fields

---

### Milestone 2 — Configuration
- [ ] Define `Config` struct in `config.go`
- [ ] Support top-level fields:
  - `api_key` (ResRobot access ID)
  - `refresh_interval` (seconds, default 120)
  - `panels` (list of panels)
- [ ] Per-panel fields:
  - `name` — display name for the panel
  - `mode` — `"separate"` (each stop gets its own column) or `"combined"` (all stops merged and sorted by time)
  - `stops` — list of stops
- [ ] Per-stop fields:
  - `id` — ResRobot stop ID
  - `name` — override display name (optional; auto-fetched otherwise)
  - `max_departures` — how many rows to show (default 10)
  - `transport_types` — list: `bus`, `tram`, `train`, `metro`, `ferry`; empty = all
  - `filter_lines` — optional list of line numbers to include
  - `filter_directions` — optional list of direction substrings to include
- [ ] Load config from path given as CLI flag `-config` (default `./config.yaml`)
- [ ] Validate required fields and exit with clear error if missing

---

### Milestone 3 — ResRobot API Client
- [ ] Implement `api/resrobot.go`:
  - `FetchDepartures(stopID string, maxJourneys int, apiKey string) ([]Departure, error)`
  - Parse JSON response from `https://api.resrobot.se/v2.1/departureBoard`
  - Map ResRobot `catCode` to transport type label and icon name
  - Extract fields: `name` (line name), `num` (line number), `direction`, `time`, `date`, `rtTime`, `rtDate`
- [ ] Transport type code mapping:
  | catCode | Type |
  |---|---|
  | 1, 2, 4 | train |
  | 3, 7 | bus |
  | 5 | metro |
  | 6 | tram |
  | 8 | ferry |
- [ ] Filter departures that have already departed
- [ ] Apply `transport_types`, `filter_lines`, `filter_directions` from config
- [ ] Cache responses per stop with TTL equal to `refresh_interval`

---

### Milestone 4 — HTTP Server
- [ ] Implement `server/server.go`:
  - Serve embedded static files from `web/` via `embed.FS`
  - `GET /` — serve `index.html`
  - `GET /api/departures` — return JSON of all panels and their departures
  - `GET /api/config` — return refresh interval and panel/stop metadata for the frontend
- [ ] JSON response shape for `/api/departures`:
  ```json
  {
    "panels": [
      {
        "name": "Central",
        "mode": "combined",
        "departures": [
          {
            "stop": "Stockholm C",
            "line": "55",
            "direction": "Hornstull",
            "scheduled": "14:32",
            "realtime": "14:34",
            "countdown_min": 3,
            "transport_type": "bus",
            "cancelled": false
          }
        ]
      }
    ],
    "generated_at": "2026-04-04T14:29:00Z"
  }
  ```
- [ ] Graceful shutdown on SIGINT/SIGTERM
- [ ] Listen address configurable via `-addr` flag (default `:8080`)

---

### Milestone 5 — Frontend
- [ ] `index.html` — minimal shell, loads CSS and JS
- [ ] `style.css` — dark LED board theme:
  - Black background (`#0a0a0a`)
  - Amber/orange primary text (`#ffb300`) for times and countdowns
  - White text for line numbers and directions
  - Monospace font (e.g. `JetBrains Mono` or `Roboto Mono` from Google Fonts CDN, with monospace fallback)
  - Full-viewport layout, no scrollbars on the board itself
  - Subtle scanline or grid overlay (optional, CSS only)
  - Responsive: stacks panels vertically on small screens
- [ ] `app.js`:
  - Fetch `/api/departures` on load and every `refresh_interval` seconds
  - Render panels side-by-side (`separate` mode) or as single table (`combined` mode)
  - Departure row columns: **Transport icon · Line · Direction · Scheduled · Countdown**
  - Countdown computed client-side from `scheduled` time, updated every 30 seconds without refetching
  - Highlight rows where countdown ≤ 2 min (dimmed/different color = "boarding")
  - Show "Now" when countdown ≤ 0
  - Animate new rows in when data refreshes (fade transition)
  - Show last-updated timestamp in footer
  - Show error state if API fetch fails (keep showing stale data with a warning banner)
- [ ] Transport type icons — inline SVG or Unicode characters (no external icon library dependency)

---

### Milestone 6 — Binary & Distribution
- [ ] All frontend files embedded using `//go:embed web/*`
- [ ] `go build -o bussar .` produces a single self-contained binary
- [ ] `config.example.yaml` shipped alongside binary
- [ ] Dockerfile (optional, simple `FROM scratch` or `FROM alpine`)
- [ ] `README.md` with:
  - How to get a ResRobot API key (Trafiklab)
  - How to find stop IDs (reference `stops.txt`)
  - How to configure `config.yaml`
  - How to run

---

### Milestone 7 — Polish & Testing
- [ ] Graceful handling of empty departure lists per stop
- [ ] Graceful handling of ResRobot API errors / rate limits
- [ ] Request timeout on API calls (5 seconds)
- [ ] Unit tests for:
  - Config parsing
  - Departure filtering logic
  - Countdown calculation
- [ ] Manual test against live ResRobot API with at least 2 stops

---

## Config Example (preview)

```yaml
api_key: "YOUR_TRAFIKLAB_API_KEY"
refresh_interval: 120

panels:
  - name: "City Centre"
    mode: combined          # merge all stops into one sorted list
    stops:
      - id: 740000001       # Stockholm Centralstation
        max_departures: 10
        transport_types: [bus, tram]
      - id: 740000005       # Uppsala Centralstation
        max_departures: 5

  - name: "Local Routes"
    mode: separate          # each stop shown as its own column
    stops:
      - id: 740000027       # Märsta station
        name: "Märsta"
        max_departures: 8
        filter_lines: ["583", "592"]
      - id: 740000031       # Flemingsberg
        name: "Flemingsberg"
        max_departures: 8
        transport_types: [train]
        filter_directions: ["Stockholm"]
```
