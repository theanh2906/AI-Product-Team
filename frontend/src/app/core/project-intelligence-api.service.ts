import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { apiUrl } from './api-url';
import { ProjectAgentProfile, ProjectStudyJob, ProjectStudyResult } from './project.models';

@Injectable({ providedIn: 'root' })
export class ProjectIntelligenceApiService {
  private readonly http = inject(HttpClient);

  getLatest(projectId: string): Observable<ProjectStudyJob | null> {
    return this.http.get<ProjectStudyJob | null>(`/api/projects/${encodeURIComponent(projectId)}/intelligence`);
  }

  start(projectId: string): Observable<ProjectStudyJob> {
    return this.http.post<ProjectStudyJob>(`/api/projects/${encodeURIComponent(projectId)}/intelligence/studies`, {});
  }

  watch(projectId: string, studyId: string): Observable<ProjectStudyJob> {
    return new Observable((subscriber) => {
      const source = new EventSource(apiUrl(`/api/projects/${encodeURIComponent(projectId)}/intelligence/studies/${encodeURIComponent(studyId)}/events`));
      source.addEventListener('study', (event) => {
        const job = JSON.parse((event as MessageEvent<string>).data) as ProjectStudyJob;
        subscriber.next(job);
        if (job.status !== 'running') {
          subscriber.complete();
          source.close();
        }
      });
      source.addEventListener('study-error', () => subscriber.error(new Error('Project study stream is unavailable.')));
      return () => source.close();
    });
  }

  applyGuidance(projectId: string, studyId: string): Observable<{ result: ProjectStudyResult; profile: ProjectAgentProfile }> {
    return this.http.post<{ result: ProjectStudyResult; profile: ProjectAgentProfile }>(
      `/api/projects/${encodeURIComponent(projectId)}/intelligence/studies/${encodeURIComponent(studyId)}/apply-guidance`,
      {},
    );
  }
}
