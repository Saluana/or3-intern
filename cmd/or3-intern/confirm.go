package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type destructiveConfirmation struct {
	Action      string
	ItemName    string
	Consequence string
	Undo        string
	Force       bool
	Stdin       io.Reader
	Stdout      io.Writer
	StdinTTY    bool
	StdoutTTY   bool
}

func confirmDestructiveAction(c destructiveConfirmation) (bool, error) {
	if c.Force || !c.StdinTTY || !c.StdoutTTY {
		return true, nil
	}
	in := c.Stdin
	if in == nil {
		in = os.Stdin
	}
	out := c.Stdout
	if out == nil {
		out = os.Stdout
	}
	action := strings.TrimSpace(c.Action)
	if action == "" {
		action = "Continue"
	}
	item := strings.TrimSpace(c.ItemName)
	if item == "" {
		item = "this item"
	}
	fmt.Fprintf(out, "%s: %s\n", action, item)
	if strings.TrimSpace(c.Consequence) != "" {
		fmt.Fprintf(out, "Consequence: %s\n", strings.TrimSpace(c.Consequence))
	}
	if strings.TrimSpace(c.Undo) != "" {
		fmt.Fprintf(out, "Undo: %s\n", strings.TrimSpace(c.Undo))
	}
	fmt.Fprint(out, "Type yes to continue: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

func stdioIsTerminal(r io.Reader, w io.Writer) (bool, bool) {
	return fileIsTerminal(r), fileIsTerminal(w)
}

func fileIsTerminal(v any) bool {
	file, ok := v.(*os.File)
	if !ok || file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func splitForceFlag(args []string) ([]string, bool, error) {
	next := make([]string, 0, len(args))
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "--force", "-f":
			return append(next, args[len(next)+1:]...), true, nil
		default:
			next = append(next, arg)
		}
	}
	return next, false, nil
}
