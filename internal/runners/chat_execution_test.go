package runners

import "testing"

func TestChatExecutionInputReplayUsesReplayPrompt(t *testing.T) {
	got := ChatExecutionInput(RunnerChatCommandRequest{
		ContinuationMode: ContinuationReplay,
		ReplayPrompt:     "compiled replay with soul",
		UserMessage:      "raw user",
	}, "run task fallback")
	if got != "compiled replay with soul" {
		t.Fatalf("replay mode should prefer replay prompt, got %q", got)
	}
}

func TestChatExecutionInputNativeContinuationUsesUserMessage(t *testing.T) {
	got := ChatExecutionInput(RunnerChatCommandRequest{
		ContinuationMode: ContinuationNative,
		NativeSessionRef: "thread_1",
		ReplayPrompt:     "compiled replay with soul",
		UserMessage:      "continue here",
	}, "run task fallback")
	if got != "continue here" {
		t.Fatalf("native continuation should use user delta, got %q", got)
	}
}

func TestChatExecutionInputNativeFirstTurnUsesCompiledTask(t *testing.T) {
	got := ChatExecutionInput(RunnerChatCommandRequest{
		ContinuationMode: ContinuationNative,
		ReplayPrompt:     "bootstrap envelope",
		UserMessage:      "start here",
	}, "bootstrap envelope")
	if got != "bootstrap envelope" {
		t.Fatalf("native first turn should use compiled bootstrap, got %q", got)
	}
}

func TestCodexNativeReplayRuntimeUsesReplayNotUserMessage(t *testing.T) {
	req := RunnerChatCommandRequest{
		ContinuationMode: ContinuationReplay,
		ReplayPrompt:     "<trusted_or3_system_instructions>\nSOUL\n</trusted_or3_system_instructions>",
		UserMessage:      "raw only",
	}
	if got := ChatExecutionInput(req, req.ReplayPrompt); got != req.ReplayPrompt {
		t.Fatalf("expected replay prompt for codex replay mode, got %q", got)
	}
}
