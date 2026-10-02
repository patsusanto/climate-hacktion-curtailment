import type {
  PlaygroundEvent,
  PlaygroundMeta,
  PlaygroundRequest,
  PlaygroundSummary,
  PlaygroundTick,
  StepDecision,
} from './types'

export type StreamHandlers = {
  onMeta: (meta: PlaygroundMeta, runId: string | null) => void
  onTick: (tick: PlaygroundTick) => void
  onDone: (summary: PlaygroundSummary) => void
}

export async function streamPlayground(
  request: PlaygroundRequest,
  handlers: StreamHandlers,
  signal: AbortSignal,
): Promise<'done' | 'stopped'> {
  let res: Response
  try {
    res = await fetch('/api/v1/playground/run', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/x-ndjson, text/event-stream',
      },
      body: JSON.stringify(request),
      signal,
      cache: 'no-store',
    })
  } catch (err) {
    if (signal.aborted) return 'stopped'
    throw err
  }

  if (!res.ok) {
    if (res.status === 404) {
      throw new Error('The backend has no /v1/playground/run route.')
    }
    throw new Error(await readError(res))
  }
  if (!res.body) throw new Error('The backend returned an empty body.')

  const runId = res.headers.get('X-Run-Id')
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let sawDone = false

  const take = (chunk: string, flushTail: boolean) => {
    buffer += chunk
    if (flushTail && buffer.trim() && !buffer.endsWith('\n')) buffer += '\n'
    const lines = buffer.split(/\r?\n/)
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      const event = parseLine(line)
      if (!event) continue
      if (event.type === 'error') {
        throw new Error(event.message || 'The playground run failed.')
      }
      if (event.type === 'meta') handlers.onMeta(event.meta, runId)
      if (event.type === 'step') handlers.onTick(event.tick)
      if (event.type === 'done') {
        sawDone = true
        handlers.onDone(event.summary)
      }
    }
  }

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      take(decoder.decode(value, { stream: true }), false)
    }
    take(decoder.decode(), true)
  } catch (err) {
    if (signal.aborted) return 'stopped'
    throw err
  }

  if (signal.aborted) return 'stopped'
  if (!sawDone) throw new Error('The stream ended before the summary.')
  return 'done'
}

export async function fetchStep(
  runId: string,
  i: number,
  signal: AbortSignal,
): Promise<StepDecision> {
  const res = await fetch(
    `/api/v1/playground/run/${encodeURIComponent(runId)}/steps/${i}`,
    { signal, cache: 'no-store' },
  )
  if (!res.ok) throw new Error(await readError(res))
  return (await res.json()) as StepDecision
}

async function readError(res: Response): Promise<string> {
  const text = await res.text()
  try {
    const body = JSON.parse(text) as { message?: string }
    if (body.message) return body.message
  } catch {
    // The body was not JSON.
  }
  const trimmed = text.trim()
  return trimmed || `Request failed (${res.status}).`
}

function parseLine(line: string): PlaygroundEvent | null {
  let payload = line.trim()
  if (!payload || payload.startsWith(':')) return null
  if (payload.startsWith('data:')) payload = payload.slice(5).trim()
  if (!payload || payload === '[DONE]') return null
  const value: unknown = JSON.parse(payload)
  if (!isEvent(value)) throw new Error('The stream sent an event this page does not understand.')
  return value
}

function isEvent(value: unknown): value is PlaygroundEvent {
  if (!value || typeof value !== 'object') return false
  const event = value as { type?: unknown; meta?: unknown; tick?: unknown; summary?: unknown }
  if (event.type === 'meta') return event.meta != null
  if (event.type === 'step') return event.tick != null
  if (event.type === 'done') return event.summary != null
  if (event.type === 'error') return true
  return false
}
