package accounts

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Backup struct {
	Name      string    `json:"name"`
	Path      string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) Backup(ctx context.Context, directory string, keep int) (Backup, error) {
	if s == nil || s.db == nil {
		return Backup{}, fmt.Errorf("sqlite store unavailable")
	}
	directory = filepath.Clean(strings.TrimSpace(directory))
	if directory == "." || directory == "" {
		return Backup{}, fmt.Errorf("backup directory required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Backup{}, fmt.Errorf("create backup directory: %w", err)
	}
	createdAt := time.Now().UTC()
	name := "qoder-" + createdAt.Format("20060102T150405.000000000Z") + ".db"
	finalPath := filepath.Join(directory, name)
	tempPath := finalPath + ".tmp"
	_ = os.Remove(tempPath)

	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", tempPath); err != nil {
		return Backup{}, fmt.Errorf("snapshot sqlite: %w", err)
	}
	if err := verifySQLiteBackup(ctx, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return Backup{}, err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return Backup{}, fmt.Errorf("secure sqlite backup: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return Backup{}, fmt.Errorf("publish sqlite backup: %w", err)
	}
	if keep <= 0 {
		keep = 5
	}
	if err := pruneSQLiteBackups(directory, keep); err != nil {
		return Backup{}, err
	}
	return Backup{Name: name, Path: finalPath, CreatedAt: createdAt}, nil
}

func verifySQLiteBackup(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open sqlite backup: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("verify sqlite backup: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("verify sqlite backup: %s", result)
	}
	return nil
}

func pruneSQLiteBackups(directory string, keep int) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("list sqlite backups: %w", err)
	}
	type candidate struct {
		name string
		time time.Time
	}
	backups := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "qoder-") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect sqlite backup: %w", err)
		}
		backups = append(backups, candidate{name: entry.Name(), time: info.ModTime()})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].time.After(backups[j].time) })
	if len(backups) <= keep {
		return nil
	}
	for _, backup := range backups[keep:] {
		if err := os.Remove(filepath.Join(directory, backup.name)); err != nil {
			return fmt.Errorf("prune sqlite backup %s: %w", backup.name, err)
		}
	}
	return nil
}
