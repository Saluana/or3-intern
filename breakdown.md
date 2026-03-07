## Simple Architecture Breakdown (Core Layers)

1. **Gateway** (The Central Hub / Control Plane)  
   This is the single main process you run (e.g., `openclaw gateway`).  
   - It listens for incoming messages from all your connected apps at once.  
   - It manages sessions (so conversations stay coherent across WhatsApp → Telegram → Discord).  
   - It handles routing: message comes in → Gateway wakes the right agent → agent thinks/acts → Gateway sends reply back through the original channel.  
   - It also runs background stuff like heartbeats and crons.

2. **Agent Runtime / Reasoning Loop** (The Brain in Action)  
   When there's input (your message, a scheduled cron, a webhook, or a heartbeat trigger):  
   - **Gather context** — Pulls from conversation history + persistent memory files + workspace files.  
   - **Call the LLM** — Sends the assembled prompt to your chosen model (Claude Opus, Sonnet, etc.).  
   - **Model decides** — Outputs normal text reply OR tool calls (e.g., "use browser to check site", "write file X", "run shell command").  
   - **Execute tools/skills** → Loop repeats if needed (classic ReAct/agent loop: observe → think → act → repeat).  
   - Finally streams the response back via Gateway.  
   This loop makes it feel "smart" and capable of multi-step tasks.

3. **Memory System** (What Makes It Feel Persistent)  
   Everything is file-based (super simple, no database hassle):  
   - Core files like `soul.md` (personality/vibe/boundaries), `identity.md` (who/what it is), `MEMORY.md` (short-term recall), `HEARTBEAT.md` (what to check periodically).  
   - Long-term: daily logs, project folders, thematic notes in a `memory/` dir.  
   - Agent reads these on startup/wakeup → "remembers" across restarts/sessions.  
   - Semantic search across files for pulling relevant old info without bloating context.

4. **Tools & Skills** (The Hands — What Lets It Act)  
   - Built-in tools: browser control, file ops, shell execution, voice on macOS/iOS, Canvas UI, etc.  
   - **Skills** — Community plugins (thousands in ClawHub marketplace). These are installable extensions (e.g., GitHub integration, semantic scraping, email summarizer, custom browser stealth).  
   - Agent decides which skill/tool to call based on descriptions (like function calling).  
   - Very extensible — you (or community) can write new ones in code.

5. **Proactivity / Autonomy Layer** (Why It Feels "Alive")  
   - **Heartbeat** — Agent wakes every X minutes/hours, reads `HEARTBEAT.md`, checks for pending work (new emails, calendar, mentions), acts if needed, then sleeps.  
   - **Cron jobs** — Precise scheduled tasks (e.g., "at 3 AM scrape report and notify me").  
   - **Multi-agent support** — Main agent can spawn sub-agents (specialized ones for research/coding/writing) that run in parallel/isolated sessions.  
   - Triggers: messages, schedules, webhooks, file changes → agent can start working without you prompting.

### Quick Summary Flow
- You message via WhatsApp: "Summarize my emails and draft replies."  
- Gateway receives → routes to agent session.  
- Agent assembles context (history + MEMORY.md + email skill).  
- LLM thinks → calls email tool + browser if needed → loops until done.  
- Gateway sends reply + any side effects (files written, calendar events added).  
- Later (heartbeat/cron): agent wakes independently → checks if anything new needs doing → messages you proactively.
