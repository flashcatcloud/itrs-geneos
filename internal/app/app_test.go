package app

import (
	"bytes"
	"context"
	"log"
	"testing"

	"github.com/flashcatcloud/itrs-geneos/internal/config"
	"github.com/flashcatcloud/itrs-geneos/internal/event"
	"github.com/flashcatcloud/itrs-geneos/internal/flashduty"
)

type fakeSender struct {
	payloads []event.Payload
}

func (f *fakeSender) Send(_ context.Context, payload event.Payload) (flashduty.Response, error) {
	f.payloads = append(f.payloads, payload)
	return flashduty.Response{RequestID: "req", Attempts: 1}, nil
}

func TestRunAutomaticBuildsCriticalEvent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sender := &fakeSender{}
	env := map[string]string{
		"FLASHDUTY_INTEGRATION_KEY": "key",
		"_VARIABLEPATH":             "/geneos/test/cell",
		"_SEVERITY":                 "CRITICAL",
		"_RULE":                     "CPU high",
	}
	err := Run(context.Background(), nil, Dependencies{
		Getenv:     func(key string) string { return env[key] },
		Stdout:     &stdout,
		Stderr:     &stderr,
		WorkingDir: t.TempDir(),
		HomeDir:    t.TempDir(),
		NewSender: func(config.FlashDutyConfig, *log.Logger) Sender {
			return sender
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 1 || sender.payloads[0].EventStatus != "Critical" {
		t.Fatalf("unexpected payloads %#v", sender.payloads)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(sender.payloads[0].AlertKey)) {
		t.Fatalf("output does not contain alert key: %q", stdout.String())
	}
}

func TestRunResolveUsesVariablePathOverride(t *testing.T) {
	sender := &fakeSender{}
	err := Run(context.Background(), []string{"resolve", "--variable-path", "/manual/path"}, Dependencies{
		Getenv: func(key string) string {
			if key == "FLASHDUTY_INTEGRATION_KEY" {
				return "key"
			}
			return ""
		},
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		WorkingDir: t.TempDir(),
		HomeDir:    t.TempDir(),
		NewSender:  func(config.FlashDutyConfig, *log.Logger) Sender { return sender },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 1 || sender.payloads[0].EventStatus != "Ok" || sender.payloads[0].Labels["geneos_variable_path"] != "/manual/path" {
		t.Fatalf("unexpected payload %#v", sender.payloads)
	}
}

func TestRunTestSendsInfoThenMatchingRecovery(t *testing.T) {
	sender := &fakeSender{}
	err := Run(context.Background(), []string{"test"}, Dependencies{
		Getenv: func(key string) string {
			if key == "FLASHDUTY_INTEGRATION_KEY" {
				return "key"
			}
			return ""
		},
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		WorkingDir: t.TempDir(),
		HomeDir:    t.TempDir(),
		Random:     bytes.NewReader(make([]byte, 16)),
		NewSender:  func(config.FlashDutyConfig, *log.Logger) Sender { return sender },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 2 {
		t.Fatalf("payload count=%d", len(sender.payloads))
	}
	if sender.payloads[0].EventStatus != "Info" || sender.payloads[1].EventStatus != "Ok" {
		t.Fatalf("unexpected statuses %#v", sender.payloads)
	}
	if sender.payloads[0].AlertKey != sender.payloads[1].AlertKey {
		t.Fatal("test trigger and recovery keys differ")
	}
}

func TestParseArgsAcceptsFlagsBeforeAndAfterCommand(t *testing.T) {
	opts, err := parseArgs([]string{"--config", "a.yaml", "trigger", "--variable-path=/x"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.name != "trigger" || opts.configPath != "a.yaml" || opts.variablePath != "/x" {
		t.Fatalf("unexpected options %#v", opts)
	}
}
