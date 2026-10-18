// Wire types mirroring backend/internal/infra/httpapi DTOs.

export interface User {
  id: number
  first_name: string
  username: string
  display_name: string
  created_at: string
}

export interface Expense {
  id: string
  user_id: number
  amount: string
  category: CategorySlug
  spent_at: string
  comment: string
  created_at: string
  updated_at: string
}

export type CategorySlug =
  | 'food'
  | 'transport'
  | 'housing'
  | 'entertainment'
  | 'health'
  | 'shopping'
  | 'other'

export const CATEGORIES: { slug: CategorySlug; title: string }[] = [
  { slug: 'food', title: 'Еда' },
  { slug: 'transport', title: 'Транспорт' },
  { slug: 'housing', title: 'Жильё' },
  { slug: 'entertainment', title: 'Развлечения' },
  { slug: 'health', title: 'Здоровье' },
  { slug: 'shopping', title: 'Покупки' },
  { slug: 'other', title: 'Другое' },
]

export function categoryTitle(slug: string): string {
  return CATEGORIES.find((c) => c.slug === slug)?.title ?? slug
}

export interface ExpensesPage {
  items: Expense[]
  total: number
}

export interface ExpenseInput {
  user_id: number
  amount: string // decimal rubles, e.g. "350.00"
  category: CategorySlug
  spent_at: string // YYYY-MM-DD
  comment?: string
}
