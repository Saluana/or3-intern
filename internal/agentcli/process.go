package agentcli

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// DefaultEventChunkMaxBytes is the maximum size of a single output event chunk.
const DefaultEventChunkMaxBytes = 16384

// DefaultPreviewMaxBytes is the maximum retained preview for stdout/stderr.
const DefaultPreviewMaxBytes = 65536

// ProcessManager launches external CLI processes and streams output events.
type ProcessManager struct {
	ChunkMaxBytes   int
	PreviewMaxBytes int
}

// NewProcessManager creates a ProcessManager with safe defaults.
func NewProcessManager(chunkMaxBytes, previewMaxBytes int) *ProcessManager {
	if chunkMaxBytes <= 0 {
		chunkMaxBytes = DefaultEventChunkMaxBytes
	}
	if previewMaxBytes <= 0 {
		previewMaxBytes = DefaultPreviewMaxBytes
	}
	return &ProcessManager{
		ChunkMaxBytes:   chunkMaxBytes,
		PreviewMaxBytes: previewMaxBytes,
	}
}

// ProcessOutput holds the final results of a completed process run.
type ProcessOutput struct {
	ExitCode         int
	StdoutPreview    string
	StderrPreview    string
	FinalTextPreview string
	DurationMS       int64
}

// Run launches a command and streams events through the provided callback.
// The callback receives typed AgentRunEvent values with monotonic sequence numbers.
func (p *ProcessManager) Run(ctx context.Context, spec CommandSpec, onEvent func(AgentRunEvent)) ProcessOutput {
	startedAt := time.Now()

	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Stdin = nil
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}
	if len(spec.Env) > 0 {
		cmd.Env = spec.Env
	}
	p.setProcessGroup(cmd)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		emitError(onEvent, 0, fmt.Sprintf("stdout pipe: %v", err))
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(startedAt).Milliseconds()}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		emitError(onEvent, 0, fmt.Sprintf("stderr pipe: %v", err))
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(startedAt).Milliseconds()}
	}

	if err := cmd.Start(); err != nil {
		emitError(onEvent, 0, fmt.Sprintf("process start: %v", err))
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(startedAt).Milliseconds()}
	}

	var seq int64
	collector := newOutputCollector(p.PreviewMaxBytes)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readStream(stdoutPipe, "stdout", p.ChunkMaxBytes, &seq, collector, onEvent, spec.OutputMode)
	}()
	go func() {
		defer wg.Done()
		readStream(stderrPipe, "stderr", p.ChunkMaxBytes, &seq, collector, onEvent, OutputPlain)
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	var exitCode int
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			if ctx.Err() != nil {
				emitError(onEvent, seq+1, fmt.Sprintf("process error: %v", waitErr))
			}
		}
	}

	durMS := time.Since(startedAt).Milliseconds()
	out := ProcessOutput{
		ExitCode:         exitCode,
		StdoutPreview:    collector.stdout.String(),
		StderrPreview:    collector.stderr.String(),
		FinalTextPreview: collector.stdout.String(),
		DurationMS:       durMS,
	}

	return out
}

func emitError(onEvent func(AgentRunEvent), seq int64, msg string) {
	if onEvent == nil {
		return
	}
	onEvent(AgentRunEvent{
		Type:    "error",
		Seq:     seq,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Message: msg,
	})
}
