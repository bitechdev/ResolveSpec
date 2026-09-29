# Audit — `pkg/testmodels`

- **Date:** 2026-09-29
- **Scope:** `pkg/testmodels/business.go` (161 LOC, 1 file, **no tests**)
- **Axes:** thread locking/waiting · slowness · security · panic handling & logging
- **Threat model:** hostile internet client. These models are registered into
  `pkg/modelregistry`, which means any model here becomes a reachable entity for the spec handlers.

## Summary

Six GORM struct definitions (`Department`, `Employee`, `Project`, `ProjectTask`, `Document`,
`Comment`) used as fixtures, plus two registration helpers. No concurrency, no I/O, no panics, no
logging — so three of the four audit axes are trivially clean.

Two real issues: **all six registration errors are discarded**, and this fixture package ships in
`pkg/` (not `_test.go`, not `internal/`) where a consuming application can register test tables
into a production registry.

| # | Severity | Axis | Finding |
|---|----------|------|---------|
| 1 | Medium | Correctness | `RegisterTestModels` discards all six `RegisterModel` error returns |
| 2 | Medium | Security | Fixtures live in exported `pkg/`, registerable into a production model registry |
| 3 | Low | Security | Models are registered via `RegisterModel`, which applies permissive `DefaultModelRules` |
| 4 | Low | Correctness | `GetTestModels()` return order is unrelated to FK dependency order |
| 5 | Low | Correctness | `Document.Path` is an unconstrained filesystem path exposed as a writable API field |

---

## Findings

### 1. All registration errors discarded (Medium, Correctness)

`business.go:142-149`

```go
func RegisterTestModels(registry *modelregistry.DefaultModelRegistry) {
	registry.RegisterModel("departments", Department{})
	registry.RegisterModel("employees", Employee{})
	registry.RegisterModel("projects", Project{})
	registry.RegisterModel("project_tasks", ProjectTask{})
	registry.RegisterModel("documents", Document{})
	registry.RegisterModel("comments", Comment{})
}
```

`RegisterModel` returns `error` and every return value is dropped. The function itself returns
nothing, so a caller cannot detect failure either.

This matters more than usual because of how `pkg/modelregistry.RegisterModel` fails. It has two
error paths (`pkg/modelregistry/model_registry.go:151-158`):

```go
if !r.tryLock() {
	return fmt.Errorf("failed to register model %s: registry locked", name)
}
...
if _, exists := r.models[name]; exists {
	return fmt.Errorf("model %s already registered", name)
}
```

The first is a **transient lock-contention failure** — see `audit/pkg/modelregistry.audit.md`
finding 7, where a contended `tryLock` gives up after ~20 ms. So under concurrent registration, some
subset of these six models silently fails to register, with no error, no log, and no panic. The
process then runs with, say, `documents` and `comments` missing from the registry.

That is not merely a missing-fixture annoyance. Per `audit/pkg/modelregistry.audit.md` finding 1, an
unregistered model causes `pkg/security/hooks.go:274-294` to take the
`return nil // model not registered, allow by default` branch — so a silently-failed registration
turns into **authorisation fail-open** for that entity.

Note `errcheck` is enabled (golangci-lint v2 standard set) but `.golangci.json` excludes
`"tests?"` paths — `pkg/testmodels` does not match that pattern, so this *should* be flagged
today. Worth checking whether the linter is actually run in CI.

**Recommendation:** return `error`, and use `errors.Join` so a partial failure is reported in full:

```go
func RegisterTestModels(registry *modelregistry.DefaultModelRegistry) error {
	return errors.Join(
		registry.RegisterModel("departments", Department{}),
		registry.RegisterModel("employees", Employee{}),
		...
	)
}
```

### 2. Fixtures are exported from `pkg/` (Medium, Security)

The package path is `github.com/bitechdev/ResolveSpec/pkg/testmodels`, not a `_test.go` file and not
under `internal/`. Consequences:

- The six structs and both helpers are part of ResolveSpec's **public API surface**. They are
  compiled into every binary that imports anything which transitively imports this package.
- A consuming application (or a copy-pasted quickstart) that calls
  `testmodels.RegisterTestModels(registry)` against its production registry makes
  `departments`, `employees`, `projects`, `project_tasks`, `documents` and `comments` live entities
  on the spec handlers, addressable by name. If the production database happens to have tables with
  those names — `documents` and `comments` are very common names — the handlers will happily
  read and write them under the permissive default rules of finding 3.
- It also means any future model added here for test convenience automatically becomes reachable.

Nothing in `pkg/` currently calls `RegisterTestModels` (only the test tree does), so this is a
packaging hazard rather than a live exposure.

**Recommendation:** move to `internal/testmodels` (blocks external import outright) or to a
`testmodels_test` package / `testdata` helper. If it must stay importable for downstream tests,
document loudly and consider a build tag.

### 3. Registered with permissive default rules (Low, Security)

`RegisterTestModels` uses `RegisterModel`, not `RegisterModelWithRules`. Per
`pkg/modelregistry/model_registry.go:191-194`, that initialises each model with
`DefaultModelRules()`, which grants `CanRead`, `CanUpdate`, `CanCreate` and `CanDelete` — see
`audit/pkg/modelregistry.audit.md` finding 13. `CanPublic*` are `false`, which is the saving grace.

If finding 2 is acted on this becomes moot; if these models are intended to stay registerable, they
should be registered read-only.

### 4. `GetTestModels()` order is not dependency order (Low, Correctness)

`business.go:152-160` returns the models in declaration order:
`Department, Employee, Project, ProjectTask, Document, Comment`.

The FK graph is not satisfied by that order. `Employee.DepartmentID → Department.ID` happens to work,
but `Document.OwnerID → Employee.ID` and `Document.ProjectID → Project.ID` mean `Document` must
follow both, and `ProjectTask.AssigneeID → Employee.ID` and `ProjectTask.ProjectID → Project.ID`
likewise. Coincidentally the declaration order does satisfy these — but nothing enforces it, and
`Employee.ManagerID → Employee.ID` is self-referential, which several migration/auto-migrate paths
handle only if the self-FK is deferred.

Also the two `many2many` joins (`department_projects`, `employee_projects`, declared at
`business.go:20`, `:45`, `:67-68`) are not in the returned list at all, so a caller using
`GetTestModels()` to drive `AutoMigrate` gets the join tables only because GORM infers them from the
tags — a Bun-based migration path (`pkg/common/adapters/database/bun.go`) would not.

**Recommendation:** document that the order is migration-safe and add a comment stating the
constraint, or return an explicitly ordered list with a test that asserts it.

### 5. `Document.Path` is an unconstrained path field (Low, Security)

`business.go:107`

```go
Path        string    `json:"path"`
```

No validation, no length limit, no `gorm` constraint. As a plain string column it is inert — the
risk only materialises if some handler or downstream consumer uses it to open a file, at which point
an attacker who can `POST`/`PATCH` a `Document` controls a filesystem path (`../../etc/passwd`,
`/proc/self/environ`). The same applies to `ContentType` (`business.go:105`) if it is ever echoed
into a response header unvalidated, and `Size` (`business.go:106`) which is a client-settable
`int64` that can disagree with reality.

Nothing in `pkg/` reads these fields, so this is a note about the fixture's shape rather than a
present vulnerability — but it is a bad example to ship, since fixtures get copied.

**Recommendation:** if these stay, mark `Path` as server-set (a `gorm:"->"` read-only tag, or
exclude it from the writable column set) so the fixture demonstrates the safe pattern.

---

## Axis-by-axis

- **Thread locking / waiting:** nothing to report. The package declares no goroutines, channels,
  mutexes or atomics. Its only concurrency exposure is *through* `pkg/modelregistry`, covered in
  finding 1 and in that package's audit.
- **Slowness:** nothing to report. `RegisterTestModels` and `GetTestModels` are O(1) with six
  elements and are startup-only. The `TableName()` methods (`business.go:23`, `:49`, `:73`, `:96`,
  `:119`, `:137`) return constants — no allocation, no reflection.
- **Security:** findings 2, 3, 5 — all about packaging and field shape, none about code behaviour.
- **Panic handling and logging:** the package contains no `panic`, no `recover`, and does not import
  `pkg/logger`. For plain struct definitions that is correct. The one place where logging *would*
  belong is the discarded errors of finding 1 — silently dropping six error returns is the
  panic/error-handling defect in this package, even though no panic is involved.

## What looks right

- Struct tags are consistent and complete: `json` on every field, `gorm:"primaryKey"` on every ID,
  `gorm:"uniqueIndex"` on the natural keys (`Department.Code`, `Employee.Email`, `Project.Code`),
  and explicit `foreignKey`/`references` on every relation rather than relying on GORM's inference.
  That makes these fixtures genuinely useful for exercising the relation-expansion paths in
  `pkg/restheadspec` and `pkg/resolvespec`.
- `omitempty` on every relation field prevents empty relation arrays from bloating responses — which
  matters, because these fixtures are what the handler tests measure payloads against.
- Nullable FKs are correctly modelled as `*string` (`Employee.ManagerID` `business.go:35`,
  `Document.ProjectID` `business.go:109`) rather than empty-string sentinels.
- The self-referential manager/reports pair (`business.go:43-44`) and the two `many2many` relations
  give reasonable coverage of the harder relation shapes — a genuinely well-chosen fixture set for
  the recursive-preload logic audited in `audit/pkg/restheadspec.audit.md`.
- `TableName()` is defined on the value receiver for all six, so it works whether a value or a
  pointer is passed — which matters given `pkg/modelregistry.RegisterModel` normalises pointers to
  values.

## Suggested follow-up

1. Return and check errors from `RegisterTestModels` (finding 1). One-line-per-call change, and it
   closes a silent path to authorisation fail-open.
2. Decide whether this package belongs in `pkg/` at all (finding 2). `internal/testmodels` is the
   low-effort fix.
3. Confirm `golangci-lint` runs in CI and that `errcheck` flags `business.go:143-148` — if it does
   not, the exclusion patterns in `.golangci.json` need review, since this is exactly the class of
   bug it exists to catch.
