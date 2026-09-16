import { HttpInterceptorFn } from '@angular/common/http';

const desktopApiOrigin = 'http://127.0.0.1:8081';

function isTauriDesktop(): boolean {
  if (typeof window === 'undefined') return false;
  return window.location.hostname === 'tauri.localhost' || '__TAURI_INTERNALS__' in window;
}

export function apiUrl(path: string): string {
  if (!path.startsWith('/api') || !isTauriDesktop()) return path;
  return `${desktopApiOrigin}${path}`;
}

export const apiOriginInterceptor: HttpInterceptorFn = (request, next) => {
  const url = apiUrl(request.url);
  return next(url === request.url ? request : request.clone({ url }));
};
