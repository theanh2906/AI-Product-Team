import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import {
  GitCommitsResponse,
  GitDelivery,
  GitDeliveryPreview,
  GitStatus,
  TaskGitBranchResponse,
  TaskGitReferenceRequest,
} from './git-status.models';
import { ProjectBoard } from './work-item.models';

@Injectable({ providedIn: 'root' })
export class GitStatusApiService {
  private readonly http = inject(HttpClient);

  getStatus(projectId: string): Observable<GitStatus> {
    return this.http.get<GitStatus>(`/api/projects/${encodeURIComponent(projectId)}/git/status`);
  }

  getCommits(projectId: string, limit = 20): Observable<GitCommitsResponse> {
    return this.http.get<GitCommitsResponse>(`/api/projects/${encodeURIComponent(projectId)}/git/commits`, {
      params: { limit: String(limit) },
    });
  }

  getDeliveryPreview(projectId: string): Observable<GitDeliveryPreview> {
    return this.http.get<GitDeliveryPreview>(`/api/projects/${encodeURIComponent(projectId)}/git/delivery-preview`);
  }

  getDelivery(projectId: string, deliveryId: string): Observable<GitDelivery> {
    return this.http.get<GitDelivery>(`${this.deliveryUrl(projectId)}/${encodeURIComponent(deliveryId)}`);
  }

  startDelivery(projectId: string, fingerprint: string): Observable<GitDelivery> {
    return this.http.post<GitDelivery>(this.deliveryUrl(projectId), { fingerprint });
  }

  stopDelivery(projectId: string, deliveryId: string): Observable<GitDelivery> {
    return this.http.post<GitDelivery>(`${this.deliveryUrl(projectId)}/${encodeURIComponent(deliveryId)}/stop`, {});
  }

  retryDelivery(projectId: string, deliveryId: string): Observable<GitDelivery> {
    return this.http.post<GitDelivery>(`${this.deliveryUrl(projectId)}/${encodeURIComponent(deliveryId)}/retry`, {});
  }

  linkTaskGitReference(projectId: string, taskId: string, request: TaskGitReferenceRequest): Observable<ProjectBoard> {
    return this.http.put<ProjectBoard>(this.taskGitUrl(projectId, taskId), request);
  }

  clearTaskGitReference(projectId: string, taskId: string): Observable<ProjectBoard> {
    return this.http.delete<ProjectBoard>(this.taskGitUrl(projectId, taskId));
  }

  proposeTaskGitBranch(projectId: string, taskId: string): Observable<TaskGitBranchResponse> {
    return this.http.post<TaskGitBranchResponse>(`${this.taskGitUrl(projectId, taskId)}/branch`, { confirm: false });
  }

  createTaskGitBranch(projectId: string, taskId: string, branchName: string): Observable<TaskGitBranchResponse> {
    return this.http.post<TaskGitBranchResponse>(`${this.taskGitUrl(projectId, taskId)}/branch`, {
      confirm: true,
      branchName,
    });
  }

  private taskGitUrl(projectId: string, taskId: string): string {
    return `/api/projects/${encodeURIComponent(projectId)}/board/tasks/${encodeURIComponent(taskId)}/git`;
  }

  private deliveryUrl(projectId: string): string {
    return `/api/projects/${encodeURIComponent(projectId)}/git/deliveries`;
  }
}
