package main

import (
	"strings"
	"testing"
)

func TestReplaceManagedBlockAppendsOnce(t *testing.T) {
	existing := "# User instructions\n\nKeep this text.\n"
	block := agentsBlockStart + "\nmanaged\n" + agentsBlockEnd

	first := replaceManagedBlock(existing, agentsBlockStart, agentsBlockEnd, block)
	second := replaceManagedBlock(first, agentsBlockStart, agentsBlockEnd, block)

	if strings.Count(second, agentsBlockStart) != 1 {
		t.Fatalf("managed block count = %d, want 1", strings.Count(second, agentsBlockStart))
	}
	if !strings.Contains(second, "Keep this text.") {
		t.Fatal("existing user instructions were removed")
	}
}

func TestReplaceManagedBlockUpdatesOnlyManagedSection(t *testing.T) {
	existing := "before\n\n" + agentsBlockStart + "\nold\n" + agentsBlockEnd + "\n\nafter\n"
	block := agentsBlockStart + "\nnew\n" + agentsBlockEnd

	got := replaceManagedBlock(existing, agentsBlockStart, agentsBlockEnd, block)

	if strings.Contains(got, "\nold\n") {
		t.Fatal("old managed content remains")
	}
	if !strings.Contains(got, "\nnew\n") {
		t.Fatal("new managed content missing")
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatal("content outside managed block changed")
	}
}
