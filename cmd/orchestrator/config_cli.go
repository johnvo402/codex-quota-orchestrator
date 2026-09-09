package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"codex-desktop-quota-guard/internal/config"
)

func configCommand(cfg config.Config, args []string, jsonOut bool) error {
	action := "show"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch action {
	case "show":
		if jsonOut {
			b, err := json.MarshalIndent(map[string]any{
				"path":   cfg.ConfigPath(),
				"config": cfg,
			}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		fmt.Println("Config:", cfg.ConfigPath())
		fmt.Printf("Listen:           %s\n", cfg.ListenAddr)
		fmt.Printf("Hard threshold:   %.0f%%\n", cfg.HardThresholdPercent)
		fmt.Printf("Soft threshold:   %.0f%%\n", cfg.SoftThresholdPercent)
		fmt.Printf("Resume threshold: %.0f%%\n", cfg.ResumeThresholdPercent)
		fmt.Printf("Quota poll:       %ds\n", cfg.PollIntervalSeconds)
		fmt.Printf("Companion poll:   %ds\n", cfg.CompanionPollSeconds)
		fmt.Printf("Request timeout:  %ds\n", cfg.RequestTimeoutSeconds)
		fmt.Printf("Auto dispatch:    %t\n", cfg.AutoDispatch)
		return nil
	case "path":
		fmt.Println(cfg.ConfigPath())
		return nil
	case "validate":
		if err := cfg.Validate(); err != nil {
			return err
		}
		fmt.Println("Config valid:", cfg.ConfigPath())
		return nil
	default:
		return errors.New("config action must be show, path, or validate")
	}
}
