import { useLogto } from '@logto/react'
import { useNavigate } from 'react-router-dom'
import { useEffect } from 'react'

// Callback completes the Logto sign-in flow by exchanging the authorization
// code for tokens, then navigates back to the dashboard.
export default function Callback() {
  const { handleSignInCallback } = useLogto()
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      try {
        await handleSignInCallback(
          `${window.location.origin}${window.location.pathname}${window.location.search}`,
        )
        if (!cancelled) navigate('/', { replace: true })
      } catch (e) {
        console.error('logto callback failed', e)
        if (!cancelled) navigate('/signin', { replace: true })
      }
    }
    run()
    return () => { cancelled = true }
  }, [handleSignInCallback, navigate])

  return <div className="loading">Completing sign-in…</div>
}
