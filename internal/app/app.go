package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/flashcatcloud/itrs-geneos/internal/config"
	"github.com/flashcatcloud/itrs-geneos/internal/event"
	"github.com/flashcatcloud/itrs-geneos/internal/flashduty"
	"github.com/flashcatcloud/itrs-geneos/internal/geneos"
)

type Sender interface {
	Send(context.Context, event.Payload) (flashduty.Response, error)
}

type Dependencies struct {
	Getenv     func(string) string
	Stdout     io.Writer
	Stderr     io.Writer
	WorkingDir string
	HomeDir    string
	Random     io.Reader
	NewSender  func(config.FlashDutyConfig, *log.Logger) Sender
}

type commandOptions struct {
	mode         event.Mode
	name         string
	configPath   string
	variablePath string
	test         bool
}

func Run(ctx context.Context, args []string, deps Dependencies) error {
	deps = withDefaults(deps)
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	cfg, selectedConfig, err := config.Load(config.Options{
		ExplicitPath: opts.configPath,
		WorkingDir:   deps.WorkingDir,
		HomeDir:      deps.HomeDir,
		Getenv:       deps.Getenv,
	})
	if err != nil {
		return err
	}
	logger := log.New(deps.Stderr, "", log.LstdFlags)
	if selectedConfig != "" {
		logger.Printf("level=info message=%q path=%q", "loaded configuration", selectedConfig)
	}
	sender := deps.NewSender(cfg.FlashDuty, logger)
	if opts.test {
		return runTest(ctx, cfg.FlashDuty, sender, deps, logger)
	}

	geneosContext := geneos.FromEnv(deps.Getenv)
	result, err := event.Build(geneosContext, cfg.FlashDuty, event.Options{
		Mode:                 opts.mode,
		VariablePathOverride: opts.variablePath,
		Lookup:               deps.Getenv,
		Random:               deps.Random,
	})
	if err != nil {
		return err
	}
	logWarnings(logger, result.Warnings)
	response, err := sender.Send(ctx, result.Payload)
	if err != nil {
		return err
	}
	fmt.Fprintf(deps.Stdout, "delivered status=%s alert_key=%s request_id=%s attempts=%d\n",
		result.Payload.EventStatus, result.Payload.AlertKey, response.RequestID, response.Attempts)
	return nil
}

func runTest(ctx context.Context, cfg config.FlashDutyConfig, sender Sender, deps Dependencies, logger *log.Logger) error {
	token, err := randomToken(deps.Random)
	if err != nil {
		return fmt.Errorf("generate test event identity: %w", err)
	}
	testContext := geneos.FromMap(map[string]string{
		"_GATEWAY":      "geneos-flashduty-test",
		"_RULE":         "Geneos FlashDuty integration test",
		"_SEVERITY":     "INFO",
		"_VARIABLEPATH": "geneos-flashduty/test/" + token,
		"_VALUE":        "test event",
	})
	trigger, err := event.Build(testContext, cfg, event.Options{Mode: event.Trigger, Lookup: deps.Getenv, Random: deps.Random})
	if err != nil {
		return err
	}
	logWarnings(logger, trigger.Warnings)
	triggerResponse, err := sender.Send(ctx, trigger.Payload)
	if err != nil {
		return fmt.Errorf("send test trigger: %w", err)
	}
	recovery, err := event.Build(testContext, cfg, event.Options{Mode: event.Resolve, Lookup: deps.Getenv, Random: deps.Random})
	if err != nil {
		return err
	}
	if recovery.Payload.AlertKey != trigger.Payload.AlertKey {
		return errors.New("internal error: test trigger and recovery alert keys differ")
	}
	recoveryResponse, err := sender.Send(ctx, recovery.Payload)
	if err != nil {
		return fmt.Errorf("send test recovery: %w", err)
	}
	fmt.Fprintf(deps.Stdout, "test delivered alert_key=%s trigger_request_id=%s recovery_request_id=%s\n",
		trigger.Payload.AlertKey, triggerResponse.RequestID, recoveryResponse.RequestID)
	return nil
}

func parseArgs(args []string) (commandOptions, error) {
	opts := commandOptions{mode: event.Automatic, name: "automatic"}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "trigger" || arg == "resolve" || arg == "test":
			if opts.name != "automatic" {
				return commandOptions{}, fmt.Errorf("multiple commands specified: %s and %s", opts.name, arg)
			}
			opts.name = arg
			switch arg {
			case "trigger":
				opts.mode = event.Trigger
			case "resolve":
				opts.mode = event.Resolve
			case "test":
				opts.test = true
			}
		case arg == "--config" || arg == "-c":
			index++
			if index >= len(args) {
				return commandOptions{}, fmt.Errorf("%s requires a path", arg)
			}
			opts.configPath = args[index]
		case strings.HasPrefix(arg, "--config="):
			opts.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--variable-path":
			index++
			if index >= len(args) {
				return commandOptions{}, errors.New("--variable-path requires a value")
			}
			opts.variablePath = args[index]
		case strings.HasPrefix(arg, "--variable-path="):
			opts.variablePath = strings.TrimPrefix(arg, "--variable-path=")
		case arg == "--help" || arg == "-h":
			return commandOptions{}, errors.New(usage())
		default:
			return commandOptions{}, fmt.Errorf("unknown argument %q\n%s", arg, usage())
		}
	}
	if opts.test && opts.variablePath != "" {
		return commandOptions{}, errors.New("--variable-path cannot be used with test")
	}
	return opts, nil
}

func usage() string {
	return "usage: geneos-flashduty [trigger|resolve|test] [--config PATH] [--variable-path PATH]"
}

func withDefaults(deps Dependencies) Dependencies {
	if deps.Getenv == nil {
		deps.Getenv = os.Getenv
	}
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if deps.Random == nil {
		deps.Random = rand.Reader
	}
	if deps.NewSender == nil {
		deps.NewSender = func(cfg config.FlashDutyConfig, logger *log.Logger) Sender {
			return flashduty.New(cfg, logger)
		}
	}
	return deps
}

func randomToken(reader io.Reader) (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func logWarnings(logger *log.Logger, warnings []string) {
	for _, warning := range warnings {
		logger.Printf("level=warning message=%q", warning)
	}
}
