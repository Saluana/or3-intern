# Running chat

## Interactive mode

Start a chat session in your terminal:

```bash
or3-intern chat
```

Type your messages and press Enter. The agent will respond. It can use tools, remember context, and run skills.

Type `/help` to see available commands during a session. Type `/exit` or press Ctrl+C to quit.

## One-shot mode

For scripts or quick questions, use one-shot mode:

```bash
or3-intern agent -m "What files are in the current directory?"
```

The agent runs your request and prints the result. This is useful for automation and piping.

## Multi-turn sessions

The interactive mode keeps the conversation going. The agent remembers what you talked about earlier in the session. It can also search its memory from older sessions.

## Tips

- Be specific about what you want
- Ask the agent to explain what it's doing
- Use the agent to run commands, read files, or research topics
- If the agent asks for approval, you'll be prompted to confirm

## Next step

Set up [service mode](running-service-mode.md) to connect the OR3 App.
