export type AccountQuota = {
  used?: number
  total?: number
  remaining?: number
  percentage?: number
  unit?: string
  exceeded?: boolean
  has_add_on?: boolean
  add_on_used?: number
  add_on_total?: number
  add_on_unit?: string
  fetched_at?: string
}

export type Overview = {
  ok?: boolean
  time?: string
  proxy?: {
    ok?: boolean
    service?: string
    provider?: string
    port?: number | string
    chat_url?: string
  }
  worker?: {
    ok?: boolean
    hot?: boolean
    endpoint?: string
    rewarmCount?: number
    rewarm_count?: number
    lastError?: string
    last_error?: string
  }
  auth?: {
    has_user_blob?: boolean
    has_pat?: boolean
    user_blob_bytes?: number
    machine_id?: string
  }
  login?: any
  models?: Array<{
    id: string
    display_name?: string
    mapped_key?: string
    route_display_name?: string
    settings_key?: string
    stale?: boolean
    context_length?: number
    default_context_length?: number
    context_custom?: boolean
  }>
  accounts?: Array<{
    id: string
    provider?: string
    name?: string
    remote_uid?: string
    auth_type?: string
    enabled?: boolean
    max_inflight?: number
    priority?: number
    status?: string
    cooldown_until?: string | null
    url?: string
    ready?: boolean
    hot?: boolean
    in_flight?: number
    inFlight?: number
    restarts?: number
    kind?: string
    down_until?: string | null
    last_error?: string
    lastError?: string
    last_error_kind?: string
    quota?: AccountQuota
    created_at?: string
    updated_at?: string
  }>
  access?: {
    openai_base_url?: string
    chat_completions?: string
    models?: string
    health?: string
  }
}
