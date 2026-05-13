# Runner Chat Endpoints

Runner chat lets the app talk to external AI CLIs. Think of it as a bridge to tools like OpenCode.

## What Is a Runner?

A runner is an external AI CLI program. OR3 Intern can launch and communicate with these programs. The runner chat endpoints manage this communication.

## Send Message to Runner

`POST /api/v1/runner/:runner_id/messages`

```json
{
  "message": "Can you review the code in src/?",
  "session_key": "sess_abc123"
}
```

## Get Runner Messages

`GET /api/v1/runner/:runner_id/messages`

Returns the conversation history with that runner.

## List Runners

`GET /api/v1/runner`

Lists available runners and their status.

## How It Works

OR3 Intern starts a runner process. It sends messages to the runner's stdin. It reads responses from the runner's stdout. This creates a two-way chat between the user and the external AI.

Runner conversations are stored separately from main chat history. The `runner_chat_store` in the database handles this.
