package event

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/flashcatcloud/itrs-geneos/internal/config"
	"github.com/flashcatcloud/itrs-geneos/internal/geneos"
)

const (
	maxTitleRunes       = 512
	maxDescriptionRunes = 2048
	maxLabelKeyRunes    = 128
	maxLabelValueRunes  = 2048
	maxLabels           = 50
)

type Mode int

const (
	Automatic Mode = iota
	Trigger
	Resolve
)

type Payload struct {
	EventStatus string            `json:"event_status"`
	AlertKey    string            `json:"alert_key"`
	TitleRule   string            `json:"title_rule"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type Options struct {
	Mode                 Mode
	VariablePathOverride string
	Lookup               func(string) string
	Random               io.Reader
}

type Result struct {
	Payload  Payload
	Warnings []string
}

func Build(ctx geneos.Context, cfg config.FlashDutyConfig, opts Options) (Result, error) {
	variablePath := ctx.Get("_VARIABLEPATH")
	if opts.VariablePathOverride != "" {
		variablePath = opts.VariablePathOverride
	}

	alertKey, source, warnings, err := deriveAlertKey(ctx, variablePath, cfg.AlertKeyPrefix, opts.Random)
	if err != nil {
		return Result{}, err
	}
	lookup := func(name string) string {
		if name == "_VARIABLEPATH" && opts.VariablePathOverride != "" {
			return opts.VariablePathOverride
		}
		if value := ctx.Get(name); value != "" {
			return value
		}
		if opts.Lookup != nil {
			return opts.Lookup(name)
		}
		return ""
	}

	title := strings.TrimSpace(expand(cfg.Title, lookup))
	description := strings.TrimSpace(expand(cfg.Description, lookup))
	labels := configuredLabels(cfg.Labels, lookup)
	for key, value := range builtInLabels(ctx, variablePath, source) {
		labels[key] = value
	}

	if title == "" {
		title = "Geneos alert"
		warnings = append(warnings, "title template rendered empty; using default title")
	}
	title, truncated := truncateRunes(title, maxTitleRunes)
	if truncated {
		warnings = append(warnings, "title_rule was truncated to 512 characters")
	}
	description, truncated = truncateRunes(description, maxDescriptionRunes)
	if truncated {
		warnings = append(warnings, "description was truncated to 2048 characters")
	}
	labels, labelWarnings := limitLabels(labels)
	warnings = append(warnings, labelWarnings...)

	return Result{Payload: Payload{
		EventStatus: selectStatus(ctx, cfg.SeverityMap, opts.Mode),
		AlertKey:    alertKey,
		TitleRule:   title,
		Description: description,
		Labels:      labels,
	}, Warnings: warnings}, nil
}

func deriveAlertKey(ctx geneos.Context, variablePath, prefix string, random io.Reader) (string, string, []string, error) {
	if variablePath != "" {
		return prefix + digest(variablePath), "variable_path", nil, nil
	}
	if canonical, ok := fallbackIdentity(ctx); ok {
		return prefix + "fallback:" + digest(canonical), "fallback", []string{"_VARIABLEPATH is missing; alert_key uses stable Geneos component fallback"}, nil
	}
	if random == nil {
		random = rand.Reader
	}
	uuid, err := newUUID(random)
	if err != nil {
		return "", "", nil, fmt.Errorf("generate random alert identity: %w", err)
	}
	return prefix + "random:" + uuid, "random", []string{"_VARIABLEPATH and sufficient stable Geneos components are missing; recovery correlation is not guaranteed"}, nil
}

func fallbackIdentity(ctx geneos.Context) (string, bool) {
	keys := []string{"_GATEWAY", "_PROBE", "_MANAGED_ENTITY", "_SAMPLER", "_DATAVIEW", "_ROWNAME", "_COLUMN", "_VARIABLE"}
	locationKeys := map[string]bool{"_MANAGED_ENTITY": true, "_SAMPLER": true, "_DATAVIEW": true, "_ROWNAME": true, "_COLUMN": true, "_VARIABLE": true}
	hasGateway := false
	hasLocation := false
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := ctx.Get(key)
		if value == "" {
			continue
		}
		if key == "_GATEWAY" {
			hasGateway = true
		}
		if locationKeys[key] {
			hasLocation = true
		}
		parts = append(parts, key+"="+strconv.Itoa(len([]byte(value)))+":"+value)
	}
	return strings.Join(parts, "|"), hasGateway && hasLocation
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func newUUID(reader io.Reader) (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(reader, bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

func selectStatus(ctx geneos.Context, severityMap map[string]string, mode Mode) string {
	if mode == Resolve {
		return "Ok"
	}
	if strings.EqualFold(strings.TrimSpace(ctx.Get("_ALERT_TYPE")), "clear") {
		return "Ok"
	}
	mapped := canonicalStatus(severityMap[strings.ToUpper(strings.TrimSpace(ctx.Get("_SEVERITY")))])
	if mapped == "Ok" {
		return "Ok"
	}
	if mode == Trigger {
		if mapped == "Critical" || mapped == "Warning" || mapped == "Info" {
			return mapped
		}
		return "Warning"
	}
	if mapped == "" {
		return "Info"
	}
	return mapped
}

func canonicalStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "critical":
		return "Critical"
	case "warning":
		return "Warning"
	case "info":
		return "Info"
	case "ok":
		return "Ok"
	default:
		return ""
	}
}

func builtInLabels(ctx geneos.Context, variablePath, source string) map[string]string {
	mapping := map[string]string{
		"source":               "geneos",
		"gateway":              ctx.Get("_GATEWAY"),
		"probe":                ctx.Get("_PROBE"),
		"managed_entity":       ctx.Get("_MANAGED_ENTITY"),
		"sampler":              ctx.Get("_SAMPLER"),
		"sampler_group":        ctx.Get("_SAMPLER_GROUP"),
		"dataview":             ctx.Get("_DATAVIEW"),
		"row":                  ctx.Get("_ROWNAME"),
		"column":               ctx.Get("_COLUMN"),
		"rule":                 ctx.Get("_RULE"),
		"geneos_severity":      ctx.Get("_SEVERITY"),
		"geneos_alert_type":    ctx.Get("_ALERT_TYPE"),
		"geneos_variable_path": variablePath,
		"alert_key_source":     source,
	}
	for key, value := range mapping {
		if value == "" {
			delete(mapping, key)
		}
	}
	return mapping
}

func configuredLabels(labels map[string]string, lookup func(string) string) map[string]string {
	result := make(map[string]string, len(labels))
	for key, value := range labels {
		expanded := strings.TrimSpace(expand(value, lookup))
		if expanded != "" {
			result[key] = expanded
		}
	}
	return result
}

func expand(value string, lookup func(string) string) string {
	return osExpand(value, lookup)
}

var osExpand = func(value string, lookup func(string) string) string {
	return osExpandImpl(value, lookup)
}

func osExpandImpl(value string, lookup func(string) string) string {
	var result strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' || i+1 >= len(value) || value[i+1] != '{' {
			result.WriteByte(value[i])
			i++
			continue
		}
		end := strings.IndexByte(value[i+2:], '}')
		if end < 0 {
			result.WriteString(value[i:])
			break
		}
		nameEnd := i + 2 + end
		result.WriteString(lookup(value[i+2 : nameEnd]))
		i = nameEnd + 1
	}
	return result.String()
}

func limitLabels(labels map[string]string) (map[string]string, []string) {
	priorityNames := []string{
		"source", "gateway", "probe", "managed_entity", "sampler", "sampler_group",
		"dataview", "row", "column", "rule", "geneos_severity", "geneos_alert_type",
		"geneos_variable_path", "alert_key_source",
	}
	priority := make([]string, 0, len(priorityNames))
	prioritySet := make(map[string]bool, len(priorityNames))
	for _, key := range priorityNames {
		if _, ok := labels[key]; ok {
			priority = append(priority, key)
			prioritySet[key] = true
		}
	}
	keys := make([]string, 0, len(labels)-len(priority))
	for key := range labels {
		if !prioritySet[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	keys = append(priority, keys...)
	result := make(map[string]string, min(len(keys), maxLabels))
	warnings := []string{}
	for _, key := range keys {
		if len(result) == maxLabels {
			warnings = append(warnings, "labels were limited to 50 entries")
			break
		}
		limitedKey, keyTruncated := truncateRunes(key, maxLabelKeyRunes)
		limitedValue, valueTruncated := truncateRunes(labels[key], maxLabelValueRunes)
		if keyTruncated {
			warnings = append(warnings, fmt.Sprintf("label key %q was truncated to 128 characters", key))
		}
		if valueTruncated {
			warnings = append(warnings, fmt.Sprintf("label %q was truncated to 2048 characters", limitedKey))
		}
		result[limitedKey] = limitedValue
	}
	return result, warnings
}

func truncateRunes(value string, maxRunes int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value, false
	}
	return string(runes[:maxRunes]), true
}
