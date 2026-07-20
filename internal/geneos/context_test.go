package geneos

import "testing"

func TestFromEnvReadsKnownVariablesOnly(t *testing.T) {
	ctx := FromEnv(func(key string) string { return "value:" + key })
	if ctx.Get("_VARIABLEPATH") != "value:_VARIABLEPATH" {
		t.Fatalf("unexpected variable path %q", ctx.Get("_VARIABLEPATH"))
	}
	if ctx.Get("SECRET") != "" {
		t.Fatal("unknown variable should not be present")
	}
}

func TestValuesReturnsCopy(t *testing.T) {
	ctx := FromMap(map[string]string{"_RULE": "original"})
	values := ctx.Values()
	values["_RULE"] = "changed"
	if ctx.Get("_RULE") != "original" {
		t.Fatal("context was mutated through Values")
	}
}
