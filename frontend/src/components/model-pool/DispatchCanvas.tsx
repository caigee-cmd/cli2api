import { useEffect, useLayoutEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { ArrowsIn, Cube, GitBranch, Lightning, MagnifyingGlassMinus, MagnifyingGlassPlus, Plus, Stack, TrashSimple, Tray, User } from '@phosphor-icons/react'
import { ProviderMark } from '@/components/ProviderMark'
import type { PipelineStep } from '@/components/model-pool/pipeline'

const NODE_W = 276
const NODE_H = 128

type FaceChip = { text: string; tone: 'model' | 'account' | 'more' | 'plain'; title?: string }
type Face = { title: string; detail: string; note: string; chips?: FaceChip[]; facts?: string[] }
const GAP_X = 64
const GAP_Y = 96
const MIN_K = 0.4
const MAX_K = 1.7

type Kind = 'request' | 'strategy' | 'member' | 'group' | 'branch'
type SeatKind = 'member' | 'group' | 'branch'

type Labels = {
  request: string
  requestLine: string
  strategyKind: string
  model: string
  modelLine: string
  group: string
  groupLine: string
  intent: string
  intentLine: string
  add: string
  remove: string
  hint: string
  palette: string
  incomplete: string
  skipped: string
  fit: string
  zoomIn: string
  zoomOut: string
  drag: string
}

type NodeView = {
  id: string
  kind: Kind
  title: string
  line: string
  well: string
  chip: string
  tone: 'index' | 'warn' | 'skip'
  chips: FaceChip[]
  facts: string[]
  provider?: string
  x: number
  y: number
  parentPathId: string | null
  index: number
  from: string | null
  port: number
  ports: number[]
}

type AddSpot = {
  key: string
  parentPathId: string | null
  x: number
  y: number
  place: 'under' | 'end'
}

type Box = { items: NodeView[]; width: number; height: number; adds: AddSpot[] }

type View = { x: number; y: number; k: number }

type DropAt = { parentPathId: string | null; index: number; x: number; y: number }

export function DispatchCanvas({
  steps,
  selected,
  strategyTitle,
  strategyLine,
  strategyChips,
  requestTitle,
  requestToken,
  skipped,
  labels,
  describe,
  busy,
  onSelect,
  onSelectStrategy,
  onInsert,
  onMove,
  onRemove,
}: {
  steps: PipelineStep[]
  selected: string | null
  strategyTitle: string
  strategyLine: string
  strategyChips?: FaceChip[]
  requestTitle: string
  requestToken?: string
  skipped?: Record<string, 'skipped'>
  labels: Labels
  describe: (step: PipelineStep) => Face
  busy?: boolean
  onSelect: (id: string) => void
  onSelectStrategy: () => void
  onInsert: (parentPathId: string | null, kind: SeatKind, index?: number) => void
  onMove: (parentPathId: string | null, from: number, to: number) => void
  onRemove: (id: string) => void
}) {
  const vpRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<View>({ x: 220, y: 36, k: 1 })
  const [view, setView] = useState<View>(viewRef.current)
  const [menu, setMenu] = useState<{ parentPathId: string | null; x: number; y: number } | null>(null)
  const [drag, setDrag] = useState<{ id: string; x: number; y: number; moved: boolean } | null>(null)
  const [panning, setPanning] = useState(false)
  const [ghost, setGhost] = useState<{ kind: SeatKind; x: number; y: number; over: boolean; drop: DropAt | null } | null>(null)
  const gesture = useRef<{ type: 'pan' | 'node'; sx: number; sy: number; ox: number; oy: number; id?: string; moved: boolean } | null>(null)
  const dragRef = useRef(drag)
  dragRef.current = drag

  const laid = useMemo(
    () => layoutTree(steps, describe, skipped, { requestTitle, requestToken, strategyTitle, strategyLine, strategyChips, labels }),
    [steps, describe, skipped, requestTitle, requestToken, strategyTitle, strategyLine, strategyChips, labels],
  )
  const laidRef = useRef(laid)
  laidRef.current = laid

  const nodes = laid.items.map((node) => (drag?.id === node.id ? { ...node, x: drag.x, y: drag.y } : node))
  const byId = new Map(nodes.map((node) => [node.id, node]))
  const edges = laid.edges.map((edge) => {
    const [fromId, toId] = edge.id.split('>')
    const from = byId.get(fromId)
    const to = byId.get(toId)
    if (!from || !to) return edge
    const next = bend(from, to)
    return { ...edge, d: next.d, tagX: next.x, tagY: next.y }
  })
  const slot = drag?.moved ? insertionOf(laid.items, drag.id, drag.x) : ghost?.over ? ghost.drop : null

  function commitView(next: View) {
    viewRef.current = next
    setView(next)
  }

  function fit() {
    const host = vpRef.current
    if (!host || laidRef.current.items.length === 0) return
    const items = laidRef.current.items
    const minX = Math.min(...items.map((node) => node.x))
    const minY = Math.min(...items.map((node) => node.y)) - 28
    const maxX = Math.max(...items.map((node) => node.x + NODE_W))
    const maxY = Math.max(...items.map((node) => node.y + NODE_H))
    const rect = host.getBoundingClientRect()
    const padL = 248
    const availW = Math.max(160, rect.width - padL - 36)
    const availH = Math.max(160, rect.height - 88)
    const k = clamp(Math.min(availW / Math.max(1, maxX - minX), availH / Math.max(1, maxY - minY), 1), MIN_K, MAX_K)
    commitView({
      k,
      x: padL + (availW - (maxX - minX) * k) / 2 - minX * k,
      y: 28 + (availH - (maxY - minY) * k) / 2 - minY * k,
    })
  }

  const fitted = useRef(false)
  useLayoutEffect(() => {
    if (fitted.current) return
    fitted.current = true
    fit()
  })

  useEffect(() => {
    const host = vpRef.current
    if (!host) return
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = host.getBoundingClientRect()
      zoomAt(event.deltaY < 0 ? 1.08 : 1 / 1.08, event.clientX - rect.left, event.clientY - rect.top)
    }
    host.addEventListener('wheel', onWheel, { passive: false })
    return () => host.removeEventListener('wheel', onWheel)
  }, [])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target?.closest('input, textarea, select, [contenteditable="true"]')) return
      if (!vpRef.current?.contains(document.activeElement) && document.activeElement !== document.body) return
      const node = laidRef.current.items.find((item) => item.id === selected)
      if (!node || node.kind === 'request' || node.kind === 'strategy') return
      if (event.key === 'Backspace' || event.key === 'Delete') {
        event.preventDefault()
        onRemove(node.id)
        return
      }
      if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
        event.preventDefault()
        onMove(node.parentPathId, node.index, node.index + (event.key === 'ArrowLeft' ? -1 : 1))
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onMove, onRemove, selected])

  function zoomAt(factor: number, mx: number, my: number) {
    const current = viewRef.current
    const k = clamp(current.k * factor, MIN_K, MAX_K)
    const wx = (mx - current.x) / current.k
    const wy = (my - current.y) / current.k
    commitView({ k, x: mx - wx * k, y: my - wy * k })
  }

  function worldAt(clientX: number, clientY: number) {
    const rect = vpRef.current?.getBoundingClientRect()
    const current = viewRef.current
    return {
      x: (clientX - (rect?.left ?? 0) - current.x) / current.k,
      y: (clientY - (rect?.top ?? 0) - current.y) / current.k,
    }
  }

  function openMenu(parentPathId: string | null, anchor: HTMLElement) {
    const rect = anchor.getBoundingClientRect()
    const host = vpRef.current?.getBoundingClientRect()
    const width = host?.width ?? 0
    const height = host?.height ?? 0
    let x = rect.left - (host?.left ?? 0)
    let y = rect.bottom - (host?.top ?? 0) + 8
    if (y + 150 > height) y = rect.top - (host?.top ?? 0) - 150
    if (x + 188 > width) x = width - 196
    setMenu({ parentPathId, x: Math.max(8, x), y: Math.max(8, y) })
  }

  function onPointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return
    const target = event.target as HTMLElement
    if (target.closest('button, a, input, textarea')) return
    const card = target.closest<HTMLElement>('[data-node]')
    const node = card ? byId.get(card.dataset.node || '') : undefined
    vpRef.current?.focus({ preventScroll: true })
    event.currentTarget.setPointerCapture(event.pointerId)
    if (node && node.kind !== 'request' && node.kind !== 'strategy') {
      gesture.current = { type: 'node', sx: event.clientX, sy: event.clientY, ox: node.x, oy: node.y, id: node.id, moved: false }
      return
    }
    if (node) {
      gesture.current = { type: 'pan', sx: event.clientX, sy: event.clientY, ox: viewRef.current.x, oy: viewRef.current.y, id: node.id, moved: false }
      return
    }
    gesture.current = { type: 'pan', sx: event.clientX, sy: event.clientY, ox: viewRef.current.x, oy: viewRef.current.y, moved: false }
    setMenu(null)
  }

  function onPointerMove(event: ReactPointerEvent<HTMLDivElement>) {
    const g = gesture.current
    if (!g) return
    const dx = event.clientX - g.sx
    const dy = event.clientY - g.sy
    if (!g.moved && Math.hypot(dx, dy) < 4) return
    g.moved = true
    if (g.type === 'pan') {
      setPanning(true)
      commitView({ ...viewRef.current, x: g.ox + dx, y: g.oy + dy })
      return
    }
    if (g.id) {
      const k = viewRef.current.k || 1
      const next = { id: g.id, x: g.ox + dx / k, y: g.oy + dy / k, moved: true }
      dragRef.current = next
      setDrag(next)
    }
  }

  function onPointerUp() {
    const g = gesture.current
    gesture.current = null
    setPanning(false)
    if (!g) return
    if (g.type === 'node' && g.id) {
      const node = laid.items.find((item) => item.id === g.id)
      const current = dragRef.current
      if (node && g.moved && current) {
        const to = insertionOf(laid.items, node.id, current.x)
        if (to && to.index !== node.index) onMove(node.parentPathId, node.index, to.index)
      } else if (node && !g.moved) {
        onSelect(node.id)
      }
    } else if (!g.moved && g.id) {
      if (g.id === 'strategy' || g.id === 'request') onSelectStrategy()
    }
    dragRef.current = null
    setDrag(null)
  }

  function startPalette(kind: SeatKind, event: ReactPointerEvent<HTMLButtonElement>) {
    if (event.button !== 0 || busy) return
    event.stopPropagation()
    const pending = { kind, sx: event.clientX, sy: event.clientY, id: event.pointerId, started: false }
    const place = (clientX: number, clientY: number, over: boolean) => {
      const host = vpRef.current?.getBoundingClientRect()
      const point = worldAt(clientX, clientY)
      const drop = over ? dropAt(laidRef.current.items, point.x, point.y) : null
      setGhost({
        kind,
        x: clientX - (host?.left ?? 0),
        y: clientY - (host?.top ?? 0),
        over,
        drop,
      })
    }
    const move = (e: PointerEvent) => {
      if (e.pointerId !== pending.id) return
      if (!pending.started && Math.hypot(e.clientX - pending.sx, e.clientY - pending.sy) < 5) return
      pending.started = true
      const host = vpRef.current?.getBoundingClientRect()
      const pal = vpRef.current?.querySelector('.dg-pal')?.getBoundingClientRect()
      const inside = !!host && e.clientX >= host.left && e.clientX <= host.right && e.clientY >= host.top && e.clientY <= host.bottom
      const onPalette = !!pal && e.clientX >= pal.left && e.clientX <= pal.right && e.clientY >= pal.top && e.clientY <= pal.bottom
      place(e.clientX, e.clientY, inside && !onPalette)
    }
    const up = (e: PointerEvent) => {
      if (e.pointerId !== pending.id) return
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      const host = vpRef.current?.getBoundingClientRect()
      const pal = vpRef.current?.querySelector('.dg-pal')?.getBoundingClientRect()
      const inside = !!host && e.clientX >= host.left && e.clientX <= host.right && e.clientY >= host.top && e.clientY <= host.bottom
      const onPalette = !!pal && e.clientX >= pal.left && e.clientX <= pal.right && e.clientY >= pal.top && e.clientY <= pal.bottom
      if (!pending.started) {
        const parent = contextParent(laidRef.current.items, selected)
        onInsert(parent, kind)
      } else if (e.type !== 'pointercancel' && inside && !onPalette) {
        const point = worldAt(e.clientX, e.clientY)
        const drop = dropAt(laidRef.current.items, point.x, point.y)
        onInsert(drop.parentPathId, kind, drop.index)
      }
      setGhost(null)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
  }

  const grid = 22 * view.k

  return (
    <div className="dg-wf">
      <style>{css}</style>
      <div
        ref={vpRef}
        className="dg-vp"
        tabIndex={0}
        data-panning={panning ? '' : undefined}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        style={{
          backgroundPosition: `${view.x}px ${view.y}px`,
          backgroundSize: `${grid}px ${grid}px`,
        }}
      >
        <div className="dg-world" style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.k})` }}>
          <svg className="dg-edges" aria-hidden="true">
            {edges.map((edge) => <path key={edge.id} className="dg-edge" d={edge.d} />)}
          </svg>
          {edges.map((edge) => edge.tag ? (
            <span key={`${edge.id}-tag`} className="dg-tag" style={{ left: edge.tagX, top: edge.tagY }}>{edge.tag}</span>
          ) : null)}
          {slot ? <span className="dg-slot" style={{ left: slot.x, top: slot.y }} /> : null}
          {nodes.map((node) => (
            <article
              key={node.id}
              data-node={node.id}
              data-kind={node.kind}
              data-selected={selected === node.id || (node.kind === 'strategy' && selected === 'strategy') ? '' : undefined}
              data-fixed={node.kind === 'request' || node.kind === 'strategy' ? '' : undefined}
              className="dg-node"
              style={{ left: node.x, top: node.y, width: NODE_W, height: NODE_H }}
              title={node.kind === 'request' || node.kind === 'strategy' ? node.title : labels.drag}
            >
              <span className="dg-kind">{kindLabel(node.kind, labels)}</span>
              <span className="dg-head">
                <span className="dg-tile">
                  {node.kind === 'member' && node.provider ? <ProviderMark provider={node.provider} size={14} /> : kindIcon(node.kind)}
                </span>
                <b>{node.title}</b>
                {node.chip ? <span className="dg-chip" data-tone={node.tone}>{node.chip}</span> : null}
              </span>
              {node.facts.length > 0 ? (
                <span className="dg-facts" title={node.facts.join(' · ')}>
                  {node.facts.map((fact, index) => <span key={`${index}-${fact}`} className="dg-fact">{fact}</span>)}
                </span>
              ) : (
                <span className="dg-line" title={node.line}>{node.line}</span>
              )}
              <span className="dg-well" title={node.chips.length > 1 ? node.chips.map((chip) => chip.text).join(' · ') : undefined}>
                {node.chips.length > 0 ? (
                  <span className="dg-chips">
                    {packChips(node.chips).map((chip, index) => (
                      <span key={`${chip.tone}-${index}`} className="dg-tagchip" data-tone={chip.tone} title={chip.title || chip.text}>
                        {chip.tone === 'model' ? <Cube size={10} /> : chip.tone === 'account' ? <User size={10} /> : null}
                        <span>{chip.text}</span>
                      </span>
                    ))}
                  </span>
                ) : <span className="dg-mono">{node.well || '—'}</span>}
              </span>
              {node.kind !== 'request' ? <span className="dg-port dg-port-in" /> : null}
              {node.ports.map((port) => (
                <span key={port} className="dg-port dg-port-out" style={{ left: `${port * 100}%` }} />
              ))}
              {node.kind !== 'request' && node.kind !== 'strategy' ? (
                <button
                  type="button"
                  className="dg-x"
                  aria-label={labels.remove}
                  disabled={busy}
                  onPointerDown={(event) => event.stopPropagation()}
                  onClick={(event) => {
                    event.stopPropagation()
                    onRemove(node.id)
                  }}
                >
                  <TrashSimple size={12} />
                </button>
              ) : null}
            </article>
          ))}
          {laid.adds.map((spot) => (
            <button
              key={spot.key}
              type="button"
              className="dg-add"
              data-place={spot.place}
              style={{ left: spot.x - 11, top: spot.y - 11 }}
              aria-label={labels.add}
              disabled={busy}
              onPointerDown={(event) => event.stopPropagation()}
              onClick={(event) => {
                event.stopPropagation()
                openMenu(spot.parentPathId, event.currentTarget)
              }}
            >
              <Plus size={12} weight="bold" />
            </button>
          ))}
        </div>

        <aside className="dg-pal" onPointerDown={(event) => event.stopPropagation()}>
          <p className="dg-pal-title">{labels.palette}</p>
          <p className="dg-pal-hint">{labels.hint}</p>
          {([
            ['member', labels.model, labels.modelLine],
            ['group', labels.group, labels.groupLine],
            ['branch', labels.intent, labels.intentLine],
          ] as const).map(([kind, title, line]) => (
            <button
              key={kind}
              type="button"
              className="dg-pal-item"
              disabled={busy}
              onPointerDown={(event) => startPalette(kind, event)}
            >
              <span className="dg-tile" data-kind={kind}>{kindIcon(kind)}</span>
              <span>
                <b>{title}</b>
                <small>{line}</small>
              </span>
            </button>
          ))}
        </aside>

        {menu ? (
          <div className="dg-menu" style={{ left: menu.x, top: menu.y }} onPointerDown={(event) => event.stopPropagation()}>
            {([
              ['member', labels.model],
              ['group', labels.group],
              ['branch', labels.intent],
            ] as const).map(([kind, title]) => (
              <button
                key={kind}
                type="button"
                disabled={busy}
                onClick={() => {
                  onInsert(menu.parentPathId, kind)
                  setMenu(null)
                }}
              >
                <span className="dg-tile" data-kind={kind}>{kindIcon(kind)}</span>
                {title}
              </button>
            ))}
          </div>
        ) : null}

        {ghost ? (
          <div className="dg-ghost" data-over={ghost.over ? '' : undefined} style={{ left: ghost.x, top: ghost.y }}>
            <span className="dg-kind">{kindLabel(ghost.kind, labels)}</span>
            <span className="dg-head">
              <span className="dg-tile" data-kind={ghost.kind}>{kindIcon(ghost.kind)}</span>
              <b>{kindLabel(ghost.kind, labels)}</b>
            </span>
          </div>
        ) : null}

        <div className="dg-tb" onPointerDown={(event) => event.stopPropagation()}>
          <button type="button" aria-label={labels.zoomOut} onClick={() => {
            const host = vpRef.current?.getBoundingClientRect()
            zoomAt(1 / 1.12, (host?.width ?? 0) / 2, (host?.height ?? 0) / 2)
          }}>
            <MagnifyingGlassMinus size={15} />
          </button>
          <button type="button" className="dg-zoom" onClick={() => zoomAt(1 / viewRef.current.k, (vpRef.current?.clientWidth ?? 0) / 2, (vpRef.current?.clientHeight ?? 0) / 2)}>
            {Math.round(view.k * 100)}%
          </button>
          <button type="button" aria-label={labels.zoomIn} onClick={() => {
            const host = vpRef.current?.getBoundingClientRect()
            zoomAt(1.12, (host?.width ?? 0) / 2, (host?.height ?? 0) / 2)
          }}>
            <MagnifyingGlassPlus size={15} />
          </button>
          <i />
          <button type="button" aria-label={labels.fit} onClick={fit}>
            <ArrowsIn size={15} />
          </button>
        </div>
      </div>
    </div>
  )
}

function contextParent(items: NodeView[], selected: string | null) {
  const node = items.find((item) => item.id === selected)
  if (!node || node.kind === 'request' || node.kind === 'strategy') return null
  return node.parentPathId
}

function insertionOf(items: NodeView[], id: string, x: number): { index: number; x: number; y: number } | null {
  const node = items.find((item) => item.id === id)
  if (!node) return null
  const others = items
    .filter((item) => item.id !== id && item.parentPathId === node.parentPathId && item.from === node.from)
    .sort((a, b) => a.x - b.x)
  const index = others.filter((item) => item.x + NODE_W / 2 < x).length
  const prev = others[index - 1]
  const next = others[index]
  const line = !prev
    ? (next ? next.x - 18 : x)
    : !next
      ? prev.x + NODE_W + 18
      : (prev.x + NODE_W + next.x) / 2
  return { index, x: line, y: node.y }
}

function dropAt(items: NodeView[], wx: number, wy: number): DropAt {
  const seats = items.filter((item) => item.kind !== 'request' && item.kind !== 'strategy')
  const groups = new Map<string, NodeView[]>()
  for (const seat of seats) {
    const key = seat.parentPathId ?? '*'
    const list = groups.get(key) ?? []
    list.push(seat)
    groups.set(key, list)
  }
  let best: { parentPathId: string | null; list: NodeView[]; dy: number } | null = null
  for (const [key, list] of groups) {
    const y = list[0]?.y ?? 0
    const dy = Math.abs(wy - (y + NODE_H / 2))
    if (dy > NODE_H) continue
    if (!best || dy < best.dy) best = { parentPathId: key === '*' ? null : key, list, dy }
  }
  if (!best) return { parentPathId: null, index: Number.MAX_SAFE_INTEGER, x: wx, y: wy }
  const index = best.list.filter((item) => item.x + NODE_W / 2 < wx).length
  const line = gapLine(best.list, index, wx)
  return { parentPathId: best.parentPathId, index, x: line.x, y: line.y }
}

function gapLine(list: NodeView[], index: number, fallbackX: number) {
  const ordered = [...list].sort((a, b) => a.x - b.x)
  const y = (ordered[0]?.y ?? 0)
  if (ordered.length === 0) return { x: fallbackX, y }
  if (index <= 0) return { x: ordered[0].x - 18, y }
  if (index >= ordered.length) return { x: ordered[ordered.length - 1].x + NODE_W + 18, y }
  return { x: (ordered[index - 1].x + NODE_W + ordered[index].x) / 2, y }
}

function kindLabel(kind: Kind, labels: Labels) {
  if (kind === 'request') return labels.request
  if (kind === 'strategy') return labels.strategyKind
  if (kind === 'group') return labels.group
  if (kind === 'branch') return labels.intent
  return labels.model
}

function kindIcon(kind: Kind) {
  if (kind === 'strategy') return <Lightning size={14} />
  if (kind === 'group') return <Stack size={14} />
  if (kind === 'branch') return <GitBranch size={14} />
  if (kind === 'request') return <Tray size={14} />
  return <Cube size={14} />
}

function bend(from: NodeView, to: NodeView) {
  const x1 = from.x + NODE_W * (to.port || 0.5)
  const y1 = from.y + NODE_H
  const x2 = to.x + NODE_W / 2
  const y2 = to.y
  const dy = Math.max(32, Math.abs(y2 - y1) * 0.45)
  const c1y = y1 + dy
  const c2y = y2 - dy
  const t = 0.42
  const mt = 1 - t
  const x = mt ** 3 * x1 + 3 * mt ** 2 * t * x1 + 3 * mt * t ** 2 * x2 + t ** 3 * x2
  const y = mt ** 3 * y1 + 3 * mt ** 2 * t * c1y + 3 * mt * t ** 2 * c2y + t ** 3 * y2
  return { d: `M ${x1} ${y1} C ${x1} ${c1y}, ${x2} ${c2y}, ${x2} ${y2}`, x, y }
}

function packChips(chips: FaceChip[]) {
  if (chips.length <= 3) return chips
  return [
    chips[0],
    chips[1],
    { text: `+${chips.length - 2}`, tone: 'more' as const, title: chips.slice(2).map((chip) => chip.text).join(', ') },
  ]
}

function layoutTree(
  steps: PipelineStep[],
  describe: (step: PipelineStep) => Face,
  skipped: Record<string, 'skipped'> | undefined,
  copy: { requestTitle: string; requestToken?: string; strategyTitle: string; strategyLine: string; strategyChips?: FaceChip[]; labels: Labels },
) {
  const body = placeRow(steps, 0, (NODE_H + GAP_Y) * 2, 'strategy', 0.5, null, describe, skipped, copy.labels)
  const width = Math.max(NODE_W, body.width)
  const strategyX = Math.max(0, (width - NODE_W) / 2)
  const strategyY = NODE_H + GAP_Y
  const request: NodeView = {
    id: 'request',
    kind: 'request',
    title: copy.requestTitle,
    line: copy.labels.requestLine,
    well: copy.labels.request,
    chip: '',
    tone: 'index',
    chips: copy.requestToken ? [{ text: copy.requestToken, tone: 'plain' }] : [],
    facts: [],
    x: strategyX,
    y: 0,
    parentPathId: null,
    index: 0,
    from: null,
    port: 0.5,
    ports: [0.5],
  }
  const strategy: NodeView = {
    id: 'strategy',
    kind: 'strategy',
    title: copy.strategyTitle,
    line: copy.strategyLine,
    well: copy.labels.strategyKind,
    chip: '',
    tone: 'index',
    chips: copy.strategyChips ?? [],
    facts: [],
    x: strategyX,
    y: strategyY,
    parentPathId: null,
    index: 0,
    from: 'request',
    port: 0.5,
    ports: [0.5],
  }
  const items = [request, strategy, ...body.items]
  const rowY = (NODE_H + GAP_Y) * 2
  const rootAdd: AddSpot = body.width > 0
    ? { key: 'root', parentPathId: null, x: body.width + 36, y: rowY + NODE_H / 2, place: 'end' }
    : { key: 'root', parentPathId: null, x: strategyX + NODE_W / 2, y: strategyY + NODE_H + 28, place: 'under' }
  const edges = items.flatMap((node) => {
    if (!node.from) return []
    const from = items.find((item) => item.id === node.from)
    if (!from) return []
    const bent = bend(from, node)
    const tag = pathTag(steps, node)
    return [{ id: `${from.id}>${node.id}`, d: bent.d, tag, tagX: bent.x, tagY: bent.y }]
  })
  return { items, adds: [rootAdd, ...body.adds], edges, width, height: body.height }
}

function pathTag(steps: PipelineStep[], node: NodeView) {
  if (!node.parentPathId) return undefined
  const owner = findPath(steps, node.parentPathId)
  if (!owner) return undefined
  const direct = owner.steps[0]?.id === node.id
  return direct ? owner.label : undefined
}

function findPath(steps: PipelineStep[], pathId: string): { label: string; steps: PipelineStep[] } | null {
  for (const step of steps) {
    for (const path of step.paths ?? []) {
      if (path.id === pathId) return path
      const nested = findPath(path.steps, pathId)
      if (nested) return nested
    }
  }
  return null
}

function placeRow(
  steps: PipelineStep[],
  x: number,
  y: number,
  from: string,
  port: number,
  parentPathId: string | null,
  describe: (step: PipelineStep) => Face,
  skipped: Record<string, 'skipped'> | undefined,
  labels: Labels,
): Box {
  let cursor = x
  let height = y + NODE_H
  const items: NodeView[] = []
  const adds: AddSpot[] = []
  steps.forEach((step, index) => {
    const copy = describe(step)
    const kind: Kind = step.kind
    let blockW = NODE_W
    const nested: NodeView[] = []
    if (step.kind === 'branch') {
      const paths = step.paths ?? []
      let pathX = cursor
      const childY = y + NODE_H + GAP_Y
      paths.forEach((path, pathIndex) => {
        const frac = (pathIndex + 1) / (paths.length + 1)
        const placed = placeRow(path.steps, pathX, childY, step.id, frac, path.id, describe, skipped, labels)
        nested.push(...placed.items)
        adds.push(...placed.adds)
        const columnW = Math.max(placed.width, NODE_W)
        const hasDirect = placed.items.some((item) => item.parentPathId === path.id && item.from === step.id)
        if (hasDirect) {
          adds.push({ key: path.id, parentPathId: path.id, x: pathX + columnW + 36, y: childY + NODE_H / 2, place: 'end' })
        }
        pathX += columnW + GAP_X
        height = Math.max(height, placed.height)
      })
      blockW = Math.max(NODE_W, pathX - cursor - GAP_X)
    }
    const nodeX = cursor + (blockW - NODE_W) / 2
    items.push({
      id: step.id,
      kind,
      title: copy.title,
      line: copy.detail,
      well: copy.note,
      chips: copy.chips ?? [],
      facts: copy.facts ?? [],
      ...chipFor(step, index, skipped, labels),
      provider: step.provider,
      x: nodeX,
      y,
      parentPathId,
      index,
      from,
      port,
      ports: step.kind === 'branch'
        ? (step.paths ?? []).map((_, pathIndex, paths) => (pathIndex + 1) / (paths.length + 1))
        : [],
    })
    if (step.kind === 'branch') {
      const paths = step.paths ?? []
      paths.forEach((path, pathIndex) => {
        const hasDirect = nested.some((item) => item.parentPathId === path.id && item.from === step.id)
        if (hasDirect) return
        const frac = (pathIndex + 1) / (paths.length + 1)
        adds.push({
          key: path.id,
          parentPathId: path.id,
          x: nodeX + NODE_W * frac,
          y: y + NODE_H + 28,
          place: 'under',
        })
      })
    }
    items.push(...nested)
    cursor += blockW + GAP_X
  })
  return { items, width: Math.max(0, cursor - x - (steps.length ? GAP_X : 0)), height, adds }
}

function chipFor(step: PipelineStep, index: number, skipped: Record<string, 'skipped'> | undefined, labels: Labels) {
  if (skipped?.[step.id]) return { chip: labels.skipped, tone: 'skip' as const }
  const incomplete = step.kind === 'member'
    ? !step.provider || !step.model
    : step.kind === 'group'
      ? !step.aggregation_id
      : false
  if (incomplete) return { chip: labels.incomplete, tone: 'warn' as const }
  return { chip: String(index + 1).padStart(2, '0'), tone: 'index' as const }
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

const css = `
.dg-wf { position: relative; height: calc(100dvh - 8.5rem); min-height: 680px; overflow: hidden; border-radius: var(--radius-3xl, 24px); background: var(--surface-secondary); box-shadow: inset 0 0 0 1px var(--border); }
.dg-vp { position: absolute; inset: 0; overflow: hidden; outline: none; touch-action: none; user-select: none; cursor: grab;
  background-image: radial-gradient(circle at 1px 1px, color-mix(in oklch, var(--foreground) 16%, transparent) 0.7px, transparent 1.05px); }
.dg-vp:active, .dg-vp[data-panning] { cursor: grabbing; }
.dg-world { position: absolute; top: 0; left: 0; width: 0; height: 0; transform-origin: 0 0; }
.dg-edges { position: absolute; top: 0; left: 0; width: 1px; height: 1px; overflow: visible; pointer-events: none; }
.dg-edge { fill: none; stroke: color-mix(in oklch, var(--foreground) 28%, transparent); stroke-width: 1.25; stroke-linecap: round; }
.dg-tag { position: absolute; padding: 1px 6px; border-radius: 6px; background: var(--surface); color: var(--muted); font-size: 10px; font-weight: 600; line-height: 16px; white-space: nowrap; pointer-events: none; translate: -50% -50%; box-shadow: 0 0 0 0.5px var(--border); }
.dg-slot { position: absolute; width: 2px; height: ${NODE_H}px; border-radius: 1px; background: var(--accent); translate: -1px 0; pointer-events: none; }
.dg-node { position: absolute; display: flex; flex-direction: column; gap: 6px; padding: 12px 12px 8px; border-radius: var(--field-radius, 12px); background: var(--surface); color: var(--foreground); cursor: grab; box-shadow: 0 0 0 0.5px var(--border), 0 10px 24px -16px color-mix(in oklch, var(--foreground) 22%, transparent); }
.dg-node[data-fixed] { cursor: pointer; }
.dg-node[data-selected] { box-shadow: 0 0 0 1.5px var(--accent), 0 10px 24px -16px color-mix(in oklch, var(--foreground) 18%, transparent); }
.dg-node::before { content: ""; position: absolute; top: 12px; bottom: 12px; left: 0; width: 2px; border-radius: 2px; background: var(--kind); }
.dg-kind { position: absolute; bottom: calc(100% + 6px); left: 0; padding: 2px 6px; border-radius: 5px; background: var(--kind-bg); color: var(--kind); font-size: 10px; font-weight: 600; letter-spacing: .04em; line-height: 16px; white-space: nowrap; }
.dg-node[data-kind="request"] { --kind: var(--accent); --kind-bg: var(--accent-soft); }
.dg-node[data-kind="strategy"] { --kind: var(--chart-3); --kind-bg: color-mix(in oklch, var(--chart-3) 16%, var(--surface)); }
.dg-node[data-kind="member"] { --kind: var(--chart-2); --kind-bg: color-mix(in oklch, var(--chart-2) 16%, var(--surface)); }
.dg-node[data-kind="group"] { --kind: var(--chart-6); --kind-bg: color-mix(in oklch, var(--chart-6) 16%, var(--surface)); }
.dg-node[data-kind="branch"] { --kind: var(--chart-1); --kind-bg: color-mix(in oklch, var(--chart-1) 16%, var(--surface)); }
.dg-head { display: flex; align-items: center; gap: 8px; min-width: 0; height: 24px; }
.dg-head b { min-width: 0; overflow: hidden; font-size: 13px; font-weight: 600; white-space: nowrap; text-overflow: ellipsis; }
.dg-tile { display: grid; flex: none; width: 24px; height: 24px; place-items: center; border-radius: 7px; background: var(--surface-secondary); color: var(--foreground); }
.dg-tile[data-kind="member"] { color: var(--chart-2); }
.dg-tile[data-kind="group"] { color: var(--chart-6); }
.dg-tile[data-kind="branch"] { color: var(--chart-1); }
.dg-chip { display: inline-flex; flex: none; align-items: center; max-width: 88px; height: 20px; padding: 0 6px; border-radius: 6px; background: var(--surface-secondary); color: var(--muted); font-family: var(--font-mono); font-size: 11px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.dg-chip[data-tone="warn"] { background: color-mix(in oklch, var(--warning) 16%, var(--surface)); color: var(--warning); }
.dg-chip[data-tone="skip"] { background: color-mix(in oklch, var(--muted) 16%, var(--surface)); color: var(--muted); }
.dg-line, .dg-facts { display: flex; align-items: center; gap: 4px; height: 18px; min-width: 0; padding-left: 32px; overflow: hidden; }
.dg-line { display: block; font-size: 12px; line-height: 18px; white-space: nowrap; text-overflow: ellipsis; color: var(--muted); }
.dg-fact { flex: 0 1 auto; min-width: 0; overflow: hidden; height: 18px; padding: 0 6px; border-radius: 5px; background: var(--surface-secondary); color: var(--muted); font-size: 11px; line-height: 18px; white-space: nowrap; text-overflow: ellipsis; }
.dg-well { display: flex; align-items: center; height: 36px; margin-top: auto; padding: 4px; overflow: hidden; border-radius: 8px; background: var(--surface-secondary); }
.dg-chips { display: flex; gap: 4px; min-width: 0; margin: -3px; padding: 3px; overflow: hidden; }
.dg-mono { overflow: hidden; min-width: 0; padding: 0 4px; font-family: var(--font-mono); font-size: 11px; line-height: 18px; white-space: nowrap; text-overflow: ellipsis; color: var(--muted); }
.dg-tagchip { display: inline-flex; flex: 0 1 auto; align-items: center; gap: 3px; max-width: 118px; height: 24px; min-width: 0; padding: 0 7px; border-radius: 7px; background: var(--surface); color: var(--foreground); font-size: 12px; line-height: 16px; white-space: nowrap; box-shadow: 0 0 0 0.5px var(--border); }
.dg-tagchip span { overflow: hidden; min-width: 0; text-overflow: ellipsis; }
.dg-tagchip svg { flex: none; }
.dg-tagchip[data-tone="account"] svg { color: var(--muted); }
.dg-tagchip[data-tone="model"] { flex: none; max-width: 132px; background: var(--kind-bg); color: var(--kind); box-shadow: none; }
.dg-tagchip[data-tone="more"] { flex: none; color: var(--muted); font-family: var(--font-mono); }
.dg-port { position: absolute; left: 50%; width: 8px; height: 8px; border-radius: 50%; background: var(--surface); box-shadow: 0 0 0 1px color-mix(in oklch, var(--foreground) 32%, transparent); }
.dg-port-in { top: -4px; translate: -50% 0; }
.dg-port-out { bottom: -4px; translate: -50% 0; }
.dg-x { position: absolute; top: 8px; right: 8px; display: grid; width: 22px; height: 22px; place-items: center; border: 0; border-radius: 7px; background: var(--surface); color: var(--muted); opacity: 0; box-shadow: 0 0 0 0.5px var(--border); }
.dg-node:hover .dg-x, .dg-node[data-selected] .dg-x { opacity: 1; }
.dg-x:hover { color: var(--danger); }
.dg-add { position: absolute; z-index: 2; display: grid; width: 22px; height: 22px; place-items: center; border: 0; border-radius: 50%; background: var(--accent); color: var(--accent-foreground); }
.dg-add[data-place="under"]::before { content: ""; position: absolute; bottom: 100%; left: 10px; width: 1px; height: 16px; background: color-mix(in oklch, var(--foreground) 28%, transparent); }
.dg-add[data-place="end"]::before { content: ""; position: absolute; top: 10px; right: 100%; width: 16px; height: 1px; background: color-mix(in oklch, var(--foreground) 28%, transparent); }
.dg-add:hover { transform: scale(1.08); }
.dg-add:disabled { opacity: 0.5; }
.dg-pal { position: absolute; top: 12px; bottom: 64px; left: 12px; z-index: 3; display: flex; flex-direction: column; gap: 6px; width: 216px; padding: 12px; overflow: auto; border-radius: 16px; background: var(--surface); cursor: default; box-shadow: 0 0 0 0.5px var(--border), 0 12px 28px -16px color-mix(in oklch, var(--foreground) 24%, transparent); }
.dg-pal-title { margin: 0; font-size: 13px; font-weight: 600; }
.dg-pal-hint { margin: 0 0 4px; font-size: 11px; line-height: 15px; color: var(--muted); }
.dg-pal-item, .dg-menu button { display: flex; align-items: center; gap: 8px; width: 100%; min-height: 36px; padding: 6px 8px; border: 0; border-radius: 10px; background: transparent; color: var(--foreground); text-align: left; }
.dg-pal-item { cursor: grab; }
.dg-pal-item span, .dg-menu button { min-width: 0; }
.dg-pal-item b, .dg-menu button { font-size: 13px; font-weight: 500; }
.dg-pal-item small { display: block; overflow: hidden; color: var(--muted); font-size: 11px; line-height: 14px; white-space: nowrap; text-overflow: ellipsis; }
.dg-pal-item:hover, .dg-menu button:hover { background: var(--surface-secondary); }
.dg-menu { position: absolute; z-index: 4; display: flex; flex-direction: column; gap: 2px; width: 180px; padding: 6px; border-radius: 14px; background: var(--surface); box-shadow: 0 0 0 0.5px var(--border), 0 16px 32px -18px color-mix(in oklch, var(--foreground) 30%, transparent); }
.dg-ghost { position: absolute; z-index: 5; width: 180px; height: 64px; padding: 10px; border-radius: 12px; background: var(--surface); pointer-events: none; translate: -90px -28px; box-shadow: 0 0 0 1px var(--accent), 0 16px 32px -18px color-mix(in oklch, var(--foreground) 28%, transparent); }
.dg-ghost:not([data-over]) { opacity: 0.45; }
.dg-tb { position: absolute; bottom: 14px; left: calc(50% + 108px); z-index: 3; display: flex; align-items: center; gap: 2px; padding: 4px; border-radius: 12px; background: var(--surface); translate: -50% 0; box-shadow: 0 0 0 0.5px var(--border); }
.dg-tb button { display: grid; width: 28px; height: 28px; place-items: center; border: 0; border-radius: 8px; background: transparent; color: var(--muted); }
.dg-tb button:hover { background: var(--surface-secondary); color: var(--foreground); }
.dg-zoom { width: auto !important; padding: 0 6px; font-family: var(--font-mono); font-size: 12px; font-variant-numeric: tabular-nums; }
.dg-tb i { width: 1px; height: 16px; margin: 0 4px; background: var(--border); }
@media (max-width: 720px) {
  .dg-pal { width: 168px; }
  .dg-tb { left: calc(50% + 84px); }
}
`
