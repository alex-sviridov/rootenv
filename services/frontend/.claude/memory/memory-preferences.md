---
description: Coding style, tooling, and workflow preferences specific to the frontend module
paths:
  - "frontend/*"
---

# Frontend Preferences

## Tests first
Write tests before implementation. Feature is not done until tests pass.

## Less code is better
Prefer extracting shared logic (e.g. `list(filter)` helper, `withLoading`) to eliminate duplication. Target fewer lines without sacrificing readability.

## Arrow functions for simple exports
Single-expression API functions use arrow syntax: `export const fetchFolders = () => ...`

## Security is a priority
- Passwords must never be stored in reactive state
- Validate all user input at the store boundary before hitting the API
- Auth tokens stay in PocketBase's own store, not exposed to app state
