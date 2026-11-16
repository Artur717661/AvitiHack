import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'

const alpha = { user_id: 'demo-intern-1', team_id: 'alpha', role: 'intern' }
const beta = { user_id: 'demo-intern-2', team_id: 'beta', role: 'intern' }
const draft = {
  draft_id: 'draft-1', draft_url: '/drafts/draft-1', resource_name: 'Вики команды Alpha',
  reason: 'Нужны инструкции', status: 'awaiting_confirmation', version: 1,
  request_id: null, next_step: 'Подтверди отправку.',
}
const confirmed = { ...draft, status: 'pending_approval', request_id: 'REQ-a001', next_step: 'Дождись согласования.' }
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })
const fail = (status: number, message: string) => json({ error: { code: 'test', message } }, status)

function connect(routes: Record<string, () => Response | Promise<Response>>) {
  const fetchMock = vi.fn(async (path: string, options?: RequestInit) => {
    const route = routes[(options?.method ?? 'GET') + ' ' + path]
    return route ? route() : fail(404, 'Запрос не найден.')
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('AI-buddy', () => {
  it('signs in, reads an instruction and creates a draft', async () => {
    const fetchMock = connect({
      'GET /api/session': () => fail(401, 'Войди.'),
      'POST /api/demo/login': () => json(alpha),
      'POST /api/demo/logout': () => json({ ok: true }),
      'GET /api/drafts/current': () => json(null),
      'GET /api/knowledge/demo_access_alpha_v1': () => json({ text: 'Тестовый источник' }),
      'POST /api/drafts': () => json(draft, 201),
      'POST /api/chat': () => {
        const latest = fetchMock.mock.calls.at(-1)
        const body = JSON.parse(String(latest?.[1]?.body))
        return body.message.startsWith('Как') ? json({ intent: 'explain_access', text: 'Порядок доступа', draft: null, source: 'demo_access_alpha_v1' }) :
          json({ intent: 'request_access', text: 'Укажи причину', draft: null, needs_reason: true })
      },
    })
    render(<App path="/" />)
    const login = await screen.findByRole('button', { name: 'Войти как стажёр' })
    fireEvent.change(screen.getByLabelText('Логин'), { target: { value: 'demo-intern-1' } })
    fireEvent.change(screen.getByLabelText('Пароль'), { target: { value: 'alpha-demo-1' } })
    fireEvent.click(login)
    await screen.findByRole('heading', { name: 'Помощник стажёра' })
    fireEvent.click(screen.getByRole('button', { name: samples[0] }))
    await screen.findByText('Порядок доступа')
    const source = screen.getByRole('button', { name: 'Инструкция Alpha' })
    fireEvent.click(source)
    await screen.findByText('Тестовый источник')
    fireEvent.click(source)
    expect(screen.queryByText('Тестовый источник')).toBeNull()
    fireEvent.change(screen.getByRole('textbox', { name: 'Сообщение' }), { target: { value: 'Подготовь заявку' } })
    fireEvent.click(screen.getByRole('button', { name: 'Отправить', exact: true }))
    const reason = await screen.findByLabelText('Зачем нужен доступ?')
    fireEvent.change(reason, { target: { value: draft.reason } })
    fireEvent.click(screen.getByRole('button', { name: 'Создать черновик' }))
    const link = await screen.findByRole('link', { name: 'Открыть заявку', exact: true })
    expect(link.getAttribute('href')).toBe(draft.draft_url)
    expect(fetchMock).toHaveBeenCalledWith('/api/drafts', expect.objectContaining({ body: JSON.stringify({ resource_id: 'team_space', reason: draft.reason }) }))
    fireEvent.click(screen.getByRole('button', { name: 'Выйти' }))
    await screen.findByRole('heading', { name: 'Вход стажёра' })
  })

  it('shows waiting, service failure and the preserved ID on retry', async () => {
    let finish: (value: Response) => void = () => {}
    const first = new Promise<Response>(resolve => { finish = resolve })
    let calls = 0
    connect({
      'GET /api/session': () => json(alpha),
      'GET /api/drafts/draft-1': () => json(draft),
      'POST /api/drafts/draft-1/confirm': () => ++calls === 1 ? first : json(confirmed),
      'GET /api/knowledge/demo_intro_alpha_v1': () => json({ text: 'Материалы команды' }),
    })
    render(<App path="/drafts/draft-1" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Отправить заявку' }))
    expect((screen.getByRole('button', { name: 'Отправляем…' }) as HTMLButtonElement).disabled).toBe(true)
    finish(fail(503, 'Сервис заявок временно недоступен.'))
    expect((await screen.findByRole('alert')).textContent).toMatch(/недоступен/)
    fireEvent.click(screen.getByRole('button', { name: 'Отправить заявку' }))
    await screen.findByRole('heading', { name: 'Заявка отправлена' })
    await screen.findByText(confirmed.request_id!, { exact: true })
    fireEvent.click(screen.getByRole('button', { name: 'Вводная команды' }))
    await screen.findByText('Материалы команды')
  })

  it('denies the scenario to the second team', async () => {
    const fetchMock = connect({ 'GET /api/session': () => json(beta) })
    render(<App path="/drafts/draft-1" />)
    await screen.findByRole('heading', { name: 'Сценарий недоступен' })
    expect(fetchMock.mock.calls.length).toBe(1)
  })

  it('handles failed chat and offers the direct form', async () => {
    connect({
      'GET /api/session': () => json(alpha),
      'GET /api/drafts/current': () => json(null),
      'POST /api/chat': () => { throw new Error('network disconnected') },
      'POST /api/drafts': () => json({ unexpected: true }),
    })
    render(<App path="/" />)
    fireEvent.click(await screen.findByRole('button', { name: samples[0] }))
    expect((await screen.findByRole('alert')).textContent).toMatch(/Связь/)
    fireEvent.click(screen.getByRole('button', { name: 'Открыть форму черновика' }))
    fireEvent.change(screen.getByLabelText('Зачем нужен доступ?'), { target: { value: 'Нужна вики' } })
    fireEvent.click(screen.getByRole('button', { name: 'Создать черновик' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toMatch(/неожиданный ответ/))
  })

  it('keeps a draft form open during the reply and lets the user retry the source', async () => {
    let complete: (value: Response) => void = () => {}
    const pending = new Promise<Response>(resolve => { complete = resolve })
    let sourceAttempts = 0
    connect({
      'GET /api/session': () => json(alpha),
      'GET /api/drafts/current': () => json(null),
      'POST /api/chat': () => pending,
      'GET /api/knowledge/demo_access_alpha_v1': () => ++sourceAttempts === 1
        ? fail(503, 'Инструкция временно недоступна.')
        : json({ text: 'Инструкция доступна.' }),
    })
    render(<App path="/" />)
    fireEvent.click(await screen.findByRole('button', { name: samples[0] }))
    fireEvent.click(screen.getByRole('button', { name: 'Открыть форму черновика' }))
    fireEvent.change(screen.getByLabelText('Зачем нужен доступ?'), { target: { value: 'Нужна вики' } })
    complete(json({ intent: 'explain_access', text: 'Порядок доступа', draft: null, source: 'demo_access_alpha_v1' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Инструкция Alpha' }))
    expect((screen.getByLabelText('Зачем нужен доступ?') as HTMLTextAreaElement).value).toBe('Нужна вики')
    fireEvent.click(await screen.findByRole('button', { name: 'Повторить загрузку' }))
    await screen.findByText('Инструкция доступна.')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('ignores a previous account’s chat reply after sign-out', async () => {
    let complete: (value: Response) => void = () => {}
    const pending = new Promise<Response>(resolve => { complete = resolve })
    connect({
      'GET /api/session': () => json(alpha),
      'GET /api/drafts/current': () => json(null),
      'POST /api/chat': () => pending,
      'POST /api/demo/logout': () => json({ ok: true }),
      'POST /api/demo/login': () => json({ ...alpha, user_id: 'demo-intern-3' }),
    })
    render(<App path="/" />)
    fireEvent.click(await screen.findByRole('button', { name: samples[0] }))
    fireEvent.click(screen.getByRole('button', { name: 'Выйти' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Войти как стажёр' }))
    await screen.findByRole('heading', { name: 'Помощник стажёра' })
    await act(async () => complete(json({ intent: 'request_status', text: 'Ответ первой учётки', draft })))
    expect(screen.queryByText('Ответ первой учётки')).toBeNull()
    expect(screen.queryByRole('link', { name: /Открыть заявку/ })).toBeNull()
  })
})

const samples = ['Как получить доступ к вики?']
