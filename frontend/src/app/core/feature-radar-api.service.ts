import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { FeatureRadarJob, UpdateFeatureSuggestionResponse } from './feature-radar.models';
import { apiUrl } from './api-url';

@Injectable({ providedIn: 'root' })
export class FeatureRadarApiService {
  private readonly http = inject(HttpClient);

  getLatest(projectId: string): Observable<FeatureRadarJob | null> {
    return this.http.get<FeatureRadarJob | null>(`/api/feature-radar/latest?projectId=${encodeURIComponent(projectId)}`);
  }

  start(projectId: string, depth: string, reanalyze: boolean): Observable<FeatureRadarJob> {
    return this.http.post<FeatureRadarJob>('/api/feature-radar', { projectId, depth, reanalyze });
  }

  watch(analysisId: string): Observable<FeatureRadarJob> {
    return new Observable((subscriber) => {
      const source = new EventSource(apiUrl(`/api/feature-radar/${encodeURIComponent(analysisId)}/events`));
      source.addEventListener('radar', (event) => {
        const job = JSON.parse((event as MessageEvent<string>).data) as FeatureRadarJob;
        subscriber.next(job);
        if (job.status !== 'running') {
          subscriber.complete();
          source.close();
        }
      });
      source.addEventListener('radar-error', () => subscriber.error(new Error('Feature Radar stream is unavailable.')));
      return () => source.close();
    });
  }

  update(projectId: string, suggestionId: string, action: 'plan' | 'backlog' | 'ignore'): Observable<UpdateFeatureSuggestionResponse> {
    return this.http.patch<UpdateFeatureSuggestionResponse>(
      `/api/feature-radar/${encodeURIComponent(projectId)}/suggestions/${encodeURIComponent(suggestionId)}`,
      { action },
    );
  }
}
