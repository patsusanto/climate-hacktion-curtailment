import { useEffect, useRef, useState } from 'react'

/** What the model does before the first step arrives, in the order it does it. */
const stages = [
  'Reading the prices and weather for this period',
  'Building the house on the observed weather',
  'Forecasting prices, solar and demand 8 hours ahead',
  'Planning the battery every 5 minutes',
  'Replaying a year for the payback',
]

const STAGE_SECONDS = 1.1 // how long each stage shows while the model is still working
const FINISH_MS = 140 // per remaining stage, once the result is ready
const INSTANT_MS = 300 // a result this quick (a precomputed house) skips the screen

type Props = {
  reduce: boolean
  /** The result has arrived. */
  ready: boolean
  /** Called once every stage is ticked, so the page can show the playback. */
  onFinished: () => void
}

/** Shown while a house is being worked out: a moving bar, the step it is on, and the time taken. */
export default function Loading({ reduce, ready, onFinished }: Props) {
  const started = useRef(Date.now())
  const [elapsed, setElapsed] = useState(0)
  const [stage, setStage] = useState(0)
  const finished = useRef(false)
  const [visible, setVisible] = useState(false) // hidden at first, so an instant result does not flash it

  useEffect(() => {
    const timer = window.setTimeout(() => setVisible(true), INSTANT_MS - 50)
    return () => window.clearTimeout(timer)
  }, [])

  // While working: advance on a clock (the model does not report progress), stopping on the last.
  useEffect(() => {
    if (ready) return
    const timer = window.setInterval(() => {
      const s = (Date.now() - started.current) / 1000
      setElapsed(s)
      setStage(Math.min(stages.length - 1, Math.floor(s / STAGE_SECONDS)))
    }, 200)
    return () => window.clearInterval(timer)
  }, [ready])

  // Once ready: tick the rest off quickly, then hand over.
  useEffect(() => {
    if (!ready || finished.current) return
    const done = () => {
      if (!finished.current) {
        finished.current = true
        onFinished()
      }
    }
    if (reduce || Date.now() - started.current < INSTANT_MS) {
      done()
      return
    }
    const timer = window.setInterval(() => {
      setStage((s) => {
        if (s >= stages.length) {
          window.clearInterval(timer)
          window.setTimeout(done, FINISH_MS * 2)
          return s
        }
        return s + 1
      })
    }, FINISH_MS)
    return () => window.clearInterval(timer)
  }, [ready, reduce, onFinished])

  if (!visible) return null
  return (
    <div className="card loading" role="status" aria-live="polite">
      <p className="eyebrow">{stage >= stages.length ? 'Ready' : 'Starting this house'}</p>
      <div className={reduce || stage >= stages.length ? 'loading-bar still' : 'loading-bar'} aria-hidden="true">
        <span />
      </div>
      <ol className="loading-stages">
        {stages.map((text, i) => (
          <li key={text} className={i < stage ? 'done' : i === stage ? 'now' : ''}>
            {text}
          </li>
        ))}
      </ol>
      <p className="note">
        {ready
          ? 'Done. Starting the replay.'
          : elapsed < 1.5
            ? 'Ready examples start at once.'
            : `${elapsed.toFixed(0)} s. A new house takes a few seconds to compute.`}
      </p>
    </div>
  )
}
