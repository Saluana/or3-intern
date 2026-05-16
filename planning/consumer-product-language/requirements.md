# Consumer Product Language Requirements

## Introduction

OR3 should keep its current runtime contracts while presenting a simpler product model to normal users. The app and user-facing docs should talk about conversations and connected apps instead of raw session keys, channels, scopes, and routing details. Technical terms remain available in Advanced or Developer contexts for debugging and power users.

## Requirements

### Requirement 1: Hide raw runtime identity from normal users

**User Story:** As a normal OR3 user, I want chat history and scheduled task memory described as conversations, so that I do not need to understand session keys or runtime identity.

#### Acceptance Criteria

1. WHEN the app shows chat history, scheduled task memory, or recent conversation choices THEN it SHALL use "conversation" language instead of "session" language.
2. WHEN a normal app surface references a raw `session_key` value THEN it SHALL hide that value or replace it with a friendly label.
3. IF a raw session key is needed for debugging THEN it SHALL appear only inside Advanced or Developer-labeled UI.

### Requirement 2: Present channels as connected apps

**User Story:** As a normal OR3 user, I want Telegram, Slack, Discord, WhatsApp, and Email presented as connected apps, so that message delivery feels like app setup instead of routing configuration.

#### Acceptance Criteria

1. WHEN settings summarize messaging integrations THEN they SHALL use "Connected Apps" or "messaging apps" language.
2. WHEN a connected app detail page is open THEN it SHALL avoid calling the app a channel in visible title, subtitle, description, and empty-state copy.
3. IF a destination override is shown THEN it SHALL use "destination" language and reserve channel IDs for Advanced copy.

### Requirement 3: Keep agents and subagents terminology

**User Story:** As a technical or semi-technical OR3 user, I want agents and subagents to keep their standard names, so that OR3 stays aligned with common AI product language.

#### Acceptance Criteria

1. WHEN the app or docs describe delegated AI work THEN they MAY continue to use "agent" and "subagent".
2. WHEN simplifying copy THEN the implementation SHALL NOT rename agent concepts to assistant, helper, or people.

### Requirement 4: Preserve backend and API compatibility

**User Story:** As an OR3 maintainer, I want the cleanup to be presentation-only, so that existing APIs, configs, jobs, and stored data continue to work.

#### Acceptance Criteria

1. WHEN implementing the terminology cleanup THEN no API route, config key, database table, event payload, or TypeScript wire shape SHALL be renamed.
2. WHEN code still needs `session_key`, `channel`, or `scope` internally THEN those identifiers SHALL remain unchanged.
3. IF docs describe architecture or API contracts THEN they MAY keep exact technical names.

### Requirement 5: Update user-facing documentation

**User Story:** As a new OR3 user, I want the getting-started and user-guide docs to match the app language, so that setup teaches the same mental model the app uses.

#### Acceptance Criteria

1. WHEN README and getting-started docs describe messaging integrations THEN they SHALL prefer "connected apps" over "channels".
2. WHEN user guides describe normal chat history THEN they SHALL prefer "conversation" over "session".
3. IF a CLI page documents an advanced command such as `scope`, `configure`, or API integration THEN it MAY keep exact technical terminology.
