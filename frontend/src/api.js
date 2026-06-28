import { API_BASE, VITE } from './config'

// Thin fetch wrapper that injects the Logto access token and speaks the
// Management API's JSON protocol. Each call acquires a fresh access token so
// refresh is handled by the Logto SDK automatically.
//
// `getToken` is injected (from useLogto) rather than imported, keeping this
// module free of React hooks so it is unit-testable in isolation.
export async function apiCall(path, { method = 'GET', body, getToken } = {}) {
  const headers = { 'Content-Type': 'application/json' }
  if (getToken) {
    const token = await getToken(VITE.logtoResource)
    if (token) headers.Authorization = `Bearer ${token}`
  }

  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'include',
  })

  if (res.status === 204) return null
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    const msg = (data && data.error) || `request failed (${res.status})`
    throw new Error(msg)
  }
  return data
}

// Convenience verbs.
export const api = {
  me: (getToken) => apiCall('/api/me', { getToken }),
  listTemplates: (getToken) => apiCall('/api/templates', { getToken }),
  createTemplate: (getToken, input) =>
    apiCall('/api/templates', { method: 'POST', body: input, getToken }),
  buildTemplate: (getToken, id) =>
    apiCall(`/api/templates/${id}/build`, { method: 'POST', getToken }),
  deleteTemplate: (getToken, id) =>
    apiCall(`/api/templates/${id}`, { method: 'DELETE', getToken }),
  listSessions: (getToken) => apiCall('/api/sessions', { getToken }),
  createSession: (getToken, input) =>
    apiCall('/api/sessions', { method: 'POST', body: input, getToken }),
  resumeSession: (getToken, id) =>
    apiCall(`/api/sessions/${id}/resume`, { method: 'POST', getToken }),
  hibernateSession: (getToken, id) =>
    apiCall(`/api/sessions/${id}/hibernate`, { method: 'POST', getToken }),
  deleteSession: (getToken, id) =>
    apiCall(`/api/sessions/${id}`, { method: 'DELETE', getToken }),
}
