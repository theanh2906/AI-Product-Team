import { HttpClient } from '@angular/common/http';
import { computed, inject, Injectable, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { apiUrl } from './api-url';

const TOAST_DURATION_MS = 5_000;

export type NotificationLevel = 'info' | 'success' | 'warning' | 'error';

export interface AppNotification {
  id: string;
  level: NotificationLevel;
  kind: string;
  title: string;
  message: string;
  projectId?: string;
  projectName?: string;
  entityId?: string;
  route?: string;
  read: boolean;
  createdAt: string;
}

@Injectable({ providedIn: 'root' })
export class NotificationService {
  private readonly http = inject(HttpClient);
  private source?: EventSource;
  private initialized = false;
  private toastTimer?: ReturnType<typeof setTimeout>;

  readonly items = signal<AppNotification[]>([]);
  readonly loading = signal(false);
  readonly toast = signal<AppNotification | null>(null);
  readonly unreadCount = computed(() => this.items().filter((item) => !item.read).length);

  async initialize(): Promise<void> {
    if (this.initialized) return;
    this.initialized = true;
    this.loading.set(true);
    try {
      const items = await firstValueFrom(this.http.get<AppNotification[] | null>('/api/notifications?limit=100'));
      this.items.set(Array.isArray(items) ? items : []);
    } catch {
      this.items.set([]);
    } finally {
      this.loading.set(false);
      this.connect();
    }
  }

  async markRead(item: AppNotification): Promise<void> {
    if (item.read) return;
    await firstValueFrom(this.http.patch<AppNotification>(`/api/notifications/${encodeURIComponent(item.id)}/read`, {}));
    this.items.update((items) => items.map((candidate) => candidate.id === item.id ? { ...candidate, read: true } : candidate));
  }

  async markAllRead(): Promise<void> {
    if (this.unreadCount() === 0) return;
    await firstValueFrom(this.http.post<void>('/api/notifications/read-all', {}));
    this.items.update((items) => items.map((item) => ({ ...item, read: true })));
  }

  async clearRead(): Promise<void> {
    await firstValueFrom(this.http.delete<void>('/api/notifications/read'));
    this.items.update((items) => items.filter((item) => !item.read));
  }

  dismissToast(): void {
    if (this.toastTimer) clearTimeout(this.toastTimer);
    this.toast.set(null);
  }

  private connect(): void {
    if (this.source || typeof EventSource === 'undefined') return;
    this.source = new EventSource(apiUrl('/api/notifications/events'));
    this.source.addEventListener('notification', (event) => {
      const item = JSON.parse((event as MessageEvent<string>).data) as AppNotification;
      this.items.update((items) => [item, ...items.filter((candidate) => candidate.id !== item.id)].slice(0, 100));
      this.toast.set(item);
      if (this.toastTimer) clearTimeout(this.toastTimer);
      this.toastTimer = setTimeout(() => this.toast.set(null), TOAST_DURATION_MS);
    });
  }
}
