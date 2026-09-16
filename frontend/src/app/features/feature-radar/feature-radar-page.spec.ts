import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { vi } from 'vitest';

import { FeatureRadarApiService } from '../../core/feature-radar-api.service';
import { FeatureRadarResult } from '../../core/feature-radar.models';
import { ProjectApiService } from '../../core/project-api.service';
import { FeatureRadarPage } from './feature-radar-page';

describe('FeatureRadarPage', () => {
  let latestResult: FeatureRadarResult;
  let update: ReturnType<typeof vi.fn>;

  const baseResult: FeatureRadarResult = {
    analysisId: 'RADAR-1',
    projectId: 'p1',
    projectName: 'app',
    analyzedAt: '',
    durationMs: 10,
    scopeNote: 'Entire repository',
    summary: 'Analysis complete.',
    suggestions: [
      {
        id: 'sugg-1',
        category: 'Growth',
        title: 'Plan Now',
        problem: 'Problem',
        proposal: 'Proposal',
        rationale: 'Rationale',
        feasibility: 80,
        confidence: 'high',
        impact: 'high',
        effort: 'small',
        requiresUI: true,
        deliveryTarget: 'fullstack',
        evidence: [],
        implementationOutline: [],
        acceptanceCriteria: [],
        risks: [],
        status: 'suggested',
      },
    ],
  };

  beforeEach(async () => {
    latestResult = baseResult;
    update = vi.fn((_projectId: string, suggestionId: string, action: 'plan' | 'backlog' | 'ignore') =>
      of({
        result: {
          ...latestResult,
          suggestions: latestResult.suggestions.map((item) =>
            item.id === suggestionId ? { ...item, status: action === 'plan' ? 'planning' : action } : item,
          ),
        },
      }),
    );
    await TestBed.configureTestingModule({
      imports: [FeatureRadarPage],
      providers: [
        provideRouter([]),
        { provide: ProjectApiService, useValue: { getSettings: () => of({ theme: 'light' }), listProjects: () => of([{ id: 'p1', name: 'app', path: 'C:\\app', source: 'local', importedAt: '' }]) } },
        {
          provide: FeatureRadarApiService,
          useValue: {
            getLatest: () => of(null),
            start: () => of({ analysisId: 'RADAR-1', projectId: 'p1', projectName: 'app', status: 'running', startedAt: '' }),
            watch: () => of({ analysisId: 'RADAR-1', projectId: 'p1', projectName: 'app', status: 'completed', startedAt: '', result: baseResult }),
            update,
          },
        },
      ],
    }).compileComponents();
  });

  it('analyzes the active project and lists active suggestions', async () => {
    const fixture = TestBed.createComponent(FeatureRadarPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.analyze();

    expect(component.suggestions().length).toBe(1);
    expect(component.suggestions()[0].title).toBe('Plan Now');
  });

  it('sends the plan action, removes the suggestion from the active list, and shows the success notice', async () => {
    const fixture = TestBed.createComponent(FeatureRadarPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.analyze();
    latestResult = component.result()!;
    const suggestion = component.result()!.suggestions[0];

    await component.action(suggestion, 'plan');
    fixture.detectChanges();

    expect(update).toHaveBeenCalledWith('p1', suggestion.id, 'plan');
    expect(component.suggestions().length).toBe(0);
    expect(component.successMessage()).toContain(suggestion.title);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Plan now started');
  });

  it('shows the Plan now button as loading and disables the other actions on that card', async () => {
    const pending = new Subject<{ result: unknown }>();
    update.mockReturnValue(pending);
    const fixture = TestBed.createComponent(FeatureRadarPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.analyze();
    latestResult = component.result()!;
    const suggestion = component.result()!.suggestions[0];

    const request = component.action(suggestion, 'plan');
    fixture.detectChanges();

    const buttons: NodeListOf<HTMLButtonElement> = (fixture.nativeElement as HTMLElement).querySelectorAll('.finding-actions button');
    expect(buttons[0].textContent).toContain('Starting plan…');
    expect(buttons[0].disabled).toBe(true);
    expect(buttons[1].disabled).toBe(true);
    expect(buttons[2].disabled).toBe(true);

    pending.next({
      result: { ...latestResult, suggestions: latestResult.suggestions.map((item) => item.id === suggestion.id ? { ...item, status: 'planning' } : item) },
    });
    pending.complete();
    await request;
  });

  it('keeps the suggestion active and shows the plan-specific error on failure', async () => {
    update.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 500, error: {} })));
    const fixture = TestBed.createComponent(FeatureRadarPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    await component.analyze();
    latestResult = component.result()!;
    const suggestion = component.result()!.suggestions[0];

    await component.action(suggestion, 'plan');
    fixture.detectChanges();

    expect(component.errorMessage()).toBe('Could not start planning for this suggestion.');
    expect(component.successMessage()).toBeNull();
    expect(component.suggestions().length).toBe(1);
  });
});
