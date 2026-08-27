// src/redaction.ts
var REDACTED = "[REDACTED]";
function secretKey(key) {
  return /^(?:authorization|proxy-authorization|cookie|set-cookie|password|passphrase|secret|api[_-]?key)$/i.test(key) || /(?:^|[_-])(?:password|passphrase|secret|token|api[_-]?key)$/i.test(key) || /(?:Password|Passphrase|Secret|Token|ApiKey)$/.test(key);
}
function replaceLiteral(value, secret) {
  if (!secret)
    return value;
  return value.split(secret).join(REDACTED);
}
function redactString(input, explicitSecrets) {
  let value = input.replace(/\bBearer\s+[^\s"',;]+/gi, `Bearer ${REDACTED}`).replace(/(["']?(?:authorization|api[_-]?key|[a-z0-9_-]*(?:password|passphrase|secret|token))["']?\s*[:=]\s*["']?)([^"',}\s&]+)/gi, `$1${REDACTED}`);
  value = value.replace(/([?&])([^=&#]+)=([^&#]*)/g, (match, prefix, rawKey) => {
    let key = rawKey;
    try {
      key = decodeURIComponent(rawKey);
    } catch {}
    return secretKey(key) ? `${prefix}${rawKey}=${REDACTED}` : match;
  });
  for (const secret of explicitSecrets) {
    value = replaceLiteral(value, secret);
  }
  return value;
}
function redactValue(value, explicitSecrets, seen) {
  if (typeof value === "string") {
    return redactString(value, explicitSecrets);
  }
  if (value === null || value === undefined || typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return value;
  }
  if (value instanceof Error) {
    return {
      name: value.name,
      message: redactString(value.message, explicitSecrets)
    };
  }
  if (Array.isArray(value)) {
    if (seen.has(value))
      return "[Circular]";
    seen.add(value);
    return value.map((item) => redactValue(item, explicitSecrets, seen));
  }
  if (typeof value === "object") {
    if (seen.has(value))
      return "[Circular]";
    seen.add(value);
    const output = {};
    for (const [key, item] of Object.entries(value)) {
      output[key] = secretKey(key) ? REDACTED : redactValue(item, explicitSecrets, seen);
    }
    return output;
  }
  return redactString(String(value), explicitSecrets);
}
function redactInternSecrets(value, explicitSecrets = []) {
  return redactValue(value, explicitSecrets.filter(Boolean), new WeakSet);
}
function safeInternStringify(value, explicitSecrets = []) {
  try {
    return JSON.stringify(redactInternSecrets(value, explicitSecrets));
  } catch {
    return '"[Unserializable]"';
  }
}

// src/errors.ts
function safeCause(cause, secrets) {
  if (cause === undefined)
    return;
  const message = cause instanceof Error ? cause.message : typeof cause === "string" ? cause : "Request failed";
  const safe = new Error(String(redactInternSecrets(message, secrets)));
  safe.name = cause instanceof Error ? cause.name : "Error";
  return safe;
}

class InternClientError extends Error {
  code;
  status;
  remoteCode;
  requestId;
  retryable;
  details;
  cause;
  constructor(code, message, options = {}) {
    const secrets = options.secrets ?? [];
    super(String(redactInternSecrets(message, secrets)));
    this.name = "InternClientError";
    this.code = code;
    this.status = options.status;
    this.remoteCode = options.remoteCode;
    this.requestId = options.requestId;
    this.retryable = options.retryable ?? false;
    this.details = options.details === undefined ? undefined : redactInternSecrets(options.details, secrets);
    this.cause = safeCause(options.cause, secrets);
  }
}

class InternUnavailableError extends InternClientError {
  capability;
  constructor(message, options = {}) {
    super("unavailable", message, {
      retryable: false,
      ...options
    });
    this.name = "InternUnavailableError";
    this.capability = options.capability;
  }
}
function internOk(value) {
  return { ok: true, value };
}
function internErr(error) {
  return { ok: false, error };
}
function asInternClientError(error) {
  if (error instanceof InternClientError)
    return error;
  return new InternClientError("protocol", "Unexpected client failure", {
    cause: error
  });
}
async function toInternResult(value) {
  try {
    return internOk(await (typeof value === "function" ? value() : value));
  } catch (error) {
    return internErr(asInternClientError(error));
  }
}
function requireInternCapability(value, capability) {
  if (value === undefined || value === null || value === false) {
    return internErr(new InternUnavailableError(`The selected host does not advertise ${capability}.`, { capability }));
  }
  return internOk(value);
}

// src/protocol.ts
class InternProtocolError extends Error {
  code = "protocol";
  path;
  constructor(path, message) {
    super(`${path}: ${message}`);
    this.name = "InternProtocolError";
    this.path = path;
  }
}
function record(value, path) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new InternProtocolError(path, "expected an object");
  }
  return value;
}
function stringAt(input, key, path) {
  const value = input[key];
  if (typeof value !== "string") {
    throw new InternProtocolError(`${path}.${key}`, "expected a string");
  }
  return value;
}
function numberAt(input, key, path) {
  const value = input[key];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new InternProtocolError(`${path}.${key}`, "expected a finite number");
  }
  return value;
}
function booleanAt(input, key, path) {
  const value = input[key];
  if (typeof value !== "boolean") {
    throw new InternProtocolError(`${path}.${key}`, "expected a boolean");
  }
  return value;
}
function optionalString(input, key, path) {
  const value = input[key];
  if (value === undefined)
    return;
  if (typeof value !== "string") {
    throw new InternProtocolError(`${path}.${key}`, "expected a string");
  }
  return value;
}
function optionalNumber(input, key, path) {
  const value = input[key];
  if (value === undefined)
    return;
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new InternProtocolError(`${path}.${key}`, "expected a finite number");
  }
  return value;
}
function optionalBoolean(input, key, path) {
  const value = input[key];
  if (value === undefined)
    return;
  if (typeof value !== "boolean") {
    throw new InternProtocolError(`${path}.${key}`, "expected a boolean");
  }
  return value;
}
function parseInternRecord(value, path = "response") {
  return { ...record(value, path) };
}
function parseInternHealth(value) {
  const input = record(value, "health");
  return {
    ...input,
    status: stringAt(input, "status", "health"),
    runtimeAvailable: booleanAt(input, "runtimeAvailable", "health"),
    jobRegistryAvailable: booleanAt(input, "jobRegistryAvailable", "health"),
    approvalBrokerAvailable: booleanAt(input, "approvalBrokerAvailable", "health"),
    processId: numberAt(input, "processId", "health"),
    startedAt: stringAt(input, "startedAt", "health")
  };
}
function parseInternReadiness(value) {
  const input = record(value, "readiness");
  const findings = input.findings;
  if (findings !== undefined && !Array.isArray(findings)) {
    throw new InternProtocolError("readiness.findings", "expected an array");
  }
  return {
    ...input,
    status: stringAt(input, "status", "readiness"),
    ready: booleanAt(input, "ready", "readiness"),
    summary: input.summary === undefined ? undefined : record(input.summary, "readiness.summary"),
    findings
  };
}
function parseInternCapabilities(value) {
  const input = record(value, "capabilities");
  const approvalInput = record(input.approvals, "capabilities.approvals");
  const approvals = {};
  for (const [key, item] of Object.entries(approvalInput)) {
    if (typeof item !== "string") {
      throw new InternProtocolError(`capabilities.approvals.${key}`, "expected a string");
    }
    approvals[key] = item;
  }
  return {
    ...input,
    runtimeProfile: stringAt(input, "runtimeProfile", "capabilities"),
    hosted: booleanAt(input, "hosted", "capabilities"),
    hostId: stringAt(input, "hostId", "capabilities"),
    approvalBroker: record(input.approvalBroker, "capabilities.approvalBroker"),
    approvals,
    execAvailable: booleanAt(input, "execAvailable", "capabilities"),
    sandboxEnabled: booleanAt(input, "sandboxEnabled", "capabilities"),
    sandboxRequired: booleanAt(input, "sandboxRequired", "capabilities"),
    networkPolicy: record(input.networkPolicy, "capabilities.networkPolicy")
  };
}
function parseInternRunnerList(value) {
  const input = record(value, "runnerList");
  if (!Array.isArray(input.runners)) {
    throw new InternProtocolError("runnerList.runners", "expected an array");
  }
  const runners = input.runners.map((item, index) => {
    const runner = record(item, `runnerList.runners[${index}]`);
    return {
      ...runner,
      id: stringAt(runner, "id", `runnerList.runners[${index}]`),
      display_name: stringAt(runner, "display_name", `runnerList.runners[${index}]`),
      status: stringAt(runner, "status", `runnerList.runners[${index}]`),
      auth_status: stringAt(runner, "auth_status", `runnerList.runners[${index}]`),
      supports: record(runner.supports, `runnerList.runners[${index}].supports`),
      chat_capabilities: runner.chat_capabilities === undefined ? undefined : record(runner.chat_capabilities, `runnerList.runners[${index}].chat_capabilities`)
    };
  });
  return {
    ...input,
    runners,
    default_runner: optionalString(input, "default_runner", "runnerList")
  };
}
function parseInternSession(value) {
  const input = record(value, "session");
  return {
    ...input,
    id: stringAt(input, "id", "session"),
    app_session_key: stringAt(input, "app_session_key", "session"),
    runner_id: stringAt(input, "runner_id", "session"),
    continuation_mode: stringAt(input, "continuation_mode", "session"),
    created_at: numberAt(input, "created_at", "session"),
    updated_at: numberAt(input, "updated_at", "session"),
    native_session_ref: optionalString(input, "native_session_ref", "session"),
    model: optionalString(input, "model", "session"),
    mode: optionalString(input, "mode", "session"),
    isolation: optionalString(input, "isolation", "session"),
    cwd: optionalString(input, "cwd", "session"),
    max_turns: optionalNumber(input, "max_turns", "session"),
    meta: input.meta
  };
}
function parseInternSessionList(value) {
  const input = record(value, "sessionList");
  if (!Array.isArray(input.sessions)) {
    throw new InternProtocolError("sessionList.sessions", "expected an array");
  }
  return {
    ...input,
    sessions: input.sessions.map(parseInternSession)
  };
}
function parseInternTurn(value) {
  const input = record(value, "turn");
  return {
    ...input,
    id: stringAt(input, "id", "turn"),
    session_id: stringAt(input, "session_id", "turn"),
    sequence: numberAt(input, "sequence", "turn"),
    status: stringAt(input, "status", "turn"),
    continuation_mode: stringAt(input, "continuation_mode", "turn"),
    requested_at: numberAt(input, "requested_at", "turn"),
    started_at: optionalNumber(input, "started_at", "turn"),
    completed_at: optionalNumber(input, "completed_at", "turn"),
    user_message: optionalString(input, "user_message", "turn"),
    final_text: optionalString(input, "final_text", "turn"),
    error: optionalString(input, "error", "turn"),
    runner_run_id: optionalString(input, "runner_run_id", "turn"),
    runner_job_id: optionalString(input, "runner_job_id", "turn"),
    user_message_id: optionalNumber(input, "user_message_id", "turn"),
    assistant_message_id: optionalNumber(input, "assistant_message_id", "turn"),
    model: optionalString(input, "model", "turn"),
    mode: optionalString(input, "mode", "turn"),
    isolation: optionalString(input, "isolation", "turn"),
    cwd: optionalString(input, "cwd", "turn")
  };
}
function parseInternEvent(value) {
  const input = record(value, "event");
  return {
    ...input,
    id: numberAt(input, "id", "event"),
    turn_id: stringAt(input, "turn_id", "event"),
    seq: numberAt(input, "seq", "event"),
    ts: numberAt(input, "ts", "event"),
    type: stringAt(input, "type", "event"),
    stream: optionalString(input, "stream", "event"),
    text: optionalString(input, "text", "event"),
    job_id: optionalString(input, "job_id", "event"),
    payload: input.payload
  };
}
function parseInternApprovalDecision(value) {
  const input = record(value, "approvalDecision");
  return {
    ...input,
    request_id: numberAt(input, "request_id", "approvalDecision"),
    status: optionalString(input, "status", "approvalDecision"),
    token: optionalString(input, "token", "approvalDecision"),
    allowlist_id: optionalNumber(input, "allowlist_id", "approvalDecision"),
    session_key: optionalString(input, "session_key", "approvalDecision")
  };
}
function parseInternTurnList(value) {
  const input = record(value, "turnList");
  if (!Array.isArray(input.turns)) {
    throw new InternProtocolError("turnList.turns", "expected an array");
  }
  return {
    ...input,
    turns: input.turns.map(parseInternTurn)
  };
}
function parseInternEventList(value) {
  const input = record(value, "eventList");
  if (!Array.isArray(input.events)) {
    throw new InternProtocolError("eventList.events", "expected an array");
  }
  return {
    ...input,
    events: input.events.map(parseInternEvent)
  };
}
function parseInternStartedTurn(value) {
  const input = record(value, "startedTurn");
  return {
    ...input,
    session_id: stringAt(input, "session_id", "startedTurn"),
    turn_id: stringAt(input, "turn_id", "startedTurn"),
    job_id: stringAt(input, "job_id", "startedTurn"),
    status: stringAt(input, "status", "startedTurn")
  };
}
function parseInternActionAcknowledgement(value) {
  const input = record(value, "action");
  return {
    ...input,
    status: stringAt(input, "status", "action")
  };
}
function parseInternTurnDecision(value) {
  const input = record(value, "turnDecision");
  return {
    ...input,
    status: stringAt(input, "status", "turnDecision"),
    decision: stringAt(input, "decision", "turnDecision"),
    route: optionalString(input, "route", "turnDecision"),
    approval_id: optionalNumber(input, "approval_id", "turnDecision"),
    native_continued: optionalBoolean(input, "native_continued", "turnDecision"),
    fallback_to_token: optionalBoolean(input, "fallback_to_token", "turnDecision"),
    allowlist_session: optionalBoolean(input, "allowlist_session", "turnDecision"),
    allowlist_id: optionalNumber(input, "allowlist_id", "turnDecision"),
    token: optionalString(input, "token", "turnDecision")
  };
}
function parseInternArtifact(value) {
  const input = record(value, "artifact");
  return {
    ...input,
    id: stringAt(input, "id", "artifact"),
    mime: stringAt(input, "mime", "artifact"),
    size_bytes: numberAt(input, "size_bytes", "artifact"),
    offset: numberAt(input, "offset", "artifact"),
    read_bytes: numberAt(input, "read_bytes", "artifact"),
    truncated: booleanAt(input, "truncated", "artifact"),
    content: stringAt(input, "content", "artifact")
  };
}
function parseInternApproval(value, index) {
  const path = `approvalList.items[${index}]`;
  const input = record(value, path);
  const id = input.id;
  if (typeof id !== "string" && typeof id !== "number" || typeof id === "number" && !Number.isFinite(id)) {
    throw new InternProtocolError(`${path}.id`, "expected an ID");
  }
  return {
    ...input,
    id,
    type: stringAt(input, "type", path),
    status: stringAt(input, "status", path),
    requested_at: numberAt(input, "requested_at", path),
    expires_at: optionalNumber(input, "expires_at", path),
    resolved_at: optionalNumber(input, "resolved_at", path),
    preview: optionalString(input, "preview", path)
  };
}
function parseInternApprovalList(value) {
  const input = record(value, "approvalList");
  if (!Array.isArray(input.items)) {
    throw new InternProtocolError("approvalList.items", "expected an array");
  }
  return {
    ...input,
    items: input.items.map(parseInternApproval)
  };
}
function parseInternPairResult(value) {
  const input = record(value, "pairResult");
  return {
    ...input,
    certificate: record(input.certificate, "pairResult.certificate"),
    certificate_hash: stringAt(input, "certificate_hash", "pairResult"),
    device: record(input.device, "pairResult.device")
  };
}

// src/sse.ts
function parseJson(data) {
  if (!data)
    return;
  try {
    return JSON.parse(data);
  } catch {
    return;
  }
}
function parseInternSseBlock(block) {
  const data = [];
  const output = { data: "" };
  for (const line of block.split(/\r\n|\r|\n/)) {
    if (!line || line.startsWith(":"))
      continue;
    const separator = line.indexOf(":");
    const field = separator < 0 ? line : line.slice(0, separator);
    const rawValue = separator < 0 ? "" : line.slice(separator + 1);
    const value = rawValue.startsWith(" ") ? rawValue.slice(1) : rawValue;
    switch (field) {
      case "event":
        output.event = value;
        break;
      case "id":
        if (!value.includes("\x00"))
          output.id = value;
        break;
      case "retry": {
        const retry = Number(value);
        if (Number.isInteger(retry) && retry >= 0) {
          output.retry = retry;
        }
        break;
      }
      case "data":
        data.push(value);
        break;
    }
  }
  output.data = data.join(`
`);
  output.json = parseJson(output.data);
  if (output.id)
    output.cursor = output.id;
  return output;
}
function splitSseBuffer(buffer) {
  const blocks = [];
  let start = 0;
  const delimiter = /\r\n\r\n|\n\n|\r\r/g;
  for (let match = delimiter.exec(buffer);match; match = delimiter.exec(buffer)) {
    blocks.push(buffer.slice(start, match.index));
    start = match.index + match[0].length;
  }
  return { blocks, remainder: buffer.slice(start) };
}
async function* readInternSseStream(stream, signal) {
  const reader = stream.getReader();
  const decoder = new TextDecoder;
  let buffer = "";
  const abort = () => {
    reader.cancel().catch(() => {
      return;
    });
  };
  signal?.addEventListener("abort", abort, { once: true });
  try {
    while (true) {
      if (signal?.aborted) {
        throw signal.reason ?? new DOMException("Aborted", "AbortError");
      }
      const { done, value } = await reader.read();
      if (done)
        break;
      buffer += decoder.decode(value, { stream: true });
      const { blocks, remainder } = splitSseBuffer(buffer);
      buffer = remainder;
      for (const block of blocks) {
        if (block.trim() && !block.trimStart().startsWith(":")) {
          yield parseInternSseBlock(block);
        }
      }
    }
    buffer += decoder.decode();
    if (buffer.trim() && !buffer.trimStart().startsWith(":")) {
      yield parseInternSseBlock(buffer);
    }
  } finally {
    signal?.removeEventListener("abort", abort);
    reader.releaseLock();
  }
}
function internSseEventKey(event) {
  if (event.id)
    return `sse:${event.id}`;
  const payload = event.json && typeof event.json === "object" && !Array.isArray(event.json) ? event.json : undefined;
  if (!payload)
    return;
  if ((typeof payload.id === "string" || typeof payload.id === "number") && String(payload.id) !== "") {
    return `payload:${String(payload.id)}`;
  }
  if (typeof payload.turn_id === "string" && (typeof payload.seq === "string" || typeof payload.seq === "number")) {
    return `turn:${payload.turn_id}:${String(payload.seq)}`;
  }
  return;
}
function internSseEventCursor(event) {
  if (event.id)
    return event.id;
  const payload = event.json && typeof event.json === "object" && !Array.isArray(event.json) ? event.json : undefined;
  if (payload && (typeof payload.seq === "string" || typeof payload.seq === "number")) {
    return String(payload.seq);
  }
  return;
}

// src/transport.ts
function sensitiveQueryKey(key) {
  return /^(?:authorization|password|passphrase|secret|api[_-]?key)$/i.test(key) || /(?:^|[_-])(?:password|passphrase|secret|token|api[_-]?key)$/i.test(key) || /(?:Password|Passphrase|Secret|Token|ApiKey)$/.test(key);
}
var DEFAULT_REQUEST_TIMEOUT_MS = 15000;
var DEFAULT_STREAM_CONNECT_TIMEOUT_MS = 15000;
var DEFAULT_STREAM_INACTIVITY_TIMEOUT_MS = 60000;
function defaultSleep(milliseconds, signal) {
  if (milliseconds <= 0)
    return Promise.resolve();
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = globalThis.setTimeout(done, milliseconds);
    function done() {
      signal?.removeEventListener("abort", aborted);
      resolve();
    }
    function aborted() {
      globalThis.clearTimeout(timer);
      signal?.removeEventListener("abort", aborted);
      reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
    }
    signal?.addEventListener("abort", aborted, { once: true });
  });
}
function createClock(input) {
  return {
    now: input?.now ?? (() => Date.now()),
    random: input?.random ?? (() => Math.random()),
    sleep: input?.sleep ?? defaultSleep,
    setTimeout: input?.setTimeout ?? ((callback, milliseconds) => globalThis.setTimeout(callback, milliseconds)),
    clearTimeout: input?.clearTimeout ?? ((handle) => globalThis.clearTimeout(handle))
  };
}
function normalizeBaseUrl(value) {
  let url;
  try {
    url = new URL(value.trim());
  } catch {
    throw new InternClientError("validation_failed", "The selected host URL is invalid.");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new InternClientError("validation_failed", "The selected host must use HTTP or HTTPS.");
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new InternClientError("validation_failed", "Host credentials and query parameters must not appear in the URL.");
  }
  return url.toString().replace(/\/+$/, "");
}
function validatePath(path) {
  const trimmed = path.trim();
  if (!trimmed || /^[a-z][a-z\d+.-]*:/i.test(trimmed) || trimmed.startsWith("//")) {
    throw new InternClientError("validation_failed", "Service requests require a relative path.");
  }
  const normalized = trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
  let url;
  try {
    url = new URL(normalized, "https://or3.invalid");
  } catch {
    throw new InternClientError("validation_failed", "The service request path is invalid.");
  }
  if (url.hash) {
    throw new InternClientError("validation_failed", "Service request paths must not contain fragments.");
  }
  for (const key of url.searchParams.keys()) {
    if (sensitiveQueryKey(key)) {
      throw new InternClientError("validation_failed", "Credentials and secrets must be sent in headers or request bodies, never query parameters.");
    }
  }
  return `${url.pathname}${url.search}`;
}
function appendCursor(path, queryParameter, cursor) {
  const validated = validatePath(path);
  if (cursor === undefined || cursor === "")
    return validated;
  if (!/^[A-Za-z][A-Za-z0-9_]*$/.test(queryParameter)) {
    throw new InternClientError("validation_failed", "The stream cursor parameter is invalid.");
  }
  const url = new URL(validated, "https://or3.invalid");
  url.searchParams.set(queryParameter, cursor);
  return `${url.pathname}${url.search}`;
}
function isAbortError(error) {
  return error instanceof DOMException && error.name === "AbortError" || error instanceof Error && error.name === "AbortError";
}
function isRawBody(value) {
  if (typeof value === "string")
    return true;
  if (value instanceof ArrayBuffer || ArrayBuffer.isView(value))
    return true;
  if (typeof Blob !== "undefined" && value instanceof Blob)
    return true;
  if (typeof FormData !== "undefined" && value instanceof FormData)
    return true;
  if (typeof URLSearchParams !== "undefined" && value instanceof URLSearchParams) {
    return true;
  }
  if (typeof ReadableStream !== "undefined" && value instanceof ReadableStream) {
    return true;
  }
  return false;
}
function payloadMessage(payload, fallback) {
  if (payload && typeof payload === "object" && !Array.isArray(payload)) {
    const record2 = payload;
    for (const key of ["message", "error", "detail"]) {
      if (typeof record2[key] === "string" && record2[key].trim()) {
        return record2[key].trim();
      }
    }
  }
  if (typeof payload === "string" && payload.trim())
    return payload.trim();
  return fallback;
}
function payloadString(payload, key) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return;
  }
  const value = payload[key];
  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }
  return;
}
function errorCodeForResponse(status, remoteCode) {
  const code = remoteCode?.toLowerCase() ?? "";
  if (status === 503 || code === "capability_unavailable" || code === "runner_disabled" || code === "runner_missing" || code === "runner_auth_missing") {
    return "unavailable";
  }
  if (status === 401)
    return "unauthorized";
  if (status === 403)
    return "forbidden";
  if (status === 404)
    return "not_found";
  if (status === 408 || status === 504 || code === "timeout")
    return "timeout";
  if (status === 409)
    return "conflict";
  if (status === 400 || status === 422)
    return "validation_failed";
  return "http";
}
function makeResponseError(response, payload, secrets) {
  const remoteCode = payloadString(payload, "code");
  const requestId = response.headers.get("X-Request-Id") ?? payloadString(payload, "request_id");
  const code = errorCodeForResponse(response.status, remoteCode);
  const options = {
    status: response.status,
    remoteCode,
    requestId,
    retryable: code === "timeout" || code === "offline" || response.status === 429 || response.status >= 500,
    details: {
      status: response.status,
      payload,
      requestId
    },
    secrets
  };
  const message = payloadMessage(payload, `Request failed with status ${response.status}.`);
  return code === "unavailable" ? new InternUnavailableError(message, options) : new InternClientError(code, message, options);
}
async function readResponsePayload(response) {
  const text = await response.text().catch(() => "");
  if (!text)
    return;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
function statusAccepted(response, accepted) {
  if (response.ok)
    return true;
  if (Array.isArray(accepted))
    return accepted.includes(response.status);
  return typeof accepted === "function" ? accepted(response.status, response) : false;
}
function createAbortScope(externalSignal, timeoutMs, clock) {
  const controller = new AbortController;
  let timeoutHandle;
  let didTimeout = false;
  const externalAbort = () => {
    controller.abort(externalSignal?.reason ?? new DOMException("Aborted", "AbortError"));
  };
  if (externalSignal?.aborted) {
    externalAbort();
  } else {
    externalSignal?.addEventListener("abort", externalAbort, {
      once: true
    });
  }
  const resetTimeout = (milliseconds) => {
    clearTimeout();
    if (Number.isFinite(milliseconds) && milliseconds > 0) {
      timeoutHandle = clock.setTimeout(() => {
        didTimeout = true;
        controller.abort(new DOMException("Timed out", "TimeoutError"));
      }, milliseconds);
    }
  };
  const clearTimeout = () => {
    if (timeoutHandle !== undefined) {
      clock.clearTimeout(timeoutHandle);
      timeoutHandle = undefined;
    }
  };
  resetTimeout(timeoutMs);
  return {
    signal: controller.signal,
    timedOut: () => didTimeout,
    clearTimeout,
    resetTimeout,
    dispose() {
      clearTimeout();
      externalSignal?.removeEventListener("abort", externalAbort);
    }
  };
}
function monitorStreamActivity(stream, onActivity) {
  const reader = stream.getReader();
  return new ReadableStream({
    async pull(controller) {
      const { done, value } = await reader.read();
      if (done) {
        controller.close();
        return;
      }
      onActivity();
      controller.enqueue(value);
    },
    async cancel(reason) {
      await reader.cancel(reason);
    }
  });
}
function requestFailure(error, scope, externalSignal, secrets) {
  if (error instanceof InternClientError)
    return error;
  if (scope.timedOut()) {
    return new InternClientError("timeout", "The request timed out.", {
      retryable: true,
      cause: error,
      secrets
    });
  }
  if (externalSignal?.aborted || isAbortError(error)) {
    return new InternClientError("aborted", "The request was stopped.", {
      cause: error,
      secrets
    });
  }
  return new InternClientError("offline", "Could not reach the selected host.", {
    retryable: true,
    cause: error,
    secrets
  });
}
function reconnectConfig(input) {
  if (!input)
    return null;
  const options = input === true ? {} : input;
  const finite = (value, fallback) => Number.isFinite(value) ? value : fallback;
  return {
    maxAttempts: Math.min(100, Math.max(0, Math.floor(finite(options.maxAttempts, 5)))),
    minDelayMs: Math.min(60000, Math.max(0, finite(options.minDelayMs, 250))),
    maxDelayMs: Math.min(60000, Math.max(0, finite(options.maxDelayMs, 1e4))),
    factor: Math.min(10, Math.max(1, finite(options.factor, 2))),
    jitter: Math.min(1, Math.max(0, finite(options.jitter, 0.2)))
  };
}
function reconnectDelay(attempt, config, random, serverRetry) {
  const exponential = serverRetry ?? Math.min(config.maxDelayMs, config.minDelayMs * config.factor ** Math.max(0, attempt - 1));
  const bounded = Math.min(config.maxDelayMs, Math.max(0, exponential));
  const jittered = bounded * (1 - config.jitter + config.jitter * 2 * Math.min(1, Math.max(0, random)));
  return Math.round(jittered);
}
function shouldReconnect(error) {
  return error.retryable || error.code === "offline" || error.code === "timeout" || error.code === "http" && (error.status ?? 0) >= 500;
}

class BoundedEventKeys {
  limit;
  values = new Set;
  order = [];
  constructor(limit) {
    this.limit = limit;
  }
  hasOrAdd(key) {
    if (this.values.has(key))
      return true;
    this.values.add(key);
    this.order.push(key);
    while (this.order.length > this.limit) {
      const oldest = this.order.shift();
      if (oldest !== undefined)
        this.values.delete(oldest);
    }
    return false;
  }
}
function createInternTransport(options) {
  const fetchImpl = options.fetch ?? globalThis.fetch;
  if (typeof fetchImpl !== "function") {
    throw new InternUnavailableError("No Fetch-compatible transport is available.", { capability: "fetch" });
  }
  const clock = createClock(options.clock);
  const configuredTimeout = options.defaultTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  const configuredStreamTimeout = options.streamConnectTimeoutMs ?? DEFAULT_STREAM_CONNECT_TIMEOUT_MS;
  const configuredStreamInactivityTimeout = options.streamInactivityTimeoutMs ?? DEFAULT_STREAM_INACTIVITY_TIMEOUT_MS;
  const currentBaseUrl = (explicit) => normalizeBaseUrl(explicit ?? (typeof options.baseUrl === "function" ? options.baseUrl() : options.baseUrl));
  const buildUrl = (path, explicitBaseUrl) => `${currentBaseUrl(explicitBaseUrl)}${validatePath(path)}`;
  async function resolvedHeaders(method, path, requestOptions, accept) {
    const requireAuth = requestOptions.requireAuth !== false;
    const baseUrl = currentBaseUrl(requestOptions.baseUrl);
    const headers = new Headers(requestOptions.headers);
    if (!headers.has("Accept"))
      headers.set("Accept", accept);
    const auth = requireAuth ? await options.resolveAuth?.({
      method,
      path,
      baseUrl,
      requireAuth
    }) : undefined;
    const authHeaders = new Headers(auth?.headers);
    authHeaders.forEach((value, key) => headers.set(key, value));
    const token = auth?.token?.trim();
    if (token) {
      headers.set("Authorization", `${auth?.scheme?.trim() || "Bearer"} ${token}`);
    }
    if (requireAuth && options.resolveAuth && !headers.has("Authorization")) {
      throw new InternClientError("unauthorized", "No credential is available for the selected host.", { status: 401 });
    }
    const secrets = [];
    if (token)
      secrets.push(token);
    for (const [key, value] of headers.entries()) {
      if (/authorization|cookie|secret|password|token|api[_-]?key/i.test(key)) {
        secrets.push(value, value.replace(/^\S+\s+/, ""));
      }
    }
    return { headers, secrets: secrets.filter(Boolean) };
  }
  async function request(path, requestOptions = {}) {
    const method = requestOptions.method ?? (requestOptions.body === undefined ? "GET" : "POST");
    const validatedPath = validatePath(path);
    const { headers, secrets } = await resolvedHeaders(method, validatedPath, requestOptions, "application/json");
    let body;
    if (requestOptions.body !== undefined) {
      if (isRawBody(requestOptions.body)) {
        body = requestOptions.body;
      } else {
        body = JSON.stringify(requestOptions.body);
        if (!headers.has("Content-Type")) {
          headers.set("Content-Type", "application/json");
        }
      }
    }
    const scope = createAbortScope(requestOptions.signal, requestOptions.timeoutMs ?? configuredTimeout, clock);
    try {
      const response = await fetchImpl(buildUrl(validatedPath, requestOptions.baseUrl), {
        method,
        headers,
        body,
        cache: "no-store",
        signal: scope.signal
      });
      await requestOptions.onResponse?.({
        method,
        path: validatedPath,
        response
      });
      if (!statusAccepted(response, requestOptions.acceptedStatuses)) {
        const payload = await readResponsePayload(response);
        throw makeResponseError(response, payload, secrets);
      }
      const responseType = requestOptions.responseType ?? "json";
      let value;
      if (responseType === "void" || response.status === 204) {
        value = undefined;
      } else if (responseType === "text") {
        value = await response.text();
      } else {
        try {
          value = await response.json();
        } catch (error) {
          throw new InternClientError("protocol", "The host returned invalid JSON.", {
            status: response.status,
            cause: error,
            secrets
          });
        }
      }
      if (!requestOptions.parse)
        return value;
      try {
        return requestOptions.parse(value, response);
      } catch (error) {
        if (error instanceof InternClientError)
          throw error;
        throw new InternClientError("protocol", "The host returned an invalid response.", {
          status: response.status,
          details: redactInternSecrets(value, secrets),
          cause: error,
          secrets
        });
      }
    } catch (error) {
      throw requestFailure(error, scope, requestOptions.signal, secrets);
    } finally {
      scope.dispose();
    }
  }
  async function* stream(path, streamOptions = {}) {
    const method = streamOptions.method ?? "GET";
    const reconnect = reconnectConfig(streamOptions.reconnect);
    const resume = streamOptions.resume;
    const queryParameter = resume?.queryParameter ?? "cursor";
    const dedupeInput = streamOptions.dedupe;
    const dedupeOptions = dedupeInput === true ? {} : dedupeInput && typeof dedupeInput === "object" ? dedupeInput : null;
    const eventKeys = dedupeOptions ? new BoundedEventKeys(Math.max(1, Math.floor(dedupeOptions.maxEntries ?? 2048))) : null;
    let cursor = resume?.initialCursor === undefined ? undefined : String(resume.initialCursor);
    let attempt = 0;
    let serverRetry;
    while (true) {
      const attemptPath = appendCursor(path, queryParameter, cursor);
      const attemptHeaders = new Headers(streamOptions.headers);
      if (cursor && resume?.sendLastEventId) {
        attemptHeaders.set("Last-Event-ID", cursor);
      }
      const { headers, secrets } = await resolvedHeaders(method, attemptPath, { ...streamOptions, headers: attemptHeaders }, "text/event-stream");
      let body;
      if (streamOptions.body !== undefined) {
        if (isRawBody(streamOptions.body)) {
          body = streamOptions.body;
        } else {
          body = JSON.stringify(streamOptions.body);
          if (!headers.has("Content-Type")) {
            headers.set("Content-Type", "application/json");
          }
        }
      }
      const scope = createAbortScope(streamOptions.signal, streamOptions.timeoutMs ?? configuredStreamTimeout, clock);
      let failure;
      let ended = false;
      try {
        const response = await fetchImpl(buildUrl(attemptPath, streamOptions.baseUrl), {
          method,
          headers,
          body,
          cache: "no-store",
          signal: scope.signal
        });
        const inactivityTimeout = streamOptions.inactivityTimeoutMs ?? configuredStreamInactivityTimeout;
        scope.resetTimeout(inactivityTimeout);
        await streamOptions.onResponse?.({
          method,
          path: attemptPath,
          response
        });
        if (!statusAccepted(response, streamOptions.acceptedStatuses)) {
          const payload = await readResponsePayload(response);
          throw makeResponseError(response, payload, secrets);
        }
        if (!response.body) {
          throw new InternClientError("protocol", "The host did not return an event stream.", { status: response.status, secrets });
        }
        for await (const event of readInternSseStream(monitorStreamActivity(response.body, () => scope.resetTimeout(inactivityTimeout)), scope.signal)) {
          if (event.retry !== undefined) {
            serverRetry = event.retry;
          }
          const nextCursor = resume?.cursorFromEvent?.(event) ?? internSseEventCursor(event);
          if (nextCursor !== undefined) {
            cursor = String(nextCursor);
            event.cursor = cursor;
          }
          const key = dedupeOptions?.key?.(event) ?? internSseEventKey(event);
          if (key && eventKeys?.hasOrAdd(key))
            continue;
          yield event;
          if (streamOptions.isTerminal?.(event))
            return;
        }
        if (scope.signal.aborted) {
          throw scope.signal.reason ?? new DOMException("Aborted", "AbortError");
        }
        ended = true;
      } catch (error) {
        failure = requestFailure(error, scope, streamOptions.signal, secrets);
      } finally {
        scope.dispose();
      }
      if (streamOptions.signal?.aborted) {
        throw failure ?? new InternClientError("aborted", "The stream was stopped.");
      }
      if (!reconnect) {
        if (failure)
          throw failure;
        return;
      }
      const reconnectFailure = failure ?? new InternClientError("offline", ended ? "The event stream ended before a terminal event." : "The event stream disconnected.", { retryable: true });
      if (!shouldReconnect(reconnectFailure) || attempt >= reconnect.maxAttempts) {
        throw reconnectFailure;
      }
      attempt += 1;
      const delay = reconnectDelay(attempt, reconnect, clock.random(), serverRetry);
      try {
        await clock.sleep(delay, streamOptions.signal);
      } catch (error) {
        throw new InternClientError("aborted", "The stream was stopped.", { cause: error, secrets });
      }
    }
  }
  return { buildUrl, request, stream };
}

// src/client.ts
function pathId(value, label) {
  const normalized = String(value).trim();
  if (!normalized) {
    throw new InternClientError("validation_failed", `${label} is required.`);
  }
  return encodeURIComponent(normalized);
}
function queryPath(path, values) {
  const query = new URLSearchParams;
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== "") {
      query.set(key, String(value));
    }
  }
  const encoded = query.toString();
  return encoded ? `${path}?${encoded}` : path;
}
function positiveInteger(value, label) {
  if (value === undefined)
    return;
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new InternClientError("validation_failed", `${label} must be a positive integer.`);
  }
  return value;
}
function boundedPositiveInteger(value, label, maximum) {
  const normalized = positiveInteger(value, label);
  if (normalized !== undefined && normalized > maximum) {
    throw new InternClientError("validation_failed", `${label} must not exceed ${maximum}.`);
  }
  return normalized;
}
function appSessionKeyPrefix(value) {
  if (value === undefined)
    return;
  const normalized = value.trim();
  if (!normalized || new TextEncoder().encode(normalized).byteLength > 256 || /[\0\r\n]/.test(normalized)) {
    throw new InternClientError("validation_failed", "App session key prefix is invalid.");
  }
  return normalized;
}
function nonNegativeInteger(value, label) {
  if (value === undefined)
    return;
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new InternClientError("validation_failed", `${label} must be a non-negative integer.`);
  }
  return value;
}
function sessionPath(sessionId) {
  return `/internal/v1/runner-chat/sessions/${pathId(sessionId, "Session ID")}`;
}
function turnPath(sessionId, turnId) {
  return `${sessionPath(sessionId)}/turns/${pathId(turnId, "Turn ID")}`;
}
function parseTurnStreamEvent(event) {
  if (event.json === undefined) {
    return event;
  }
  let json;
  if (typeof event.json.turn_id === "string" && typeof event.json.seq === "number") {
    json = parseInternEvent(event.json);
  } else {
    json = parseInternRecord(event.json, "streamEvent");
  }
  return { ...event, json };
}
function findInternRunner(list, runnerId) {
  const normalized = runnerId.trim();
  const runner = list.runners.find((item) => item.id === normalized);
  if (!runner) {
    throw new InternUnavailableError(`Runner ${normalized || "(missing)"} is not advertised by the selected host.`, { capability: `runner:${normalized || "unknown"}` });
  }
  return runner;
}
function requireInternRunner(list, runnerId) {
  const runner = findInternRunner(list, runnerId);
  if (runner.status !== "available") {
    throw new InternUnavailableError(`Runner ${runner.id} is advertised but is not available (${runner.status || "unknown status"}).`, {
      capability: `runner:${runner.id}`,
      details: {
        runnerId: runner.id,
        status: runner.status,
        authStatus: runner.auth_status
      }
    });
  }
  return runner;
}
function createInternClient(options) {
  const transport = createInternTransport(options);
  const health = (callOptions = {}) => transport.request("/internal/v1/health", {
    ...callOptions,
    method: "GET",
    parse: parseInternHealth
  });
  const readiness = (callOptions = {}) => transport.request("/internal/v1/readiness", {
    ...callOptions,
    method: "GET",
    acceptedStatuses: [503],
    parse: parseInternReadiness
  });
  const capabilities = (input = {}, callOptions = {}) => transport.request(queryPath("/internal/v1/capabilities", {
    channel: input.channel,
    trigger: input.trigger
  }), {
    ...callOptions,
    method: "GET",
    parse: parseInternCapabilities
  });
  const appBootstrap = (callOptions = {}) => transport.request("/internal/v1/app/bootstrap", {
    ...callOptions,
    method: "GET",
    parse: (value) => parseInternRecord(value, "appBootstrap")
  });
  const listRunners = (callOptions = {}) => transport.request("/internal/v1/chat-runners", {
    ...callOptions,
    method: "GET",
    parse: parseInternRunnerList
  });
  const requireRunner = async (runnerId, callOptions = {}) => requireInternRunner(await listRunners(callOptions), runnerId);
  const listSessions = async (input = {}, callOptions = {}) => transport.request(queryPath("/internal/v1/runner-chat/sessions", {
    app_session_key_prefix: appSessionKeyPrefix(input.appSessionKeyPrefix),
    limit: boundedPositiveInteger(input.limit, "Session limit", 100)
  }), {
    ...callOptions,
    method: "GET",
    parse: parseInternSessionList
  });
  const createSession = (input, callOptions = {}) => transport.request("/internal/v1/runner-chat/sessions", {
    ...callOptions,
    method: "POST",
    body: input,
    parse: parseInternSession
  });
  const getSession = async (sessionId, callOptions = {}) => transport.request(sessionPath(sessionId), {
    ...callOptions,
    method: "GET",
    parse: parseInternSession
  });
  const listTurns = async (sessionId, input = {}, callOptions = {}) => transport.request(queryPath(`${sessionPath(sessionId)}/turns`, {
    limit: boundedPositiveInteger(input.limit, "Turn limit", 100)
  }), {
    ...callOptions,
    method: "GET",
    parse: parseInternTurnList
  });
  const startTurn = async (sessionId, input, callOptions = {}) => transport.request(`${sessionPath(sessionId)}/turns`, {
    ...callOptions,
    method: "POST",
    body: input,
    parse: parseInternStartedTurn
  });
  const getTurn = async (sessionId, turnId, callOptions = {}) => transport.request(turnPath(sessionId, turnId), {
    ...callOptions,
    method: "GET",
    parse: parseInternTurn
  });
  const listTurnEvents = async (sessionId, turnId, input = {}, callOptions = {}) => transport.request(queryPath(`${turnPath(sessionId, turnId)}/events`, {
    after_seq: nonNegativeInteger(input.afterSeq, "Event cursor"),
    limit: boundedPositiveInteger(input.limit, "Event limit", 1000)
  }), {
    ...callOptions,
    method: "GET",
    parse: parseInternEventList
  });
  async function* streamTurn(sessionId, turnId, streamOptions = {}) {
    const { afterSeq, ...transportOptions } = streamOptions;
    const stream = transport.stream(`${turnPath(sessionId, turnId)}/stream`, {
      ...transportOptions,
      method: "GET",
      reconnect: transportOptions.reconnect ?? true,
      dedupe: transportOptions.dedupe ?? true,
      resume: {
        initialCursor: nonNegativeInteger(afterSeq, "Event cursor"),
        queryParameter: "after_seq",
        cursorFromEvent: (event) => {
          const seq = event.json?.seq;
          return typeof seq === "string" || typeof seq === "number" ? seq : undefined;
        }
      },
      isTerminal: (event) => event.event === "done"
    });
    for await (const event of stream) {
      yield parseTurnStreamEvent(event);
    }
  }
  const abortTurn = async (sessionId, turnId, callOptions = {}) => transport.request(`${turnPath(sessionId, turnId)}/abort`, {
    ...callOptions,
    method: "POST",
    parse: parseInternActionAcknowledgement
  });
  const decideTurn = async (sessionId, turnId, decision, input = {}, callOptions = {}) => transport.request(`${turnPath(sessionId, turnId)}/${decision}`, {
    ...callOptions,
    method: "POST",
    body: {
      note: input.note,
      allow_session: input.allow_session
    },
    parse: parseInternTurnDecision
  });
  const readArtifact = async (artifactId, input, callOptions = {}) => transport.request(queryPath(`/internal/v1/artifacts/${pathId(artifactId, "Artifact ID")}`, {
    session_key: input.sessionKey,
    offset: nonNegativeInteger(input.offset, "Artifact offset"),
    max_bytes: positiveInteger(input.maxBytes, "Artifact byte limit")
  }), {
    ...callOptions,
    method: "GET",
    parse: parseInternArtifact
  });
  const listApprovals = (input = {}, callOptions = {}) => transport.request(queryPath("/internal/v1/approvals", {
    status: input.status,
    type: input.type
  }), {
    ...callOptions,
    method: "GET",
    parse: parseInternApprovalList
  });
  const decideApproval = async (requestId, decision, input = {}, callOptions = {}) => transport.request(`/internal/v1/approvals/${pathId(requestId, "Approval request ID")}/${decision}`, {
    ...callOptions,
    method: "POST",
    body: decision === "approve" ? { note: input.note, allowlist: input.allowlist } : { note: input.note },
    parse: parseInternApprovalDecision
  });
  const pair = (input, callOptions = {}) => transport.request("/internal/v1/secure-connections/pairing/approve", {
    ...callOptions,
    method: "POST",
    body: input,
    requireAuth: false,
    parse: parseInternPairResult
  });
  return {
    transport,
    health,
    readiness,
    capabilities,
    appBootstrap,
    listRunners,
    requireRunner,
    listSessions,
    createSession,
    getSession,
    listTurns,
    startTurn,
    getTurn,
    listTurnEvents,
    streamTurn,
    abortTurn,
    decideTurn,
    readArtifact,
    listApprovals,
    decideApproval,
    pair
  };
}
export {
  toInternResult,
  safeInternStringify,
  requireInternRunner,
  requireInternCapability,
  redactInternSecrets,
  readInternSseStream,
  parseInternTurnList,
  parseInternTurnDecision,
  parseInternTurn,
  parseInternStartedTurn,
  parseInternSseBlock,
  parseInternSessionList,
  parseInternSession,
  parseInternRunnerList,
  parseInternRecord,
  parseInternReadiness,
  parseInternPairResult,
  parseInternHealth,
  parseInternEventList,
  parseInternEvent,
  parseInternCapabilities,
  parseInternArtifact,
  parseInternApprovalList,
  parseInternApprovalDecision,
  parseInternActionAcknowledgement,
  internSseEventKey,
  internSseEventCursor,
  internOk,
  internErr,
  findInternRunner,
  createInternTransport,
  createInternClient,
  asInternClientError,
  InternUnavailableError,
  InternProtocolError,
  InternClientError
};
