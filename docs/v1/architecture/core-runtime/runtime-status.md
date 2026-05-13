# Runtime Status

The runtime tracks its status through a lifecycle. Other parts of the system use this status to know if the runtime is ready.

## Status Values

- **Starting** — the runtime is building (config, storage, security, integrations)
- **Ready** — the runtime is fully built and can process turns
- **Stopping** — the runtime is shutting down (draining connections, saving state)
- **Error** — something went wrong during startup (details available)

## How Status Is Used

- **Health checks** — `/ready` returns 200 only when status is Ready
- **Channels** — channels check status before accepting messages
- **Service API** — the API returns 503 if the runtime is not ready
- **CLI** — commands wait for Ready before processing turns

## Status Transitions

Starting -> Ready -> Stopping -> (exit)

If an error happens during Starting, the status goes to Error. If an error happens during Ready, the runtime tries to recover. If recovery fails, it logs the error and continues (degraded mode).

## Checking Status

You can check status through:
- The CLI: `or3-intern service status`
- The API: `GET /status`
- The health endpoint: `GET /ready`
