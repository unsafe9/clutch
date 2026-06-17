package cli

import "testing"

func TestGateReadActionPasses(t *testing.T) {
	t.Setenv(confirmEnv, "")
	assumeYes = false
	defer func() { assumeYes = false }()

	for _, action := range []string{"scan", "tasks", "task", "board.read"} {
		if err := gate(nil, action); err != nil {
			t.Errorf("gate(%q) = %v, want nil", action, err)
		}
	}
}

func TestGateWriteActionRejectedWithoutYes(t *testing.T) {
	t.Setenv(confirmEnv, "")
	assumeYes = false
	defer func() { assumeYes = false }()

	if err := gate(nil, "board.set-design"); err == nil {
		t.Fatal("gate(write) = nil, want rejection without --yes")
	}
}

func TestGateWriteActionPassesWithYes(t *testing.T) {
	t.Setenv(confirmEnv, "")
	assumeYes = true
	defer func() { assumeYes = false }()

	if err := gate(nil, "board.set-design"); err != nil {
		t.Fatalf("gate(write) with --yes = %v, want nil", err)
	}
}

func TestGateWriteActionPassesWithEnvOverride(t *testing.T) {
	assumeYes = false
	defer func() { assumeYes = false }()
	t.Setenv(confirmEnv, "1")

	if err := gate(nil, "board.set-design"); err != nil {
		t.Fatalf("gate(write) with %s = %v, want nil", confirmEnv, err)
	}
}
