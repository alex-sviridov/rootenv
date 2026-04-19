---
description: Non-obvious structural details and invariants for the frontend module
paths:
  - "frontend/*"
---

# Frontend Architecture

## Shared PocketBase instance
`src/lib/pb.js` exports a single `pb` instance used by all API modules. Never instantiate `new PocketBase()` elsewhere — auth tokens are per-instance, so separate instances break authenticated requests.

## API / Store separation
- `src/api/` — pure functions, no state, no store imports. Import `{ pb }` from `@/lib/pb`.
- `src/stores/` — reactive state only, delegate all fetching to API functions.

## Store loading pattern
Each store has a private `withLoading(fn)` that owns the try/catch/finally for `loading` and `error` state. Actions pass async closures into it rather than duplicating the boilerplate.

## Realtime subscriptions in stores
`subscribeToAttempt` (and similar) returns a Promise resolving to an unsubscribe function. Stores keep an unsubscribe fn in a plain `let _unsubscribe = null` (not reactive, not returned). `startWatching` must call `stopWatching` first to prevent subscription leaks on re-calls.

## 404-as-empty-state pattern
When PocketBase `getFirstListItem` finds no record it throws `{ status: 404 }`. This is not an error — it means the resource doesn't exist yet. Handle it inside the `withLoading` closure with an inner try/catch: intercept `e?.status === 404` → set state to null, re-throw everything else so `withLoading` captures it in `error`.

## Auth flow
- `src/api/auth.js` — `login`, `register`, `logout`, `getAuthStore`
- `src/stores/user.js` — `signIn`, `signUp`, `signOut`, `init`
- `signUp` auto-logs in after registration (register → login → set user)
- `init()` restores session from `pb.authStore` on app startup; call it in `App.vue` on mount
- Input validation lives in the store (boundary), not the API
