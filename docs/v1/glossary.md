# Glossary

Common terms used throughout OR3 Intern and these docs.

## Agent

The AI that processes your messages, uses tools, and responds. Each session has one agent.

## Channel

A way to talk to your agent. Channels include CLI, Telegram, Slack, Discord, WhatsApp, and Email. Each channel has its own configuration.

## Service Mode

Runs OR3 Intern as an HTTP API server (default port 9100). Used by the OR3 App and other HTTP clients.

## Serve Mode

Runs all configured channels and automation at once. This is the "always-on" mode.

## Tool

A capability the agent can use. Examples: Exec (run commands), ReadFile (read files), WebFetch (get web pages). Tools can be allowed or blocked by the safety system.

## Skill

A reusable set of instructions and tools. Skills teach the agent how to do specific things, like "deploy the app" or "run database migrations."

## MCP

Model Context Protocol. A way to connect external tools and data sources to the agent. MCP servers provide extra capabilities.

## Runner

The core loop that processes a conversation. The runner takes a user message, asks the AI provider for a response, executes any tool calls, and returns the result.

## Session

A conversation between you and the agent. Sessions store message history and context.

## Turn

One exchange in a conversation: your message plus the agent's response (which may include multiple tool calls).

## Job

A background task the agent runs. Jobs have a status (pending, running, completed, failed) and can be tracked.

## Subagent

A secondary agent that runs in the background. Useful for parallel tasks. The main agent can spawn subagents and collect their results.

## Memory Vector Index

A search index that stores past conversations as vectors. The agent can search this index to remember things from earlier sessions.

## FTS

Full-Text Search. A text-based search over stored messages and memory. Works alongside the vector index.

## Approval Broker

The safety system that decides if a tool call needs approval. It checks the tool, the context, and the safety profile. If approval is needed, it sends a request to a paired device.

## Pairing

A secure connection between OR3 Intern and a device (like your phone running the OR3 App). Pairing uses passkey authentication for approval requests.

## Audit Chain

A tamper-evident log of all tool calls and approval decisions. Each entry is cryptographically linked to the previous one.

## Config Profile

A named set of configuration settings. You can switch between profiles for different contexts (work, personal, etc.).

## Runtime Profile

The active configuration that the agent is currently using. Combines the config profile with runtime state.

## Safety Mode

Controls how the approval system works. Options include: relaxed (auto-approve), normal (ask for approval on sensitive tools), strict (require approval for everything).

## Cron

Scheduled tasks that run at specific times or intervals. Configured in the config file.

## Webhook

An HTTP endpoint that triggers the agent when called. Useful for integrating with other services.

## Heartbeat

A periodic signal that checks if the agent service is alive. Used for monitoring and auto-restart.
