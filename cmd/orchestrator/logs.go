package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/observability"
)

func logsCommand(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "config JSON")
	follow := fs.Bool("follow", false, "follow new log records")
	tail := fs.Int("tail", 200, "number of newest records to show; 0 shows all")
	levelText := fs.String("level", "info", "minimum level: all, debug, info, warn, error")
	componentText := fs.String("component", "all", "daemon, companion, or all")
	jsonOut := fs.Bool("json", false, "print raw JSON log records")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tail < 0 {
		return fmt.Errorf("--tail cannot be negative")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	minLevel, err := observability.ParseLevel(*levelText)
	if err != nil {
		return err
	}
	components, err := logComponents(*componentText)
	if err != nil {
		return err
	}

	records, err := observability.ReadRecords(cfg.DataDir, components, minLevel, *tail)
	if err != nil {
		return err
	}
	for _, rec := range records {
		printLogRecord(rec, *jsonOut)
	}
	if !*follow {
		if len(records) == 0 {
			fmt.Printf("No matching logs in %s\n", observability.LogDir(cfg.DataDir))
		}
		return nil
	}
	return followLogs(cfg.DataDir, components, minLevel, *jsonOut)
}

func logComponents(value string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return []string{"daemon", "companion"}, nil
	case "daemon":
		return []string{"daemon"}, nil
	case "companion":
		return []string{"companion"}, nil
	default:
		return nil, fmt.Errorf("unknown component %q; use daemon, companion, or all", value)
	}
}

func printLogRecord(rec observability.Record, jsonOut bool) {
	if jsonOut {
		fmt.Println(rec.Raw)
		return
	}
	stamp := "-"
	if !rec.Time.IsZero() {
		stamp = rec.Time.Local().Format("2006-01-02 15:04:05")
	}
	component := rec.Component
	if component == "" {
		component = "unknown"
	}
	fmt.Printf("%s %-5s %-9s %s", stamp, strings.ToUpper(rec.Level.String()), component, rec.Message)
	keys := make([]string, 0, len(rec.Fields))
	for k := range rec.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf(" %s=%v", k, rec.Fields[k])
	}
	fmt.Println()
}

func followLogs(dataDir string, components []string, minLevel slog.Level, jsonOut bool) error {
	offsets := make(map[string]int64, len(components))
	for _, component := range components {
		path := observability.LogPath(dataDir, component)
		if info, err := os.Stat(path); err == nil {
			offsets[component] = info.Size()
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, component := range components {
				path := observability.LogPath(dataDir, component)
				info, err := os.Stat(path)
				if os.IsNotExist(err) {
					offsets[component] = 0
					continue
				}
				if err != nil {
					return err
				}
				offset := offsets[component]
				if info.Size() < offset {
					// Rotation replaced the current file.
					offset = 0
				}
				if info.Size() == offset {
					continue
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if offset > int64(len(b)) {
					offset = 0
				}
				chunk := b[offset:]
				lastNewline := bytes.LastIndexByte(chunk, '\n')
				if lastNewline < 0 {
					continue
				}
				complete := chunk[:lastNewline]
				for _, line := range bytes.Split(complete, []byte{'\n'}) {
					rec, err := observability.ParseRecord(string(line))
					if err == nil && rec.Level >= minLevel {
						printLogRecord(rec, jsonOut)
					}
				}
				offsets[component] = offset + int64(lastNewline+1)
			}
		}
	}
}
