import {
  advanceAvatarPlayback,
  playAvatarAnimation,
  renderAvatarExpression,
  sampleAvatarFrame,
  validateAvatarDefinition,
  type AvatarPlaybackState,
} from '@bible-strong/avatar-core'
import '@bible-strong/avatar-react/styles.css'
import { useEffect, useId, useRef } from 'react'
import avatarJson from './strobi.avatar.json'

const validated = validateAvatarDefinition(avatarJson)
if (!validated.ok) throw new Error(validated.errors[0]?.message ?? 'Invalid avatar')
const definition = validated.value

const maxYaw = 38
const maxPitch = 22

export function Mascot({ size = 72 }: { size?: number }) {
  const root = useRef<HTMLDivElement>(null)
  const clipId = useId().replace(/:/g, '')

  useEffect(() => {
    const host = root.current
    if (!host) return
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const svg = host.querySelector('svg')
    const head = host.querySelector<SVGPathElement>('[data-head]')
    const clip = host.querySelector<SVGPathElement>('[data-clip]')
    const eyes = host.querySelector<SVGGElement>('[data-eyes]')
    const back = host.querySelector<SVGGElement>('[data-back]')
    const front = host.querySelector<SVGGElement>('[data-front]')
    if (!svg || !head || !clip || !eyes || !back || !front) return

    const started = playAvatarAnimation(definition, 'idle', performance.now())
    if (!started.ok) return
    let playback: AvatarPlaybackState = started.value
    let yaw = 0
    let pitch = 0
    let frame = 0

    const paint = (now: number) => {
      const environment = { random: Math.random, reduceMotion: reduce }
      playback = advanceAvatarPlayback(definition, playback, now, environment)
      const sampled = sampleAvatarFrame(definition, playback, now, environment)
      const scene = renderAvatarExpression(
        definition,
        {
          ...sampled.expression,
          headY: yaw,
          headX: pitch,
          headZ: sampled.expression.headZ * 0.2,
        },
        sampled.colors,
        sampled.blink,
      )
      const { geometry, colors } = scene
      head.setAttribute('d', geometry.headPath)
      head.setAttribute('fill', colors.body)
      clip.setAttribute('d', geometry.headPath)
      fillPaths(back, geometry.backPaths, colors.body)
      fillPaths(front, geometry.frontPaths, colors.body)
      const [left, right] = eyes.querySelectorAll('path')
      left?.setAttribute('d', geometry.leftPath)
      left?.setAttribute('fill', colors.eyes)
      left?.setAttribute('opacity', geometry.leftVisible ? '1' : '0')
      right?.setAttribute('d', geometry.rightPath)
      right?.setAttribute('fill', colors.eyes)
      right?.setAttribute('opacity', geometry.rightVisible ? '1' : '0')
    }

    const loop = (now: number) => {
      paint(now)
      frame = requestAnimationFrame(loop)
    }

    const onMove = (event: PointerEvent) => {
      const rect = host.getBoundingClientRect()
      const dx = event.clientX - (rect.left + rect.width / 2)
      const dy = event.clientY - (rect.top + rect.height / 2)
      const dist = Math.hypot(dx, dy) || 1
      const reach = Math.min(1, dist / 140)
      yaw = (dx / dist) * maxYaw * reach
      pitch = (-dy / dist) * maxPitch * reach
      paint(performance.now())
    }

    const onLeave = (event: PointerEvent) => {
      if (event.relatedTarget != null) return
      yaw = 0
      pitch = 0
      paint(performance.now())
    }

    paint(performance.now())
    if (!reduce) {
      frame = requestAnimationFrame(loop)
      window.addEventListener('pointermove', onMove)
      window.addEventListener('pointerout', onLeave)
    }
    return () => {
      if (frame !== 0) cancelAnimationFrame(frame)
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerout', onLeave)
    }
  }, [])

  return (
    <div
      ref={root}
      className="bs-avatar"
      style={{ width: size, height: size }}
      role="img"
      aria-label={avatarJson.name}
    >
      <svg className="bs-avatar__svg" viewBox="-150 -150 300 300" aria-hidden="true">
        <defs>
          <clipPath id={clipId}>
            <path data-clip="" />
          </clipPath>
        </defs>
        <g data-back="" />
        <path data-head="" />
        <g data-eyes="" clipPath={`url(#${clipId})`}>
          <path />
          <path />
        </g>
        <g data-front="" />
      </svg>
    </div>
  )
}

function fillPaths(group: SVGGElement, paths: string[], fill: string) {
  while (group.childElementCount < paths.length) {
    group.appendChild(document.createElementNS('http://www.w3.org/2000/svg', 'path'))
  }
  const children = group.querySelectorAll('path')
  children.forEach((path, index) => {
    path.setAttribute('d', paths[index] ?? '')
    path.setAttribute('fill', fill)
  })
}
