package invertertest

import "testing"

func TestRegistryReturnsIndependentEntries(t *testing.T) {
	t.Parallel()

	first := Registry()
	second := Registry()
	if len(first) != 4 {
		t.Fatalf("Registry() entries = %d, want 4", len(first))
	}
	for _, entry := range first {
		if len(entry.Scenarios) == 0 {
			t.Fatalf("protocol %q has no scenarios", entry.Protocol)
		}
		if !supports(entry.Protocol, ScenarioNormal) {
			t.Fatalf("protocol %q does not support normal scenario", entry.Protocol)
		}
	}

	first[0].Scenarios[0] = ScenarioMalformed
	if second[0].Scenarios[0] != ScenarioNormal {
		t.Fatal("Registry() returned shared scenario storage")
	}
}
