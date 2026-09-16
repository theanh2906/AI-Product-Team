import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';
import { vi } from 'vitest';

import { ProjectApiService } from '../../core/project-api.service';
import { ProjectIntelligenceApiService } from '../../core/project-intelligence-api.service';
import { ProjectStudyJob, ProjectStudyResult } from '../../core/project.models';
import { ProjectAtlasPage } from './project-atlas-page';

describe('ProjectAtlasPage', () => {
  const result: ProjectStudyResult = {
    schemaVersion: 1,
    studyId: 'STUDY-1',
    projectId: 'p1',
    projectName: 'sample',
    projectPath: 'C:\\sample',
    studiedAt: '2026-09-11T00:00:00Z',
    durationMs: 1200,
    status: 'completed',
    freshness: 'fresh',
    fingerprint: 'abc123',
    summary: 'Angular UI connected to a Go local service.',
    scopeNote: 'Source and configuration only.',
    coverage: { filesScanned: 120, foldersMapped: 4, evidenceSources: 7, ignoredEntries: 3 },
    folders: [{ name: 'frontend', path: 'frontend', kind: 'frontend', children: ['frontend/src'] }],
    components: [{ id: 'frontend', name: 'Angular UI', kind: 'frontend', description: 'Desktop interface', path: 'frontend', entryFiles: ['frontend/src/main.ts'], responsibilities: ['Render project workflows'], evidence: [{ file: 'frontend/package.json', line: 1, detail: 'Angular dependency' }], confidence: 94 }],
    relations: [],
    dataEntities: [],
    sequences: [{
      id: 'request-flow',
      name: 'Request to board',
      description: 'A request is planned and persisted to the local board.',
      steps: [
        { order: 1, from: 'Angular UI', to: 'Go API', label: 'Submit request' },
        { order: 2, from: 'Go API', to: 'Board Store', label: 'Persist backlog card' },
      ],
      evidence: [{ file: 'internal/web/board_handlers.go', line: 10, detail: 'Board API handler' }],
    }],
    workflows: [{
      id: 'delivery-flow',
      name: 'Delivery flow',
      description: 'ProductCrew moves a request through planning, implementation, and QA.',
      steps: ['Capture request', 'Approve plan', 'Run implementation', 'Verify QA'],
      evidence: [{ file: 'internal/web/task_queue.go', line: 10, detail: 'Task queue dispatch' }],
    }],
    roleGuidance: [{ role: 'developer', summary: 'Use Angular and Go checks.', instructions: 'Run npm test and go test ./...', confidence: 91, evidence: [] }],
    guidanceApplied: false,
  };
  const completedJob: ProjectStudyJob = { studyId: 'STUDY-1', projectId: 'p1', projectName: 'sample', status: 'completed', phase: 'ready', progress: 100, startedAt: result.studiedAt, completedAt: result.studiedAt, logs: [], result };
  let latestJob: ProjectStudyJob;
  let start: ReturnType<typeof vi.fn>;
  let applyGuidance: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    vi.stubGlobal('ResizeObserver', class ResizeObserver {
      observe(): void {}
      unobserve(): void {}
      disconnect(): void {}
    });
    latestJob = completedJob;
    start = vi.fn(() => of({ ...completedJob, status: 'running' as const, progress: 5, result: undefined }));
    applyGuidance = vi.fn(() => of({ result: { ...result, guidanceApplied: true }, profile: {} }));
    await TestBed.configureTestingModule({
      imports: [ProjectAtlasPage],
      providers: [
        provideRouter([]),
        { provide: ProjectApiService, useValue: { getSettings: () => of({ theme: 'dark' }), listProjects: () => of([{ id: 'p1', name: 'sample', path: 'C:\\sample', source: 'local', importedAt: '' }]) } },
        { provide: ProjectIntelligenceApiService, useValue: { getLatest: () => of(latestJob), start, watch: () => of(completedJob), applyGuidance } },
      ],
    }).compileComponents();
  });

  it('renders cached architecture and switches knowledge lenses without another fetch', async () => {
    const fixture = TestBed.createComponent(ProjectAtlasPage);
    await fixture.componentInstance.ngOnInit();
    fixture.detectChanges();

    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Angular UI');
    expect(fixture.componentInstance.lensCount('folders')).toBe(1);
    fixture.componentInstance.setLens('folders');
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('frontend/src');
    fixture.componentInstance.setLens('sequences');
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).querySelector('.sequence-diagram')).toBeTruthy();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Submit request');
    fixture.componentInstance.setLens('workflows');
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).querySelector('.workflow-graph')).toBeTruthy();
    expect((fixture.nativeElement as HTMLElement).querySelector('f-flow.workflow-flow')).toBeTruthy();
    expect((fixture.nativeElement as HTMLElement).querySelector('f-canvas')).toBeTruthy();
    expect((fixture.nativeElement as HTMLElement).querySelectorAll('f-connection')).toHaveLength(3);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Verify QA');
  });

  it('streams a new study and applies learned guidance explicitly', async () => {
    const fixture = TestBed.createComponent(ProjectAtlasPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.runStudy();
    await component.applyGuidance();

    expect(start).toHaveBeenCalledWith('p1');
    expect(applyGuidance).toHaveBeenCalledWith('p1', 'STUDY-1');
    expect(component.result()?.guidanceApplied).toBe(true);
    fixture.destroy();
  });

  it('shows the cached AI enrichment error for a partial study', async () => {
    latestJob = {
      ...completedJob,
      status: 'needs_attention',
      phase: 'partial',
      error: undefined,
      result: {
        ...result,
        status: 'needs_attention',
        enrichmentError: 'Codex project study failed: selected model is unavailable',
      },
    };

    const fixture = TestBed.createComponent(ProjectAtlasPage);
    await fixture.componentInstance.ngOnInit();
    fixture.detectChanges();

    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'Codex project study failed: selected model is unavailable',
    );
    expect((fixture.nativeElement as HTMLElement).textContent).not.toContain(
      'AI enrichment was limited. Folder Map is still available.',
    );
  });

  it('renders folder cards when legacy snapshots omit empty children arrays', async () => {
    latestJob = {
      ...completedJob,
      result: {
        ...result,
        coverage: { ...result.coverage, foldersMapped: 3 },
        folders: [
          { name: '.codex', path: '.codex', kind: 'source', children: ['.codex/agents'] },
          { name: 'scripts', path: 'scripts', kind: 'source' } as ProjectStudyResult['folders'][number],
          { name: '', path: 'cmd', kind: 'source' } as ProjectStudyResult['folders'][number],
        ],
      },
    };

    const fixture = TestBed.createComponent(ProjectAtlasPage);
    await fixture.componentInstance.ngOnInit();
    fixture.componentInstance.setLens('folders');
    fixture.componentInstance.selectFolder(latestJob.result!.folders[1]);
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    const folderCards = Array.from(host.querySelectorAll<HTMLButtonElement>('.folder-map > button'));
    expect(folderCards).toHaveLength(3);
    expect(folderCards.map((card) => card.querySelector('strong')?.textContent?.trim())).toEqual(['.codex', 'scripts', 'cmd']);
    expect(folderCards.map((card) => card.querySelector('small')?.textContent?.trim())).toEqual(['.codex', 'scripts', 'cmd']);
    expect(folderCards.map((card) => card.querySelector('b')?.textContent?.trim())).toEqual(['1', '0', '0']);
    expect(host.textContent).toContain('No child folders at the indexed depth.');
  });

  it('lays workflow stages out as a two-column Z path', () => {
    const fixture = TestBed.createComponent(ProjectAtlasPage);
    const component = fixture.componentInstance;

    expect([0, 1, 2, 3, 4].map((index) => [component.workflowNodeRow(index), component.workflowNodeColumn(index)])).toEqual([
      [1, 1],
      [1, 2],
      [2, 2],
      [2, 1],
      [3, 1],
    ]);
    expect([0, 1, 2, 3, 4].map((index) => component.workflowNodeDirection(index, 5))).toEqual([
      'right',
      'down',
      'left',
      'down',
      'none',
    ]);
  });

  it('opens the selected diagram in a fullscreen overlay and keeps zoom bounded', async () => {
    const fixture = TestBed.createComponent(ProjectAtlasPage);
    await fixture.componentInstance.ngOnInit();
    fixture.componentInstance.setLens('workflows');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    const fullscreenButton = host.querySelector<HTMLButtonElement>('button[aria-label="Open diagram fullscreen"]');
    expect(fullscreenButton).toBeTruthy();

    fullscreenButton?.click();
    fixture.detectChanges();

    expect(fixture.componentInstance.diagramFullscreenOpen()).toBe(true);
    expect(host.querySelector('.atlas-fullscreen-modal')).toBeTruthy();
    expect(host.querySelectorAll('.workflow-graph')).toHaveLength(2);

    for (let index = 0; index < 10; index += 1) fixture.componentInstance.zoomDiagram(10);
    expect(fixture.componentInstance.diagramZoom()).toBe(140);
    for (let index = 0; index < 12; index += 1) fixture.componentInstance.zoomDiagram(-10);
    expect(fixture.componentInstance.diagramZoom()).toBe(70);
    fixture.componentInstance.resetDiagramView();
    expect(fixture.componentInstance.diagramZoom()).toBe(100);

    fixture.componentInstance.closeDiagramFullscreen();
    fixture.detectChanges();

    expect(fixture.componentInstance.diagramFullscreenOpen()).toBe(false);
    expect(host.querySelector('.atlas-fullscreen-modal')).toBeNull();
  });

  it('keeps user-moved workflow node positions in app-owned state', async () => {
    const fixture = TestBed.createComponent(ProjectAtlasPage);
    await fixture.componentInstance.ngOnInit();
    fixture.componentInstance.setLens('workflows');

    const movedNodeId = 'workflow-delivery-flow-1';
    fixture.componentInstance.onWorkflowNodesMoved({ nodes: [{ id: movedNodeId, position: { x: 512, y: 220 } }], fNodes: [] } as unknown as Parameters<ProjectAtlasPage['onWorkflowNodesMoved']>[0]);

    expect(fixture.componentInstance.selectedWorkflowNodes().find((node) => node.id === movedNodeId)?.position).toEqual({ x: 512, y: 220 });
  });
});
