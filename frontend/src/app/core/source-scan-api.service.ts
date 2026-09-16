import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import {
  UpdateFindingResponse,
  SourceScanJob,
  SourceScanRequest,
} from './source-scan.models';
import { apiUrl } from './api-url';

@Injectable({ providedIn: 'root' })
export class SourceScanApiService {
  private readonly http = inject(HttpClient);

  startScan(request: SourceScanRequest): Observable<SourceScanJob> {
    return this.http.post<SourceScanJob>('/api/source-scans', request);
  }

  getLatestScan(projectId: string): Observable<SourceScanJob | null> {
    return this.http.get<SourceScanJob | null>(`/api/source-scans/latest?projectId=${encodeURIComponent(projectId)}`);
  }

  watchScan(scanId: string): Observable<SourceScanJob> {
    return new Observable<SourceScanJob>((subscriber) => {
      const events = new EventSource(apiUrl(`/api/source-scans/${encodeURIComponent(scanId)}/events`));
      const onScan = (event: MessageEvent<string>) => {
        try {
          const job = JSON.parse(event.data) as SourceScanJob;
          subscriber.next(job);
          if (job.status === 'completed' || job.status === 'failed') {
            subscriber.complete();
            events.close();
          }
        } catch {
          subscriber.error(new Error('The Bug Scanner stream returned invalid data.'));
          events.close();
        }
      };
      const onScanError = (event: MessageEvent<string>) => {
        try {
          const payload = JSON.parse(event.data) as { error?: string };
          subscriber.error(new Error(payload.error || 'The Bug Scanner stream is unavailable.'));
        } catch {
          subscriber.error(new Error('The Bug Scanner stream is unavailable.'));
        }
        events.close();
      };
      events.addEventListener('scan', onScan as EventListener);
      events.addEventListener('scan-error', onScanError as EventListener);
      return () => events.close();
    });
  }

  updateFinding(projectId: string, findingId: string, action: 'plan' | 'backlog' | 'ignore'): Observable<UpdateFindingResponse> {
    return this.http.patch<UpdateFindingResponse>(
      `/api/source-scans/${encodeURIComponent(projectId)}/findings/${encodeURIComponent(findingId)}`,
      { action },
    );
  }
}
