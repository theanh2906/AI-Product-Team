import { DatePipe } from '@angular/common';
import { Component, DestroyRef, EventEmitter, HostListener, Input, OnInit, Output, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { GitStatusApiService } from '../../core/git-status-api.service';
import { GitDelivery, GitDeliveryPreview, GitDeliveryStep } from '../../core/git-status.models';

@Component({
  selector: 'app-git-delivery-modal',
  imports: [DatePipe],
  templateUrl: './git-delivery-modal.html',
})
export class GitDeliveryModal implements OnInit {
  @Input({ required: true }) projectId = '';
  @Input() projectName = '';
  @Output() closed = new EventEmitter<void>();
  @Output() delivered = new EventEmitter<void>();

  private readonly api = inject(GitStatusApiService);
  private readonly destroyRef = inject(DestroyRef);
  private pollHandle?: number;

  readonly preview = signal<GitDeliveryPreview | null>(null);
  readonly delivery = signal<GitDelivery | null>(null);
  readonly loading = signal(true);
  readonly actionPending = signal(false);
  readonly error = signal<string | null>(null);

  constructor() {
    this.destroyRef.onDestroy(() => this.pollHandle && window.clearTimeout(this.pollHandle));
  }

  ngOnInit(): void { void this.load(); }

  @HostListener('document:keydown.escape')
  close(): void { this.closed.emit(); }

  async start(): Promise<void> {
    const preview = this.preview();
    if (!preview?.ready || this.actionPending()) return;
    await this.runAction(() => firstValueFrom(this.api.startDelivery(this.projectId, preview.fingerprint)));
  }

  async stop(): Promise<void> {
    const delivery = this.delivery();
    if (!delivery || this.actionPending()) return;
    await this.runAction(() => firstValueFrom(this.api.stopDelivery(this.projectId, delivery.id)));
  }

  async retry(): Promise<void> {
    const delivery = this.delivery();
    if (!delivery || this.actionPending()) return;
    await this.runAction(() => firstValueFrom(this.api.retryDelivery(this.projectId, delivery.id)));
  }

  async refresh(): Promise<void> { await this.load(); }

  isActive(delivery = this.delivery()): boolean {
    return !!delivery && ['queued', 'running', 'recovering'].includes(delivery.status);
  }

  isDone(step: GitDeliveryStep): boolean {
    const delivery = this.delivery();
    if (!delivery) return false;
    const order: GitDeliveryStep[] = ['preflight', 'commit', 'push', 'complete'];
    return delivery.status === 'completed' || order.indexOf(delivery.step) > order.indexOf(step) || (step === 'commit' && !!delivery.commitSha);
  }

  isCurrent(step: GitDeliveryStep): boolean { return this.delivery()?.step === step && this.delivery()?.status !== 'completed'; }

  stepLabel(step: GitDeliveryStep): string {
    return { preflight: 'Preflight', commit: 'Commit', push: 'Push', complete: 'Complete' }[step];
  }

  ticketFileCount(): number { return this.delivery()?.files.length ?? this.preview()?.files.length ?? 0; }

  private async load(): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      const preview = await firstValueFrom(this.api.getDeliveryPreview(this.projectId));
      this.preview.set(preview);
      this.delivery.set(preview.latestDelivery ?? null);
      this.schedulePoll();
    } catch (error) {
      this.error.set(this.errorMessage(error));
    } finally {
      this.loading.set(false);
    }
  }

  private async runAction(action: () => Promise<GitDelivery>): Promise<void> {
    this.actionPending.set(true);
    this.error.set(null);
    try {
      this.delivery.set(await action());
      this.schedulePoll();
    } catch (error) {
      this.error.set(this.errorMessage(error));
    } finally {
      this.actionPending.set(false);
    }
  }

  private schedulePoll(): void {
    if (this.pollHandle) window.clearTimeout(this.pollHandle);
    if (!this.isActive()) return;
    const deliveryId = this.delivery()!.id;
    this.pollHandle = window.setTimeout(async () => {
      try {
        const next = await firstValueFrom(this.api.getDelivery(this.projectId, deliveryId));
        this.delivery.set(next);
        if (next.status === 'completed') this.delivered.emit();
      } catch (error) {
        this.error.set(this.errorMessage(error));
      }
      this.schedulePoll();
    }, 1200);
  }

  private errorMessage(error: unknown): string {
    const candidate = error as { error?: { error?: string }; message?: string };
    return candidate?.error?.error || candidate?.message || 'Git delivery could not be updated.';
  }
}
