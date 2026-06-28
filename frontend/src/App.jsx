import { Routes, Route, Navigate, Link, useLocation } from 'react-router-dom'
import { useLogto } from '@logto/react'
import { postRedirectUri } from './logto'
import AuthScreen from './components/AuthScreen.jsx'
import Dashboard from './components/Dashboard.jsx'
import Templates from './components/Templates.jsx'
import Callback from './components/Callback.jsx'

// RequireAuth guards private routes. While Logto is still deciding whether a
// session exists it returns isAuthenticated=false; we treat that as "loading"
// rather than immediately bouncing to sign-in, to avoid a flash on refresh.
function RequireAuth({ children }) {
  const { isAuthenticated, isLoading } = useLogto()
  const location = useLocation()
  if (isLoading) return <div className="loading">Loading…</div>
  if (!isAuthenticated) {
    return <Navigate to="/signin" replace state={{ from: location }} />
  }
  return children
}

export default function App() {
  const { isAuthenticated, signOut } = useLogto()

  return (
    <div className="app">
      <header className="topbar">
        <Link to="/" className="brand">☁️ Cloud Sandbox</Link>
        <nav className="nav">
          <Link to="/">Workspaces</Link>
          <Link to="/templates">Templates</Link>
        </nav>
        {isAuthenticated && (
          <button className="btn btn-ghost" onClick={() => signOut(postRedirectUri)}>
            Sign out
          </button>
        )}
      </header>

      <main className="content">
        <Routes>
          <Route path="/signin" element={<AuthScreen />} />
          <Route path="/callback" element={<Callback />} />
          <Route path="/templates" element={
            <RequireAuth><Templates /></RequireAuth>
          } />
          <Route path="/" element={
            <RequireAuth><Dashboard /></RequireAuth>
          } />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}
