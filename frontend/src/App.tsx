import { useEffect, useState } from 'react'

export default function App() {
  const [status, setStatus] = useState('checking...')

  useEffect(() => {
    fetch('/api/health')
      .then((res) => res.json())
      .then((data: { status: string }) => setStatus(data.status))
      .catch(() => setStatus('unreachable'))
  }, [])

  return (
    <main style={{ fontFamily: 'sans-serif', padding: '2rem' }}>
      <h1>Climate Hacktion Curtailment</h1>
      <p>Backend status: {status}</p>

      <section>
        <h2>About</h2>
        <p>
          A small demo showing a React frontend talking to a Go backend, both
          running on Google Cloud Run.
        </p>
        <ul>
          <li>Frontend: React 19 + Vite, served by nginx</li>
          <li>Backend: Go HTTP server</li>
          <li>Deployed with Cloud Build triggers</li>
        </ul>
      </section>

      <footer>
        <small>Deploy test v2</small>
      </footer>
    </main>
  )
}
