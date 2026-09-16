import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: 'work-items',
    loadComponent: () =>
      import('./features/work-items/work-item-page').then(
        (module) => module.WorkItemPage,
      ),
  },
  {
    path: 'features',
    loadComponent: () =>
      import('./features/feature-library/feature-library-page').then(
        (module) => module.FeatureLibraryPage,
      ),
  },
  { path: 'work-items/new', pathMatch: 'full', redirectTo: 'work-items' },
  {
    path: 'projects',
    loadComponent: () =>
      import('./features/projects/projects-page').then(
        (module) => module.ProjectsPage,
      ),
  },
  {
    path: 'projects/new',
    loadComponent: () =>
      import('./features/create-product/create-product-page').then(
        (module) => module.CreateProductPage,
      ),
  },
  {
    path: 'project-atlas',
    loadComponent: () =>
      import('./features/project-atlas/project-atlas-page').then(
        (module) => module.ProjectAtlasPage,
      ),
  },
  {
    path: 'source-scan',
    loadComponent: () =>
      import('./features/source-scan/source-scan-page').then(
        (module) => module.SourceScanPage,
      ),
  },
  {
    path: 'feature-radar',
    loadComponent: () =>
      import('./features/feature-radar/feature-radar-page').then(
        (module) => module.FeatureRadarPage,
      ),
  },
  {
    path: 'observability',
    loadComponent: () =>
      import('./features/observability/observability-page').then(
        (module) => module.ObservabilityPage,
      ),
  },
  {
    path: 'settings',
    loadComponent: () =>
      import('./features/settings/settings-page').then(
        (module) => module.SettingsPage,
      ),
  },
  { path: '', pathMatch: 'full', redirectTo: 'work-items' },
  { path: '**', redirectTo: 'work-items' },
];
