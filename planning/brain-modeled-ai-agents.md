# Brain-Modeled AI Agent Architecture

> Ideas for making AI agents more efficient and intelligent by modeling them after the human brain. Focused on reducing token burn, improving context management, and creating more natural cognitive behavior.

---

## 1. Predictive Processing: Only Pay for Surprises

The brain doesn't perceive the world directly. It builds a running prediction of what's coming next and only processes the *error* — the gap between prediction and reality. That's why you can drive home on autopilot and not remember the trip. Nothing surprised you.

An AI agent could do the same thing with context. Instead of rebuilding the full system prompt every turn, the runtime predicts which sections are relevant based on the current conversation trajectory. If we've been talking about lawn care pricing for six turns, the code review docs probably aren't relevant. Only inject them if the conversation *surprises* the prediction model — a topic shift, an unexpected question.

**Implementation sketch:**
- Run a lightweight classifier on the incoming message
- Map it to predicted context needs based on conversation trajectory
- Only expand the full prompt when there's a mismatch
- Most turns carry a skeleton prompt instead of the full cabinet

**Token savings:** Enormous. Most turns in a long conversation are continuation of the same topic.

---

## 2. Habituation: Stop Paying Attention to Your Shirt

Right now, every turn carries the full identity block, every pinned memory, every workspace context file. But the brain does something clever with constant stimuli: it stops registering them. You don't feel your clothes against your skin until someone mentions it. Sensory gating.

What if context sections had a "novelty score"? If the SOUL.md section hasn't been referenced or influenced the last N responses, its weight in the prompt shrinks. It's still *available* if something triggers it, but it stops consuming prime real estate every single turn. The first few turns carry full identity. By turn twenty, if you're deep in a spreadsheet, the identity block is compressed to two lines instead of fifty.

**How it differs from static trimming:** Dynamic fading based on relevance decay, with re-expansion when the context shifts.

---

## 3. Consolidation Sleep: The Agent Takes Naps

The brain does its best work during sleep. During REM, it replays the day's experiences, strengthens important patterns, prunes noise, and compresses raw experience into compact representations. You literally wake up smarter than when you went to bed.

An agent could have a scheduled "sleep cycle." After a session or at the end of a day, a background process replays the conversation history, extracts the signal, and writes denser memory notes. Not just summaries, but *compressed representations*:

> "Brendon is frustrated with token costs and wants efficiency improvements for or3-intern. He responds well to blunt technical analysis. His ADHD means long walls of context overwhelm him."

Then the raw conversation history gets archived or pruned. The next session wakes up with compact, high-signal memories instead of bloated history.

**Multiple consolidation passes** could run with different goals:
- One pass for emotional patterns
- One pass for technical facts
- One pass for upcoming tasks

Like how different sleep stages serve different consolidation functions.

---

## 4. Working Memory Slots: Keep Only What's Active

Working memory holds roughly four to seven items at once. Everything else lives in long-term memory and gets pulled in on demand. The brain doesn't try to hold everything in the front of consciousness simultaneously.

**The idea:** The agent's context operates on a slot system. Five to seven active slots represent what the conversation is currently "about." Each slot holds a compressed representation: a task, a person, a constraint, a tool state. Everything else sits in the memory index and gets retrieved only when a slot is activated by relevance.

**Behavior:**
- When a new topic comes up, something gets evicted from a slot
- When an old topic resurfaces, it gets reloaded
- The agent's "attention" is explicit rather than implicit

This would be radically more efficient than stuffing everything into a prompt and hoping attention mechanisms sort it out. You'd have an explicit, managed working memory that the runtime tracks and swaps.

---

## 5. Procedural Memory: Compile Frequent Tasks to Autopilot

When you first learned to drive, every action required conscious thought. Mirror, signal, shoulder check, steer. Now you do it without thinking. The brain compiled a deliberative process into a procedural one.

Agents could do the same. If you ask for a morning status update every day, after a few iterations the runtime should compile that into a compressed procedure: a fixed sequence of tool calls and template responses that runs with minimal context. No need to re-read SOUL.md, re-retrieve memories, or re-evaluate the full prompt. Just run the procedure.

**Implementation details:**
- Procedures stored separately from deliberative context
- Versioned so they can be rolled back
- Only decompiled back to full deliberation if something changes or the user asks something unexpected during execution

---

## 6. Spreading Activation: Associative Recall

The brain doesn't retrieve memories by keyword matching. When you think "dog," concepts connected to dog — bark, leash, the time you got bitten, your neighbor's golden retriever — light up in a spreading wave. The activation fades with distance.

**Applied to agents:** Build a lightweight graph where memories connect to concepts, people, tools, and emotions. Retrieving one memory "activates" its neighbors, which activate *their* neighbors, with a decay function. The most activated nodes get retrieved.

**Advantage:** More natural, associative recall. You'd remember relevant context that wouldn't show up in a cosine similarity search because it's connected through a chain of associations rather than direct semantic overlap.

---

## 7. Metacognitive Budget Awareness

The brain tracks its own fatigue. When you're tired, you take shortcuts, defer hard thinking, simplify decisions. The prefrontal cortex monitors its own resource levels and adjusts behavior accordingly.

**Applied to agents:** An explicit token budget for each session, tracked in real time. As the budget tightens, the agent automatically shifts to:
- Shorter responses
- Reduced context retrieval
- Deferred non-urgent tool calls
- Compressed reasoning

Not because the user asked, but because the agent *knows* it's running low and adapts.

**Key distinction:** This is graceful degradation, not a hard limit. Like how you make worse decisions at 2am but you still make decisions.

---

## 8. Dreams: Simulated Rehearsal

Beyond consolidation, the brain runs simulations during sleep. It rehearses future scenarios, tests emotional responses, finds connections between disparate memories. That's why you sometimes wake up with a solution to a problem you couldn't solve the night before.

**Applied to agents:** A "dream" cycle runs simulated interactions:
- "What might the user ask tomorrow based on their patterns?"
- "What context would I need?"
- Pre-load those memories, pre-compute likely tool calls, identify gaps in knowledge that should be filled

This turns reactive agents into proactive ones. Not in a creepy way, but in the way the brain quietly prepares you for tomorrow without you consciously asking it to.

---

## The Big Picture

The common thread: the human brain is ruthlessly efficient because it doesn't treat all information equally. It predicts, filters, fades, compresses, consolidates, and simulates. Most current agent architectures treat the context window like a whiteboard: write everything on it, erase when full.

These ideas move toward a genuine cognitive architecture. Not just "stuff less into the prompt," but fundamentally rethinking how context flows through the system based on what actually matters right now.

### Priority Implementation Order

1. **Consolidation Sleep** — Highest immediate impact. Background process that compresses conversation history into dense memory notes. Could be built into or3-intern's existing memory system.
2. **Working Memory Slots** — Radical efficiency gain. Explicit slot management with eviction and reload logic.
3. **Habituation** — Relatively simple to implement. Novelty scoring on context sections with decay over turns.
4. **Procedural Memory** — Natural extension of existing tool-calling patterns. Compile repeated sequences.
5. **Predictive Processing** — Requires a lightweight topic classifier but pays off in long conversations.
6. **Metacognitive Budget Awareness** — Useful for cost control. Track tokens and adapt behavior.
7. **Spreading Activation** — Enhances existing memory retrieval. Build association graph on top of current system.
8. **Dreams** — Most ambitious. Simulated rehearsal requires understanding user patterns deeply.
