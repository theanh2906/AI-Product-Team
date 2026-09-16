import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { provideRouter } from '@angular/router';

import { routes } from './app.routes';
import { apiOriginInterceptor } from './core/api-url';
import { ProjectContextService } from './core/project-context.service';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideHttpClient(withInterceptors([apiOriginInterceptor])),
    provideRouter(routes),
    ProjectContextService,
  ]
};
