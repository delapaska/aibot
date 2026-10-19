// Thin fetch wrapper over the panel REST API. Errors are surfaced as
// Russian messages ready to show in the UI.

import type { Debt, DebtInput, DebtsPage, Expense, ExpenseInput, ExpensesPage, User } from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError(0, 'Сервер недоступен — проверь, что бэкенд запущен.')
  }
  if (!res.ok) {
    let msg = `Ошибка ${res.status}`
    try {
      const env = (await res.json()) as { error?: { message?: string } }
      if (env.error?.message) msg = env.error.message
    } catch {
      // keep the generic message
    }
    throw new ApiError(res.status, msg)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  listUsers: () => request<{ items: User[] }>('GET', '/api/v1/users'),

  listExpenses: (params: { user_id?: number; from?: string; to?: string; category?: string; limit?: number }) => {
    const q = new URLSearchParams()
    if (params.user_id) q.set('user_id', String(params.user_id))
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    if (params.category) q.set('category', params.category)
    if (params.limit) q.set('limit', String(params.limit))
    return request<ExpensesPage>('GET', `/api/v1/expenses?${q.toString()}`)
  },

  createExpense: (input: ExpenseInput) =>
    request<Expense>('POST', '/api/v1/expenses', input),

  updateExpense: (id: string, patch: Partial<Omit<ExpenseInput, 'user_id'>> & { user_id?: number }) =>
    request<Expense>('PATCH', `/api/v1/expenses/${id}`, patch),

  deleteExpense: (id: string) => request<void>('DELETE', `/api/v1/expenses/${id}`),

  listDebts: (params: { user_id?: number; open?: boolean }) => {
    const q = new URLSearchParams()
    if (params.user_id) q.set('user_id', String(params.user_id))
    if (params.open !== undefined) q.set('open', String(params.open))
    return request<DebtsPage>('GET', `/api/v1/debts?${q.toString()}`)
  },

  createDebt: (input: DebtInput) => request<Debt>('POST', '/api/v1/debts', input),

  settleDebt: (id: string) => request<Debt>('POST', `/api/v1/debts/${id}/settle`),
}
