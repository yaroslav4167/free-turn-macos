package main

import "testing"

func TestBridgeTargetsUnifiedRepository(t *testing.T) {
	if updateRepository != "yaroslav4167/free-turn-desktop" { t.Fatalf("wrong update source: %s", updateRepository) }
}
