package storage

import (
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/deploy"
	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

// DatasourceKind identifies a durable storage backend. New backends extend
// this union without changing the RepositoryBundle contract consumers depend
// on.
type DatasourceKind string

const (
	DatasourceLocalJSON DatasourceKind = "local-json"
	DatasourceSQLite    DatasourceKind = "sqlite"
)

// LocalJSONConfig configures the local-json datasource. An empty Directory
// keeps the current app data directory so existing installs stay on their
// current storage location without any settings.json change.
type LocalJSONConfig struct {
	Directory string `json:"directory,omitempty"`
}

// SQLiteConfig configures the sqlite datasource. All durable entities share
// one database file at Path.
type SQLiteConfig struct {
	Path string `json:"path,omitempty"`
}

// DatasourceConfig is the persisted bootstrap configuration for durable
// storage. It lives in settings.json so the app can decide which repository
// bundle to build before the durable store itself is opened.
type DatasourceConfig struct {
	Kind      DatasourceKind  `json:"kind,omitempty"`
	LocalJSON LocalJSONConfig `json:"localJson,omitempty"`
	SQLite    SQLiteConfig    `json:"sqlite,omitempty"`
}

// DefaultDatasourceConfig returns the local-json datasource pointed at the
// given app data directory. Existing installs use this whenever settings.json
// predates the storageDatasource field.
func DefaultDatasourceConfig(directory string) DatasourceConfig {
	return DatasourceConfig{Kind: DatasourceLocalJSON, LocalJSON: LocalJSONConfig{Directory: directory}}
}

// RepositoryBundle groups the durable repositories a datasource provider
// builds for one configured backend. Close releases backend resources (a
// no-op for local-json) and must be called once the bundle is no longer used.
type RepositoryBundle struct {
	Kind           DatasourceKind
	Boards         kanban.BoardRepository
	Insights       insights.Repository
	Events         observability.Repository
	Notifications  notifications.Repository
	BuildProfiles  buildverify.Repository
	DeployProfiles deploy.Repository
	GitDeliveries  gitdelivery.Repository
	Close          func() error
}

// NewRepositoryBundle builds the durable repository bundle for the configured
// datasource. fallbackDirectory seeds local-json when no explicit directory
// is configured, keeping existing installs on their current storage location.
// It returns an explicit error instead of falling back to another store when
// the configured target is unavailable.
func NewRepositoryBundle(config DatasourceConfig, fallbackDirectory string) (RepositoryBundle, error) {
	switch config.Kind {
	case "", DatasourceLocalJSON:
		directory := strings.TrimSpace(config.LocalJSON.Directory)
		if directory == "" {
			directory = fallbackDirectory
		}
		return newLocalJSONBundle(directory)
	case DatasourceSQLite:
		return newSQLiteBundle(config.SQLite.Path)
	default:
		return RepositoryBundle{}, fmt.Errorf("unsupported storage datasource kind %q", config.Kind)
	}
}

func newLocalJSONBundle(directory string) (RepositoryBundle, error) {
	if strings.TrimSpace(directory) == "" {
		return RepositoryBundle{}, fmt.Errorf("local-json datasource directory is required")
	}
	boards, err := NewJSONBoardRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json board repository: %w", err)
	}
	insightRepository, err := NewJSONInsightRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json insight repository: %w", err)
	}
	eventRepository, err := NewJSONLEventRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json event repository: %w", err)
	}
	notificationRepository, err := NewJSONNotificationRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json notification repository: %w", err)
	}
	buildProfileRepository, err := NewJSONBuildProfileRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json build profile repository: %w", err)
	}
	deployProfileRepository, err := NewJSONDeployProfileRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json deploy profile repository: %w", err)
	}
	gitDeliveryRepository, err := NewJSONGitDeliveryRepository(directory)
	if err != nil {
		return RepositoryBundle{}, fmt.Errorf("open local-json git delivery repository: %w", err)
	}
	return RepositoryBundle{
		Kind:           DatasourceLocalJSON,
		Boards:         boards,
		Insights:       insightRepository,
		Events:         eventRepository,
		Notifications:  notificationRepository,
		BuildProfiles:  buildProfileRepository,
		DeployProfiles: deployProfileRepository,
		GitDeliveries:  gitDeliveryRepository,
		Close:          func() error { return nil },
	}, nil
}

func newSQLiteBundle(path string) (RepositoryBundle, error) {
	if strings.TrimSpace(path) == "" {
		return RepositoryBundle{}, fmt.Errorf("sqlite datasource path is required")
	}
	db, err := openSQLiteDB(path)
	if err != nil {
		return RepositoryBundle{}, err
	}
	closeOnError := func(err error) (RepositoryBundle, error) {
		_ = db.Close()
		return RepositoryBundle{}, err
	}
	boards, err := NewSQLiteBoardRepository(db)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite board repository: %w", err))
	}
	insightRepository, err := NewSQLiteInsightRepository(db)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite insight repository: %w", err))
	}
	eventRepository, err := NewSQLiteEventRepository(db, path)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite event repository: %w", err))
	}
	notificationRepository, err := NewSQLiteNotificationRepository(db)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite notification repository: %w", err))
	}
	buildProfileRepository, err := NewSQLiteBuildProfileRepository(db)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite build profile repository: %w", err))
	}
	deployProfileRepository, err := NewSQLiteDeployProfileRepository(db)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite deploy profile repository: %w", err))
	}
	gitDeliveryRepository, err := NewSQLiteGitDeliveryRepository(db)
	if err != nil {
		return closeOnError(fmt.Errorf("open sqlite git delivery repository: %w", err))
	}
	return RepositoryBundle{
		Kind:           DatasourceSQLite,
		Boards:         boards,
		Insights:       insightRepository,
		Events:         eventRepository,
		Notifications:  notificationRepository,
		BuildProfiles:  buildProfileRepository,
		DeployProfiles: deployProfileRepository,
		GitDeliveries:  gitDeliveryRepository,
		Close:          db.Close,
	}, nil
}
