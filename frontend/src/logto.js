import { VITE } from './config'

// Logto OIDC configuration. The redirect URI must match the sign-in URI
// registered for the Logto application (SPA app).
export const logtoConfig = {
  endpoint: VITE.logtoEndpoint,
  appId: VITE.logtoAppId,
  resources: VITE.logtoResource ? [VITE.logtoResource] : [],
  scopes: VITE.logtoScopes,
}

// The full redirect URI Logto returns the browser to after sign-in. In dev this
// is the Vite origin; in production it is the deployed frontend origin.
export const redirectUri =
  typeof window !== 'undefined' ? `${window.location.origin}/callback` : ''

export const postRedirectUri =
  typeof window !== 'undefined' ? `${window.location.origin}/` : ''
