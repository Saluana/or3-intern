package main

import (
	"flag"
	"fmt"
)

func requireExactArgs(args []string, want int, usage string) error {
	if len(args) != want {
		return fmt.Errorf("usage: %s", usage)
	}
	return nil
}

func requireArgRange(args []string, minCount, maxCount int, usage string) error {
	count := len(args)
	if count < minCount || (maxCount >= 0 && count > maxCount) {
		return fmt.Errorf("usage: %s", usage)
	}
	return nil
}

func requireExactFlagArgs(fs *flag.FlagSet, want int, usage string) error {
	if fs.NArg() != want {
		return fmt.Errorf("usage: %s", usage)
	}
	return nil
}
