# Branch naming

- Branch from `main`
- Keep branches short-lived (merge within 1-3 days) — long-lived branches are hidden costs
- Delete branches after merge
- Prefer feature flags over long-lived branches for incomplete features

Branch names follow this structure:

```
<type>/<description>
```

| Type | Purpose | Example |
|---|---|---|
| `feature/` or `feat/` | New features | `feature/add-login-page` |
| `bugfix/` or `fix/` | Bug fixes | `fix/header-bug` |
| `hotfix/` | Urgent fixes | `hotfix/security-patch` |
| `release/` | Release preparation | `release/v1.2.0` |
| `chore/` | Non-code tasks | `chore/update-dependencies` |
| `prototype/` | design prototypes for decisions | `prototype/login-dashboard` |

Trunk branches (`main`, `master`, `develop`) do not require a prefix.