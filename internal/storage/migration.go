package storage

import (
	"context"
	"fmt"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/deploy"
	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

// MigrationCounts reports how many records of each durable entity a migration
// copied into the target datasource.
type MigrationCounts struct {
	Boards         int `json:"boards"`
	Insights       int `json:"insights"`
	Events         int `json:"events"`
	Notifications  int `json:"notifications"`
	BuildProfiles  int `json:"buildProfiles"`
	DeployProfiles int `json:"deployProfiles"`
	GitDeliveries  int `json:"gitDeliveries"`
}

// Total sums every entity count, used for at-a-glance migration summaries.
func (c MigrationCounts) Total() int {
	return c.Boards + c.Insights + c.Events + c.Notifications + c.BuildProfiles + c.DeployProfiles + c.GitDeliveries
}

// allInsightsLister is implemented by the concrete insight repositories this
// package builds. It is not part of insights.Repository because ordinary
// callers only ever need one project's records; migration is the exception
// that must enumerate every project.
type allInsightsLister interface {
	ListAll(context.Context) ([]insights.Record, error)
}

// allBuildProfilesLister is implemented by the concrete build profile
// repositories this package builds, mirroring allInsightsLister.
type allBuildProfilesLister interface {
	ListAll(context.Context) ([]buildverify.Profile, error)
}

// allDeployProfilesLister is implemented by the concrete deploy profile
// repositories this package builds, mirroring allBuildProfilesLister.
type allDeployProfilesLister interface {
	ListAll(context.Context) ([]deploy.Profile, error)
}

type allGitDeliveriesLister interface {
	ListAll(context.Context) ([]gitdelivery.Delivery, error)
}

// allEventsLister is implemented by the concrete event repositories this
// package builds. observability.Repository.List caps results for UI paging;
// migration needs the complete, uncapped history.
type allEventsLister interface {
	ListAll() ([]observability.Event, error)
}

// MigrateRepositoryBundle copies every durable entity from source into
// target. It never mutates source. Callers must only treat target as the new
// active datasource after this returns without error; on error the caller
// should discard target and keep source active, since a partial copy in
// target does not represent a consistent dataset.
func MigrateRepositoryBundle(ctx context.Context, source, target RepositoryBundle) (MigrationCounts, error) {
	var counts MigrationCounts

	boards, err := source.Boards.List(ctx)
	if err != nil {
		return counts, fmt.Errorf("list source boards: %w", err)
	}
	for _, board := range boards {
		if err := target.Boards.Create(ctx, board); err != nil {
			return counts, fmt.Errorf("migrate board %s: %w", board.ID, err)
		}
		counts.Boards++
	}

	insightSource, ok := source.Insights.(allInsightsLister)
	if !ok {
		return counts, fmt.Errorf("source insight repository does not support migration listing")
	}
	insightRecords, err := insightSource.ListAll(ctx)
	if err != nil {
		return counts, fmt.Errorf("list source insights: %w", err)
	}
	for _, record := range insightRecords {
		if err := target.Insights.Upsert(ctx, record); err != nil {
			return counts, fmt.Errorf("migrate insight %s/%s: %w", record.ProjectID, record.Kind, err)
		}
		counts.Insights++
	}

	eventSource, ok := source.Events.(allEventsLister)
	if !ok {
		return counts, fmt.Errorf("source event repository does not support migration listing")
	}
	events, err := eventSource.ListAll()
	if err != nil {
		return counts, fmt.Errorf("list source events: %w", err)
	}
	for _, event := range events {
		if err := target.Events.Append(event); err != nil {
			return counts, fmt.Errorf("migrate event %s: %w", event.ID, err)
		}
		counts.Events++
	}

	notificationItems, err := source.Notifications.List(ctx, 0)
	if err != nil {
		return counts, fmt.Errorf("list source notifications: %w", err)
	}
	for _, item := range notificationItems {
		if err := target.Notifications.Create(ctx, item); err != nil {
			return counts, fmt.Errorf("migrate notification %s: %w", item.ID, err)
		}
		counts.Notifications++
	}

	buildProfileSource, ok := source.BuildProfiles.(allBuildProfilesLister)
	if !ok {
		return counts, fmt.Errorf("source build profile repository does not support migration listing")
	}
	buildProfiles, err := buildProfileSource.ListAll(ctx)
	if err != nil {
		return counts, fmt.Errorf("list source build profiles: %w", err)
	}
	for _, profile := range buildProfiles {
		if err := target.BuildProfiles.Upsert(ctx, profile); err != nil {
			return counts, fmt.Errorf("migrate build profile %s: %w", profile.ProjectID, err)
		}
		counts.BuildProfiles++
	}

	deployProfileSource, ok := source.DeployProfiles.(allDeployProfilesLister)
	if !ok {
		return counts, fmt.Errorf("source deploy profile repository does not support migration listing")
	}
	deployProfiles, err := deployProfileSource.ListAll(ctx)
	if err != nil {
		return counts, fmt.Errorf("list source deploy profiles: %w", err)
	}
	for _, profile := range deployProfiles {
		if err := target.DeployProfiles.Upsert(ctx, profile); err != nil {
			return counts, fmt.Errorf("migrate deploy profile %s: %w", profile.ProjectID, err)
		}
		counts.DeployProfiles++
	}

	gitDeliverySource, ok := source.GitDeliveries.(allGitDeliveriesLister)
	if !ok {
		return counts, fmt.Errorf("source git delivery repository does not support migration listing")
	}
	deliveries, err := gitDeliverySource.ListAll(ctx)
	if err != nil {
		return counts, fmt.Errorf("list source git deliveries: %w", err)
	}
	for _, delivery := range deliveries {
		if err := target.GitDeliveries.Upsert(ctx, delivery); err != nil {
			return counts, fmt.Errorf("migrate git delivery %s: %w", delivery.ID, err)
		}
		counts.GitDeliveries++
	}

	return counts, nil
}
