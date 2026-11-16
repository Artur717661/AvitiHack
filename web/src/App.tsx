import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type Actor, type Draft, type Reply } from './api'

const samples = ['Как получить доступ к вики?', 'Подготовь заявку на доступ', 'Что с моей заявкой?']
const stageNames = ['Разобраться с доступом', 'Подготовить черновик', 'Подтвердить отправку', 'Дождаться согласования']
type Message = { text: string; from: 'user' | 'buddy'; reply?: Reply }

function notice(error: unknown) {
  return error instanceof Error ? error.message : 'Не удалось выполнить запрос.'
}

function Login({ onLogin }: { onLogin: (actor: Actor) => void }) {
  const [id, setID] = useState('demo-intern-1')
  const [password, setPassword] = useState('alpha-demo-1')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try { onLogin(await api.login(id, password)) }
    catch (error) { setError(notice(error)) }
    finally { setBusy(false) }
  }
  return <div className="login-page">
    <div className="login-copy">
      <Brand />
      <h1>Доступ к вики<br/>с первого запроса</h1>
      <p>Узнай порядок работы команды, подготовь заявку и следи за её статусом в одном месте.</p>
    </div>
    <form className="login-card" onSubmit={submit}>
      <h2>Вход стажёра</h2>
      <p>Для демонстрации уже заполнена тестовая учётка команды Alpha.</p>
      <label>Логин<input value={id} autoComplete="username" onChange={event => setID(event.target.value)} required /></label>
      <label>Пароль<input type="password" value={password} autoComplete="current-password" onChange={event => setPassword(event.target.value)} required /></label>
      <button className="primary" disabled={busy}>{busy ? 'Входим…' : 'Войти как стажёр'}</button>
      {error && <p role="alert" className="error">{error}</p>}
    </form>
  </div>
}

function Brand() {
  return <a className="brand" href="/"><span aria-hidden="true" className="mark"><i /><i /><i /></span>AI-buddy</a>
}

function Workflow({ draft, started }: { draft: Draft | null; started: boolean }) {
  const current = draft?.request_id ? 3 : draft ? 2 : started ? 1 : 0
  return <section className="workflow">
    <h2>Путь стажёра</h2>
    <ol>{stageNames.map((name, index) => <li key={name} className={index === current ? 'current' : index < current ? 'complete' : ''} aria-current={index === current ? 'step' : undefined}>
      <span aria-hidden="true">{index < current ? '✓' : index + 1}</span>{name}
    </li>)}</ol>
    <div className="next"><strong>Следующий шаг</strong><p>{draft?.next_step || (started ? 'Подготовь черновик и укажи причину доступа.' : 'Спроси, как получить доступ к вики.')}</p></div>
    {draft?.request_id && <p className="ticket">{draft.request_id} · На рассмотрении</p>}
  </section>
}

function Source({ id }: { id: string }) {
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  async function load() {
    if (loading) return
    setError('')
    setLoading(true)
    try { setText((await api.knowledge(id)).text) }
    catch (error) { setError(notice(error)) }
    finally { setLoading(false) }
  }
  function toggle() {
    setOpen(!open)
    if (!open && !text) void load()
  }
  return <div className="source">
    <button type="button" className="link" onClick={toggle} aria-expanded={open}>{id.includes('intro') ? 'Вводная команды' : 'Инструкция Alpha'}</button>
    {open && (error ? <div className="error"><p role="alert">{error}</p><button type="button" className="link" onClick={() => void load()}>Повторить загрузку</button></div> :
      <div className="source-text">{loading ? 'Загружаем…' : text}</div>)}
  </div>
}

function Chat({ draft, setDraft }: { draft: Draft | null; setDraft: (draft: Draft | null) => void }) {
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [reason, setReason] = useState('')
  const [reasonOpen, setReasonOpen] = useState(false)
  const [busy, setBusy] = useState<'chat' | 'draft' | null>(null)
  const [error, setError] = useState('')
  const [started, setStarted] = useState(false)
  async function send(message: string) {
    if (busy || !message.trim()) return
    setInput('')
    setError('')
    setBusy('chat')
    setMessages(old => [...old, { from: 'user', text: message }])
    try {
      const reply = await api.ask(message)
      setMessages(old => [...old, { from: 'buddy', text: reply.text, reply }])
      setDraft(reply.draft)
      if (reply.intent === 'explain_access' || reply.intent === 'request_access') setStarted(true)
      setReasonOpen(open => reply.draft ? false : open || !!reply.needs_reason)
    } catch (error) { setError(notice(error)) }
    finally { setBusy(null) }
  }
  async function prepare(event: FormEvent) {
    event.preventDefault()
    if (busy) return
    setError('')
    setBusy('draft')
    try {
      const created = await api.prepare(reason)
      setDraft(created)
      setReasonOpen(false)
      const text = created.request_id
        ? `${created.request_id} уже на рассмотрении. ${created.next_step}`
        : 'Черновик сохранён. Проверь данные на странице заявки.'
      setMessages(old => [...old, { from: 'buddy', text, reply: { intent: 'request_access', text, draft: created } }])
    } catch (error) { setError(notice(error)) }
    finally { setBusy(null) }
  }
  return <main className="workspace">
    <section className="column">
      <p className="overline">АДАПТАЦИЯ · КОМАНДА ALPHA</p>
      <h1>Помощник стажёра</h1>
      <p className="lede">Заявка на доступ к вики команды</p>
      <div className="conversation" aria-label="Чат с AI-buddy">
        <div className="welcome"><strong>AI-buddy</strong><p>Привет! Помогу разобраться, как получить доступ к вики Alpha, и подготовить заявку.</p></div>
        <div className="suggestions">{samples.map(sample => <button key={sample} disabled={!!busy} onClick={() => void send(sample)}>{sample}</button>)}</div>
        <div className="messages" aria-live="polite">
          {messages.map((message, index) => <div className={'message ' + message.from} key={index}>
            <strong>{message.from === 'user' ? 'Ты' : 'AI-buddy'}</strong>
            <p>{message.text}</p>
            {message.reply?.source && <Source id={message.reply.source} />}
            {message.reply?.draft && <a href={message.reply.draft.draft_url}>Открыть заявку</a>}
          </div>)}
          {busy === 'chat' && <p className="waiting" role="status">Помощник готовит ответ…</p>}
        </div>
        {reasonOpen && <form className="reason" onSubmit={prepare}>
          <label>Зачем нужен доступ?<textarea value={reason} onChange={event => setReason(event.target.value)} maxLength={400} required placeholder="Например, нужны инструкции для первой задачи" /></label>
          <button className="primary" disabled={!!busy}>{busy === 'draft' ? 'Сохраняем…' : 'Создать черновик'}</button>
        </form>}
        {error && <p className="error" role="alert">{error}</p>}
        <form className="compose" onSubmit={event => { event.preventDefault(); void send(input) }}>
          <input aria-label="Сообщение" value={input} onChange={event => setInput(event.target.value)} maxLength={500} placeholder="Напиши вопрос о доступе" />
          <button className="primary" disabled={!!busy || !input.trim()}>Отправить</button>
        </form>
        {!reasonOpen && !draft && <button className="link form-link" onClick={() => setReasonOpen(true)}>Открыть форму черновика</button>}
      </div>
    </section>
    <aside><Workflow draft={draft} started={started} />{draft && <a className="side-link" href={draft.draft_url}>Открыть заявку →</a>}</aside>
  </main>
}

function DraftPage({ id, draft, setDraft }: { id: string; draft: Draft | null; setDraft: (draft: Draft | null) => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let active = true
    api.draft(id).then(result => { if (active) setDraft(result) })
      .catch(error => { if (active) setError(notice(error)) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [id, setDraft])
  async function confirm() {
    if (!draft || busy) return
    setBusy(true)
    setError('')
    try { setDraft(await api.confirm(draft.draft_id, draft.version)) }
    catch (error) { setError(notice(error)) }
    finally { setBusy(false) }
  }
  return <main className="workspace">
    <section className="column">
      <a className="back" href="/">← К помощнику</a>
      <h1>{draft?.request_id ? 'Заявка отправлена' : 'Заявка на доступ'}</h1>
      {loading && <p role="status">Загружаем заявку…</p>}
      {draft && <div className="draft-card">
        <h2>{draft.resource_name}</h2>
        <dl><dt>Причина доступа</dt><dd>{draft.reason}</dd><dt>Статус</dt><dd>{draft.request_id ? 'На рассмотрении' : 'Ожидает подтверждения'}</dd>
          {draft.request_id && <><dt>Номер заявки</dt><dd className="request-number">{draft.request_id}</dd></>}
        </dl>
        <p>{draft.next_step}</p>
        {draft.request_id ? <Source id="demo_intro_alpha_v1" /> : <button className="primary" onClick={() => void confirm()} disabled={busy}>{busy ? 'Отправляем…' : 'Отправить заявку'}</button>}
      </div>}
      {error && <p className="error" role="alert">{error}</p>}
    </section>
    <aside><Workflow draft={draft} started /></aside>
  </main>
}

function Workspace({ id }: { id?: string }) {
  const [draft, setDraft] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (id) return
    let active = true
    api.current().then(result => { if (active) setDraft(current => current ?? result) })
      .catch(error => { if (active) setError(notice(error)) })
    return () => { active = false }
  }, [id])
  return <>
    {error && <p className="error standalone" role="alert">{error}</p>}
    {id ? <DraftPage id={id} draft={draft} setDraft={setDraft} /> : <Chat draft={draft} setDraft={setDraft} />}
  </>
}

export function App({ path = window.location.pathname }: { path?: string }) {
  const [actor, setActor] = useState<Actor | null>(null)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState('')
  const id = path.match(/^\/drafts\/([^/]+)$/)?.[1]
  useEffect(() => {
    let active = true
    api.session().then(user => { if (active) setActor(user) }).catch(error => {
      if (active && !(error instanceof ApiError && error.status === 401)) setError(notice(error))
    }).finally(() => { if (active) setReady(true) })
    return () => { active = false }
  }, [])
  async function logout() {
    setError('')
    try { await api.logout(); setActor(null) }
    catch (error) { setError(notice(error)) }
  }
  if (!ready) return <p className="standalone" role="status">Загружаем AI-buddy…</p>
  if (!actor) return <>{error && <p className="error standalone" role="alert">{error}</p>}<Login onLogin={user => { setError(''); setActor(user) }} /></>
  return <>
    <header className="header"><Brand /><div><span>Стажёр · {actor.team_id.toUpperCase()}</span><button className="quiet" onClick={() => void logout()}>Выйти</button></div></header>
    {error && <p className="error standalone" role="alert">{error}</p>}
    {actor.team_id !== 'alpha' ? <main className="standalone"><h1>Сценарий недоступен</h1><p>Запросить доступ к вики Alpha могут стажёры этой команды. Обратись к своему наставнику.</p></main> :
      <Workspace key={actor.user_id} id={id} />}
    <footer>Тестовая среда · Синтетические инструкции и заявки</footer>
  </>
}
