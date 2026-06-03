# Local Chat Workflow

This is the simplest way to use OR3 Intern. Open a terminal and start chatting.

## Step 1: Open Terminal

Open your terminal application.

## Step 2: Start Chat

```
or3-intern
```

Or explicitly:

```
or3-intern chat
```

## Step 3: Start Typing

Type your messages and press Enter. The agent responds with text and can use tools.

## What the Agent Can Do

- Read and write files
- Run code and commands
- Search the web
- Access your skills
- Use stored secrets
- Remember past conversations (same session)

## Example

```
You: What files are in my Downloads folder?
Agent: Let me check...
Here are the files in ~/Downloads:
- report.pdf
- photo.jpg
- notes.txt

You: Can you summarize report.pdf?
Agent: ...
```

## Good For

- Day-to-day file management
- Writing and reviewing code
- Quick research and web lookups
- Automating repetitive tasks
- Learning and experimentation

## Tips

- Use descriptive messages for better results
- Use `/commands` inside chat to see local commands
- Use `/new` when you want a fresh live thread without losing the broader session history
- Run `or3-intern status` to check what tools the agent can use
