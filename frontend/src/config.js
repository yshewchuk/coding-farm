// Centralized environment configuration for the frontend.
// In dev these come from Vite's import.meta.env; in production they are baked
// in at build time via the same mechanism.
export const VITE = {
  apiBase: import.meta.env.VITE_API_BASE || '',
  logtoEndpoint: import.meta.env.VITE_LOGTO_ENDPOINT || 'http://localhost:3001',
  logtoAppId: import.meta.env.VITE_LOGTO_APP_ID || '',
  logtoResource: import.meta.env.VITE_LOGTO_RESOURCE || '',
  logtoScopes: (import.meta.env.VITE_LOGTO_SCOPES || 'openid profile email').split(' '),
}

// apiBase resolves to '' in dev (proxied by Vite) or the deployed API origin.
export const API_BASE = VITE.apiBase
