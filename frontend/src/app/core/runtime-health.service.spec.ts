import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { RuntimeHealthService } from './runtime-health.service';

describe('RuntimeHealthService', () => {
  let service: RuntimeHealthService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(RuntimeHealthService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('maps a connected codexAppServer to the connected state', async () => {
    const pending = service.refresh();
    httpMock.expectOne('/api/health').flush({ status: 'ready', codexAppServer: 'connected', aiProvider: 'codex' });
    await pending;

    expect(service.status()).toEqual({ state: 'connected', aiProvider: 'codex' });
  });

  it('maps an unavailable codexAppServer to the unavailable state and keeps the provider', async () => {
    const pending = service.refresh();
    httpMock.expectOne('/api/health').flush({ status: 'ready', codexAppServer: 'unavailable', aiProvider: 'claude-code' });
    await pending;

    expect(service.status()).toEqual({ state: 'unavailable', aiProvider: 'claude-code' });
  });

  it('uses aiRuntime when a non-Codex provider is connected through its CLI', async () => {
    const pending = service.refresh();
    httpMock.expectOne('/api/health').flush({ status: 'ready', codexAppServer: 'unavailable', aiRuntime: 'connected', aiProvider: 'github-copilot' });
    await pending;

    expect(service.status()).toEqual({ state: 'connected', aiProvider: 'github-copilot' });
  });

  it('maps a failed request to the unreachable state with no provider', async () => {
    const pending = service.refresh();
    httpMock.expectOne('/api/health').flush(null, { status: 500, statusText: 'Internal Server Error' });
    await pending;

    expect(service.status()).toEqual({ state: 'unreachable', aiProvider: null });
  });

  it('initialize() performs an immediate refresh and does not create duplicate polling on repeat calls', () => {
    const setIntervalSpy = vi.spyOn(window, 'setInterval');

    service.initialize();
    httpMock.expectOne('/api/health').flush({ status: 'ready', codexAppServer: 'connected', aiProvider: 'codex' });
    expect(setIntervalSpy).toHaveBeenCalledTimes(1);

    service.initialize();
    httpMock.expectNone('/api/health');
    expect(setIntervalSpy).toHaveBeenCalledTimes(1);

    setIntervalSpy.mockRestore();
  });
});
