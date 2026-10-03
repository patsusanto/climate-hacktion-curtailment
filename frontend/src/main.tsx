import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import Home from './home/Home.tsx'
import Playground from './playground/Playground.tsx'

function Root() {
  const path = window.location.pathname.replace(/\/+$/, '') || '/'
  if (path === '/playground') return <Playground />
  return <Home />
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
)
