import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { invoke } from '@tauri-apps/api/core';

import { AppUpdateService } from './app-update.service';
import { DesktopWindowService } from './desktop-window.service';
import { ProjectApiService } from './project-api.service';
import { ProjectBoard } from './work-item.models';

vi.mock('@tauri-apps/api/core', () => ({
  invoke: vi.fn(),
}));

describe('AppUpdateService', () => {
  let service: AppUpdateService;
  let httpMock: HttpTestingController;
  const invokeMock = vi.mocked(invoke);

  beforeEach(() => {
    invokeMock.mockReset();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: DesktopWindowService, useValue: { isDesktop: true } },
        { provide: ProjectApiService, useValue: { getSettings: vi.fn() } },
      ],
    });
    service = TestBed.inject(AppUpdateService);
    httpMock = TestBed.inject(HttpTestingController);
    setUpdatePath(service, 'C:\\Tools\\updates');
  });

  afterEach(() => {
    httpMock.verify();
    vi.restoreAllMocks();
  });

  it('asks for confirmation and cancels installer launch when an AI task is active', async () => {
    const pending = service.install();
    httpMock.expectOne('/api/boards').flush([boardWithTask('in_progress')]);
    await flushMicrotasks();

    expect(service.pendingInstallConfirmation()).toEqual({
      key: 'DEV-001',
      title: '[Developer] Implement update guard',
    });
    service.cancelInstallConfirmation();
    await pending;

    expect(service.pendingInstallConfirmation()).toBeNull();
    expect(invokeMock).not.toHaveBeenCalled();
    expect(service.installing()).toBe(false);
  });

  it('keeps the existing install flow when the user accepts the active-task warning', async () => {
    invokeMock.mockResolvedValue(undefined);

    const pending = service.install();
    httpMock.expectOne('/api/boards').flush([boardWithTask('verifying')]);
    await flushMicrotasks();

    expect(service.pendingInstallConfirmation()?.key).toBe('DEV-001');
    service.confirmInstall();
    await pending;

    expect(service.pendingInstallConfirmation()).toBeNull();
    expect(invokeMock).toHaveBeenCalledWith('install_update', { updatePath: 'C:\\Tools\\updates' });
  });

  it('does not show a confirmation when no task is running', async () => {
    invokeMock.mockResolvedValue(undefined);

    const pending = service.install();
    httpMock.expectOne('/api/boards').flush([boardWithTask('queued')]);
    await pending;

    expect(service.pendingInstallConfirmation()).toBeNull();
    expect(invokeMock).toHaveBeenCalledWith('install_update', { updatePath: 'C:\\Tools\\updates' });
  });

  it('keeps update behavior unchanged if board status cannot be checked', async () => {
    invokeMock.mockResolvedValue(undefined);

    const pending = service.install();
    httpMock.expectOne('/api/boards').flush(null, { status: 500, statusText: 'Internal Server Error' });
    await pending;

    expect(service.pendingInstallConfirmation()).toBeNull();
    expect(invokeMock).toHaveBeenCalledWith('install_update', { updatePath: 'C:\\Tools\\updates' });
  });
});

function setUpdatePath(service: AppUpdateService, updatePath: string): void {
  (service as unknown as { updatePath: string }).updatePath = updatePath;
}

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

function boardWithTask(status: ProjectBoard['tasks'][number]['status']): ProjectBoard {
  return {
    id: 'board-1',
    projectId: 'project-1',
    projectName: 'ProductCrew',
    backlog: [],
    plans: [],
    tasks: [
      {
        id: 'task-1',
        planId: 'plan-1',
        key: 'DEV-001',
        title: '[Developer] Implement update guard',
        role: 'developer',
        column: 'developer',
        status,
        priority: 'high',
        description: 'Implement the active task update warning.',
        acceptanceCriteria: [],
        dependencyIds: [],
        documentIds: [],
        createdAt: '2026-08-27T00:00:00Z',
        updatedAt: '2026-08-27T00:00:00Z',
      },
    ],
    createdAt: '2026-08-27T00:00:00Z',
    updatedAt: '2026-08-27T00:00:00Z',
  };
}
