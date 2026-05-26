# Configure API

The configure API is section-and-field based. It is designed for settings UIs, not for uploading an entire free-form config blob.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/configure/sections` | List editable sections with current status summaries |
| `GET /internal/v1/configure/fields?section=...` | List fields for one section |
| `GET /internal/v1/configure/fields?section=channels&channel=...` | List fields for one channel configuration |
| `POST /internal/v1/configure/apply` | Apply one or more field changes |

## Provider-related routes

The same family also exposes provider-specific helpers:

- `GET /internal/v1/configure/providers`
- `POST /internal/v1/configure/providers`
- `PUT` or `PATCH /internal/v1/configure/providers/{key}`
- `DELETE /internal/v1/configure/providers/{key}`
- `GET /internal/v1/configure/models`
- `POST /internal/v1/configure/favorite-models`
- `POST /internal/v1/configure/test`

## Applying changes

`POST /internal/v1/configure/apply` accepts a `changes` array. Each change includes:

- `section`
- optional `channel`
- `field`
- `op` such as `set`, `toggle`, or `choose`
- `value` when the operation needs one

Example:

```json
{
  "changes": [
    {
      "section": "provider",
      "field": "model",
      "op": "set",
      "value": "gpt-5"
    }
  ]
}
```

Successful apply responses currently include:

- `ok`
- `config_path`
- `live_reloaded`

Use the field-list routes to drive the UI first, then send only supported changes back to `apply`.

Notable **context** section fields for agent behavior:

| Field key | Config path | Meaning |
| --- | --- | --- |
| `context_task_card_enabled` | `context.taskCard.enabled` | Task card tracking |
| `context_task_card_enforce_plan` | `context.taskCard.enforcePlan` | Require `create_plan` before write/exec/web tools |

In or3-app, open **Settings → Advanced**, filter **Agent behavior** or **Advanced**, choose **Context**, and toggle **Require plan before writes**.
