---
description: Key architectural and implementation decisions made for the frontend module
paths:
  - "frontend/*"
---

# Frontend Decisions

## Shared pb instance via `src/lib/pb.js`
Extracted PocketBase instantiation into a shared module so auth state is visible across all API modules. Rejected: each module owning its own instance (breaks auth token sharing).

## `withLoading` as a private store helper
Duplicated once per store rather than extracted to a shared utility. Keeps stores self-contained; two occurrences don't justify an abstraction.

## Validation in stores, not API modules
Client-side input validation (required fields, email format, password min length, confirmation match) lives in store actions — they are the user-input boundary. API modules stay pure.

## `signUp` auto-logs in
`signUp` calls `register` then `login` in sequence and sets `user` directly. Rejected: register-only (worse UX, forces a separate login step).

## Test mocking strategy
API tests mock `@/lib/pb` directly. Store tests mock `@/api/*`. Using `vi.hoisted()` for mock variables referenced inside `vi.mock()` factories.
