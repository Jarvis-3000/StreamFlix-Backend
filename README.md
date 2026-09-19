# StreamFlix Backend — Phase 1

Local HLS transcoder for the StreamFlix OTT platform.

Takes an MP4 file, converts it to HLS using FFmpeg, and produces a multi-quality
stream ready to be served to any HLS-compatible player.

---

## What this phase does

- Reads `storage/input/sample.mp4`
- Runs FFmpeg to produce 360p and 720p HLS renditions
- Writes segment `.ts` files and per-quality `index.m3u8` playlists
- Writes a top-level `master.m3u8` that media players use to pick the right quality
- Prints the output paths to the terminal

---

## Project structure

```
streamflix-backend/
│
├── cmd/app/main.go                  Entry point — wires all layers together
│
├── internal/
│   ├── config/config.go             Loads FFMPEG_PATH, OUTPUT_DIRECTORY from env
│   ├── models/video.go              Video struct (no DB yet)
│   ├── utils/ffmpeg.go              Thin wrapper around exec.Command for FFmpeg
│   │
│   ├── services/
│   │   ├── transcoder/service.go    Transcoder interface + FFmpegTranscoder impl
│   │   └── video/service.go         VideoService — orchestrates storage + transcoding
│   │
│   └── storage/
│       └── local_storage.go         Storage interface + LocalStorage impl
│
├── storage/
│   ├── input/sample.mp4             ← place your test video here
│   └── output/                      ← generated HLS files appear here
│
├── .env.example                     Copy to .env and edit
├── go.mod
└── README.md
```

### Why each package exists

| Package | Responsibility |
|---|---|
| `config` | Single place to read env vars. Future phases add DB_URL, S3_BUCKET, etc. here. |
| `models` | Plain Go structs. No ORM, no DB tags yet. |
| `utils/ffmpeg` | Isolates `exec.Command` so the transcoder service stays readable. |
| `services/transcoder` | **Interface** + FFmpeg impl. Swap for cloud transcoding without touching VideoService. |
| `services/video` | Orchestration only — calls Storage then Transcoder, returns a result. |
| `storage` | **Interface** + LocalStorage impl. Swap for DigitalOcean Spaces / S3 in Phase 2. |

---

## Prerequisites

### 1. Go 1.22+

```bash
go version
```

### 2. FFmpeg

**macOS (Homebrew):**
```bash
brew install ffmpeg
```

**Ubuntu / Debian:**
```bash
sudo apt update && sudo apt install ffmpeg
```

**Windows:**
Download from https://ffmpeg.org/download.html and add to PATH.

Verify:
```bash
ffmpeg -version
```

---

## Setup

```bash
# 1. Clone / enter the project
cd streamflix-backend

# 2. Copy environment file
cp .env.example .env

# 3. Place a test video
cp /path/to/any.mp4 storage/input/sample.mp4

# 4. Run
go run ./cmd/app
```

---

## Expected output

```
Processing: storage/input/sample.mp4

Video processed successfully

ID:              a1b2c3d4
Input:           storage/input/sample.mp4
Master Playlist: storage/output/sample_a1b2c3d4/master.m3u8
Created At:      2026-06-06 10:00:00
```

Generated file tree:

```
storage/output/sample_a1b2c3d4/
├── master.m3u8
├── 360p/
│   ├── index.m3u8
│   ├── segment_000.ts
│   └── segment_001.ts
└── 720p/
    ├── index.m3u8
    ├── segment_000.ts
    └── segment_001.ts
```

---

## Architecture decisions

### Interfaces everywhere (even with one implementation)

Both `Transcoder` and `Storage` are defined as interfaces.
This is not over-engineering — it enforces the dependency rule:
**VideoService knows nothing about FFmpeg or the filesystem.**

When Phase 2 adds DigitalOcean Spaces:

```go
// storage/spaces_storage.go  ← new file only, nothing else changes
type SpacesStorage struct { ... }
func (s *SpacesStorage) Save(outputDir string) error { ... }
```

Then in `main.go`, replace one line:

```go
// Before
localStorage := storage.NewLocalStorage(cfg.OutputDirectory)

// After
spacesStorage := storage.NewSpacesStorage(cfg.SpacesBucket, cfg.SpacesKey, cfg.SpacesSecret)
```

VideoService, Transcoder, and all business logic remain untouched.

### Why no Gin yet

Phase 1 has no HTTP traffic. Adding Gin before there is an API to serve would
be premature. Phase 2 will add `cmd/server/main.go` with a Gin router — the
existing `cmd/app/main.go` can coexist or be retired at that point.

---

## Planned phases

| Phase | Features |
|---|---|
| **Phase 1 (this)** | Local FFmpeg HLS transcoding |
| Phase 2 | REST API (Gin), PostgreSQL, DigitalOcean Spaces upload |
| Phase 3 | Authentication (JWT), user management |
| Phase 4 | Kafka event streaming, background workers |
| Phase 5 | MongoDB Atlas Search for search and recommendations |
