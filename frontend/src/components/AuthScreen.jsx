import { useLogto } from '@logto/react'
import { redirectUri } from '../logto'

// AuthScreen is the sign-in entry point. It uses Logto's authorization-code
// flow with PKCE; the Management API never sees the user's password — only the
// access token it validates against the IdP's JWKS.
export default function AuthScreen() {
  const { signIn, isAuthenticated } = useLogto()

  if (isAuthenticated) {
    return (
      <div className="card">
        <h2>You&apos;re signed in.</h2>
        <a href="/" className="btn btn-primary">Go to dashboard</a>
      </div>
    )
  }

  return (
    <div className="auth-screen">
      <div className="card">
        <h1>Cloud Sandbox</h1>
        <p className="muted">
          Self-hosted, disposable development environments on Fly.io Firecracker
          microVMs. Sign in with Logto to provision your first workspace.
        </p>
        <button
          className="btn btn-primary btn-lg"
          onClick={() => signIn(redirectUri)}
        >
          Sign in / Sign up
        </button>
      </div>
    </div>
  )
}
