import { pb } from '@/lib/pb'

export const login = (email, password) =>
  pb.collection('users').authWithPassword(email, password)

export const register = (email, password, passwordConfirm) =>
  pb.collection('users').create({ email, password, passwordConfirm })

export const logout = () => pb.authStore.clear()

export const getAuthStore = () => pb.authStore

export const changePassword = (id, oldPassword, password, passwordConfirm) =>
  pb.collection('users').update(id, { oldPassword, password, passwordConfirm })

export const deleteAccount = (id) =>
  pb.collection('users').delete(id)

export const authRefresh = () => pb.collection('users').authRefresh()
