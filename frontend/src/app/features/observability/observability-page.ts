import { DatePipe, DecimalPipe, JsonPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { AfterViewInit, Component, computed, ElementRef, HostListener, inject, OnDestroy, OnInit, signal, ViewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { firstValueFrom, Subscription } from 'rxjs';

import { ObservabilityApiService } from '../../core/observability-api.service';
import { AIModelComparison, AISessionOverview, AISessionRun, DistributionItem, ObservabilityOverview, ObservationEvent, TrendPoint } from '../../core/observability.models';
import { ProjectApiService } from '../../core/project-api.service';
import { ImportedProject } from '../../core/project.models';
import { AppShell } from '../../shared/app-shell/app-shell';

@Component({ selector: 'app-observability-page', imports: [AppShell, DatePipe, DecimalPipe, JsonPipe, FormsModule], templateUrl: './observability-page.html', styleUrl: './observability-page.css' })
export class ObservabilityPage implements OnInit, OnDestroy, AfterViewInit {
  private readonly api = inject(ObservabilityApiService);
  private readonly projectApi = inject(ProjectApiService);
  private readonly title = inject(Title);
  private stream?: Subscription;
  private refreshTimer?: ReturnType<typeof setTimeout>;
  private clockTimer?: ReturnType<typeof setInterval>;
  private sessionPageSizeTimer?: ReturnType<typeof setTimeout>;
  @ViewChild('sessionListCard') private sessionListCard?: ElementRef<HTMLElement>;
  @ViewChild('comparisonCard') private comparisonCard?: ElementRef<HTMLElement>;

  readonly projects = signal<ImportedProject[]>([]);
  readonly projectId = signal('');
  readonly days = signal(30);
  readonly overview = signal<ObservabilityOverview | null>(null);
  readonly aiSessions = signal<AISessionOverview | null>(null);
  readonly events = signal<ObservationEvent[]>([]);
  readonly loading = signal(true);
  readonly live = signal(false);
  readonly errorMessage = signal<string | null>(null);
  readonly level = signal('');
  readonly category = signal('');
  readonly correlationId = signal('');
  readonly expandedEventId = signal<string | null>(null);
  readonly expandedRunId = signal<string | null>(null);
  readonly clock = signal(Date.now());
  readonly sessionStatus = signal('all');
  readonly provider = signal('all');
  readonly sessionPageSize = signal(10);
  readonly sessionPage = signal(1);
  readonly comparisonPageSize = signal(3);
  readonly comparisonPage = signal(1);
  readonly tracePageSize = 20;
  readonly tracePage = signal(1);
  readonly filteredRuns = computed(() => (this.aiSessions()?.runs ?? []).filter((run) =>
    (this.sessionStatus() === 'all' || (this.sessionStatus() === 'active' ? run.status === 'running' || run.status === 'queued' : this.sessionStatus() === 'failed' ? run.status === 'failed' || run.status === 'interrupted' : run.status === this.sessionStatus())) &&
    (this.provider() === 'all' || run.provider === this.provider())));
  readonly totalSessionPages = computed(() => Math.max(1, Math.ceil(this.filteredRuns().length / this.sessionPageSize())));
  readonly pagedRuns = computed(() => {
    const page = this.currentSessionPage();
    const pageSize = this.sessionPageSize();
    const start = (page - 1) * pageSize;
    return this.filteredRuns().slice(start, start + pageSize);
  });
  readonly comparisons = computed(() => this.aiSessions()?.comparisons ?? []);
  readonly totalComparisonPages = computed(() => Math.max(1, Math.ceil(this.comparisons().length / this.comparisonPageSize())));
  readonly pagedComparisons = computed(() => {
    const page = this.currentComparisonPage();
    const pageSize = this.comparisonPageSize();
    const start = (page - 1) * pageSize;
    return this.comparisons().slice(start, start + pageSize);
  });
  readonly totalTracePages = computed(() => Math.max(1, Math.ceil(this.events().length / this.tracePageSize)));
  readonly pagedEvents = computed(() => {
    const page = this.currentTracePage();
    const start = (page - 1) * this.tracePageSize;
    return this.events().slice(start, start + this.tracePageSize);
  });
  readonly maxTrend = computed(() => Math.max(1, ...(this.overview()?.trend ?? []).map((point) => point.features + point.bugs + point.done)));

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Observability Center · ProductCrew');
    try {
      this.projects.set(await firstValueFrom(this.projectApi.listProjects()));
      await this.reload();
      this.stream = this.api.watch().subscribe({
        next: (event) => {
          this.live.set(true);
          if (this.matchesCurrentFilters(event)) {
            this.events.update((current) => [event, ...current].slice(0, 250));
            this.tracePage.set(this.currentTracePage());
          }
          if (event.category !== 'http') this.scheduleRefresh();
        },
        error: () => this.live.set(false),
      });
      this.live.set(true);
      this.clockTimer = setInterval(() => this.clock.set(Date.now()), 1000);
    } catch (error) { this.errorMessage.set(this.errorText(error, 'Could not load observability data.')); }
    finally { this.loading.set(false); }
  }

  ngAfterViewInit(): void { this.scheduleSessionPageSizeUpdate(); }
  ngOnDestroy(): void {
    this.stream?.unsubscribe();
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    if (this.clockTimer) clearInterval(this.clockTimer);
    if (this.sessionPageSizeTimer) clearTimeout(this.sessionPageSizeTimer);
  }
  @HostListener('window:resize')
  onWindowResize(): void { this.scheduleSessionPageSizeUpdate(); }
  async changeScope(): Promise<void> { this.sessionPage.set(1); this.comparisonPage.set(1); this.tracePage.set(1); await this.reload(); }
  async applyLogFilters(): Promise<void> { this.tracePage.set(1); await this.loadEvents(); }
  clearTraceFilters(): void { this.level.set(''); this.category.set(''); this.correlationId.set(''); this.tracePage.set(1); void this.loadEvents(); }
  traceCorrelation(event: ObservationEvent): void { if (!event.correlationId) return; this.correlationId.set(event.correlationId); this.tracePage.set(1); void this.loadEvents(); }
  toggleEvent(event: ObservationEvent): void { this.expandedEventId.set(this.expandedEventId() === event.id ? null : event.id); }
  total(items: DistributionItem[]): number { return items.reduce((sum, item) => sum + item.value, 0); }
  percent(item: DistributionItem, items: DistributionItem[]): number { const total = this.total(items); return total ? item.value / total * 100 : 0; }
  trendHeight(value: number): number { return Math.max(value ? 8 : 2, value / this.maxTrend() * 100); }
  shortDate(point: TrendPoint): string { return point.date.slice(5); }
  toggleRun(run: AISessionRun): void { this.expandedRunId.set(this.expandedRunId() === run.id ? null : run.id); }
  currentSessionPage(): number { return Math.min(this.sessionPage(), this.totalSessionPages()); }
  sessionPageStart(): number { return this.filteredRuns().length ? (this.currentSessionPage() - 1) * this.sessionPageSize() + 1 : 0; }
  sessionPageEnd(): number { return Math.min(this.currentSessionPage() * this.sessionPageSize(), this.filteredRuns().length); }
  setSessionStatus(value: string): void { this.sessionStatus.set(value); this.sessionPage.set(1); this.scheduleSessionPageSizeUpdate(); }
  setProvider(value: string): void { this.provider.set(value); this.sessionPage.set(1); this.scheduleSessionPageSizeUpdate(); }
  previousSessionPage(): void { this.sessionPage.set(Math.max(1, this.currentSessionPage() - 1)); }
  nextSessionPage(): void { this.sessionPage.set(Math.min(this.totalSessionPages(), this.currentSessionPage() + 1)); }
  currentComparisonPage(): number { return Math.min(this.comparisonPage(), this.totalComparisonPages()); }
  comparisonPageStart(): number { return this.comparisons().length ? (this.currentComparisonPage() - 1) * this.comparisonPageSize() + 1 : 0; }
  comparisonPageEnd(): number { return Math.min(this.currentComparisonPage() * this.comparisonPageSize(), this.comparisons().length); }
  previousComparisonPage(): void { this.comparisonPage.set(Math.max(1, this.currentComparisonPage() - 1)); }
  nextComparisonPage(): void { this.comparisonPage.set(Math.min(this.totalComparisonPages(), this.currentComparisonPage() + 1)); }
  currentTracePage(): number { return Math.min(this.tracePage(), this.totalTracePages()); }
  tracePageStart(): number { return this.events().length ? (this.currentTracePage() - 1) * this.tracePageSize + 1 : 0; }
  tracePageEnd(): number { return Math.min(this.currentTracePage() * this.tracePageSize, this.events().length); }
  previousTracePage(): void { this.tracePage.set(Math.max(1, this.currentTracePage() - 1)); }
  nextTracePage(): void { this.tracePage.set(Math.min(this.totalTracePages(), this.currentTracePage() + 1)); }
  providers(): string[] { return [...new Set((this.aiSessions()?.runs ?? []).map((run) => run.provider))].sort(); }
  duration(ms: number): string {
    const seconds = Math.max(0, Math.round(ms / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60); const remaining = seconds % 60;
    return minutes < 60 ? `${minutes}m ${remaining}s` : `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
  }
  runDuration(run: AISessionRun): string {
    this.clock();
    return this.duration(run.status === 'running' || run.status === 'queued' ? Date.now() - new Date(run.startedAt).getTime() : run.durationMs);
  }
  tokens(run: AISessionRun): string { return run.usage.totalTokens == null ? 'Not reported' : run.usage.totalTokens.toLocaleString(); }
  comparisonWidth(item: AIModelComparison): number { return Math.max(8, item.successRate); }
  roleIcon(agent?: string): string { return agent === 'designer' ? 'palette' : agent === 'qa' ? 'verified' : agent === 'team-lead' ? 'account_tree' : agent?.includes('radar') ? 'radar' : agent?.includes('scan') ? 'bug_report' : 'terminal'; }

  private scheduleRefresh(): void {
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.refreshTimer = setTimeout(() => void this.reload(false), 650);
  }

  private async reload(showLoading = true): Promise<void> {
    if (showLoading) this.loading.set(true);
    this.errorMessage.set(null);
    try {
      const [overview, aiSessions, events] = await Promise.all([
        firstValueFrom(this.api.overview(this.projectId(), this.days())),
        firstValueFrom(this.api.aiSessions(this.projectId(), this.days())),
        firstValueFrom(this.api.events(this.filters())),
      ]);
      this.overview.set(overview); this.aiSessions.set(aiSessions); this.events.set(events); this.sessionPage.set(this.currentSessionPage()); this.comparisonPage.set(this.currentComparisonPage()); this.tracePage.set(this.currentTracePage()); this.scheduleSessionPageSizeUpdate();
    } catch (error) { this.errorMessage.set(this.errorText(error, 'Could not refresh observability data.')); }
    finally { if (showLoading) this.loading.set(false); }
  }

  private async loadEvents(): Promise<void> {
    try { this.events.set(await firstValueFrom(this.api.events(this.filters()))); this.tracePage.set(this.currentTracePage()); }
    catch (error) { this.errorMessage.set(this.errorText(error, 'Could not filter trace events.')); }
  }

  private filters() { return { projectId: this.projectId(), days: this.days(), level: this.level(), category: this.category(), correlationId: this.correlationId().trim() }; }
  private matchesCurrentFilters(event: ObservationEvent): boolean {
    return (!this.projectId() || event.projectId === this.projectId()) &&
      (!this.level() || event.level === this.level()) && (!this.category() || event.category === this.category()) &&
      (!this.correlationId().trim() || event.correlationId === this.correlationId().trim());
  }
  private scheduleSessionPageSizeUpdate(): void {
    if (this.sessionPageSizeTimer) clearTimeout(this.sessionPageSizeTimer);
    this.sessionPageSizeTimer = setTimeout(() => this.updateSessionPageSize(), 0);
  }
  private updateSessionPageSize(): void {
    const card = this.sessionListCard?.nativeElement;
    if (!card || typeof window === 'undefined') return;
    const viewportBottomPadding = 32;
    const availableHeight = window.innerHeight - card.getBoundingClientRect().top - viewportBottomPadding;
    const cardHeaderHeight = card.querySelector('header')?.getBoundingClientRect().height ?? 72;
    const tableHeadHeight = card.querySelector('.session-head')?.getBoundingClientRect().height ?? 36;
    const paginationHeight = card.querySelector('.session-pagination')?.getBoundingClientRect().height ?? 52;
    const rowHeight = card.querySelector('.session-row:not(.expanded)')?.getBoundingClientRect().height || 68;
    const usableHeight = availableHeight - cardHeaderHeight - tableHeadHeight - paginationHeight - 2;
    const nextPageSize = Math.max(4, Math.min(14, Math.floor(usableHeight / rowHeight)));
    let sessionPageSizeChanged = false;
    if (Number.isFinite(nextPageSize) && nextPageSize !== this.sessionPageSize()) {
      this.sessionPageSize.set(nextPageSize);
      this.sessionPage.set(this.currentSessionPage());
      sessionPageSizeChanged = true;
    }
    this.updateComparisonPageSize(card);
    if (sessionPageSizeChanged) this.scheduleSessionPageSizeUpdate();
  }
  private updateComparisonPageSize(sessionCard: HTMLElement): void {
    const card = this.comparisonCard?.nativeElement;
    const total = this.comparisons().length;
    if (!card || total === 0) return;
    const targetHeight = this.sessionContentHeight(sessionCard);
    const headerHeight = card.querySelector('header')?.getBoundingClientRect().height ?? 72;
    const paginationHeight = total > 1 ? card.querySelector('.comparison-pagination')?.getBoundingClientRect().height ?? 52 : 0;
    const rowHeight = card.querySelector('.comparison-list article')?.getBoundingClientRect().height || 104;
    const usableHeight = targetHeight - headerHeight - paginationHeight - 2;
    const nextPageSize = Math.max(1, Math.min(8, Math.floor(usableHeight / rowHeight)));
    if (Number.isFinite(nextPageSize) && nextPageSize > 0 && nextPageSize !== this.comparisonPageSize()) {
      this.comparisonPageSize.set(nextPageSize);
      this.comparisonPage.set(this.currentComparisonPage());
    }
  }
  private sessionContentHeight(sessionCard: HTMLElement): number {
    const headerHeight = sessionCard.querySelector('header')?.getBoundingClientRect().height ?? 72;
    const tableHeadHeight = sessionCard.querySelector('.session-head')?.getBoundingClientRect().height ?? 36;
    const rowHeights = Array.from(sessionCard.querySelectorAll<HTMLElement>('.session-row')).reduce((sum, row) => sum + row.getBoundingClientRect().height, 0);
    const emptyHeight = sessionCard.querySelector('.session-empty')?.getBoundingClientRect().height ?? 0;
    const paginationHeight = sessionCard.querySelector('.session-pagination')?.getBoundingClientRect().height ?? 0;
    return headerHeight + tableHeadHeight + Math.max(rowHeights, emptyHeight) + paginationHeight + 2;
  }
  private errorText(error: unknown, fallback: string): string { return (error as HttpErrorResponse).error?.error ?? fallback; }
}
