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
    </main>
  )
}
