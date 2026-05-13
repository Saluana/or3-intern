# Service API Overview

The service API provides REST/HTTP endpoints for external apps. It lives in the main package files: `service.go`, `service_routes.go`, and related files.

## What the API Provides

- **Chat turns** — send messages and get responses (sync or streaming)
- **Background jobs** — create, check, and cancel jobs
- **File operations** — upload, download, and list files
- **Terminal sessions** — interactive terminal access
- **Approval management** — review and respond to approval requests
- **Runner chat** — talk to external AI CLIs (like OpenCode)
- **Cron management** — schedule and manage cron jobs
- **Configuration** — read and update settings
- **Health and readiness** — check if the service is alive

## Who Uses It

The API is designed for:
- **OR3 App** — mobile and tablet app for chatting with the agent
- **OR3 Net** — web interface for managing the agent

## Base Path

All routes are under `/api/v1`. For example: `http://localhost:9100/api/v1/turns`.

## Port

The service listens on port 9100 by default. You can change this in the config.
