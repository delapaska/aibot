import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, ApiError } from './api'
import { formatAmount, formatDay, isoOffsetDays, parseAmountToKopecks, todayISO } from './format'
import { CATEGORIES, categoryTitle } from './types'
import type { CategorySlug, Expense, User } from './types'

// period presets: daysBack null = all time, -1 = custom range
type PeriodKey = 'all' | 'today' | 'week' | 'month' | 'custom'
const PERIODS: { key: PeriodKey; title: string; daysBack: number | null }[] = [
  { key: 'all', title: 'За всё время', daysBack: null },
  { key: 'today', title: 'Сегодня', daysBack: 0 },
  { key: 'week', title: 'Неделя', daysBack: 7 },
  { key: 'month', title: 'Месяц', daysBack: 30 },
  { key: 'custom', title: 'Свой период', daysBack: -1 },
]

export default function App() {
  const [users, setUsers] = useState<User[]>([])
  const [userId, setUserId] = useState<number | null>(null)
  const [period, setPeriod] = useState<PeriodKey>('month')
  const [fromISO, setFromISO] = useState(isoOffsetDays(todayISO(), -30))
  const [toISO, setToISO] = useState(todayISO())
  const [category, setCategory] = useState<string>('')
  const [expenses, setExpenses] = useState<Expense[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<Expense | null>(null)
  const [showAdd, setShowAdd] = useState(false)

  const periodRange = useMemo((): { from?: string; to?: string } => {
    const p = PERIODS.find((p) => p.key === period)!
    if (p.daysBack === null) return {}
    if (p.daysBack === -1) return { from: fromISO, to: toISO }
    return { from: isoOffsetDays(todayISO(), -p.daysBack), to: todayISO() }
  }, [period, fromISO, toISO])

  const reload = useCallback(async () => {
    if (!userId) return
    setLoading(true)
    setError(null)
    try {
      const page = await api.listExpenses({
        user_id: userId,
        from: periodRange.from,
        to: periodRange.to,
        category: category || undefined,
        limit: 200,
      })
      setExpenses(page.items)
      setTotal(page.total)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Неизвестная ошибка')
      setExpenses([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [userId, periodRange, category])

  useEffect(() => {
    api
      .listUsers()
      .then((r) => {
        setUsers(r.items)
        if (r.items.length > 0) setUserId(r.items[0].id)
      })
      .catch((e) => setError(e instanceof ApiError ? e.message : 'Неизвестная ошибка'))
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const currentUser = users.find((u) => u.id === userId)

  const sum = useMemo(
    () => expenses.reduce((acc, e) => acc + Math.round(Number(e.amount) * 100), 0),
    [expenses],
  )

  async function onDelete(e: Expense) {
    if (!window.confirm(`Удалить трату «${formatAmount(e.amount)} ${categoryTitle(e.category)}»?`)) return
    try {
      await api.deleteExpense(e.id)
      await reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Не удалось удалить')
    }
  }

  return (
    <div className="app">
      <header className="header">
        <h1>Траты семьи</h1>
        <button className="btn btn-primary" onClick={() => setShowAdd(true)} disabled={!currentUser}>
          + Добавить трату
        </button>
      </header>

      <section className="filters">
        <label>
          Член семьи
          <select value={userId ?? ''} onChange={(e) => setUserId(Number(e.target.value))}>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {u.display_name}
              </option>
            ))}
          </select>
        </label>

        <label>
          Период
          <select value={period} onChange={(e) => setPeriod(e.target.value as PeriodKey)}>
            {PERIODS.map((p) => (
              <option key={p.key} value={p.key}>
                {p.title}
              </option>
            ))}
          </select>
        </label>

        {period === 'custom' && (
          <label className="dates">
            с
            <input type="date" value={fromISO} onChange={(e) => setFromISO(e.target.value)} />
            по
            <input type="date" value={toISO} onChange={(e) => setToISO(e.target.value)} />
          </label>
        )}

        <label>
          Категория
          <select value={category} onChange={(e) => setCategory(e.target.value)}>
            <option value="">Все</option>
            {CATEGORIES.map((c) => (
              <option key={c.slug} value={c.slug}>
                {c.title}
              </option>
            ))}
          </select>
        </label>
      </section>

      {error && <div className="error">{error}</div>}

      <section className="table-wrap">
        {loading ? (
          <div className="muted">Загрузка…</div>
        ) : expenses.length === 0 ? (
          <div className="muted">Расходов не найдено</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Дата</th>
                <th>Сумма</th>
                <th>Категория</th>
                <th>Комментарий</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {expenses.map((e) => (
                <tr key={e.id}>
                  <td>{formatDay(e.spent_at)}</td>
                  <td className="num">{formatAmount(e.amount)}</td>
                  <td>
                    <span className={`chip chip-${e.category}`}>{categoryTitle(e.category)}</span>
                  </td>
                  <td className="comment">{e.comment || '—'}</td>
                  <td className="row-actions">
                    <button className="btn btn-ghost" onClick={() => setEditing(e)}>
                      Изменить
                    </button>
                    <button className="btn btn-ghost btn-danger" onClick={() => void onDelete(e)}>
                      Удалить
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {expenses.length > 0 && (
          <div className="muted total-line">
            Показано {expenses.length} из {total} · сумма: <b>{formatAmount((sum / 100).toFixed(2))}</b>
          </div>
        )}
      </section>

      {showAdd && currentUser && (
        <ExpenseForm
          title="Новая трата"
          user={currentUser}
          onClose={() => setShowAdd(false)}
          onSaved={() => {
            setShowAdd(false)
            void reload()
          }}
        />
      )}

      {editing && (
        <ExpenseForm
          title="Изменить трату"
          user={users.find((u) => u.id === editing.user_id) ?? currentUser!}
          expense={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            void reload()
          }}
        />
      )}
    </div>
  )
}

function ExpenseForm(props: {
  title: string
  user: User
  expense?: Expense
  onClose: () => void
  onSaved: () => void
}) {
  const { title, user, expense, onClose, onSaved } = props
  const [amount, setAmount] = useState(expense?.amount ?? '')
  const [category, setCategory] = useState<CategorySlug>(expense?.category ?? 'food')
  const [spentAt, setSpentAt] = useState(expense?.spent_at ?? todayISO())
  const [comment, setComment] = useState(expense?.comment ?? '')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault()
    setErr(null)
    const kop = parseAmountToKopecks(amount)
    if (kop === null) {
      setErr('Сумма должна быть положительным числом, не больше двух знаков после точки.')
      return
    }
    if (!/^\d{4}-\d{2}-\d{2}$/.test(spentAt)) {
      setErr('Дата должна быть заполнена.')
      return
    }
    setBusy(true)
    try {
      if (expense) {
        await api.updateExpense(expense.id, {
          amount: (kop / 100).toFixed(2),
          category,
          spent_at: spentAt,
          comment,
        })
      } else {
        await api.createExpense({
          user_id: user.id,
          amount: (kop / 100).toFixed(2),
          category,
          spent_at: spentAt,
          comment,
        })
      }
      onSaved()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Не удалось сохранить')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="overlay" onClick={onClose}>
      <form className="modal" onClick={(e) => e.stopPropagation()} onSubmit={(e) => void submit(e)}>
        <h2>{title}</h2>
        <p className="muted">{user.display_name}</p>

        <label>
          Сумма, ₽
          <input
            autoFocus
            inputMode="decimal"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            placeholder="350.00"
          />
        </label>

        <label>
          Категория
          <select value={category} onChange={(e) => setCategory(e.target.value as CategorySlug)}>
            {CATEGORIES.map((c) => (
              <option key={c.slug} value={c.slug}>
                {c.title}
              </option>
            ))}
          </select>
        </label>

        <label>
          Дата
          <input type="date" value={spentAt} onChange={(e) => setSpentAt(e.target.value)} />
        </label>

        <label>
          Комментарий
          <input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="кофе с коллегами" />
        </label>

        {err && <div className="error">{err}</div>}

        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>
            Отмена
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? 'Сохраняю…' : 'Сохранить'}
          </button>
        </div>
      </form>
    </div>
  )
}
