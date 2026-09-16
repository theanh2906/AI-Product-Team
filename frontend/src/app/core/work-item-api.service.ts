import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { filter, map, Observable } from 'rxjs';

import {
  BoardColumn,
  CreateBacklogRequest,
  CreatePlanRequest,
  IntakeQuestionnaire,
  ProjectBoard,
  ReviewPlanRequest,
  TaskStatus,
  TaskSessionLog,
} from './work-item.models';
import { apiUrl } from './api-url';

export type BoardStreamState = 'connected' | 'reconnecting';

export interface BoardStreamEvent {
  state: BoardStreamState;
  board?: ProjectBoard;
}

@Injectable({ providedIn: 'root' })
export class WorkItemApiService {
  private readonly http = inject(HttpClient);

  listBoards(): Observable<ProjectBoard[]> {
    return this.http
      .get<ProjectBoard[]>('/api/boards')
      .pipe(map((boards) => boards.map((board) => this.normalizeBoard(board))));
  }

  getBoard(projectId: string): Observable<ProjectBoard> {
    return this.boardResponse(this.http.get<ProjectBoard>(this.boardUrl(projectId)));
  }

  createPlan(projectId: string, request: CreatePlanRequest): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(`${this.boardUrl(projectId)}/plans`, request),
    );
  }

  createBacklog(projectId: string, request: CreateBacklogRequest, attachments: File[] = []): Observable<ProjectBoard> {
    let body: CreateBacklogRequest | FormData = request;
    if (attachments.length > 0) {
      const form = new FormData();
      form.append('request', JSON.stringify(request));
      for (const attachment of attachments) form.append('attachments', attachment, attachment.name);
      body = form;
    }
    return this.http
      .post<{ board: ProjectBoard }>(`${this.boardUrl(projectId)}/backlog`, body)
      .pipe(map((response) => this.normalizeBoard(response.board)));
  }

  requestAttachmentUrl(projectId: string, requestId: string, attachmentId: string, download = false): string {
    const suffix = download ? '?download=1' : '';
    return apiUrl(`/api/projects/${encodeURIComponent(projectId)}/requests/${encodeURIComponent(requestId)}/attachments/${encodeURIComponent(attachmentId)}${suffix}`);
  }

  generateIntakeQuestions(
    projectId: string,
    request: { title: string; description: string; workType?: CreateBacklogRequest['type'] },
  ): Observable<IntakeQuestionnaire> {
    return this.http.post<IntakeQuestionnaire>(
      `/api/projects/${encodeURIComponent(projectId)}/intake/questions`,
      request,
    );
  }

  planBacklog(projectId: string, itemId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/backlog/${encodeURIComponent(itemId)}/plan`,
        {},
      ),
    );
  }

  removeBacklogItem(projectId: string, itemId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/backlog/${encodeURIComponent(itemId)}/remove`,
        {},
      ),
    );
  }

  reviewPlan(
    projectId: string,
    planId: string,
    request: ReviewPlanRequest,
  ): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/plans/${encodeURIComponent(planId)}/review`,
        request,
      ),
    );
  }

  retryPlan(projectId: string, planId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/plans/${encodeURIComponent(planId)}/retry`,
        {},
      ),
    );
  }

  moveTask(
    projectId: string,
    taskId: string,
    targetColumn: BoardColumn,
  ): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.patch<ProjectBoard>(
        `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/move`,
        { targetColumn },
      ),
    );
  }

  restartTask(projectId: string, taskId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/restart`,
        {},
      ),
    );
  }

  approveDesignTask(projectId: string, taskId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/approve-design`,
        {},
      ),
    );
  }

  submitDesignFeedback(
    projectId: string,
    taskId: string,
    request: { feedback: string; reviewer: string },
  ): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/design-feedback`,
        request,
      ),
    );
  }

  ignoreTask(projectId: string, taskId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(
        `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/ignore`,
        {},
      ),
    );
  }

  safeStopQueue(projectId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(`${this.boardUrl(projectId)}/queue/safe-stop`, {}),
    );
  }

  continueQueue(projectId: string): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.post<ProjectBoard>(`${this.boardUrl(projectId)}/queue/continue`, {}),
    );
  }

  updateTaskStatus(
    projectId: string,
    taskId: string,
    status: TaskStatus,
    blockedReason = '',
  ): Observable<ProjectBoard> {
    return this.boardResponse(
      this.http.patch<ProjectBoard>(
        `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/status`,
        { status, blockedReason },
      ),
    );
  }

  watchBoard(projectId: string): Observable<ProjectBoard> {
    return this.watchBoardStream(projectId).pipe(
      map((event) => event.board),
      filter((board): board is ProjectBoard => !!board),
    );
  }

  watchBoardStream(projectId: string): Observable<BoardStreamEvent> {
    return new Observable<BoardStreamEvent>((subscriber) => {
      const source = new EventSource(apiUrl(`${this.boardUrl(projectId)}/events`));
      source.onopen = () => subscriber.next({ state: 'connected' });
      source.addEventListener('board', (event) => {
        try {
          subscriber.next(
            {
              state: 'connected',
              board: this.normalizeBoard(JSON.parse((event as MessageEvent<string>).data) as ProjectBoard),
            },
          );
        } catch (error) {
          subscriber.error(error);
        }
      });
      source.onerror = () => {
        subscriber.next({ state: 'reconnecting' });
      };
      return () => source.close();
    });
  }

  watchTaskSession(projectId: string, taskId: string): Observable<TaskSessionLog> {
    return new Observable<TaskSessionLog>((subscriber) => {
      const source = new EventSource(
        apiUrl(
          `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/session/events`,
        ),
      );
      source.addEventListener('session', (event) => {
        try {
          const session = JSON.parse((event as MessageEvent<string>).data) as TaskSessionLog;
          subscriber.next({ ...session, entries: session.entries ?? [] });
        } catch (error) {
          subscriber.error(error);
        }
      });
      source.onerror = () => {
        // EventSource reconnects automatically; retain the last visible buffer.
      };
      return () => source.close();
    });
  }

  watchPlanningSession(projectId: string, planId: string): Observable<TaskSessionLog> {
    return new Observable<TaskSessionLog>((subscriber) => {
      const source = new EventSource(
        apiUrl(
          `${this.boardUrl(projectId)}/plans/${encodeURIComponent(planId)}/session/events`,
        ),
      );
      source.addEventListener('session', (event) => {
        try {
          const session = JSON.parse((event as MessageEvent<string>).data) as TaskSessionLog;
          subscriber.next({ ...session, entries: session.entries ?? [] });
        } catch (error) {
          subscriber.error(error);
        }
      });
      source.onerror = () => {
        // EventSource reconnects automatically; retain the last visible buffer.
      };
      return () => source.close();
    });
  }

  taskArtifactUrl(projectId: string, taskId: string, artifactId: string): string {
    return apiUrl(
      `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/artifacts/${encodeURIComponent(artifactId)}`,
    );
  }

  taskDesignSourceUrl(projectId: string, taskId: string, relativePath: string): string {
    return apiUrl(
      `${this.boardUrl(projectId)}/tasks/${encodeURIComponent(taskId)}/design-source?path=${encodeURIComponent(relativePath)}`,
    );
  }

  private boardUrl(projectId: string): string {
    return `/api/projects/${encodeURIComponent(projectId)}/board`;
  }

  private boardResponse(response: Observable<ProjectBoard>): Observable<ProjectBoard> {
    return response.pipe(map((board) => this.normalizeBoard(board)));
  }

  private normalizeBoard(board: ProjectBoard): ProjectBoard {
    return {
      ...board,
      backlog: (board.backlog ?? []).map((item) => ({
        ...item,
        requestId: item.requestId || `request-legacy-${item.id.replace(/^backlog-/, '')}`,
        acceptanceCriteria: item.acceptanceCriteria ?? [],
        attachments: item.attachments ?? [],
        waitingQaTaskIds: item.waitingQaTaskIds ?? [],
      })),
      plans: (board.plans ?? []).map((plan) => ({
        ...plan,
        request: {
          ...plan.request,
          acceptanceCriteria: plan.request.acceptanceCriteria ?? [],
          attachments: plan.request.attachments ?? [],
        },
        documents: (plan.documents ?? []).map((document) => ({
          ...document,
          audience: document.audience ?? [],
        })),
        reviews: plan.reviews ?? [],
      })),
      tasks: (board.tasks ?? []).map((task) => ({
        ...task,
        acceptanceCriteria: task.acceptanceCriteria ?? [],
        dependencyIds: task.dependencyIds ?? [],
        documentIds: task.documentIds ?? [],
        execution: task.execution
          ? {
              ...task.execution,
              changedFiles: task.execution.changedFiles ?? [],
              verification: task.execution.verification ?? [],
              remainingRisks: task.execution.remainingRisks ?? [],
              findings: task.execution.findings ?? [],
              artifacts: task.execution.artifacts ?? [],
            }
          : undefined,
        revisionHistory: (task.revisionHistory ?? []).map((revision) => ({
          ...revision,
          execution: {
            ...revision.execution,
            changedFiles: revision.execution.changedFiles ?? [],
            verification: revision.execution.verification ?? [],
            remainingRisks: revision.execution.remainingRisks ?? [],
            findings: revision.execution.findings ?? [],
            artifacts: revision.execution.artifacts ?? [],
          },
        })),
      })),
    };
  }
}
