import { describe, it, expect, vi, beforeEach } from 'vitest'

const { mockAuthWithPassword, mockCreate, mockUpdate, mockDelete, mockClear, mockAuthRefresh, mockCollection, mockAuthStore } = vi.hoisted(() => {
  const mockAuthWithPassword = vi.fn()
  const mockCreate = vi.fn()
  const mockUpdate = vi.fn()
  const mockDelete = vi.fn()
  const mockClear = vi.fn()
  const mockAuthRefresh = vi.fn()
  const mockAuthStore = { clear: mockClear, isValid: false, model: null }
  const mockCollection = vi.fn(() => ({ authWithPassword: mockAuthWithPassword, create: mockCreate, update: mockUpdate, delete: mockDelete, authRefresh: mockAuthRefresh }))
  return { mockAuthWithPassword, mockCreate, mockUpdate, mockDelete, mockClear, mockAuthRefresh, mockCollection, mockAuthStore }
})

vi.mock('@/lib/pb', () => ({
  pb: { collection: mockCollection, authStore: mockAuthStore },
}))

import { login, register, logout, getAuthStore, changePassword, deleteAccount, authRefresh } from '../auth'

beforeEach(() => vi.clearAllMocks())

describe('login', () => {
  it('calls authWithPassword on the users collection', async () => {
    const result = { token: 'tok', record: { id: 'u1', email: 'a@b.com' } }
    mockAuthWithPassword.mockResolvedValue(result)

    const r = await login('a@b.com', 'pass123')

    expect(mockCollection).toHaveBeenCalledWith('users')
    expect(mockAuthWithPassword).toHaveBeenCalledWith('a@b.com', 'pass123')
    expect(r).toEqual(result)
  })

  it('propagates errors', async () => {
    mockAuthWithPassword.mockRejectedValue(new Error('wrong credentials'))
    await expect(login('a@b.com', 'wrong')).rejects.toThrow('wrong credentials')
  })
})

describe('register', () => {
  it('calls create on the users collection with all fields', async () => {
    const user = { id: 'u1', email: 'a@b.com' }
    mockCreate.mockResolvedValue(user)

    const r = await register('a@b.com', 'pass123', 'pass123')

    expect(mockCollection).toHaveBeenCalledWith('users')
    expect(mockCreate).toHaveBeenCalledWith({ email: 'a@b.com', password: 'pass123', passwordConfirm: 'pass123' })
    expect(r).toEqual(user)
  })

  it('propagates errors', async () => {
    mockCreate.mockRejectedValue(new Error('email taken'))
    await expect(register('a@b.com', 'pass123', 'pass123')).rejects.toThrow('email taken')
  })
})

describe('logout', () => {
  it('clears the auth store', () => {
    logout()
    expect(mockClear).toHaveBeenCalled()
  })
})

describe('getAuthStore', () => {
  it('returns the pb auth store reference', () => {
    expect(getAuthStore()).toBe(mockAuthStore)
  })
})

describe('changePassword', () => {
  it('calls update on the users collection with password fields', async () => {
    const updated = { id: 'u1', email: 'a@b.com' }
    mockUpdate.mockResolvedValue(updated)

    const r = await changePassword('u1', 'old123', 'new12345', 'new12345')

    expect(mockCollection).toHaveBeenCalledWith('users')
    expect(mockUpdate).toHaveBeenCalledWith('u1', { oldPassword: 'old123', password: 'new12345', passwordConfirm: 'new12345' })
    expect(r).toEqual(updated)
  })

  it('propagates errors', async () => {
    mockUpdate.mockRejectedValue(new Error('wrong old password'))
    await expect(changePassword('u1', 'bad', 'new12345', 'new12345')).rejects.toThrow('wrong old password')
  })
})

describe('deleteAccount', () => {
  it('calls delete on the users collection', async () => {
    mockDelete.mockResolvedValue(undefined)

    await deleteAccount('u1')

    expect(mockCollection).toHaveBeenCalledWith('users')
    expect(mockDelete).toHaveBeenCalledWith('u1')
  })

  it('propagates errors', async () => {
    mockDelete.mockRejectedValue(new Error('not found'))
    await expect(deleteAccount('u1')).rejects.toThrow('not found')
  })
})

describe('authRefresh', () => {
  it('calls authRefresh on the users collection', async () => {
    const result = { token: 'new-tok', record: { id: 'u1', email: 'a@b.com' } }
    mockAuthRefresh.mockResolvedValue(result)

    const r = await authRefresh()

    expect(mockCollection).toHaveBeenCalledWith('users')
    expect(mockAuthRefresh).toHaveBeenCalled()
    expect(r).toEqual(result)
  })

  it('propagates errors', async () => {
    mockAuthRefresh.mockRejectedValue(new Error('401'))
    await expect(authRefresh()).rejects.toThrow('401')
  })
})
