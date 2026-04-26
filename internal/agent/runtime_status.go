package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

const statusCanonicalMemoryInputDivisor = 2

func (r *Runtime) handleStatus(ctx context.Context, ev bus.Event) error {
	replyTarget := deliveryTarget(ev)
	replyMeta := channels.ReplyMeta(ev.Meta)
	text, err := r.statusText(ctx, ev.SessionKey)
	if err != nil {
		text = "Status unavailable. Cause: " + oneLine(err.Error(), 180)
	}
	if r.Deliver != nil {
		if derr := r.deliver(ctx, ev.Channel, replyTarget, text, replyMeta); derr != nil {
			log.Printf("deliver failed: %v", derr)
		}
	}
	return nil
}

func (r *Runtime) statusText(ctx context.Context, sessionKey string) (string, error) {
	if r == nil || r.DB == nil {
		return "", fmt.Errorf("runtime database not configured")
	}
	var sb strings.Builder
	scopeKey := sessionKey
	if resolved, err := r.DB.ResolveScopeKey(ctx, sessionKey); err == nil && strings.TrimSpace(resolved) != "" {
		scopeKey = resolved
	}
	linked := r.linkedSessions(ctx, sessionKey, scopeKey)
	physicalMessages, _ := countMessagesForSessions(ctx, r.DB, []string{sessionKey})
	scopedMessages, _ := countMessagesForSessions(ctx, r.DB, linked)

	fmt.Fprintf(&sb, "Runtime status\n")
	fmt.Fprintf(&sb, "\nSession\n")
	fmt.Fprintf(&sb, "- key: %s\n", sessionKey)
	fmt.Fprintf(&sb, "- scope: %s\n", scopeKey)
	fmt.Fprintf(&sb, "- linked sessions: %d\n", len(linked))

	historyMax := effectiveHistoryMax(r)
	promptHistory := 0
	if rows, err := r.DB.GetLastMessagesScoped(ctx, sessionKey, historyMax); err == nil {
		promptHistory = len(rows)
	}
	fmt.Fprintf(&sb, "\nMessages\n")
	fmt.Fprintf(&sb, "- stored: %d session / %d scoped\n", physicalMessages, scopedMessages)
	fmt.Fprintf(&sb, "- prompt history: %d / %d messages\n", promptHistory, historyMax)
	fmt.Fprintf(&sb, "- messages until active history rollover: %d\n", max(0, historyMax-promptHistory+1))

	r.writeConsolidationStatus(ctx, &sb, sessionKey)
	r.writeBudgetStatus(ctx, &sb, sessionKey)
	r.writeRuntimeSettingsStatus(ctx, &sb, scopeKey)

	return strings.TrimSpace(sb.String()), nil
}

func (r *Runtime) writeConsolidationStatus(ctx context.Context, sb *strings.Builder, sessionKey string) {
	fmt.Fprintf(sb, "\nConsolidation\n")
	if r.Consolidator == nil {
		fmt.Fprintf(sb, "- enabled: false\n")
		return
	}
	historyMax := effectiveHistoryMax(r)
	windowSize := r.Consolidator.WindowSize
	if windowSize <= 0 {
		windowSize = 10
	}
	maxMessages := r.Consolidator.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 50
	}
	maxInputChars := r.Consolidator.MaxInputChars
	if maxInputChars <= 0 {
		maxInputChars = 12000
	}
	lastID, oldestActiveID, err := r.DB.GetConsolidationRange(ctx, sessionKey, historyMax)
	if err != nil {
		fmt.Fprintf(sb, "- enabled: true\n")
		fmt.Fprintf(sb, "- range: unavailable (%s)\n", oneLine(err.Error(), 120))
		return
	}
	eligible, _ := countConsolidationCandidates(ctx, r.DB, sessionKey, lastID, oldestActiveID)
	unconsolidated, _ := countMessagesAfterID(ctx, r.DB, sessionKey, lastID)
	msgsUntil := messagesUntilConsolidation(unconsolidated, eligible, historyMax, windowSize)
	candidateRows, _ := r.DB.GetConsolidationMessages(ctx, sessionKey, lastID, oldestActiveID, maxMessages)
	transcriptChars := statusConsolidationTranscriptChars(candidateRows, maxInputChars)
	adaptiveTriggerChars := max(1, maxInputChars/statusCanonicalMemoryInputDivisor)

	fmt.Fprintf(sb, "- enabled: true (async: %v)\n", r.ConsolidationScheduler != nil)
	fmt.Fprintf(sb, "- last consolidated message id: %d\n", lastID)
	fmt.Fprintf(sb, "- eligible messages: %d / %d trigger window\n", eligible, windowSize)
	fmt.Fprintf(sb, "- messages until consolidation: %d\n", msgsUntil)
	fmt.Fprintf(sb, "- candidate transcript chars: %d / %d adaptive trigger\n", transcriptChars, adaptiveTriggerChars)
	fmt.Fprintf(sb, "- pass limits: %d messages, %d input chars\n", maxMessages, maxInputChars)
}

func (r *Runtime) writeBudgetStatus(ctx context.Context, sb *strings.Builder, sessionKey string) {
	fmt.Fprintf(sb, "\nContext budget\n")
	if r.Builder == nil {
		fmt.Fprintf(sb, "- builder: not configured\n")
		return
	}
	pp, _, err := r.Builder.BuildWithOptions(ctx, BuildOptions{SessionKey: sessionKey})
	if err != nil {
		fmt.Fprintf(sb, "- budget: unavailable (%s)\n", oneLine(err.Error(), 120))
		return
	}
	report := pp.Budget
	usedWithReserve := report.EstimatedInputTokens + report.OutputReserveTokens
	usable := report.MaxInputTokens - report.OutputReserveTokens
	if r.Builder.contextSafetyMarginTokens() > 0 {
		usable -= r.Builder.contextSafetyMarginTokens()
	}
	if usable <= 0 {
		usable = report.MaxInputTokens
	}
	fmt.Fprintf(sb, "- estimated input: %d / %d tokens (%.1f%%, %s)\n", report.EstimatedInputTokens, report.MaxInputTokens, report.BudgetUsedPercent, report.Pressure)
	fmt.Fprintf(sb, "- system/history: %d / %d tokens\n", report.SystemTokens, report.HistoryTokens)
	fmt.Fprintf(sb, "- output reserve: %d tokens\n", report.OutputReserveTokens)
	fmt.Fprintf(sb, "- safety margin: %d tokens\n", r.Builder.contextSafetyMarginTokens())
	fmt.Fprintf(sb, "- tokens until usable budget: %d\n", max(0, usable-report.EstimatedInputTokens))
	fmt.Fprintf(sb, "- tokens until hard max: %d\n", max(0, report.MaxInputTokens-usedWithReserve))
	fmt.Fprintf(sb, "- next pressure threshold: %s\n", nextPressureThreshold(report.MaxInputTokens, usedWithReserve))
	if len(report.Pruned) > 0 {
		fmt.Fprintf(sb, "- pruning events: %d\n", len(report.Pruned))
	}
	sections := append([]SectionUsage{}, report.Sections...)
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].EstimatedTokens > sections[j].EstimatedTokens })
	limit := min(6, len(sections))
	if limit > 0 {
		fmt.Fprintf(sb, "\nLargest context sections\n")
		for i := 0; i < limit; i++ {
			s := sections[i]
			capText := "uncapped"
			if s.LimitTokens > 0 {
				capText = fmt.Sprintf("cap %d", s.LimitTokens)
			}
			fmt.Fprintf(sb, "- %s: %d tokens, %s, truncated=%v\n", s.Name, s.EstimatedTokens, capText, s.Truncated)
		}
	}
}

func (r *Runtime) writeRuntimeSettingsStatus(ctx context.Context, sb *strings.Builder, scopeKey string) {
	fmt.Fprintf(sb, "\nRetrieval and tools\n")
	if r.Builder != nil {
		fmt.Fprintf(sb, "- memory retrieval: topK=%d vectorK=%d ftsK=%d\n", r.Builder.TopK, r.Builder.VectorK, r.Builder.FTSK)
		fmt.Fprintf(sb, "- indexed docs: enabled=%v retrieveLimit=%d\n", r.Builder.DocRetriever != nil, r.Builder.DocRetrieveLimit)
		fmt.Fprintf(sb, "- workspace context: enabled=%v\n", strings.TrimSpace(r.Builder.WorkspaceDir) != "")
	}
	fmt.Fprintf(sb, "- dynamic tool exposure: %v\n", r.DynamicToolExposure)
	fmt.Fprintf(sb, "- max tool loops: %d\n", r.MaxToolLoops)
	fmt.Fprintf(sb, "- max tool output bytes: %d\n", r.MaxToolBytes)
	if r.Hardening.Quotas.Enabled {
		fmt.Fprintf(sb, "- turn quotas: tools=%d exec=%d web=%d subagents=%d\n",
			r.Hardening.Quotas.MaxToolCalls,
			r.Hardening.Quotas.MaxExecCalls,
			r.Hardening.Quotas.MaxWebCalls,
			r.Hardening.Quotas.MaxSubagentCalls,
		)
	}
	activeNotes, _ := countMemoryRows(ctx, r.DB, "memory_notes", "session_key", []string{scopeKey, scope.GlobalMemoryScope}, "status='active'")
	docs, _ := countMemoryRows(ctx, r.DB, "memory_docs", "scope_key", []string{scopeKey, scope.GlobalMemoryScope}, "")
	fmt.Fprintf(sb, "- active memory notes: %d\n", activeNotes)
	fmt.Fprintf(sb, "- indexed document chunks: %d\n", docs)
}

func effectiveHistoryMax(r *Runtime) int {
	if r != nil && r.Builder != nil && r.Builder.HistoryMax > 0 {
		return r.Builder.HistoryMax
	}
	return 40
}

func (r *Runtime) linkedSessions(ctx context.Context, sessionKey, scopeKey string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if r != nil && r.DB != nil {
		if linked, err := r.DB.ListScopeSessions(ctx, scopeKey); err == nil {
			for _, key := range linked {
				add(key)
			}
		}
	}
	add(sessionKey)
	return out
}

func messagesUntilConsolidation(unconsolidated, eligible, historyMax, windowSize int) int {
	if windowSize <= 0 {
		windowSize = 10
	}
	if historyMax <= 0 {
		historyMax = 40
	}
	if eligible >= windowSize {
		return 0
	}
	if unconsolidated <= historyMax {
		return max(0, historyMax-unconsolidated+windowSize)
	}
	return max(0, windowSize-eligible)
}

func nextPressureThreshold(maxInput, usedWithReserve int) string {
	if maxInput <= 0 {
		return "unknown"
	}
	thresholds := []struct {
		name string
		pct  int
	}{
		{name: "warning", pct: 70},
		{name: "high", pct: 85},
		{name: "emergency", pct: 95},
	}
	for _, threshold := range thresholds {
		tokens := maxInput * threshold.pct / 100
		if usedWithReserve < tokens {
			return fmt.Sprintf("%s in %d tokens", threshold.name, tokens-usedWithReserve)
		}
	}
	return "emergency"
}

func statusConsolidationTranscriptChars(msgs []db.ConsolidationMessage, maxInputChars int) int {
	if maxInputChars <= 0 {
		maxInputChars = 12000
	}
	total := 0
	for _, msg := range msgs {
		if msg.Role == "tool" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		lineLen := len(msg.Role) + 2 + len(content)
		if total+lineLen+1 > maxInputChars {
			if total == 0 {
				remaining := maxInputChars - len(msg.Role) - 3
				if remaining > 0 {
					return len(msg.Role) + 2 + remaining + len("…")
				}
			}
			break
		}
		total += lineLen + 1
	}
	if total > 0 {
		total--
	}
	return total
}

func countMessagesForSessions(ctx context.Context, d *db.DB, sessions []string) (int, error) {
	if d == nil || len(sessions) == 0 {
		return 0, nil
	}
	placeholders := make([]string, 0, len(sessions))
	args := make([]any, 0, len(sessions))
	seen := map[string]struct{}{}
	for _, session := range sessions {
		session = strings.TrimSpace(session)
		if session == "" {
			continue
		}
		if _, ok := seen[session]; ok {
			continue
		}
		seen[session] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, session)
	}
	if len(args) == 0 {
		return 0, nil
	}
	var count int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_key IN (`+strings.Join(placeholders, ",")+`)`, args...).Scan(&count)
	return count, err
}

func countMessagesAfterID(ctx context.Context, d *db.DB, sessionKey string, afterID int64) (int, error) {
	var count int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_key=? AND id>?`, sessionKey, afterID).Scan(&count)
	return count, err
}

func countConsolidationCandidates(ctx context.Context, d *db.DB, sessionKey string, afterID, beforeID int64) (int, error) {
	if beforeID <= 0 {
		return 0, nil
	}
	var count int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_key=? AND id>? AND id<?`, sessionKey, afterID, beforeID).Scan(&count)
	return count, err
}

func countMemoryRows(ctx context.Context, d *db.DB, table, column string, values []string, extraWhere string) (int, error) {
	if d == nil || len(values) == 0 {
		return 0, nil
	}
	allowed := map[string]map[string]struct{}{
		"memory_notes": {"session_key": {}},
		"memory_docs":  {"scope_key": {}},
	}
	if _, ok := allowed[table][column]; !ok {
		return 0, fmt.Errorf("unsupported status count")
	}
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	if len(args) == 0 {
		return 0, nil
	}
	query := `SELECT COUNT(*) FROM ` + table + ` WHERE ` + column + ` IN (` + strings.Join(placeholders, ",") + `)`
	if strings.TrimSpace(extraWhere) != "" {
		query += ` AND ` + extraWhere
	}
	var count int
	err := d.SQL.QueryRowContext(ctx, query, args...).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}
