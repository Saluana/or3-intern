# Tool Call Loop

The tool call loop is how the agent uses tools. It runs inside each turn until the agent produces a final response.

## The Loop

1. **Agent decides** — the agent looks at the prompt and decides to call a tool (e.g., ReadFile)
2. **Validate** — the tool call is checked: are the arguments valid? Is the tool allowed?
3. **Approve** — if the tool needs approval, the user gets a prompt
4. **Execute** — the tool runs and produces a result
5. **Return result** — the result goes back into the prompt for the agent
6. **Continue** — the agent looks at the result and decides what to do next

## Loop Termination

The loop keeps going until the agent produces a text response. This means the agent might call many tools in one turn. Each tool call adds to the context, so the loop is limited by the token budget.

## Parallel Tool Calls

Some providers support parallel tool calls. The agent can call multiple tools at once. All results come back at the same time. This speeds up tasks that need independent information.

## Error Handling in the Loop

If a tool call fails, the error goes back to the agent. The agent can try again, try a different approach, or tell the user about the problem.
