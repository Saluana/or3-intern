**let the agent query its own memory through narrow tools and services, not through arbitrary SQL.**

That fits the architecture you already have. The repo already says the system is built around SQLite persistence with hybrid retrieval, and history fetches are intentionally bounded with `LIMIT` rather than full scans.  The memory layer already has a `Retriever` that does hybrid vector + FTS search over session memory, with weighted scoring and top-K trimming.  

So the best answer is:

## Give it read access, but only through curated interfaces

That absolutely makes sense because the agent should be able to:

* search prior conversations
* search pinned memory
* search indexed docs
* fetch recent history
* fetch a specific artifact or document summary

That is basically the whole point of moving to SQLite in the first place. You already have sessions, messages, artifacts, pinned memory, and memory docs in structured tables. 

But I would expose this as tools like:

* `search_memory(query, scope, top_k)`
* `get_recent_messages(session, limit)`
* `get_pinned_memory(scope)`
* `search_docs(query, scope, top_k)`
* `read_artifact(id)`

Not:

* `sql_query("SELECT * FROM messages ...")`

## Do not give the model raw SQL access

I would avoid direct agent-generated SQL for four reasons.

### 1. It breaks your safety model

You just added capability tiers, quotas, and profile enforcement around tools. The registry enforces a tool guard before execution. 

If you give the model arbitrary SQLite update access, it can bypass the intended meaning of those controls by changing memory/state directly.

### 2. It can corrupt memory quality

If the model can freely update messages, memory rows, docs, or session metadata, it will eventually:

* rewrite history
* over-summarize things badly
* store speculative facts as truth
* duplicate junk
* poison retrieval quality

That is worse than forgetting.

### 3. It creates hidden state mutations

One of the nice things about your current design is that memory flow is legible:

* messages are stored
* retrieval reads them
* consolidation promotes selected material
* pinned memory is distinct from raw history

Raw SQL writes let the model silently alter the substrate in ways that are hard to reason about.

### 4. It adds a new prompt injection sink

If the model can run updates against SQLite, then any hostile content that reaches the model may be able to influence durable state. That is a dangerous path.

## What I would allow it to update

I would allow **very narrow writes**, but only through specific APIs with strong rules.

Good writable actions:

* `remember_fact(key, content, scope)`
  Writes to pinned memory only
* `save_note(title, content, scope)`
  Writes to a dedicated agent_notes table or note artifact
* `mark_memory_relevant(id)`
  Optional metadata, not content rewrite
* `queue_memory_candidate(text, source_msg_ids)`
  Adds a candidate for later consolidation/review
* `store_structured_summary(session, summary, source_range)`
  Writes to a summaries table, append-only

Bad writable actions:

* arbitrary `UPDATE messages`
* arbitrary `DELETE FROM messages`
* direct mutation of retrieved docs
* raw `INSERT/UPDATE/DELETE` access anywhere

## My recommendation for or3-intern specifically

### Best design

Split DB access into 3 layers:

**1. Immutable conversational record**

* `messages`
* `artifacts`
* audit-linked event records

Agent should not directly edit these.

**2. Curated mutable memory**

* `memory_pinned`
* summaries
* agent notes
* maybe task state / reminders

Agent can write here, but only through narrow tools.

**3. Internal indexes**

* FTS tables
* vector embeddings
* doc index bookkeeping

Agent should never touch these directly. Runtime maintains them.

That matches the architecture direction you already have. The retriever is already a proper service layer over the DB, and docs are already indexed through dedicated code paths rather than ad hoc model writes.  

## Practical rule

Use this rule:

**The model may ask for memory operations. The runtime performs validated memory operations.**

Not:

**The model owns the database.**

## A clean lightweight version

If you want to keep it very lightweight, I would add only 4 memory tools:

* `memory_search`
* `memory_recent`
* `memory_pin_upsert`
* `memory_note_append`

And maybe one more:

* `memory_summary_upsert`

With hard constraints:

* scope-limited
* top-k capped
* payload size capped
* append-only where possible
* no delete
* no raw SQL
* no direct writes to messages/history

## Bottom line

So:

* **Read/search access:** yes, definitely
* **Raw SQLite update access:** no
* **Narrow write access to curated memory tables:** yes, that is the sweet spot

That gives you the benefits of self-searching memory without turning the database into an unsafe, model-controlled state machine.

A good default principle is:

**history is append-only, memory is curated, indexes are internal.**
