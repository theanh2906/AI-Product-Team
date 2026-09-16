import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { vi } from 'vitest';

import { ProjectApiService } from '../../core/project-api.service';
import { SourceScanApiService } from '../../core/source-scan-api.service';
import { SourceScanJob } from '../../core/source-scan.models';
import { SourceScanPage } from './source-scan-page';

describe('SourceScanPage', () => {
  let latestScan: SourceScanJob | null;
  let latestResult: NonNullable<SourceScanJob['result']>;
  let updateFinding: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    localStorage.clear();
    latestScan = null;
    latestResult = undefined as never;
    updateFinding = vi.fn((_projectId: string, findingId: string, action: 'plan' | 'backlog' | 'ignore') =>
      of({
        result: {
          ...latestResult,
          findings: latestResult.findings.map((finding) =>
            finding.id === findingId ? { ...finding, status: action === 'plan' ? 'planning' : action } : finding,
          ),
        },
      }),
    );
    await TestBed.configureTestingModule({
      imports: [SourceScanPage],
      providers: [
        provideRouter([]),
        { provide: ProjectApiService, useValue: { getSettings: () => of({ theme: 'light' }), listProjects: () => of([{ id: 'p1', name: 'app', path: 'C:\\app', source: 'local', importedAt: '' }]) } },
        { provide: SourceScanApiService, useValue: { getLatestScan: () => of(latestScan), startScan: () => of({ scanId: 'SCAN-1', projectId: 'p1', projectName: 'app', status: 'running', startedAt: '' }), watchScan: () => of({ scanId: 'SCAN-1', projectId: 'p1', projectName: 'app', status: 'completed', startedAt: '', result: { scanId: 'SCAN-1', projectId: 'p1', projectName: 'app', scannedAt: '', durationMs: 10, filesNote: 'Entire repository', summary: 'Review complete.', findings: [{ id: 'low', severity: 'low', category: 'Quality', title: 'Low issue', description: 'Description', evidence: 'Evidence', file: 'a.go', line: 1, recommendation: 'Fix it', status: 'suggested' }, { id: 'high', severity: 'high', category: 'Correctness', title: 'High issue', description: 'Description', evidence: 'Evidence', file: 'b.go', line: 2, recommendation: 'Fix it', status: 'suggested' }] } }), updateFinding } },
      ],
    }).compileComponents();
  });

  it('loads the first imported project and scans it', async () => {
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.startScan();

    expect(component.selectedProject()?.id).toBe('p1');
    expect(component.activeFindingCount()).toBe(2);
    expect(component.countSeverity('high')).toBe(1);
  });

  it('selects every review focus by default', () => {
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;

    expect([...component.focusAreas()].sort()).toEqual([
      'correctness',
      'maintainability',
      'performance',
      'reliability',
      'security',
    ]);
  });

  it('can ignore a finding from the active list', async () => {
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.startScan();
    latestResult = component.result()!;
    await component.updateFinding(component.result()!.findings[0], 'ignore');

    expect(component.activeFindingCount()).toBe(1);
  });

  it('sends the plan action, removes the finding from the active list, and shows the success notice', async () => {
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.startScan();
    latestResult = component.result()!;
    const finding = component.result()!.findings[0];

    await component.updateFinding(finding, 'plan');
    fixture.detectChanges();

    expect(updateFinding).toHaveBeenCalledWith('p1', finding.id, 'plan');
    expect(component.activeFindingCount()).toBe(1);
    expect(component.successMessage()).toContain(finding.title);
    expect(fixture.nativeElement.textContent).toContain('Plan now started');
  });

  it('shows the Plan now button as loading and disables the other actions on that card', async () => {
    const pending = new Subject<{ result: unknown }>();
    updateFinding.mockReturnValue(pending);
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.startScan();
    latestResult = component.result()!;
    const finding = component.result()!.findings[0];

    const request = component.updateFinding(finding, 'plan');
    fixture.detectChanges();

    const buttons: NodeListOf<HTMLButtonElement> = (fixture.nativeElement as HTMLElement).querySelectorAll('.finding-actions button');
    expect(buttons[0].textContent).toContain('Starting plan…');
    expect(buttons[0].disabled).toBe(true);
    expect(buttons[1].disabled).toBe(true);
    expect(buttons[2].disabled).toBe(true);

    pending.next({
      result: { ...latestResult, findings: latestResult.findings.map((item) => item.id === finding.id ? { ...item, status: 'planning' } : item) },
    });
    pending.complete();
    await request;
  });

  it('keeps the finding active and shows the plan-specific error on failure', async () => {
    updateFinding.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 500, error: {} })));
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.startScan();
    latestResult = component.result()!;
    const finding = component.result()!.findings[0];

    await component.updateFinding(finding, 'plan');
    fixture.detectChanges();

    expect(component.errorMessage()).toBe('Could not start planning for this finding.');
    expect(component.successMessage()).toBeNull();
    expect(component.activeFindingCount()).toBe(2);
  });

  it('restores the latest scan without browser-local state', async () => {
    latestScan = { scanId: 'SCAN-1', projectId: 'p1', projectName: 'app', status: 'running', startedAt: '' };
    const fixture = TestBed.createComponent(SourceScanPage);
    const component = fixture.componentInstance;

    await component.ngOnInit();

    expect(component.activeScanId()).toBe('SCAN-1');
    expect(component.result()?.scanId).toBe('SCAN-1');
    expect(component.scanning()).toBe(false);
  });
});
