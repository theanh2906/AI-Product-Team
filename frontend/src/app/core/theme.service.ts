import { computed, inject, Injectable, signal } from '@angular/core';

import { ProjectApiService } from './project-api.service';

export type AppTheme = 'light' | 'dark';

@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly projectApi = inject(ProjectApiService);
  private loaded = false;

  readonly theme = signal<AppTheme>('light');
  readonly isDark = computed(() => this.theme() === 'dark');
  readonly saving = signal(false);

  load(): void {
    if (this.loaded) return;
    this.loaded = true;
    this.projectApi.getSettings().subscribe({
      next: (settings) => this.apply(settings.theme),
      error: () => {
        this.loaded = false;
      },
    });
  }

  toggle(): void {
    if (this.saving()) return;
    const previous = this.theme();
    const next: AppTheme = previous === 'dark' ? 'light' : 'dark';
    this.apply(next);
    this.saving.set(true);
    this.projectApi.updateTheme(next).subscribe({
      next: (settings) => {
        this.apply(settings.theme);
        this.saving.set(false);
      },
      error: () => {
        this.apply(previous);
        this.saving.set(false);
      },
    });
  }

  private apply(theme: AppTheme): void {
    this.theme.set(theme);
    document.documentElement.dataset['theme'] = theme;
    document.documentElement.style.colorScheme = theme;
  }
}
