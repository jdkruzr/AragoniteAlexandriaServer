package main

import (
	"log/slog"
	"os"
	"testing"
)

func TestUnknownCommand(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run([]string{"definitely-not-a-command"}, logger); err == nil {
		t.Fatal("expected unknown command error")
	}
}
