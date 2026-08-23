package client

import "testing"

// normalizeForPut is the safety-critical half of the write path, so it is tested
// from inside the package: the API has no partial update, and a PUT that echoes
// back the object as read is rejected (nested plan) or destructive (secret_envs).
func TestNormalizeForPutFlattensRelationsAndDropsServerFields(t *testing.T) {
	t.Parallel()

	app := map[string]any{
		"name":      "my-api",
		"replicas":  float64(2),
		"plan":      map[string]any{"id": "73904144-6592-40ce-a96f-3789f611b5e4", "plan_type": "app"},
		"namespace": map[string]any{"id": float64(156357), "name": "rahacloud"},
		// Sending the nested relation back fails validation with
		// "plan: … is not a valid UUID", so these must collapse to ids.
		"organization": map[string]any{"id": float64(22123), "name": "rahacloud"},

		"cluster":     map[string]any{"id": float64(46)},
		"state":       map[string]any{"state_type": "healthy"},
		"secret_envs": []any{map[string]any{"name": "DB_PASSWORD", "value": ""}},
		"latest_build": map[string]any{
			"id": "b1",
		},
		"creator": map[string]any{"id": float64(7448)},
	}

	normalizeForPut(app)

	if got := app["plan"]; got != "73904144-6592-40ce-a96f-3789f611b5e4" {
		t.Errorf("plan: want the bare uuid, got %#v", got)
	}
	if got := app["namespace"]; got != float64(156357) {
		t.Errorf("namespace: want the bare id, got %#v", got)
	}
	if got := app["organization"]; got != float64(22123) {
		t.Errorf("organization: want the bare id, got %#v", got)
	}

	// secret_envs must go: it is read-only on this serializer and its values
	// never come back from a read, so echoing it risks clearing stored secrets.
	for _, key := range []string{"cluster", "state", "secret_envs", "latest_build", "creator"} {
		if _, present := app[key]; present {
			t.Errorf("%s: want it dropped from the PUT body, but it is still present", key)
		}
	}

	// Everything the caller might want to change has to survive untouched.
	if app["name"] != "my-api" || app["replicas"] != float64(2) {
		t.Errorf("writable fields were altered: name=%#v replicas=%#v", app["name"], app["replicas"])
	}
}

// An app read straight after creation has null relations rather than nested
// objects; normalizing must leave those alone instead of panicking.
func TestNormalizeForPutToleratesNullRelations(t *testing.T) {
	t.Parallel()

	app := map[string]any{"plan": nil, "namespace": nil, "name": "x"}
	normalizeForPut(app)

	if app["plan"] != nil || app["namespace"] != nil {
		t.Errorf("null relations should pass through, got plan=%#v namespace=%#v", app["plan"], app["namespace"])
	}
}
