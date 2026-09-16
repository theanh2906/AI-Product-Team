import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ProjectApiService } from '../../core/project-api.service';
import { ProjectContextService } from '../../core/project-context.service';
import {
  ProductBlueprint,
  ProductCreationProfile,
  ProjectSettings,
} from '../../core/project.models';
import { AppShell } from '../../shared/app-shell/app-shell';

type StartingPoint = 'blank' | 'guided' | 'template';

@Component({
  selector: 'app-create-product-page',
  imports: [ReactiveFormsModule, RouterLink, AppShell],
  templateUrl: './create-product-page.html',
})
export class CreateProductPage implements OnInit {
  private readonly formBuilder = inject(FormBuilder);
  private readonly projectApi = inject(ProjectApiService);
  private readonly projectContext = inject(ProjectContextService);
  private readonly router = inject(Router);
  private readonly title = inject(Title);

  readonly profiles = signal<ProductCreationProfile[]>([]);
  readonly settings = signal<ProjectSettings | null>(null);
  readonly startingPoint = signal<StartingPoint>('blank');
  readonly blueprint = signal<ProductBlueprint | null>(null);
  readonly loading = signal(true);
  readonly shaping = signal(false);
  readonly creating = signal(false);
  readonly error = signal<string | null>(null);

  readonly form = this.formBuilder.nonNullable.group({
    idea: ['', [Validators.required, Validators.minLength(20), Validators.maxLength(6000)]],
    productName: ['', [Validators.required, Validators.pattern(/^[a-z0-9]+(?:-[a-z0-9]+)*$/)]],
    destinationParent: ['', Validators.required],
    profileId: ['angular-go', Validators.required],
    initializeGit: true,
  });

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Create product · ProductCrew');
    try {
      const [profiles, settings] = await Promise.all([
        firstValueFrom(this.projectApi.listProductCreationProfiles()),
        firstValueFrom(this.projectApi.getSettings()),
      ]);
      this.profiles.set(profiles);
      this.settings.set(settings);
      this.form.controls.destinationParent.setValue(settings.clonePath);
    } catch (error) {
      this.error.set(this.errorText(error, 'Could not load the product foundations.'));
    } finally {
      this.loading.set(false);
    }
  }

  setStartingPoint(value: StartingPoint): void {
    this.startingPoint.set(value);
    if (value === 'guided' && !this.form.controls.idea.value) {
      this.form.controls.idea.setValue(
        'Build a local-first product for [target user] that helps them [outcome]. The first release must support [core workflow] and clearly handle [important constraint].',
      );
    }
    if (value === 'template') this.form.controls.profileId.setValue('tauri-angular-go');
  }

  async pickDestination(): Promise<void> {
    this.error.set(null);
    try {
      const selection = await firstValueFrom(this.projectApi.selectFolder());
      if (selection?.path) this.form.controls.destinationParent.setValue(selection.path);
    } catch (error) {
      this.error.set(this.errorText(error, 'Could not open the folder picker.'));
    }
  }

  async shapeBlueprint(): Promise<void> {
    if (this.form.invalid || this.shaping()) {
      this.form.markAllAsTouched();
      return;
    }
    this.shaping.set(true);
    this.error.set(null);
    try {
      const value = this.form.getRawValue();
      this.blueprint.set(
        await firstValueFrom(
          this.projectApi.createProductBlueprint({
            ...value,
            startingPoint: this.startingPoint(),
          }),
        ),
      );
      window.scrollTo({ top: 0, behavior: 'smooth' });
    } catch (error) {
      this.error.set(this.errorText(error, 'Could not shape this blueprint.'));
    } finally {
      this.shaping.set(false);
    }
  }

  backToBrief(): void {
    this.blueprint.set(null);
    this.error.set(null);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  async approveAndCreate(): Promise<void> {
    const blueprint = this.blueprint();
    if (!blueprint || !blueprint.preflight.ready || this.creating()) return;
    this.creating.set(true);
    this.error.set(null);
    try {
      const result = await firstValueFrom(this.projectApi.approveProductBlueprint(blueprint.id));
      await this.projectContext.refresh();
      this.projectContext.select(result.project.id);
      await this.router.navigate(['/work-items'], {
        queryParams: { projectId: result.project.id, taskId: result.backlogItemId },
      });
    } catch (error) {
      this.error.set(this.errorText(error, 'Could not create the product safely.'));
    } finally {
      this.creating.set(false);
    }
  }

  providerLabel(): string {
    switch (this.settings()?.aiProvider) {
      case 'claude-code': return 'Claude Code';
      case 'github-copilot': return 'GitHub Copilot';
      default: return 'Codex';
    }
  }

  private errorText(error: unknown, fallback: string): string {
    return (error as HttpErrorResponse).error?.error ?? fallback;
  }
}
