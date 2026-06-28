import { useState, useEffect, useCallback } from 'react'

// useApiData is a small data-fetching hook used by the dashboard and templates
// views. It tracks loading/error state and exposes a refresh function. The
// fetcher is expected to be one of the api.* verbs bound with the Logto token.
//
// Passing `null` as the fetcher suspends fetching (e.g. while not authenticated).
export function useApiData(fetcher, deps = []) {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const refresh = useCallback(async () => {
    if (!fetcher) return
    setLoading(true)
    setError(null)
    try {
      const result = await fetcher()
      setData(result)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [fetcher])

  useEffect(() => {
    refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, loading, error, refresh }
}
