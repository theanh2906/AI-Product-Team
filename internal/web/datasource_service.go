package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

// storageDatasourceStatus is the structured payload the Settings UI renders
// for both the active datasource summary and an on-demand status check.
// Details stays free of connection secrets since local-json and sqlite carry
// none; a future remote backend must keep credentials out of this payload.
type storageDatasourceStatus struct {
	Kind          storage.DatasourceKind `json:"kind"`
	Summary       string                 `json:"summary"`
	Status        string                 `json:"status"`
	Message       string                 `json:"message,omitempty"`
	LastCheckedAt time.Time              `json:"lastCheckedAt"`
	Details       map[string]string      `json:"details,omitempty"`
}

// storageDatasourceRequest carries a candidate datasource configuration from
// the check/switch HTTP endpoints. It mirrors storage.DatasourceConfig
// instead of embedding it so decodeRequest's DisallowUnknownFields rejects
// unexpected fields on the wire, independent of that type's own JSON shape.
type storageDatasourceRequest struct {
	Kind      storage.DatasourceKind  `json:"kind"`
	LocalJSON storage.LocalJSONConfig `json:"localJson"`
	SQLite    storage.SQLiteConfig    `json:"sqlite"`
}

func (r storageDatasourceRequest) config() storage.DatasourceConfig {
	return storage.DatasourceConfig{Kind: r.Kind, LocalJSON: r.LocalJSON, SQLite: r.SQLite}
}

type switchStorageDatasourceRequest struct {
	Kind      storage.DatasourceKind  `json:"kind"`
	LocalJSON storage.LocalJSONConfig `json:"localJson"`
	SQLite    storage.SQLiteConfig    `json:"sqlite"`
	Confirm   bool                    `json:"confirm"`
}

func (r switchStorageDatasourceRequest) config() storage.DatasourceConfig {
	return storage.DatasourceConfig{Kind: r.Kind, LocalJSON: r.LocalJSON, SQLite: r.SQLite}
}

// storageDatasourceMigrationResult reports the outcome of a guarded datasource
// switch: the counts the UI shows per entity, and which datasource ends up
// active. Status is "applied" on success or "rolled-back" when migration or
// persistence failed and the previous datasource remained active.
type storageDatasourceMigrationResult struct {
	SourceKind       storage.DatasourceKind   `json:"sourceKind"`
	TargetKind       storage.DatasourceKind   `json:"targetKind"`
	Applied          bool                     `json:"applied"`
	Status           string                   `json:"status"`
	Message          string                   `json:"message,omitempty"`
	Counts           storage.MigrationCounts  `json:"counts"`
	DurationMS       int64                    `json:"durationMs"`
	ActiveDatasource storage.DatasourceConfig `json:"activeDatasource"`
}

// storageDatasourceSummary reads the active bootstrap datasource
// configuration and runs a non-destructive reachability check against it, so
// Settings can show whether durable writes are currently safe.
func (s *projectService) storageDatasourceSummary() (storageDatasourceStatus, error) {
	s.mu.Lock()
	stored, err := s.readSettings()
	s.mu.Unlock()
	if err != nil {
		return storageDatasourceStatus{}, err
	}
	return s.checkStorageDatasource(stored.StorageDatasource), nil
}

// checkStorageDatasource opens and immediately closes the candidate
// datasource without writing or reading any durable record, reporting
// whether it is reachable. It never mutates settings.json.
func (s *projectService) checkStorageDatasource(config storage.DatasourceConfig) storageDatasourceStatus {
	kind := config.Kind
	if kind == "" {
		kind = storage.DatasourceLocalJSON
	}
	status := storageDatasourceStatus{
		Kind:          kind,
		Summary:       datasourceSummaryText(config, s.directory),
		LastCheckedAt: time.Now().UTC(),
		Details:       datasourceDetails(config, s.directory),
	}
	bundle, err := storage.NewRepositoryBundle(config, s.directory)
	if err != nil {
		status.Status = "unavailable"
		status.Message = err.Error()
		return status
	}
	_ = bundle.Close()
	status.Status = "ok"
	return status
}

// switchStorageDatasource performs the guarded datasource change: it requires
// explicit confirmation, migrates every durable entity from the currently
// active datasource into the target, and only then persists the target as
// active. A failure at any step keeps the previous datasource active in
// settings.json and leaves the source data untouched.
func (s *projectService) switchStorageDatasource(target storage.DatasourceConfig, confirm bool) (storageDatasourceMigrationResult, error) {
	if !confirm {
		return storageDatasourceMigrationResult{}, fmt.Errorf("switching datasource requires explicit backup/migration confirmation")
	}
	if target.Kind == "" {
		return storageDatasourceMigrationResult{}, fmt.Errorf("target datasource kind is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.readSettings()
	if err != nil {
		return storageDatasourceMigrationResult{}, err
	}
	current := stored.StorageDatasource
	if current.Kind == "" {
		current.Kind = storage.DatasourceLocalJSON
	}
	if datasourceConfigsEqual(current, target) {
		return storageDatasourceMigrationResult{}, fmt.Errorf("target datasource is already active")
	}

	started := time.Now()
	sourceBundle, err := storage.NewRepositoryBundle(current, s.directory)
	if err != nil {
		return storageDatasourceMigrationResult{}, fmt.Errorf("open active datasource: %w", err)
	}
	defer sourceBundle.Close()

	targetBundle, err := storage.NewRepositoryBundle(target, s.directory)
	if err != nil {
		return storageDatasourceMigrationResult{
			SourceKind: current.Kind, TargetKind: target.Kind, Status: "rolled-back",
			Message: fmt.Sprintf("target datasource unavailable: %s", err.Error()), ActiveDatasource: current,
		}, fmt.Errorf("open target datasource: %w", err)
	}
	defer targetBundle.Close()

	counts, migrateErr := storage.MigrateRepositoryBundle(context.Background(), sourceBundle, targetBundle)
	if migrateErr != nil {
		return storageDatasourceMigrationResult{
			SourceKind: current.Kind, TargetKind: target.Kind, Status: "rolled-back",
			Message: migrateErr.Error(), Counts: counts, DurationMS: time.Since(started).Milliseconds(),
			ActiveDatasource: current,
		}, fmt.Errorf("migrate datasource: %w", migrateErr)
	}

	stored.StorageDatasource = target
	if err := s.writeJSON("settings.json", stored); err != nil {
		return storageDatasourceMigrationResult{
			SourceKind: current.Kind, TargetKind: target.Kind, Status: "rolled-back",
			Message: fmt.Sprintf("persist new datasource configuration: %s", err.Error()), Counts: counts,
			DurationMS: time.Since(started).Milliseconds(), ActiveDatasource: current,
		}, fmt.Errorf("persist storage datasource configuration: %w", err)
	}

	return storageDatasourceMigrationResult{
		SourceKind: current.Kind, TargetKind: target.Kind, Applied: true, Status: "applied",
		Counts: counts, DurationMS: time.Since(started).Milliseconds(), ActiveDatasource: target,
	}, nil
}

func datasourceConfigsEqual(a, b storage.DatasourceConfig) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case storage.DatasourceSQLite:
		return strings.TrimSpace(a.SQLite.Path) == strings.TrimSpace(b.SQLite.Path)
	default:
		return strings.TrimSpace(a.LocalJSON.Directory) == strings.TrimSpace(b.LocalJSON.Directory)
	}
}

func datasourceSummaryText(config storage.DatasourceConfig, fallbackDirectory string) string {
	switch config.Kind {
	case storage.DatasourceSQLite:
		return fmt.Sprintf("SQLite database at %s", strings.TrimSpace(config.SQLite.Path))
	default:
		directory := strings.TrimSpace(config.LocalJSON.Directory)
		if directory == "" {
			directory = fallbackDirectory
		}
		return fmt.Sprintf("Local JSON files in %s", directory)
	}
}

func datasourceDetails(config storage.DatasourceConfig, fallbackDirectory string) map[string]string {
	switch config.Kind {
	case storage.DatasourceSQLite:
		return map[string]string{"path": strings.TrimSpace(config.SQLite.Path)}
	default:
		directory := strings.TrimSpace(config.LocalJSON.Directory)
		if directory == "" {
			directory = fallbackDirectory
		}
		return map[string]string{"directory": directory}
	}
}
