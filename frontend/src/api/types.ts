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
  models?: Array<{ id: string; mapped_key?: string; stale?: boolean }>
  access?: {
    openai_base_url?: string
    chat_completions?: string
    models?: string
    health?: string
  }
}
