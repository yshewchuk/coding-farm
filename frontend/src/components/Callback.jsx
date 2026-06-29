import { useHandleSignInCallback } from '@logto/react'
import { useNavigate } from 'react-router-dom'
import { redirectUri } from '../logto'

// Callback completes the Logto sign-in flow by exchanging the authorization
// code for tokens, then navigates back to the dashboard.
//
// We use the dedicated `useHandleSignInCallback` hook (NOT `useLogto()`), which
// is the correct API: `useLogto()` does NOT expose `handleSignInCallback` —
// destructuring it yields `undefined` and calling it throws
// "TypeError: e is not a function". The hook reads the provider context,
// confirms the browser was redirected from Logto (isSignInRedirected), calls
// the client's handleSignInCallback with the full href, flips isAuthenticated,
// and then invokes our completion callback.
//
// On failure the hook surfaces its error via the returned `error` (and logs to
// console), so a failed exchange is diagnosable rather than looking like
// "nothing happens".
export default function Callback() {
  const navigate = useNavigate()
  const { isLoading, error } = useHandleSignInCallback(() => {
    navigate('/', { replace: true })
  })

  if (error) {
    const code = error?.code || error?.name || 'unknown'
    const message = error?.message || String(error)
    const hint = errorHint(code)
    return (
      <div className="auth-screen">
        <div className="card">
          <h1>Sign-in failed</h1>
          <p className="muted">
            The Logto callback could not complete the token exchange.
          </p>
          <p><strong>{code}</strong></p>
          <p className="muted" style={{ wordBreak: 'break-word' }}>{message}</p>
          {hint && <p className="muted">{hint}</p>}
          <details style={{ marginTop: '1rem' }}>
            <summary className="muted">Callback URL</summary>
            <code style={{ wordBreak: 'break-all' }}>
              {typeof window !== 'undefined' ? window.location.href : ''}
            </code>
          </details>
          <a href="/signin" className="btn btn-primary" style={{ marginTop: '1rem' }}>
            Back to sign in
          </a>
        </div>
      </div>
    )
  }

  return <div className="loading">{isLoading ? 'Completing sign-in…' : 'Redirecting…'}</div>
}

// Friendly hints for the Logto SDK error codes we're most likely to hit.
function errorHint(code) {
  switch (code) {
    case 'sign_in_session.not_found':
      return 'The sign-in session was lost (sessionStorage). Re-open the sign-in page from the app origin and try again; avoid opening the callback URL in a new tab.'
    case 'callback_uri_verification.redirect_uri_mismatched':
      return `The redirect URI registered in Logto must be exactly ${redirectUri}. Re-run 'scripts/deploy.sh logto-setup' with FRONTEND_URL set to this app's origin.`
    case 'callback_uri_verification.error_found':
      return 'Logto returned an error instead of a code. If you recently changed LOGTO_AUDIENCE, re-run logto-setup and redeploy the web app so the API resource indicator matches the baked VITE_LOGTO_RESOURCE.'
    case 'callback_uri_verification.missing_code':
    case 'callback_uri_verification.missing_state':
    case 'callback_uri_verification.state_mismatched':
      return 'The callback is missing the expected code/state — start a fresh sign-in from the app.'
    case 'id_token.invalid_iat':
    case 'id_token.invalid_token':
      return 'ID token validation failed (clock skew or signature). Check that VITE_LOGTO_ENDPOINT is the bare Logto domain and your device clock is correct.'
    case 'LogtoRequestError':
      return 'A network/CORS error reached the Logto token endpoint. Confirm the Logto domain is reachable and allows CORS from this origin.'
    default:
      return ''
  }
}
