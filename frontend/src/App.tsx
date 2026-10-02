import { useEffect, useState } from 'react'
import { Mascot } from './mascot/Mascot'

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
      <header style={{ display: 'flex', alignItems: 'center', gap: '0.85rem' }}>
        <Mascot size={88} />
        <h1 style={{ margin: 0 }}>Climate Hacktion Curtailment</h1>
      </header>
      <p>Backend status: {status}</p>
      <p>
        <a href="/playground">Playground</a>
      </p>

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
