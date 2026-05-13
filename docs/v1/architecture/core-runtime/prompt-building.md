# Prompt Building

The prompt builder (`internal/agent/prompt.go`) assembles the prompt that gets sent to the AI provider. It combines several parts into one message.

## What Goes Into the Prompt

1. **Base system instructions** — the agent's identity, personality, and rules
2. **Tool definitions** — descriptions of every tool the agent can use
3. **Memory context** — relevant past conversations and documents (from vector search)
4. **Session history** — recent messages in the current conversation
5. **User message** — the current input from the user

## How It Works

The prompt builder takes these parts and formats them for the provider. Different providers expect different formats (OpenAI chat format, Anthropic format, etc.). The builder knows how to format for each one.

## Order Matters

The system prompt comes first. It tells the agent who it is and what it can do. Tool definitions come next — the agent needs to know what tools are available. Memory context follows, so the agent remembers relevant information. Session history comes next, providing the conversation flow. The user message comes last.

## Dynamic Content

Some parts of the prompt change every turn:
- Memory context changes based on the current query
- Session history grows with each message
- User message is different each time

Other parts stay the same:
- Base system instructions
- Tool definitions (unless tools change)
