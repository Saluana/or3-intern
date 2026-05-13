# Request Lifecycle (Agent Turn)

When the agent receives a message, it processes it in a loop called a "turn." Here is what happens:

## Step 1: Parse Input

The incoming message is parsed. This extracts the text, any attachments, and session information. The system figures out who is talking and what conversation this belongs to.

## Step 2: Load Context

The context manager loads conversation history for this session. It searches memory for relevant past conversations and documents. It loads pinned context items and workspace context.

## Step 3: Build Prompt

The prompt builder combines the system prompt, context, tool definitions, and the user message into one prompt. This is what gets sent to the AI provider.

## Step 4: Call Provider

The prompt is sent to an OpenAI-compatible API. The provider processes the prompt and returns a response. This might be text, tool calls, or both.

## Step 5: Process Response

The response is parsed. If it contains text, that text is streamed to the user. If it contains tool calls, those go to the next step.

## Step 6: Tool Execution

Each tool call is validated and executed. The tool result is formatted and sent back to the agent.

## Step 7: Repeat

The tool results go back into the prompt. The agent gets another chance to respond. It can call more tools or produce a final text response.

## Step 8: Return

When the agent produces a final text response, it is returned to the user. The turn is complete.

## Loop Until Done

The tool call loop repeats until the agent decides it is done. Each iteration is one "turn" in the conversation.
