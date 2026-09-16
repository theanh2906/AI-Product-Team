import { HttpClient, HttpParams } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { AISessionOverview, ObservabilityOverview, ObservationEvent } from './observability.models';
import { apiUrl } from './api-url';

@Injectable({ providedIn: 'root' })
export class ObservabilityApiService {
  private readonly http = inject(HttpClient);

  overview(projectId: string, days: number): Observable<ObservabilityOverview> {
    let params = new HttpParams().set('days', days);
    if (projectId) params = params.set('projectId', projectId);
    return this.http.get<ObservabilityOverview>('/api/observability/overview', { params });
  }

  aiSessions(projectId: string, days: number): Observable<AISessionOverview> {
    let params = new HttpParams().set('days', days);
    if (projectId) params = params.set('projectId', projectId);
    return this.http.get<AISessionOverview>('/api/observability/ai-sessions', { params });
  }

  events(filters: { projectId: string; days: number; level: string; category: string; correlationId: string }): Observable<ObservationEvent[]> {
    let params = new HttpParams().set('days', filters.days).set('limit', 250);
    for (const [key, value] of Object.entries(filters)) if (value && key !== 'days') params = params.set(key, value);
    return this.http.get<ObservationEvent[]>('/api/observability/events', { params });
  }

  watch(): Observable<ObservationEvent> {
    return new Observable((subscriber) => {
      const source = new EventSource(apiUrl('/api/observability/events/stream'));
      source.addEventListener('observation', (event) => {
        try { subscriber.next(JSON.parse((event as MessageEvent<string>).data) as ObservationEvent); }
        catch { subscriber.error(new Error('Observability stream returned invalid data.')); }
      });
      return () => source.close();
    });
  }
}
