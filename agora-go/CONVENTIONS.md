# Porting conventions (Kotlin → Go, strict parity)

Read this before porting any slice. The goal is **identical observable
behaviour** to the Kotlin backend (`src/main/kotlin/fr/gouv/agora`, frozen at
commit `2898584`): same status codes, headers, JSON/XML bytes, database
writes and Strapi calls — while being much faster.

## 1. Parity policy

Every difference must be **class A** (identical), unless recorded in your
slice's `parity/divergences/<slice>.md` with its class:

- **A** – identical (default, no entry needed).
- **B** – Go is only *fresher* (a Kotlin cache bug/timing artifact fixed) or
  fails more cleanly (resource leak fixed). Allowed list (decided by the
  product owner): frozen participant count, `userFeedbackQags` storing the
  previous answer, `AgoraQueue` lock never released after an exception, the
  never-read `reponseConsultationCache`, the never-filled Moderatus lock
  cache, per-user `RedisCacheManager` entries.
- **C** – anything else: do NOT introduce it; write it in the divergences file
  as a question for the lead.

Deterministic business quirks MUST be reproduced, including bugs (SQL
operator-precedence bug in the supported-count query, trending duplicates,
100-item Strapi cap, slugs not resolved on `/questions`, …).

## 2. Where code goes

```
internal/modules/<module>/
  routes.go     Routes(a *app.App): a.Server.GET/POST/PUT/DELETE(pattern, handler)
  *_handler.go  one Go handler per Kotlin controller method
  *_usecase.go  ports of usecase/** classes
  *_repo.go     ports of infrastructure/**/repository (SQL, Strapi, caches)
  *_dto.go      JSON DTOs (ports of *Json.kt) and Strapi DTOs
  service.go    `func Get(a *app.App) *Service { return app.Singleton(a, "<module>", …) }`
```

- Cross-module calls go through the other module's exported `Get(a)` service
  (never import a module's internals). Never edit another slice's module:
  if you need something it does not expose, add a minimal exported function
  in YOUR module or ask the lead in your ledger file.
- Do **not** modify foundations (`internal/{httpx,jsonjava,xmljava,cache,store,
  auth,strapi,config,javacompat,common,domain,app}`, `cmd/`). If you hit a
  foundation bug or missing feature, work around it locally and describe it
  under "Foundation requests" in `parity/ledger/<slice>.md`.
- Do not add Go module dependencies (already available: pgx v5, go-redis v9,
  x/crypto, x/sync, x/text, yaml.v3).

## 3. HTTP handlers (Spring MVC semantics)

`internal/httpx` already reproduces the Spring pipeline (Tomcat checks,
firewall, CORS, security headers, JWT auth, the ordered authorization rules of
`WebSecurityConfig`, routing, `?mediaType=` negotiation, ETag, Spring error
bodies). Your handler only reproduces the controller method:

- Bind arguments **in the Kotlin declaration order** (the first failing one
  decides the 400/415):
  - `@RequestHeader("X") x: String` → `c.RequiredHeader("X")`
  - `@RequestHeader("X", required=false) x: String?` → `c.OptionalHeader("X")`
  - `@RequestParam("p") p: String` → `c.RequiredParam("p")`; `String?` → `c.OptionalParam("p")`
  - `@RequestParam(defaultValue=d)` → `c.ParamDefault("p", d)` (absent or empty → d)
  - `List<String>?` params → `c.ParamList("p")` (comma splitting like Spring)
  - `@PathVariable` → `c.PathVar("name")`; Int conversions → `httpx.SpringIntPathVar(v)`,
    Boolean → `httpx.SpringBoolParam(v)`; enums: exact `valueOf` after trim, else 400.
  - `@RequestBody dto` → `c.BindBody(&dto)` (415/400 handled).
  - `authentificationHelper.getUserId()!!` → `c.UserID()`; nullable → `c.OptionalUserID()`.
  - `IpAddressUtils.retrieveIpAddressHash(request)` → `c.IPHash()`.
- Return values:
  - `ResponseEntity.ok().body(x)` → `httpx.OK(x)`; other status → `httpx.JSON(status, x)`
  - `.body(Unit)` → `httpx.Unit(status)` (writes `{}`)
  - `.build()` → `httpx.Empty(status)` (no body)
  - `ResponseEntity<String>` bodies → `httpx.String(status, s)`
  - preset content type (e.g. TSV) → `httpx.Bytes(status, contentType, b)`
  - `.cacheControl(CacheControl.maxAge(N, SECONDS).cachePublic())` → `.CacheControl(N, true)`
  - extra headers → `.With("Name", "value")` (keep Tomcat's casing)
- Exceptions:
  - `@RestControllerAdvice` exceptions → `panic(&httpx.AdviceError{Status: 404, Title: "…"})`
    (titles in `infrastructure/common/DefaultControllerAdvice.kt`; territory/departments
    errors use `domain.InvalidTerritoryError` / `domain.InvalidNumberOfDepartmentsMessage`).
  - Spring binding errors → `panic(&httpx.SpringError{Status: 400})`.
  - Anything Kotlin would throw uncaught (NPE on `!!`, `.first()` on empty, Strapi
    single type failure, …) → `panic(err)` → Spring 500 body. Reproduce these!
- Every response DTO type must have `func (T) JavaName() string` returning the
  Kotlin simple class name (root element for `?mediaType=xml`), and Moderatus-style
  annotations map to `xml:"name,attr|cdata|unwrapped"` tags.

## 4. JSON DTOs (`internal/jsonjava`)

- One Go struct per Kotlin `*Json` class, **fields in declaration order**, tag
  `json:"<@JsonProperty value, else Kotlin property name>"`.
- Nullable Kotlin types → pointers (`*string`, `*int`, `*Struct`); `null` is
  written unless the Kotlin class has `@JsonInclude(NON_NULL)` → add `omitnull`
  to every field of that class.
- Kotlin `List<T>` (non-null) → `[]T` (nil is written `[]`); `List<T>?` → `[]T`
  with `json:"x,nullable"` (nil → `null`).
- `Int`→`int`, `Long`→`int64`, `Double`→`float64` (formatted like Java),
  `Boolean`→`bool`. Dates are almost always pre-formatted strings:
  `common.FormatDate(t)` = DateMapper `yyyy-MM-dd HH:mm:ss` (process zone).
- Input DTOs (request bodies, Strapi payloads): non-pointer string/slice/struct
  fields are required (missing or null → 400 / empty Strapi list), pointers are
  nullable, primitives default to zero; Kotlin default values → tag option
  `def` and pre-initialize the struct before `BindBody`.
- Verify byte parity of every DTO with the oracle (`jsonRoundTrip`, className =
  the Kotlin FQCN) in a test guarded by `oracle.Available()`.

## 5. Kotlin/Java semantics you must keep

Use `internal/javacompat` instead of the Go standard library when the Kotlin
code used: `String.take/length` (UTF-16: `Take16`, `Len16`), `trim/isBlank`
(`KotlinTrim`, `KotlinIsBlank`), `lowercase()` (`KotlinLowercase`),
`toIntOrNull` (`KotlinToIntOrNull`), `contains(ignoreCase)`,
`UUID.fromString` / `toUuidOrNull` (`ParseUUID`, `ToUUIDOrNull` — lenient!),
`Math.round`/`roundToInt` (`JavaMathRound`, `KotlinRoundToInt`),
`URLEncoder.encode`, `replaceDiacritics`, Java regex `\s` (ASCII only),
`String.hashCode`, HashMap iteration order (`JavaHashMapOrder`) when a
`HashMap/HashSet` order leaks into an output. Kotlin `sortedBy/sortedWith`
are **stable** (`sort.SliceStable`); `distinct()`, `groupBy`, `associate`,
`toSet`, `mapOf`, `mutableMapOf` keep insertion order. `first()` on an empty
list throws (→ 500), `firstOrNull()` does not. `LocalDateTime.now(clock)` →
`a.Now()` (never `time.Now()` directly). Kotlin `Random`/`shuffled` →
document as non-deterministic and compare with `compare.unordered` in scenarios.

## 6. Database (`internal/store`)

- Copy every native `@Query` **verbatim** (only `:name` → `$n`). Hibernate
  expands a collection parameter `IN :ids` to `IN ($1,$2,…)`: build the
  placeholder list; for an empty collection Hibernate 6 emits `IN ()`-like SQL
  that FAILS → reproduce the failure path (check the reference!).
- Spring Data derived methods / `save` / `saveAll` / `deleteAll` → write the
  equivalent SQL. `save(entity)` with a preset id = SELECT + INSERT/UPDATE
  (merge); generating a fresh random v4 UUID in Go is observably identical for
  inserts. `saveAll` → one transaction.
- `timestamp without time zone`: write `store.Millis(a.Now())` for
  `java.util.Date` fields, `store.Micros(a.Now())` for `LocalDateTime` fields;
  read with `store.Local(t)`.
- Entities with non-null Kotlin fields mapped to NULL columns crash in Kotlin
  (NPE / PropertyAccessException → 500): reproduce when reachable.
- Use `a.DB.Pool` (pgx). Keep the number of queries per request ≤ Kotlin's.

## 7. Caches (`internal/cache`)

For each Kotlin cache (`getCache(name)` calls), keep the same TTL (or
shorter) and apply the same eviction events:
`cache.GetOrLoad(a.Cache, "<name>", key, ttl, loader)` and
`a.Cache.Invalidate(ctx, "<name>", key)` / `InvalidateAll`.
In coexistence mode also delete the Kotlin keys the Kotlin code would have
evicted: `a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey("consultationResults", id))`.
Keys shared with Kotlin in its exact format (rate limit, signup counters,
feature flags) use `GetSharedJSON/SetSharedJSON`. Data Kotlin does NOT cache:
only shared (non per-user) aggregates may be micro-cached for
`a.Cfg.MicroCacheTTL` (≤ 5 s). Never serve per-user data older than the
user's own last write.

## 8. Strapi (`internal/strapi`)

Port each `StrapiRequestBuilder` chain call by call (`strapi.NewRequest(model)
.FilterIn(...).WithDateBefore(a.Now(), ...)...`), then
`strapi.Collection[T](ctx, a.Strapi, b)` (errors → empty list, like Kotlin) or
`strapi.Single[T]` (errors → return err → handler panics → 500). Strapi DTOs
mirror the Kotlin DTOs with section 4 decoding rules (a missing required field
anywhere makes the whole list empty — that is the Kotlin behaviour).
Rich text: `strapi.RichText` (`ToHTML()`, `ToHTMLBody()`); media:
`strapi.MediaPicture.MediaURL()`.

## 9. Tests and parity scenarios (mandatory)

1. Port the Kotlin unit tests of your slice (`src/test/kotlin/...`) into Go
   table tests (`go test ./internal/modules/<module>/...`).
2. Write parity scenarios `parity/scenarios/<slice>_*.yaml` covering **every
   route** of the slice: happy paths, every error branch (400/401/403/404/
   412/423/429…), auth variants (anonymous, regular, banned, publisher,
   moderator, admin via `as: "{{seed.UserX}}"`), `?mediaType=xml`, and DB
   effects (`dbdiff: step`). See `parity/scenarios/00_framework.yaml` and
   `parity/seed/README.md` (seeded ids: `{{seed.<ConstName>}}`),
   `parity/fixtures/strapi/README.md` (Strapi fixtures, overrides, faults).
3. Run against your own slot (N given by the lead):
   ```
   PARITY_SLOT=N ./parity/run.sh up          # once (Kotlin ref + Go + PG + Redis + fake Strapi)
   PARITY_SLOT=N ./parity/run.sh restart-go  # after each Go change
   PARITY_SLOT=N ./parity/run.sh test -tags <SLICE> -v
   PARITY_ORACLE=1 go test ./internal/modules/<module>/...
   ```
   Iterate until **0 unexplained diffs**. Use `compare.unordered` only for
   genuinely unordered outputs (SQL ties, random), never to hide a bug.
4. Record the file mapping in `parity/ledger/<slice>.md`: every Kotlin file of
   the slice → Go file(s), tests ported, scenarios, divergences, open questions.

## 10. Hygiene

`gofmt`, `go vet ./...`, no data races (`go test -race`), comments only where
they explain a Kotlin quirk being reproduced. Commit in your worktree branch
with clear messages; do not push.
