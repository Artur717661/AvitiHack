import { z } from 'zod'

const actor = z.object({ user_id: z.string(), team_id: z.string(), role: z.string() })
const draft = z.object({
  draft_id: z.string(),
  draft_url: z.string().startsWith('/drafts/'),
  resource_name: z.string(),
  reason: z.string(),
  status: z.enum(['awaiting_confirmation', 'pending_approval']),
  version: z.number().int(),
  request_id: z.string().nullable(),
  next_step: z.string(),
})
const reply = z.object({
  intent: z.string(),
  text: z.string(),
  draft: draft.nullable(),
  needs_reason: z.boolean().optional(),
  source: z.enum(['demo_access_alpha_v1', 'demo_intro_alpha_v1']).optional(),
})
const problem = z.object({ error: z.object({ code: z.string(), message: z.string() }) })
export type Actor = z.infer<typeof actor>
export type Draft = z.infer<typeof draft>
export type Reply = z.infer<typeof reply>

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function request<T>(path: string, schema: z.ZodType<T>, data?: object): Promise<T> {
  let response: Response
  const signal = AbortSignal.timeout(25_000)
  try {
    response = await fetch('/api/' + path, data === undefined ? { signal } : {
      signal,
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    })
  } catch {
    if (signal.aborted) throw new ApiError(0, 'Сервер не ответил вовремя. Попробуй ещё раз.')
    throw new ApiError(0, 'Связь с сервером прервалась. Попробуй ещё раз.')
  }
  const body: unknown = await response.json().catch(() => null)
  if (!response.ok) {
    const parsed = problem.safeParse(body)
    throw new ApiError(response.status, parsed.success ? parsed.data.error.message : 'Сервер вернул ошибку.')
  }
  const parsed = schema.safeParse(body)
  if (!parsed.success) throw new ApiError(response.status, 'Сервер вернул неожиданный ответ.')
  return parsed.data
}

export const api = {
  session: () => request('session', actor),
  login: (user_id: string, password: string) => request('demo/login', actor, { user_id, password }),
  logout: () => request('demo/logout', z.object({ ok: z.boolean() }), {}),
  ask: (message: string) => request('chat', reply, { message }),
  current: () => request('drafts/current', draft.nullable()),
  draft: (id: string) => request('drafts/' + encodeURIComponent(id), draft),
  prepare: (reason: string) => request('drafts', draft, { resource_id: 'team_space', reason }),
  confirm: (id: string, version: number) => request('drafts/' + encodeURIComponent(id) + '/confirm', draft, { version }),
  knowledge: (source: string) => request('knowledge/' + encodeURIComponent(source), z.object({ text: z.string() })),
}
