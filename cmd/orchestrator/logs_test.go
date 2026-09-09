package main

import "testing"

func TestLogComponents(t *testing.T) {
	all, err := logComponents("all")
	if err != nil || len(all) != 2 || all[0] != "daemon" || all[1] != "companion" {
		t.Fatalf("unexpected all components: %#v, %v", all, err)
	}
	one, err := logComponents("daemon")
	if err != nil || len(one) != 1 || one[0] != "daemon" {
		t.Fatalf("unexpected daemon component: %#v, %v", one, err)
	}
	if _, err := logComponents("api"); err == nil {
		t.Fatal("expected invalid component error")
	}
}
