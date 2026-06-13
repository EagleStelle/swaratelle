<div align="center">
  <img src="./frontend/public/logo.svg" alt="Swaratelle logo" width="96" height="96" />

  <h1>Swaratelle</h1>

  <p>
    Swaratelle is a self-hosted web interface for managing Iwara downloads through a bundled <code>iwaradl</code> runtime.
    It provides a browser UI for queueing videos, monitoring active downloads, searching history, and reconciling downloaded files with a SQLite-backed record store.
  </p>
</div>

## Features

- Queue Iwara video URLs from a browser UI.
- Track active downloads with progress parsed from `iwaradl`.
- Deduplicate downloads by Iwara `video_id`.
- Browse and search completed download history.
- Reconcile history against files on disk.
- Expose a token-protected API for scripts and server-to-server integrations.

## Docker

Swaratelle is distributed as `eaglestelle/swaratelle:latest`.

Create a `docker-compose.yml` like this:

```yaml
services:
  swaratelle:
    image: eaglestelle/swaratelle:latest
    container_name: swaratelle
    restart: unless-stopped
    environment:
      SWARATELLE_API_TOKEN: "replace-this-with-a-long-random-token"
    volumes:
      - ./data:/data
      - ./media:/media
      - ./scratch:/scratch
    ports:
      - "8842:8842"
```

Replace `SWARATELLE_API_TOKEN` with a long random value before starting the container.

Start it:

```sh
docker compose up -d
```

Open:

```text
http://localhost:8842
```

Stop the service:

```sh
docker compose down
```

The default port mapping is `8842:8842`. If host port `8842` is already in use, change only the left side in `docker-compose.yml`, for example:

```yaml
ports:
  - "9000:8842"
```

Then open `http://localhost:9000`.

## Windows

On Windows, `run.cmd` provides a Docker-only setup path that does not depend on Docker Compose.

```bat
run.cmd
```

The script checks Docker, creates `.env` from `.env.example` when missing, creates `local\data`, `local\media`, and `local\scratch`, pulls `eaglestelle/swaratelle:latest`, and starts the `swaratelle` container with `docker run`.

Stop or restart the Windows helper container:

```bat
docker stop swaratelle
docker start swaratelle
```

## Configuration

Swaratelle has one runtime environment variable. Set it inline in Docker Compose or pass it to `docker run`.

| Variable               |    Required | Description                                                                                    |
| ---------------------- | ----------: | ---------------------------------------------------------------------------------------------- |
| `SWARATELLE_API_TOKEN` | Recommended | Shared API token for queue, history, and scan endpoints. Keep it server-side for integrations. |

If `SWARATELLE_API_TOKEN` is empty, API routes are open to anyone who can reach the service. For shared or network-accessible deployments, set a strong token and control access at the network or reverse-proxy layer.

Storage is configured with Docker bind mounts. Container paths are fixed:

| Purpose        | Docker Compose | Windows `run.cmd` | Container Path |
| -------------- | -------------- | ----------------- | -------------- |
| Database       | `./data`       | `./local/data`    | `/data`        |
| Media output   | `./media`      | `./local/media`   | `/media`       |
| Temporary work | `./scratch`    | `./local/scratch` | `/scratch`     |

## Architecture

| Layer           | Path                                                  | Technology                     | Responsibility                                                                                                                      |
| --------------- | ----------------------------------------------------- | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| Runtime service | `backend/cmd/service`                                 | Go `net/http`                  | Starts the API server, serves the exported frontend, opens SQLite, configures the downloader, and handles shutdown.                 |
| API layer       | `backend/internal/api`                                | Go                             | Defines HTTP routes, token/session authentication, JSON responses, history pagination, static serving, and disk scan orchestration. |
| Download worker | `backend/internal/downloader`                         | Go + `iwaradl`                 | Extracts video IDs, queues work, launches `iwaradl`, parses progress, moves completed media, and reconciles files from disk.        |
| Persistence     | `backend/internal/db`                                 | SQLite                         | Stores download records, status transitions, deduplication, history queries, indexes, and cursor pagination.                        |
| Frontend app    | `frontend/app`, `frontend/components`, `frontend/lib` | Next.js, React, TanStack Query | Provides the Downloads and History screens, UI primitives, same-origin API client, polling, mutations, and formatting.              |
| Tests           | `backend/internal/**/_test.go`, `frontend/tests`      | Go test, Vitest, Playwright    | Covers persistence, disk reconciliation, UI primitives, utilities, and browser flows.                                               |

## API

All endpoints except `/api/health` require authentication when `SWARATELLE_API_TOKEN` is configured. External clients authenticate with:

```http
Authorization: Bearer <SWARATELLE_API_TOKEN>
```

| Method | Endpoint                                         | Description                                                                  |
| ------ | ------------------------------------------------ | ---------------------------------------------------------------------------- |
| `GET`  | `/api/health`                                    | Liveness check.                                                              |
| `GET`  | `/api/downloads`                                 | Lists all download records.                                                  |
| `GET`  | `/api/downloads/active`                          | Lists pending, downloading, and failed records with live progress when available. |
| `GET`  | `/api/history?limit=50&cursor=<cursor>&q=<term>` | Lists completed records, with cursor pagination and title/artist search.     |
| `POST` | `/api/queue`                                     | Queues one or more Iwara video URLs.                                         |
| `POST` | `/api/scan`                                      | Reconciles database history with files currently present in `/media`.        |

Queue example:

```sh
curl -X POST http://localhost:8842/api/queue \
  -H "Authorization: Bearer $SWARATELLE_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"urls":["https://www.iwara.tv/video/abc123"]}'
```

## Development

Backend tests:

```sh
cd backend
go test ./...
```

Frontend setup and tests:

```sh
cd frontend
npm install
npm run test
npm run test:e2e
```

Frontend development server:

```sh
npm run dev
```

## Credits

- [iwara-dl / `iwaradl`](https://github.com/Izumiko/iwaradl) by Izumiko: the downloader used by Swaratelle. The Docker image builds it from the upstream `v1.5.4` tag.
- [Next.js](https://nextjs.org/) and [React](https://react.dev/) for the frontend application.
- [TanStack Query](https://tanstack.com/query/latest) for client-side API state.
- [Lucide](https://lucide.dev/) for interface icons.
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) for the pure-Go SQLite driver.

Swaratelle is an independent project and is not affiliated with Iwara or the `iwaradl` maintainers.
