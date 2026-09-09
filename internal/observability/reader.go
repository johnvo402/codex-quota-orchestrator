package observability

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
)

type Record struct {
	Time      time.Time
	Level     slog.Level
	Component string
	Message   string
	Fields    map[string]any
	Raw       string
}

func ParseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "all", "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q; use all, debug, info, warn, or error", value)
	}
}

func ParseRecord(line string) (Record, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Record{}, errors.New("empty log record")
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Record{}, err
	}

	rec := Record{Fields: make(map[string]any), Raw: line}
	if v, _ := raw["time"].(string); v != "" {
		rec.Time, _ = time.Parse(time.RFC3339Nano, v)
	}
	levelText, _ := raw["level"].(string)
	level, err := ParseLevel(levelText)
	if err != nil {
		return Record{}, err
	}
	rec.Level = level
	rec.Component, _ = raw["component"].(string)
	rec.Message, _ = raw["msg"].(string)
	for k, v := range raw {
		switch k {
		case "time", "level", "component", "msg":
		default:
			rec.Fields[k] = v
		}
	}
	return rec, nil
}

// ReadRecords reads current and rotated logs for the selected components,
// filters by minimum level, sorts by timestamp, and returns the newest tail
// records. Invalid/non-JSON lines are ignored so one damaged line does not make
// diagnostics unusable.
func ReadRecords(dataDir string, components []string, minLevel slog.Level, tail int) ([]Record, error) {
	if tail < 0 {
		return nil, errors.New("tail cannot be negative")
	}
	var records []Record
	for _, component := range components {
		base := LogPath(dataDir, component)
		paths := []string{base + ".3", base + ".2", base + ".1", base}
		for _, path := range paths {
			file, err := os.Open(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			scanner := bufio.NewScanner(file)
			buf := make([]byte, 64*1024)
			scanner.Buffer(buf, 1024*1024)
			for scanner.Scan() {
				rec, err := ParseRecord(scanner.Text())
				if err == nil && rec.Level >= minLevel {
					records = append(records, rec)
				}
			}
			scanErr := scanner.Err()
			_ = file.Close()
			if scanErr != nil {
				return nil, scanErr
			}
		}
	}

	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Time.Equal(records[j].Time) {
			return records[i].Component < records[j].Component
		}
		return records[i].Time.Before(records[j].Time)
	})
	if tail > 0 && len(records) > tail {
		records = records[len(records)-tail:]
	}
	return records, nil
}
