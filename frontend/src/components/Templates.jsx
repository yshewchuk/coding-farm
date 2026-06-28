import { useMemo, useState } from 'react'
import { useLogto } from '@logto/react'
import { api } from '../api'
import { useApiData } from '../hooks/useApiData'

const DEFAULT_DOCKERFILE = `FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    curl git ca-certificates \
    golang-go \
    && rm -rf /var/lib/apt/lists/*

# code-server: open-source VS Code in the browser.
RUN curl -fsSL https://code-server.dev/install.sh | sh

EXPOSE 8080
WORKDIR /workspace
CMD ["code-server", "--bind-addr", "0.0.0.0:8080", "/workspace"]
`

const STATUS_LABEL = {
  draft: 'Draft',
  building: 'Building',
  ready: 'Ready',
  failed: 'Failed',
}

export default function Templates() {
  const { getAccessToken, isAuthenticated } = useLogto()
  const getToken = getAccessToken
  const [refreshKey, setRefreshKey] = useState(0)

  const fetcher = useMemo(
    () => (isAuthenticated ? () => api.listTemplates(getToken) : null),
    [isAuthenticated, getToken],
  )
  const { data, loading, error } = useApiData(fetcher, [refreshKey])
  const templates = data?.templates || []

  return (
    <div>
      <div className="page-head">
        <div>
          <h2>Templates</h2>
          <p className="muted">Define workspaces with a Dockerfile, then build the image on Fly.io.</p>
        </div>
      </div>

      <NewTemplateForm onCreate={async (input) => {
        await api.createTemplate(getToken, input)
        setRefreshKey((k) => k + 1)
      }} />

      {loading && !data ? (
        <div className="loading">Loading templates…</div>
      ) : error ? (
        <div className="error-box">{error}</div>
      ) : templates.length === 0 ? (
        <div className="empty">No templates yet. Create your first one above.</div>
      ) : (
        <div className="template-grid">
          {templates.map((t) => (
            <TemplateCard
              key={t.id}
              template={t}
              onBuild={async () => {
                await api.buildTemplate(getToken, t.id)
                setRefreshKey((k) => k + 1)
              }}
              onDelete={async () => {
                if (confirm(`Delete template "${t.name}"?`)) {
                  await api.deleteTemplate(getToken, t.id)
                  setRefreshKey((k) => k + 1)
                }
              }}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function NewTemplateForm({ onCreate }) {
  const [name, setName] = useState('')
  const [dockerfile, setDockerfile] = useState(DEFAULT_DOCKERFILE)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState(null)

  const submit = async (e) => {
    e.preventDefault()
    setBusy(true); setErr(null)
    try {
      await onCreate({ name, dockerfile_contents: dockerfile })
      setName('')
      setDockerfile(DEFAULT_DOCKERFILE)
    } catch (e2) {
      setErr(e2.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="card template-form" onSubmit={submit}>
      <div className="form-row">
        <label>Name</label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. go-codeserver"
          required
        />
      </div>
      <div className="form-row">
        <label>Dockerfile</label>
        <textarea
          value={dockerfile}
          onChange={(e) => setDockerfile(e.target.value)}
          rows={12}
          className="code-area"
          spellCheck={false}
          required
        />
      </div>
      <button type="submit" className="btn btn-primary" disabled={busy}>
        {busy ? 'Saving…' : 'Save template'}
      </button>
      {err && <span className="error-inline">{err}</span>}
    </form>
  )
}

function TemplateCard({ template, onBuild, onDelete }) {
  const [building, setBuilding] = useState(false)
  const [err, setErr] = useState(null)

  const build = async () => {
    setBuilding(true); setErr(null)
    try {
      await onBuild()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBuilding(false)
    }
  }

  return (
    <div className="card template-card">
      <div className="template-card-head">
        <strong>{template.name}</strong>
        <span className={`badge badge-${template.status}`}>{STATUS_LABEL[template.status] || template.status}</span>
      </div>
      {template.image_ref && (
        <div className="mono small muted">image: {template.image_ref}</div>
      )}
      {template.fly_app_name && (
        <div className="mono small muted">app: {template.fly_app_name}</div>
      )}
      <details>
        <summary className="small muted">Dockerfile</summary>
        <pre className="code-block">{template.dockerfile_contents}</pre>
      </details>
      <div className="template-card-actions">
        <button className="btn btn-sm" onClick={build} disabled={building}>
          {building ? 'Building…' : 'Build image'}
        </button>
        <button className="btn btn-sm btn-danger" onClick={onDelete}>Delete</button>
      </div>
      {err && <div className="error-inline">{err}</div>}
    </div>
  )
}
