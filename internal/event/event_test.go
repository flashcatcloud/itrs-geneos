package event

import (
	"bytes"
	"strings"
	"testing"

	"github.com/flashcatcloud/itrs-geneos/internal/config"
	"github.com/flashcatcloud/itrs-geneos/internal/geneos"
)

func baseContext(severity string) geneos.Context {
	return geneos.FromMap(map[string]string{
		"_GATEWAY":        "PROD",
		"_PROBE":          "host01",
		"_MANAGED_ENTITY": "Payment-Service",
		"_SAMPLER":        "CPU",
		"_DATAVIEW":       "CPU",
		"_ROWNAME":        "cpu0",
		"_COLUMN":         "utilisation",
		"_RULE":           "CPU threshold",
		"_VALUE":          "95%",
		"_SEVERITY":       severity,
		"_VARIABLEPATH":   `/geneos/gateway[(@name="PROD")]/directory/probe/managedEntity/sampler/dataview/rows/row/cell`,
	})
}

func TestTriggerAndRecoveryUseSameVariablePathKey(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	trigger, err := Build(baseContext("CRITICAL"), cfg, Options{Mode: Automatic})
	if err != nil {
		t.Fatal(err)
	}
	clearContext := baseContext("CRITICAL")
	values := clearContext.Values()
	values["_ALERT_TYPE"] = "clear"
	recovery, err := Build(geneos.FromMap(values), cfg, Options{Mode: Automatic})
	if err != nil {
		t.Fatal(err)
	}
	if trigger.Payload.AlertKey != recovery.Payload.AlertKey {
		t.Fatalf("trigger key %q != recovery key %q", trigger.Payload.AlertKey, recovery.Payload.AlertKey)
	}
	if trigger.Payload.EventStatus != "Critical" || recovery.Payload.EventStatus != "Ok" {
		t.Fatalf("unexpected statuses %q and %q", trigger.Payload.EventStatus, recovery.Payload.EventStatus)
	}
	if trigger.Payload.Labels["geneos_variable_path"] == "" {
		t.Fatal("raw variable path label is missing")
	}
}

func TestDifferentCellsUseDifferentKeys(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	first, _ := Build(baseContext("WARNING"), cfg, Options{})
	values := baseContext("WARNING").Values()
	values["_VARIABLEPATH"] += "-different"
	second, _ := Build(geneos.FromMap(values), cfg, Options{})
	if first.Payload.AlertKey == second.Payload.AlertKey {
		t.Fatal("different variable paths produced the same key")
	}
}

func TestStableFallbackIsDeterministic(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	values := baseContext("WARNING").Values()
	delete(values, "_VARIABLEPATH")
	first, err := Build(geneos.FromMap(values), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(geneos.FromMap(values), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Payload.AlertKey != second.Payload.AlertKey || first.Payload.Labels["alert_key_source"] != "fallback" {
		t.Fatalf("fallback identity is not deterministic: %#v %#v", first.Payload, second.Payload)
	}
}

func TestRandomFallbackUsesUUID(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	result, err := Build(geneos.FromMap(map[string]string{}), cfg, Options{Random: bytes.NewReader(make([]byte, 16))})
	if err != nil {
		t.Fatal(err)
	}
	if result.Payload.AlertKey != "geneos:v1:random:00000000-0000-4000-8000-000000000000" {
		t.Fatalf("unexpected random fallback %q", result.Payload.AlertKey)
	}
	if len(result.Warnings) == 0 || result.Payload.Labels["alert_key_source"] != "random" {
		t.Fatal("random fallback warning/source missing")
	}
}

func TestStatusPrecedenceAndExplicitModes(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	tests := []struct {
		name       string
		ctx        geneos.Context
		mode       Mode
		wantStatus string
	}{
		{"warning action", baseContext("WARNING"), Automatic, "Warning"},
		{"ok action", baseContext("OK"), Automatic, "Ok"},
		{"resolve command", baseContext("CRITICAL"), Resolve, "Ok"},
		{"trigger defaults warning", baseContext("UNDEFINED"), Trigger, "Info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Build(tt.ctx, cfg, Options{Mode: tt.mode})
			if err != nil {
				t.Fatal(err)
			}
			if result.Payload.EventStatus != tt.wantStatus {
				t.Fatalf("status=%q, want %q", result.Payload.EventStatus, tt.wantStatus)
			}
		})
	}
}

func TestTemplatesAndUnicodeSafeTruncation(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	cfg.Title = "${TEAM}:${_RULE}"
	cfg.Description = strings.Repeat("界", 2050)
	cfg.Labels["team"] = "${TEAM}"
	result, err := Build(baseContext("CRITICAL"), cfg, Options{Lookup: func(key string) string {
		if key == "TEAM" {
			return "payments"
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Payload.TitleRule != "payments:CPU threshold" || result.Payload.Labels["team"] != "payments" {
		t.Fatalf("templates not expanded: %#v", result.Payload)
	}
	if len([]rune(result.Payload.Description)) != 2048 {
		t.Fatalf("description length=%d", len([]rune(result.Payload.Description)))
	}
}

func TestLongVariablePathHashesFullValueButTruncatesLabel(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	values := baseContext("CRITICAL").Values()
	values["_VARIABLEPATH"] = strings.Repeat("x", 3000)
	first, err := Build(geneos.FromMap(values), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	values["_VARIABLEPATH"] = strings.Repeat("x", 2999) + "y"
	second, err := Build(geneos.FromMap(values), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Payload.AlertKey == second.Payload.AlertKey {
		t.Fatal("key was derived from truncated path")
	}
	if len([]rune(first.Payload.Labels["geneos_variable_path"])) != 2048 {
		t.Fatal("path label was not truncated")
	}
}

func TestBuiltInIdentityLabelsSurviveFiftyLabelLimit(t *testing.T) {
	cfg := config.Defaults().FlashDuty
	for index := 0; index < 60; index++ {
		cfg.Labels["aaa_custom_"+strings.Repeat("0", index%3)+string(rune('A'+index%26))+strings.Repeat("x", index)] = "value"
	}
	result, err := Build(baseContext("CRITICAL"), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Payload.Labels) != 50 {
		t.Fatalf("label count=%d", len(result.Payload.Labels))
	}
	if result.Payload.Labels["geneos_variable_path"] == "" || result.Payload.Labels["alert_key_source"] != "variable_path" {
		t.Fatalf("identity labels were dropped: %#v", result.Payload.Labels)
	}
}
