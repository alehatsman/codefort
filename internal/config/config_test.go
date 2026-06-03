package config

import "testing"

// The mooncake-agent iteration cap has a single source of truth: config carries
// only an explicit override, defaulting to 0 (unset) so newMooncakeExecutor
// applies its run-until-done backstop. Before this fix the env defaulted to "3",
// which silently capped every turn at 3 and made the executor's backstop dead
// code (#198).
func TestLoadMooncakeMaxIterationsDefault(t *testing.T) {
	t.Setenv("MOONGIT_AGENT_MOONCAKE_MAX_ITERATIONS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentMooncakeMaxIterations != 0 {
		t.Errorf("default AgentMooncakeMaxIterations = %d, want 0 (defer to the executor backstop)", cfg.AgentMooncakeMaxIterations)
	}
}

func TestLoadMooncakeMaxIterationsOverride(t *testing.T) {
	t.Setenv("MOONGIT_AGENT_MOONCAKE_MAX_ITERATIONS", "5")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentMooncakeMaxIterations != 5 {
		t.Errorf("AgentMooncakeMaxIterations = %d, want 5 (explicit override honored)", cfg.AgentMooncakeMaxIterations)
	}
}
