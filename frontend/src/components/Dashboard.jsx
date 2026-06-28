import { useMemo, useState } from 'react'
import { useLogto } from '@logto/react'
import { api } from '../api'
import { useApiData } from '../hooks/useApiData'

const STATUS_LABEL = {
  pending: 'Pending',
  running: 'Running',
  suspended: 'Suspended',
  stopped: 'Stopped',
  error: 'Error',
}

function StatusBadge({ status }) {
  return <span className={`badge badge-${status}`}>{STATUS_LABEL[status] || status}</span>
}

export default function Dashboard() {
  const { getAccessToken, isAuthenticated } = useLogto()
  const getToken = getAccessToken
  const [refreshKey, setRefreshKey] = useState(0)

  // Fetch sessions + templates (templates power the "new workspace" picker).
  const sessionsFetcher = useMemo(
    () => (isAuthenticated ? () => api.listSessions(getToken) : null),
    [isAuthenticated, getToken],
  )
  const templatesFetcher = useMemo(
    () => (isAuthenticated ? () => api.listTemplates(getToken) : null),
    [isAuthenticated, getToken],
  )

  const { data: sessionsData, loading, error, refresh } =
    useApiData(sessionsFetcher, [refreshKey])
  const { data: templatesData } = useApiData(templatesFetcher, [refreshKey])

  const sessions = sessionsData?.sessions || []
  const templates = templatesData?.templates || []
  const readyTemplates = templates.filter((t) => t.status === 'ready' || t.image_ref)

  return (
    <div>
      <div className="page-head">
        <div>
          <h2>Workspaces</h2>
          <p className="muted">Disposable development environments on Fly.io.</p>
        </div>
      </div>

      <NewWorkspaceForm
        templates={readyTemplates}
        onCreate={async (templateId, name) => {
          await api.createSession(getToken, { template_id: templateId, name })
          setRefreshKey((k) => k + 1)
        }}
      />

      {loading && !sessionsData ? (
        <div className="loading">Loading workspaces…</div>
      ) : error ? (
        <div className="error-box">{error}</div>
      ) : sessions.length === 0 ? (
        <div className="empty">
          No workspaces yet. Build a template, then create your first sandbox above.
        </div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Template</th>
              <th>Region</th>
              <th>URL</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s) => (
              <tr key={s.id}>
                <td>{s.name}</td>
                <td><StatusBadge status={s.status} /></td>
                <td className="mono">{s.template_id?.slice(0, 8)}</td>
                <td>{s.region || '—'}</td>
                <td>
                  {s.url ? (
                    <a href={s.url} target="_blank" rel="noreferrer" className="link">
                      Open IDE ↗
                    </a>
                  ) : '—'}
                </td>
                <td className="actions">
                  {s.status === 'suspended' && (
                    <button className="btn btn-sm" onClick={async () => {
                      await api.resumeSession(getToken, s.id); refresh()
                    }}>Resume</button>
                  )}
                  {s.status === 'running' && (
                    <button className="btn btn-sm" onClick={async () => {
                      await api.hibernateSession(getToken, s.id); refresh()
                    }}>Hibernate</button>
                  )}
                  <button className="btn btn-sm btn-danger" onClick={async () => {
                    if (confirm(`Delete workspace "${s.name}"? This destroys the VM and its volume.`)) {
                      await api.deleteSession(getToken, s.id); refresh()
                    }
                  }}>Delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

function NewWorkspaceForm({ templates, onCreate }) {
  const [templateId, setTemplateId] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState(null)

  const submit = async (e) => {
    e.preventDefault()
    setBusy(true); setErr(null)
    try {
      await onCreate(templateId, name)
      setName('')
    } catch (e2) {
      setErr(e2.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="row-form" onSubmit={submit}>
      <select value={templateId} onChange={(e) => setTemplateId(e.target.value)} required>
        <option value="">Select a template…</option>
        {templates.map((t) => (
          <option key={t.id} value={t.id}>{t.name}</option>
        ))}
      </select>
      <input
        type="text"
        placeholder="workspace name (optional)"
        value={name}
        onChange={(e) => setName(e.target.value)}
      />
      <button type="submit" className="btn btn-primary" disabled={busy || !templateId}>
        {busy ? 'Provisioning…' : 'Create workspace'}
      </button>
      {err && <span className="error-inline">{err}</span>}
    </form>
  )
}
