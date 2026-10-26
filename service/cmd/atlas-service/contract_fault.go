package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Called only after contract-mode's temporary-root and marker checks. This is
// process orchestration, never an HTTP API. Production mode must reject this env.
func contractFault(root string) (func(string) error, error) {
	raw := os.Getenv("ATLAS_CONTRACT_FAULT")
	if raw == "" {
		return nil, nil
	}
	var cfg struct {
		Point  string `json:"point"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	switch cfg.Point {
	case "summary.before_write", "summary.before_commit", "obsidian.snapshot_verified", "refresh.before_commit", "refresh.after_commit", "feed.before_commit", "feed.after_commit", "delete.before_commit", "delete.after_commit", "restore.before_commit", "restore.after_commit":
	default:
		return nil, errors.New("unknown contract fault point")
	}
	if cfg.Action != "fail" && cfg.Action != "pause" && cfg.Action != "pause_fail" {
		return nil, errors.New("unknown contract fault action")
	}
	used := false
	return func(point string) error {
		if used || point != cfg.Point {
			return nil
		}
		used = true
		if cfg.Action == "fail" {
			return errors.New("Injected disposable write failure")
		}
		dir := filepath.Join(root, ".service", "contract-fault")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(point), 0600); err != nil {
			return err
		}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if info, err := os.Lstat(filepath.Join(dir, "release")); err == nil && info.Mode().IsRegular() {
				if cfg.Action == "pause_fail" {
					return errors.New("Injected disposable commit failure after barrier")
				}
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return errors.New("Disposable contract barrier timed out")
	}, nil
}
