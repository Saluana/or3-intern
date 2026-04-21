package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// isTTY is true when stdout is an interactive terminal.
var stdoutIsTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
var stdinIsTTY = isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
var isTTY = stdoutIsTTY

func isInteractiveTTY() bool {
	return stdinIsTTY && stdoutIsTTY
}

// ---------- ANSI helpers ----------

const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiItalic    = "\033[3m"
	ansiCyan      = "\033[36m"
	ansiYellow    = "\033[33m"
	ansiGray      = "\033[90m"
	ansiWhite     = "\033[97m"
	ansiGreen     = "\033[32m"
	ansiCursorUp  = "\033[1A" // move cursor up one line
	ansiClearLine = "\033[2K" // clear entire line
)

func style(codes, text string) string {
	if !isTTY {
		return text
	}
	return codes + text + ansiReset
}

// ---------- Banner ----------

// Banner returns the startup header shown when the CLI launches.
// Banner returns the startup header shown when the CLI launches.
func Banner() string {
	if !isTTY {
		return "or3-intern CLI. Type /exit to quit.\n"
	}
	top := style(ansiDim, "╭───────────────────────────────────────────────╮")
	mid1 := style(ansiDim, "│") + "  " + style(ansiBold+ansiCyan, "or3-intern") + "                                  " + style(ansiDim, "│")
	mid2 := style(ansiDim, "│") + "  " + style(ansiGray, "Type /exit to quit · /new for new session") + "  " + style(ansiDim, "│")
	bot := style(ansiDim, "╰───────────────────────────────────────────────╯")
	return fmt.Sprintf("\n%s\n%s\n%s\n%s\n", top, mid1, mid2, bot)
}

// ---------- Prompt / separators ----------

// Prompt returns the input prompt string.
// Prompt returns the input prompt string.
func Prompt() string {
	if !isTTY {
		return "> "
	}
	return ansiBold + ansiCyan + "❯ " + ansiReset
}

// ShowPrompt prints a blank line gap then the prompt, signalling the user
// that input is ready. Called by the Deliverer after finishing output.
// ShowPrompt renders the next interactive prompt.
func ShowPrompt() {
	if !isTTY {
		fmt.Print(Prompt())
		return
	}
	fmt.Print("\n" + Prompt())
}

// Separator returns a faint horizontal rule placed after a response block.
// Separator returns the faint rule shown after a response block.
func Separator() string {
	if !isTTY {
		return ""
	}
	return "  " + ansiDim + strings.Repeat("─", 50) + ansiReset
}

// ---------- User message formatting ----------

// RewriteUserMessage moves the cursor up to overwrite the raw prompt line
// with a styled version of the user's message. This transforms the bare
// "❯ text" into a clearly labeled user block. No-op when not a TTY.
// RewriteUserMessage restyles the raw prompt line into a formatted user block.
func RewriteUserMessage(text string) {
	if !isTTY {
		return
	}
	// Move up over the raw prompt line and replace it.
	fmt.Print(ansiCursorUp + ansiClearLine)
	fmt.Printf("  %s%s▌%s %s%s\n",
		ansiBold, ansiCyan, ansiReset,
		style(ansiBold+ansiWhite, text), ansiReset)
}

// ---------- Assistant header ----------

// AssistantHeader returns the header line printed before each response.
// AssistantHeader returns the header line printed before each response.
func AssistantHeader() string {
	if !isTTY {
		return ""
	}
	name := ansiBold + ansiGreen + "◆ or3-intern" + ansiReset
	line := ansiDim + " " + strings.Repeat("─", 38) + ansiReset
	return "\n  " + name + line + "\n"
}

// ---------- Response formatting ----------

// ResponsePrefix returns the prefix printed before the first streaming delta.
// ResponsePrefix returns the prefix printed before the first streamed delta.
func ResponsePrefix() string {
	if !isTTY {
		return "\n"
	}
	return AssistantHeader() + "\n    "
}

// FormatResponse wraps a complete (non-streamed) response for display.
// FormatResponse wraps a complete non-streamed response for display.
func FormatResponse(text string) string {
	if !isTTY {
		return "[cli] " + text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return AssistantHeader() + "\n" + strings.Join(lines, "\n")
}

// ---------- Spinner ----------

// Spinner provides a braille-dot animation on stdout while the agent thinks.
// Only animates when stdout is a TTY; safe for concurrent Start/Stop.
// Spinner renders a terminal animation while the agent is working.
type Spinner struct {
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	stopped chan struct{}
}

// NewSpinner creates a ready-to-use Spinner (initially stopped).
// NewSpinner constructs a stopped Spinner.
func NewSpinner() *Spinner {
	return &Spinner{}
}

// Start begins the animation with the given label (e.g. "thinking…").
// No-op if already running or stdout is not a TTY.
// Start begins the spinner animation with label.
func (s *Spinner) Start(label string) {
	if !isTTY {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.stopped = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer close(s.stopped)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		// First frame immediately.
		fmt.Fprintf(os.Stdout, "\r  %s%s %s%s", ansiDim, frames[0], label, ansiReset)
		for {
			select {
			case <-s.stopCh:
				// Clear the spinner line.
				fmt.Fprint(os.Stdout, "\r\033[K")
				return
			case <-ticker.C:
				i++
				fmt.Fprintf(os.Stdout, "\r  %s%s %s%s", ansiDim, frames[i%len(frames)], label, ansiReset)
			}
		}
	}()
}

// Stop halts the animation and clears the spinner line.
// Blocks until the animation goroutine exits. Safe to call when not running.
// Stop halts the spinner and clears its line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	stopped := s.stopped
	s.mu.Unlock()
	<-stopped
}
