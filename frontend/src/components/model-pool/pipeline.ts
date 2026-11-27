import type { AggregationMember } from '@/api/modelPools'

export const ENTRY_ID = 'entry'
export const STRATEGY_ID = 'strategy'

export type PipelineClassifier = {
  provider: string
  region?: string
  model: string
}

export type PipelineStep = {
  id: string
  kind: 'member' | 'group' | 'branch'
  provider?: string
  region?: string
  model?: string
  account_id?: string
  /** Checked accounts, in check order. Empty means every account of the provider. Not stored yet. */
  account_ids?: string[]
  weight?: number
  aggregation_id?: string
  classifier?: PipelineClassifier
  paths?: PipelinePath[]
}

export type PipelinePath = {
  id: string
  label: string
  keywords?: string[]
  intent?: string
  fallback?: boolean
  steps: PipelineStep[]
}

export type PipelineDocument = {
  version: 1
  steps: PipelineStep[]
}

const RESERVED = new Set([ENTRY_ID, STRATEGY_ID])

export function emptyDocument(): PipelineDocument {
  return { version: 1, steps: [] }
}

export function documentFromRecord(document: string | undefined, members: AggregationMember[]): PipelineDocument {
  const raw = String(document || '').trim()
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as PipelineDocument
      if (parsed && parsed.version === 1 && Array.isArray(parsed.steps) && parsed.steps.length > 0) {
        return { version: 1, steps: parsed.steps.map(reserveStep) }
      }
    } catch {
      // A document that does not parse still opens as the saved member list.
    }
  }
  return documentFromMembers(members)
}

export function documentFromMembers(members: AggregationMember[]): PipelineDocument {
  const ordered = [...members].sort((a, b) => a.priority - b.priority || a.model.localeCompare(b.model))
  return {
    version: 1,
    steps: ordered.map((member, index) => ({
      id: `member-${index + 1}`,
      kind: 'member' as const,
      provider: member.provider,
      region: member.region || '',
      model: member.model,
      ...(member.account_id ? { account_id: member.account_id, account_ids: [member.account_id] } : {}),
      weight: member.weight > 0 ? member.weight : 100,
    })),
  }
}

let stepSeq = 0

export function createStep(kind: PipelineStep['kind'], labels: { match: string; fallback: string }): PipelineStep {
  stepSeq += 1
  const id = `${kind}-${Date.now().toString(36)}${stepSeq.toString(36)}`
  if (kind === 'branch') {
    return {
      id,
      kind,
      paths: [
        { id: `${id}-match`, label: labels.match, keywords: [], intent: '', steps: [] },
        { id: `${id}-default`, label: labels.fallback, fallback: true, steps: [] },
      ],
    }
  }
  if (kind === 'group') return { id, kind, aggregation_id: '', weight: 100 }
  return { id, kind, provider: '', region: '', model: '', weight: 100 }
}

export function insertStep(doc: PipelineDocument, parentPathId: string | null, index: number, step: PipelineStep): PipelineDocument {
  return { version: 1, steps: editList(doc.steps, parentPathId, (list) => {
    const next = [...list]
    const at = Math.max(0, Math.min(index, next.length))
    next.splice(at, 0, step)
    return next
  }) }
}

export function moveStep(doc: PipelineDocument, parentPathId: string | null, from: number, to: number): PipelineDocument {
  return { version: 1, steps: editList(doc.steps, parentPathId, (list) => {
    if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list
    const next = [...list]
    const [item] = next.splice(from, 1)
    next.splice(to, 0, item)
    return next
  }) }
}

export function removeStep(doc: PipelineDocument, id: string): PipelineDocument {
  return { version: 1, steps: withoutStep(doc.steps, id) }
}

export function updateStep(doc: PipelineDocument, id: string, patch: Partial<PipelineStep>): PipelineDocument {
  return {
    version: 1,
    steps: mapSteps(doc.steps, id, (step) => ({ ...step, ...patch, id: step.id, kind: step.kind })),
  }
}

export function updatePath(doc: PipelineDocument, pathId: string, patch: Partial<PipelinePath>): PipelineDocument {
  const visit = (steps: PipelineStep[]): PipelineStep[] => steps.map((step) => {
    if (step.kind !== 'branch' || !step.paths) return step
    return {
      ...step,
      paths: step.paths.map((path) => path.id === pathId
        ? { ...path, ...patch, id: path.id, steps: path.steps }
        : { ...path, steps: visit(path.steps) }),
    }
  })
  return { version: 1, steps: visit(doc.steps) }
}

export function setOnlyFallback(doc: PipelineDocument, pathId: string): PipelineDocument {
  const visit = (steps: PipelineStep[]): PipelineStep[] => steps.map((step) => {
    if (step.kind !== 'branch' || !step.paths) return step
    const hit = step.paths.some((path) => path.id === pathId)
    return {
      ...step,
      paths: step.paths.map((path) => ({
        ...path,
        fallback: hit ? path.id === pathId : path.fallback,
        steps: visit(path.steps),
      })),
    }
  })
  return { version: 1, steps: visit(doc.steps) }
}

export function addPath(doc: PipelineDocument, branchId: string, label: string): PipelineDocument {
  return {
    version: 1,
    steps: mapSteps(doc.steps, branchId, (step) => {
      if (step.kind !== 'branch') return step
      const paths = step.paths ?? []
      const id = `${branchId}-p${paths.length}-${Date.now().toString(36)}`
      return {
        ...step,
        paths: [...paths, { id, label, keywords: [], intent: '', fallback: false, steps: [] }],
      }
    }),
  }
}

export function removePath(doc: PipelineDocument, pathId: string): PipelineDocument {
  const visit = (steps: PipelineStep[]): PipelineStep[] => steps.map((step) => {
    if (step.kind !== 'branch' || !step.paths) return step
    const removed = step.paths.some((path) => path.id === pathId)
    let paths = step.paths
      .filter((path) => path.id !== pathId)
      .map((path) => ({ ...path, steps: visit(path.steps) }))
    if (removed && paths.length > 0 && !paths.some((path) => path.fallback)) {
      paths = paths.map((path, index) => ({ ...path, fallback: index === paths.length - 1 }))
    }
    return { ...step, paths }
  })
  return { version: 1, steps: visit(doc.steps) }
}

export function findStep(steps: PipelineStep[], id: string): PipelineStep | null {
  for (const step of steps) {
    if (step.id === id) return step
    for (const path of step.paths ?? []) {
      const nested = findStep(path.steps, id)
      if (nested) return nested
    }
  }
  return null
}

export type PipelineIssue = 'empty' | 'incomplete' | 'group' | 'self' | 'classifier'

export function validatePipeline(doc: PipelineDocument, selfId: string): PipelineIssue | null {
  let reachable = 0
  let issue: PipelineIssue | null = null
  const walk = (steps: PipelineStep[]) => {
    for (const step of steps) {
      if (step.kind === 'member') {
        if (!step.provider || !step.model) issue = issue ?? 'incomplete'
        else reachable += 1
      } else if (step.kind === 'group') {
        if (!step.aggregation_id) issue = issue ?? 'group'
        else if (selfId && step.aggregation_id === selfId) issue = issue ?? 'self'
        else reachable += 1
      } else if (step.kind === 'branch') {
        const classifier = step.classifier
        if (classifier && Boolean(classifier.provider) !== Boolean(classifier.model)) issue = issue ?? 'classifier'
        for (const path of step.paths ?? []) walk(path.steps)
      }
    }
  }
  walk(doc.steps)
  if (!doc.steps.length || reachable === 0) return issue ?? 'empty'
  return issue
}

export function selectedAccountIds(step: PipelineStep): string[] {
  const ids = (step.account_ids ?? []).map((id) => id.trim()).filter(Boolean)
  if (ids.length) return ids
  const one = step.account_id?.trim()
  return one ? [one] : []
}

export function accountRangePending(doc: PipelineDocument): boolean {
  let pending = false
  const walk = (steps: PipelineStep[]) => {
    for (const step of steps) {
      if (step.kind === 'member' && selectedAccountIds(step).length > 1) pending = true
      for (const path of step.paths ?? []) walk(path.steps)
    }
  }
  walk(doc.steps)
  return pending
}

export function canonicalDocument(doc: PipelineDocument): PipelineDocument {
  return { version: 1, steps: doc.steps.map(cleanStep) }
}

export function previewPathStatus(doc: PipelineDocument, pathLabel: string): Record<string, 'skipped'> {
  if (!pathLabel) return {}
  const status: Record<string, 'skipped'> = {}
  const visit = (steps: PipelineStep[]) => {
    for (const step of steps) {
      if (step.kind !== 'branch' || !step.paths?.length) continue
      const chosen = step.paths.find((path) => path.label === pathLabel)
      if (chosen) {
        for (const path of step.paths) {
          if (path.id !== chosen.id) status[path.id] = 'skipped'
        }
      }
      for (const path of step.paths) visit(path.steps)
    }
  }
  visit(doc.steps)
  return status
}

function reserveStep(step: PipelineStep): PipelineStep {
  const id = RESERVED.has(step.id) ? `step-${step.id}` : step.id
  return {
    ...step,
    id,
    paths: step.paths?.map((path) => ({ ...path, steps: path.steps.map(reserveStep) })),
  }
}

function mapSteps(steps: PipelineStep[], id: string, fn: (step: PipelineStep) => PipelineStep): PipelineStep[] {
  return steps.map((step) => {
    if (step.id === id) return fn(step)
    if (step.kind !== 'branch' || !step.paths) return step
    return { ...step, paths: step.paths.map((path) => ({ ...path, steps: mapSteps(path.steps, id, fn) })) }
  })
}

function editList(
  steps: PipelineStep[],
  parentPathId: string | null,
  edit: (list: PipelineStep[]) => PipelineStep[],
): PipelineStep[] {
  if (!parentPathId) return edit(steps)
  return steps.map((step) => {
    if (step.kind !== 'branch' || !step.paths) return step
    return {
      ...step,
      paths: step.paths.map((path) => path.id === parentPathId
        ? { ...path, steps: edit(path.steps) }
        : { ...path, steps: editList(path.steps, parentPathId, edit) }),
    }
  })
}

function withoutStep(steps: PipelineStep[], id: string): PipelineStep[] {
  return steps.filter((step) => step.id !== id).map((step) => {
    if (step.kind !== 'branch' || !step.paths) return step
    return { ...step, paths: step.paths.map((path) => ({ ...path, steps: withoutStep(path.steps, id) })) }
  })
}

function cleanStep(step: PipelineStep): PipelineStep {
  if (step.kind === 'group') {
    return {
      id: step.id,
      kind: 'group',
      aggregation_id: step.aggregation_id || '',
      weight: positiveWeight(step.weight),
    }
  }
  if (step.kind === 'branch') {
    const classifier = step.classifier
    return {
      id: step.id,
      kind: 'branch',
      ...(classifier?.provider && classifier.model
        ? { classifier: { provider: classifier.provider, ...(classifier.region ? { region: classifier.region } : {}), model: classifier.model } }
        : {}),
      paths: (step.paths ?? []).map((path) => {
        const keywords = (path.keywords ?? []).map((word) => word.trim()).filter(Boolean)
        const intent = (path.intent ?? '').trim()
        return {
          id: path.id,
          label: path.label.trim() || path.id,
          ...(keywords.length ? { keywords } : {}),
          ...(intent ? { intent } : {}),
          ...(path.fallback ? { fallback: true } : {}),
          steps: path.steps.map(cleanStep),
        }
      }),
    }
  }
  return {
    id: step.id,
    kind: 'member',
    provider: step.provider || '',
    ...(step.region ? { region: step.region } : {}),
    model: step.model || '',
    ...(selectedAccountIds(step).length === 1 ? { account_id: selectedAccountIds(step)[0] } : {}),
    weight: positiveWeight(step.weight),
  }
}

function positiveWeight(weight: number | undefined) {
  if (!weight || weight < 1) return 100
  if (weight > 1_000_000) return 1_000_000
  return Math.round(weight)
}
