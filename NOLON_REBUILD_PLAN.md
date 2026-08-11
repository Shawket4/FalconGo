# Nolon (نولون) — Ground-Up Rebuild Plan

**Handover document.** This plan is the single source of truth for rebuilding the FalconGo fleet-management system as a multi-tenant Rust + Postgres SaaS called **Nolon**. It was produced after a full audit of the legacy codebases and a decision round with the owner. Follow it as written; where it says "port from X", read that source file and translate the *business logic*, not the code style.

---

## 1. Context & Goals

The legacy system is **FalconGo** (`/Users/shawket/GolandProjects/FalconGo`): a Go/Fiber + GORM + Postgres monolith serving one transporter ("Apex") that hauls fuel for cargo clients (Watanya, TAQA, Petromin, Petrol Arrows). It grew organically: no tenancy, `company` is a bare string, fee pricing is a flat mutable lookup table (past trips silently reprice when fees change), per-client revenue formulas are hardcoded in a satellite service, and there is significant dead code.

**Nolon** is a ground-up rebuild:

- **All Rust + Postgres.** One modular monolith owning the schema, modeled directly on the owner's **MadarRust** codebase (`/Users/shawket/Desktop/MadarRust`) — that project is the architecture template and its conventions are binding (see §3).
- **Multi-tenant SaaS**: Organization → Transporter(s) → operational data. First tenant is org **"Apex"** with one transporter **"Apex"**.
- **Flexible pricing is the core differentiator**: versioned, effective-dated rate cards with full CRUD, per-trip fee snapshots, and typed calculation strategies with DB-stored parameters — replacing both the flat `fee_mappings` table and the hardcoded per-company formulas.
- Two existing Rust satellites are **kept as-is** (minimal config/SQL adjustments only): `apex-petroapp` and `etit-proxy-rust`. Both are scoped to the Apex transporter for now (see §9).
- The third Rust service, `apex-rust` (trip statistics), is **absorbed into the monolith** (see §8).
- The frontend is **rebuilt from scratch** as `nolon-dashboard` (see §11). The legacy dashboard is a feature-parity/UX reference only.

### Decisions already made (do not re-litigate)

| Topic | Decision |
|---|---|
| Name | **Nolon** (نولون — the freight-fee term). Repos: `nolon-rust`, `nolon-dashboard`. DB: `nolon`. |
| Tenancy | Org → Transporter(s). Creating an org auto-creates one transporter; CRUD to add more. Madar-style `org_id` scoping + JWT claims + org-suspension kill-switch. |
| Clients | Org-level entities. Rate cards belong to **(client, transporter)** with effective dates. |
| Fee history | Versioned rate cards (`valid_from`/`valid_to`) **plus** per-trip fee snapshot. Normal flow = new card version, but dates are editable and a trip/delivery can be remapped to a different card (flexibility is a requirement). |
| Calc engine | Typed Rust strategies + parameters stored in DB per rate card. Fully data-driven formula language explicitly rejected. |
| Topology | Modular monolith + 2 satellites. `apex-rust` absorbed. |
| v1 scope | Core (trips, clients/rate cards, fuel, vehicles, drivers, users/auth) + **Financial suite** + **Maintenance suite**. **Deferred**: driver-ops stack (tablets, PIN auth, sessions, GPS pings, receipt batches, trip-summary worker) and monitoring (speed violations/alerts, Slack/WhatsApp, document-expiry alerts). |
| Schema quality | Rearchitect cleanly. **Never** degrade the new schema to make migration easier — migration code absorbs the complexity. |
| Migration | Full migration of v1-scope data. Every row goes into org "Apex" / transporter "Apex". Old DB stays untouched, read-only, as the archive for deferred-module data (sessions, pings, receipts, violations are NOT migrated). |
| SaaS billing | None in v1. But super-admin can **freeze/suspend orgs exactly like Madar** (kill-switch middleware). No plans/subscriptions/payments yet. |
| Frontend | Full rebuild. |

---

## 2. Reference Codebases (read these)

| Path | Role in this rebuild |
|---|---|
| `/Users/shawket/Desktop/MadarRust` | **Architecture template.** Copy its patterns: module layout, auth/JWT/middleware, RBAC, error handling, sqlx usage, testing, migrations, OpenAPI, org suspension. Read its `CLAUDE.md` and `src/auth/`, `src/errors.rs`, `src/permissions/` first. |
| `/Users/shawket/GolandProjects/FalconGo` | **Legacy business logic source of truth.** Models in `Models/`, routes in `FiberConfig/Routes.go`, payroll logic in `services/PaySlip*.go`, fee lookup in `Controllers/fee.go`. |
| `/Users/shawket/Rust/apex` (apex-rust) | Source of the per-client statistics/revenue formulas to port: `src/handlers/trip_stats.rs` (company dispatch, lines ~50–92) and `src/db/stats_queries.rs`. Absorbed into the monolith, then retired. |
| `/Users/shawket/Downloads/apex` | Legacy React dashboard. Reference for feature parity, API surface actually in use, and UX flows. Not reused. |
| `/Users/shawket/Downloads/apex-petroapp` | Satellite, kept. Read `ARCHITECTURE.md` + `migrations/` for its DB contract (§9). |
| `/Users/shawket/Downloads/etit-proxy-rust-main` | Satellite, kept. Read `API.md` + `src/auth/mod.rs` for its JWT contract (§9). |

---

## 3. Architecture (Madar conventions, binding)

- **Framework**: Actix-web 4. **DB**: sqlx 0.8 with compile-time-checked queries against Postgres. **Runtime**: Tokio. **Docs**: utoipa OpenAPI with `#[utoipa::path]` on every handler and an `export-openapi` bin.
- **Migrations**: sqlx, timestamp-prefixed, embedded, auto-applied on boot.
- **Module layout** — one directory per feature, each with `mod.rs`, `handlers.rs`, `routes.rs`, `tests.rs`:

```
nolon-rust/src/
├── main.rs, lib.rs
├── errors.rs            # AppError + SQLSTATE→HTTP mapping (copy Madar's errors.rs approach)
├── auth/                # jwt.rs, middleware.rs, guards.rs, handlers.rs, org_status.rs
├── permissions/         # RBAC matrix: checker.rs, seeder.rs, handlers.rs
├── orgs/                # super-admin org CRUD, onboarding, suspension
├── transporters/        # transporter CRUD within org
├── users/
├── clients/             # cargo clients (Watanya, TAQA, …)
├── pricing/             # rate cards, entries, calc strategies, fee resolution ← the heart
├── fleet/               # vehicles, vehicle documents
├── drivers/             # drivers, driver documents (admin-managed; NO driver login in v1)
├── trips/               # trips + deliveries, fee snapshots, remap/reprice
├── fuel/                # fuel events (petroapp-compatible, §9)
├── maintenance/         # oil changes, service events, service invoices, tires/trucks
├── payroll/             # payslips + line items + PDF
├── finance/             # loans, driver expenses, credit balances/ledger, vendors
├── reports/             # statistics engine (ported from apex-rust), exports
├── uploads/             # org-scoped file storage: uploads/{org_id}/…
├── geo/                 # OSRM client (localhost:5000), haversine
└── bin/export_openapi.rs, bin/migrate_falcon.rs   # §10
```

- **Handler pattern**: `extract_claims(req)` → permission check via `check_permission(pool, claims, resource, action)` → org-scoped query. Every org-scoped SQL statement includes `AND org_id = $n` — a missing org filter is a security bug.
- **Errors**: `thiserror` `AppError` enum; SQLSTATE classification (23505/23503 → 409, 22*/23514/23502 → 400, else 500); JSON body `{ "error": "...", "code": "..." }`.
- **Pagination**: offset-based envelope `{ data, total, page, per_page, total_pages, summary? }`, `per_page` capped at 1000, aggregate summary alongside data where it's cheap (Madar's `PaginatedOrders` pattern).
- **Money**: `NUMERIC(14,2)` + `rust_decimal::Decimal` for all EGP amounts (fees, revenue, payroll, expenses). Quantities like liters `NUMERIC(12,3)`. Distances km `NUMERIC(10,3)`. Never `f64` for money.
- **Soft deletes** (`deleted_at`) on: orgs, transporters, users, clients, drivers, vehicles, rate cards. Hard deletes only for genuinely disposable rows (draft payslip items etc.).
- **Time**: `TIMESTAMPTZ` everywhere. Each org has a `timezone` column (IANA, validated against `pg_timezone_names`, default `Africa/Cairo`). Reports translate with `AT TIME ZONE` using a bound parameter — never string-interpolate the timezone (Madar audit V1).
- **Concurrency discipline** (from Madar's AUDIT_REPORT): `FOR UPDATE` + in-transaction re-checks for state transitions (payslip finalize, rate-card activation), status-gated `UPDATE ... WHERE status = 'x'` idempotent writes, partial unique indexes for one-active-per-scope invariants, advisory locks where needed.
- **Testing**: `#[sqlx::test]` per-test ephemeral DBs; seed helpers for org/transporter/user/client/rate-card; every module ships regression tests; money/pricing code gets exhaustive unit tests. Adopt Madar's `scripts/preflight.sh` idea (fmt, clippy, tests as pre-push gate).
- **Env**: `DATABASE_URL`, `JWT_SECRET`, `BIND_ADDR` (default `0.0.0.0:8095`), `UPLOADS_DIR`, `CORS_ALLOWED_ORIGINS`, `OSRM_URL` (default `http://localhost:5000`), `NOLON_ENABLE_SWAGGER_UI`.

### Roles (RBAC matrix, seeded like Madar)

| Role | Notes |
|---|---|
| `super_admin` | No `org_id` in token. Org CRUD, freeze/unfreeze orgs, global visibility. |
| `org_admin` | Full control of their org (maps legacy permission 4). |
| `manager` | Operational CRUD incl. pricing and payroll (legacy 3). |
| `editor` | Day-to-day data entry: trips, fuel, maintenance (legacy 2). Financial figures visible but payroll finalize/pricing CRUD denied. |
| `viewer` | Read-only; revenue/VAT/financials **masked** (preserve apex-rust's permission<3 masking behavior as a permission on `reports:financials`). |

Resources × actions live in a `role_permissions` table with per-user overrides — copy Madar's `permissions/` module wholesale.

JWT claims: `{ sub, org_id?, transporter_ids?/scope, role, user_type: "admin_user", exp, iat }`. The `user_type: "admin_user"` claim is **required** for etit-proxy compatibility (§9). Middleware checks user active + org not suspended on every request (Madar's `org_status.rs` cache).

---

## 4. Domain Model & Schema

Below are the load-bearing tables. Sonnet designs the full DDL; these shapes and constraints are requirements. Every table listed (except `organizations`, `role_permissions`) carries `org_id UUID NOT NULL REFERENCES organizations(id)` plus `created_at`/`updated_at`. Use UUID v4 PKs.

### 4.1 Tenancy & identity

```
organizations   id, name, slug, timezone, currency_code (default 'EGP'),
                is_active, suspended_at, deleted_at
transporters    id, org_id, name, is_default, deleted_at
                -- onboarding creates one with is_default = true, named after the org
users           id, org_id (NULL for super_admin), name, email UNIQUE per org,
                password_hash (bcrypt), role, is_active, deleted_at
role_permissions(role, resource, action)  + user_permission_overrides
```

### 4.2 Clients & pricing (the heart — get this right first)

```
clients             id, org_id, name, notes, is_active, deleted_at
                    -- org-level: Watanya, TAQA, Petromin, Petrol Arrows

terminals           id, org_id, name, lat, lng          -- pickup points
drop_off_points     id, org_id, name, lat, lng          -- delivery points
                    -- first-class geo entities, deduplicated; legacy strings become FKs

rate_cards          id, org_id, client_id, transporter_id, name,
                    valid_from DATE NOT NULL, valid_to DATE NULL,   -- NULL = open-ended
                    status ('draft'|'active'|'archived'),
                    calc_strategy TEXT NOT NULL,        -- see §4.3
                    calc_params JSONB NOT NULL DEFAULT '{}',
                    notes, deleted_at
    -- Overlap guard: EXCLUDE USING gist on (client_id, transporter_id,
    --   daterange(valid_from, valid_to, '[]')) WHERE status='active' AND deleted_at IS NULL.
    -- valid_from/valid_to are EDITABLE (owner requirement). The exclusion constraint
    --   makes bad edits fail loudly instead of silently double-matching.

rate_card_entries   id, rate_card_id, terminal_id, drop_off_point_id,
                    distance_km NUMERIC(10,3), fee NUMERIC(14,2) NOT NULL,
                    osrm_distance_km, osrm_duration_min,
                    UNIQUE (rate_card_id, terminal_id, drop_off_point_id)
```

**Rate-card lifecycle & CRUD (all endpoints under `/api/v1/pricing`):**
- Full CRUD on clients, terminals, drop-off points, rate cards, entries.
- **Clone-as-new-version** endpoint: copy a card + entries with a new `valid_from`; old card's `valid_to` auto-set to the day before (the "normal flow": new card, no edit).
- **But edits stay possible**: dates on any card can be remapped; entries on an active card can be corrected. Every mutation of an active card writes to `rate_card_audits` (who/when/what — Madar's payslip-audit pattern).
- Bulk import endpoint (CSV) and OSRM enrichment endpoint (port `POST /api/mappings/enrich-osrm` from `Controllers/fee.go`).

### 4.3 Calculation strategies

A Rust enum, one variant per formula *shape*; parameters live in `rate_cards.calc_params` (typed via serde structs, validated on save — reject unknown/missing fields):

```rust
enum CalcStrategy {
    RouteRateCard { vat_percent, rounding },                 // fee = entry.fee per delivery
    PerKmTiered   { tiers: Vec<{up_to_km, rate}>, vat_percent, ... },
    FixedPlusVat  { ... },
    // ← exact variant set comes from porting apex-rust:
    //   read src/handlers/trip_stats.rs + src/db/stats_queries.rs and extract what
    //   Petrol Arrows / TAQA / Petromin / Watanya actually compute differently
    //   (revenue, VAT treatment, car-rental component, expense netting).
}
```

Rules:
- Strategy code is **pure and unit-tested exhaustively** (given inputs → Decimal outputs). No DB access inside strategy evaluation.
- Onboarding a new client with an existing formula shape = data entry only. A genuinely new shape = one new enum variant + tests.
- The four legacy formulas must reproduce apex-rust's numbers **exactly** on migrated data (§10 verification).

### 4.4 Fleet & drivers

```
vehicles            id, org_id, transporter_id, plate_number, vehicle_type,
                    tank_capacity_liters, compartments JSONB, model_year,
                    is_active, deleted_at
vehicle_documents   id, vehicle_id, doc_type ('license'|'calibration'|'tank_license'|…),
                    file_path_front, file_path_back, expires_on
drivers             id, org_id, transporter_id, name, mobile, national_id,
                    is_active, deleted_at
driver_documents    id, driver_id, doc_type ('id'|'license'|'safety'|'drug_test'|…),
                    file_path_front, file_path_back, expires_on
vehicle_driver_assignments  id, vehicle_id, driver_id, assigned_from, assigned_to NULL
                    -- replaces the mutable cars.driver_id pointer; history preserved,
                    -- partial unique index: one open assignment per vehicle and per driver
```

Documents become a generic pattern (one table per entity, `doc_type` enum) instead of FalconGo's 12 hardcoded upload folders. Files stored under `uploads/{org_id}/{entity}/{id}/…`. No driver PIN/tablet/session tables in v1 (deferred).

### 4.5 Trips

Legacy `ParentTrip` + `TripStruct` (containers) becomes:

```
trips           id, org_id, transporter_id, client_id, vehicle_id, driver_id,
                trip_date DATE, terminal_id, receipt_no, status, notes, deleted_at
trip_deliveries id, trip_id, drop_off_point_id, capacity_liters NUMERIC(12,3),
                gas_type,
                -- fee snapshot (immutable record of what was charged):
                rate_card_id NULL, rate_card_entry_id NULL,      -- provenance
                fee_snapshot NUMERIC(14,2) NOT NULL,
                distance_snapshot_km NUMERIC(10,3),
                fee_source ('rate_card'|'manual'|'migrated'),
                priced_at TIMESTAMPTZ
```

**Fee resolution service** (in `pricing/`): given (client, transporter, terminal, drop_off_point, trip_date) → find the active rate card covering that date → entry → return fee+distance+provenance. Trip creation calls it and writes the snapshot. Missing mapping → explicit `fee_source='manual'` with entered fee, or a 422 with a "create entry?" hint — never a silent zero.

**Remap/reprice endpoints (owner requirement):**
- `POST /trips/{id}/reprice` — re-resolve all deliveries against current cards (or a specific `rate_card_id` in the body); returns a diff preview with `?dry_run=true`.
- `POST /pricing/reprice-bulk` — same for a (client, transporter, date-range) slice; always preview-then-apply, audited.
- Snapshots mean editing a card never silently changes historic revenue — repricing is always an explicit, audited action.

Port from legacy: duplicate-trip detection on create (409 + collision details — the dashboard depended on this), Watanya report export (Excel via `rust_xlsxwriter`), driver-analytics aggregation (`GET /api/trips/watanya/driver-analytics` becomes client-generic `/reports/driver-analytics?client_id=`).

### 4.6 Fuel (petroapp contract — see §9 before changing anything here)

```
fuel_events     id, org_id, transporter_id, vehicle_id, driver_id NULL,
                event_date DATE, event_time, liters NUMERIC(12,3),
                price_per_liter NUMERIC(14,2), total_price NUMERIC(14,2),
                odometer_before BIGINT, odometer_after BIGINT,
                fuel_rate_km_per_l NUMERIC(8,3), source ('manual'|'petroapp'),
                petroapp_bill_id BIGINT NULL UNIQUE   -- dedup key, partial unique index
vehicles.last_fuel_odometer BIGINT                    -- petroapp updates it (GREATEST-guarded)
```

On manual fuel event create, call apex-petroapp's `POST /odometer/push` (X-Api-Key) — port that hook from the legacy Go flow.

### 4.7 Maintenance suite

```
oil_changes         id, org_id, vehicle_id, change_date, odometer_at_change,
                    current_odometer, cost, supervisor, notes
service_events      id, org_id, vehicle_id, service_type, service_date,
                    odometer, cost, supervisor, proof_file_path
service_invoices    id, org_id, vehicle_id, invoice_date, driver_name, supervisor,
                    operating_region, meter_reading, notes
service_invoice_items id, service_invoice_id, description, notes, item_order
                    -- pgvector embeddings from legacy are DROPPED (feature was never
                    -- wired to search endpoints); note as possible later feature
trucks / tires / tire_positions   -- port legacy model 1:1 but org-scoped:
                    tire inventory + position assignment (steering/axles/spare)
```

### 4.8 Financial suite

Port the legacy payslip system's *behavior* (it's the best-designed legacy module) onto clean tables:

```
employees           id, org_id, name, mobile, employee_type ('garage'|'office'), is_active
loans               id, org_id, counterparty_type ('driver'|'employee'), counterparty_id,
                    amount, loan_date, method, is_settled
                    -- CHECK constraint replaces Go-side "exactly one FK" validation
driver_expenses     id, org_id, driver_id, category, description, amount,
                    expense_date, payment_method, is_settled
credit_balances     id, org_id, counterparty_type, counterparty_id, balance
credit_transactions id, org_id, counterparty_*, payslip_id NULL, tx_type, amount,
                    balance_before, balance_after   -- append-only ledger
payslips            id, org_id, counterparty_type, counterparty_id,
                    period_start, period_end, status ('draft'|'finalized'|'paid'),
                    gross_income, total_expenses, total_deductions, total_adjustments,
                    net_payable, opening_balance, closing_balance,
                    created_by, finalized_by, finalized_at, paid_at
payslip_income_items / payslip_expense_items / payslip_deduction_items /
payslip_adjustment_items          -- port line-item semantics from Models/pay_slip.go
payslip_audits      -- action, changed_by, details, ts (port PaySlipAuditService)
vendors             id, org_id, name, contact, notes
vendor_transactions id, org_id, vendor_id, tx_date, description, amount (+/-)
```

Rules to preserve: totals formula (`net = gross + expense reimbursements − deductions + adjustments`, see `services/PaySlipCalculationService.go`), auto-populate-unpaid-items endpoint, finalize workflow with `FOR UPDATE` status gating, PDF generation (use `printpdf` or `genpdf`; layout parity with `services/PaySlipPDFService.go` — note Arabic text shaping may need `rustybuzz`/embedded font), credit-balance carry-forward on finalize, batch create.

### 4.9 Deferred modules (design-aware, not built)

Driver ops (tablets, PIN auth, sessions, location pings, receipt batches, trip-summary worker) and monitoring (speed violations/alerts, Slack/WhatsApp, doc-expiry alerts) are **out of v1**. Do not create their tables. The v1 schema must not block them: `trips` carries no session FK; drivers/vehicles are stable anchors; live tracking meanwhile comes from etit-proxy directly. Their historic data stays in the old FalconGo DB (read-only archive).

---

## 5. Dead code — explicitly NOT carried over

From the legacy audit; none of this gets ported: Excel/SMB export code paths (`Apis/ExcelFunctions.go`, commented v1 excelize), FCM token storage, Google Generative AI dep, WhatsApp QR-login module, document-expiry worker (rebuilt later with monitoring), `DailyTask` model, `Database/DB.go` (dead MySQL connector), `PreviewData`, `Instapay` models, `MultiRouteOptimizer` (revisit later if wanted), duplicate controllers in `Apis/` vs `Controllers/`, pgvector embeddings on inspection items, legacy `salaries` table (superseded by payslips), hardcoded ETIT credentials in `Scrapper/login.go` (scraping lives in etit-proxy now), the legacy Go PetroApp module (superseded by apex-petroapp).

Also **do not replicate** legacy API sins: POST-for-read endpoints (`POST /GetDriverLoans`), FormData-wrapped JSON (`request` field), mixed naming (`/api/protected/GetFuelEvents` vs `/api/trips`), string-typed dates. New API is uniformly `/api/v1/{resource}` REST+JSON, multipart only for real file uploads.

---

## 6. API Surface (v1 sketch)

All under `/api/v1`, JWT bearer, org-scoped. Per module:

- `auth`: `POST /auth/login`, `GET /auth/me`, `GET /auth/permissions`
- `orgs` (super-admin): CRUD, `POST /orgs/{id}/suspend`, `/unsuspend`, onboarding (creates default transporter + admin user + seeds permissions)
- `transporters`, `users`, `clients`, `terminals`, `drop-off-points`: standard CRUD
- `pricing`: rate-card CRUD, `POST /rate-cards/{id}/clone`, entry CRUD, CSV import, OSRM enrich, `GET /pricing/resolve?client&transporter&terminal&dropoff&date`, reprice endpoints (§4.5), audit list
- `vehicles`, `drivers`: CRUD + document upload/download + assignment endpoints
- `trips`: CRUD (multi-delivery create/update), duplicate detection, list with filters (date range, client, transporter, missing-data, pagination + summary envelope), delete parent+children
- `fuel`: CRUD + filters; petroapp rows read-only except odometer fields
- `maintenance`: oil / service events / service invoices (+ search) / trucks / tires CRUD
- `payroll`: payslip CRUD, batch-create, auto-populate, finalize, mark-paid, PDF, stats; line-item CRUD; loans/expenses/credit endpoints
- `finance`: vendors + transactions + monthly/top-vendor analytics (port `Controllers/vendor_analytics.go`)
- `reports`: `GET /reports/trip-statistics?start&end&client_id` (ported apex-rust engine; JSON default, `format=msgpack` supported via `rmp-serde` — the old dashboard used it, keep it cheap to offer), driver analytics, Watanya-style Excel export (client-generic)

OpenAPI generated for all of it; the dashboard build consumes the generated client.

---

## 7. SaaS mechanics in v1

- Org onboarding endpoint (super-admin): org + default transporter + org_admin user + permission seed, in one transaction.
- Suspension kill-switch: middleware consults a per-process org-status cache on every authed request (copy `MadarRust/src/auth/org_status.rs`). Suspended org → all requests 403 `ORG_SUSPENDED`; super-admin unaffected.
- No plans, billing, or feature flags in v1. Leave `organizations` extensible (JSONB `settings`).

---

## 8. Absorbing apex-rust (trip statistics)

1. Read `/Users/shawket/Rust/apex/src/handlers/trip_stats.rs` and `src/db/stats_queries.rs`.
2. For each company branch (Petrol Arrows, TAQA, Petromin, Watanya) document the formula: what constitutes revenue, VAT %, car-rental component, expense netting, distance/volume aggregation.
3. Implement each as a `CalcStrategy` variant + params (§4.3); assign to the migrated rate cards per client (§10).
4. Reimplement the statistics endpoint in `reports/` querying the **new** schema, respecting the financial-masking permission.
5. Verification: for several (month, client) slices, old service output == new endpoint output on migrated data, to the piaster.
6. Retire apex-rust (nginx `/api/rust` route removed after cutover).

---

## 9. Satellites — kept as-is, configured for Apex

**Principle:** no logic changes. Allowed: env/config values, and the *enumerated* SQL/claim compatibility items below.

### apex-petroapp (`/Users/shawket/Downloads/apex-petroapp`)
- Points `DATABASE_URL` at the **new** `nolon` Postgres. Its own migrations (`petroapp_credentials`, `petroapp_vehicle_map`, `petroapp_raw_bills`, `petroapp_sync_runs`) run there unchanged, except `petroapp_vehicle_map.db_car_id` FK now targets `vehicles(id)`.
- Its cross-service SQL touches: `fuel_events` INSERT, `cars.last_fuel_odometer` UPDATE (GREATEST-guarded), `cars`/`drivers` reads. The new schema renames `cars`→`vehicles` and adds `org_id`. Resolution, in order of preference: **(a)** adjust the handful of queries in its `src/sync.rs`/`src/db.rs` to the new table/column names, with `org_id`/`transporter_id` hardcoded via new env vars `NOLON_ORG_ID` / `NOLON_TRANSPORTER_ID` (set to Apex's UUIDs); or (b) compatibility views. Prefer (a) — it's a few statements and keeps the schema honest.
- The monolith calls its `POST /odometer/push` with `X-Api-Key` on manual fuel events, and proxies `POST /token` for the settings screen (legacy `set-petroapp-token`).

### etit-proxy-rust (`/Users/shawket/Downloads/etit-proxy-rust-main`)
- Fully self-contained (own SQLite, scrapes ETIT directly). No DB coupling to the monolith. One deployment = one ETIT account = the Apex transporter. Multi-transporter later = one instance per account behind routing (documented, not built).
- **JWT contract**: it validates the same `JWT_SECRET` and requires claim `user_type == "admin_user"`. Nolon's tokens must include `user_type: "admin_user"` for org users so the dashboard can call `/api/etit/*` directly with the same token. Verify against `src/auth/mod.rs`. Share `JWT_SECRET` between nolon-rust and etit-proxy.
- Dashboard consumes it directly (vehicles, live SSE, history, zones) through nginx — same as today.

### Deployment topology (initial, single VPS)
nginx: `/api` → nolon-rust (:8095), `/api/etit` → etit-proxy (:3000), apex-petroapp (:8090) internal-only (monolith-to-service via X-Api-Key). Legacy `/api/go` and `/api/rust` stay up until cutover, then die. OSRM stays at :5000. SaaS domain naming decided at launch; keep `CORS_ALLOWED_ORIGINS` config-driven.

---

## 10. Data Migration (FalconGo Postgres → nolon)

A standalone bin: `cargo run --bin migrate-falcon -- --source $FALCON_DB --target $NOLON_DB [--dry-run]`. Idempotent (upsert on natural keys or a `migration_map(old_table, old_id, new_id)` bookkeeping table). Old DB opened read-only, never modified.

Order & mapping:

1. **Seed**: org "Apex" (timezone Africa/Cairo, EGP) + default transporter "Apex" + users (map permission 1→viewer, 2→editor, 3→manager, 4→org_admin; passwords re-hashed or force-reset).
2. **Clients**: distinct `company` values across `trips`, `parent_trips`, `fee_mappings` → `clients` rows (trim/case-normalize; expect Watanya, TAQA, Petromin, Petrol Arrows + strays — log strays for manual review).
3. **Geo**: distinct terminals / drop-off points (strings) → `terminals` / `drop_off_points`, coordinates from `fee_mappings.latitude/longitude` and the legacy `terminals` table where present.
4. **Rate cards**: per client → one card v1: `valid_from` = that client's earliest trip date, `valid_to` = NULL, `status` = active, `calc_strategy`/`calc_params` per the ported apex-rust formula for that client; entries from `fee_mappings` rows (fee, distance, OSRM fields). `fee_source` provenance = `'migrated'`.
5. **Fleet**: `cars` → `vehicles` (+ documents from upload folders → new `uploads/{org_id}/...` layout, files copied not moved); `drivers` (+ documents). Current `car.driver_id` pointers → one open row each in `vehicle_driver_assignments`.
6. **Trips**: `parent_trips` + `trips` → `trips` + `trip_deliveries`. Each legacy container's stored `revenue` becomes `fee_snapshot` (source `'migrated'`); where it matches the migrated rate-card entry, also link `rate_card_entry_id`. Clean string dates → real DATEs; log unparseable rows to a rejects report, don't guess.
7. **Fuel**: `fuel_events` (preserve `petroapp_bill_id`), then `oil_changes`, `services`, `service_invoices` + items (drop embedding column), trucks/tires/positions.
8. **Financial**: employees, loans, expenses, credit balances + transactions, payslips + all four item tables + audits, vendors + transactions.
9. **NOT migrated** (old DB is the archive): driver_sessions, location_pings, tablets, receipt_batches/receipts/receipt_steps, trip_summaries/trip_legs, etit_trip_routes, speed_violations, speed_alerts, fcm_tokens, daily_tasks, salaries (legacy), petro_app_stations (superseded by apex-petroapp's vehicle map).

**Verification report** (the bin prints it; cutover blocks on it): row counts per entity old vs new; Σ revenue per (client, month) legacy vs Σ `fee_snapshot`; Σ payslip `net_payable` per period; Σ fuel liters/cost per month; apex-rust statistics parity check (§8.5). Any mismatch is listed with row-level detail.

**Cutover**: freeze writes on legacy → final migration run → verification green → nginx switch → legacy stack stopped but retained (DB + binaries) as archive.

---

## 11. Frontend — `nolon-dashboard` (rebuild)

- New repo, built against the OpenAPI spec. Match **MadarDashboard**'s (`~/Desktop/MadarDashboard`) stack and conventions for consistency across the owner's products; use the legacy dashboard (`~/Downloads/apex`) as the feature-parity and UX reference, including its Tauri packaging if mobile/desktop wrappers are still wanted.
- v1 pages: login; org-aware dashboard (KPIs); trips (list/create multi-delivery/edit/duplicate-warning/export); **pricing center** (clients, terminals/drop-offs, rate cards with version timeline UI, entry grid, clone-version flow, reprice preview) — this is the flagship screen, design it first; vehicles + documents; drivers + documents; fuel events; maintenance (oil, service events/invoices, tires/trucks); payroll (payslip workflow + PDF); finance (loans, expenses, credit ledger, vendors); reports (trip statistics with client/date filters, driver analytics, exports); ETIT live map + history (against etit-proxy, unchanged API); settings (PetroApp credentials, users & roles); super-admin panel (orgs, freeze/unfreeze, onboarding).
- Not in v1 UI: tablets, receipts review, speed violations (deferred modules).

---

## 12. Build Phases (each ends green: tests + OpenAPI regenerated)

| Phase | Deliverable | Acceptance |
|---|---|---|
| 0 | Repo scaffold: workspace, CI, preflight script, errors.rs, DB pool, migrations runner, health endpoint | boots; `#[sqlx::test]` harness works |
| 1 | Tenancy & auth: orgs, transporters, users, JWT (+`user_type` claim), RBAC seed, suspension kill-switch, onboarding | Madar-parity auth tests; suspended org 403s |
| 2 | Clients & **pricing engine**: clients, geo entities, rate cards (+overlap constraint, clone, audit), calc strategies (all four legacy formulas ported w/ exhaustive unit tests), fee resolution service | resolution + strategy tests green; CSV import; OSRM enrich |
| 3 | Fleet & drivers: vehicles, drivers, documents, assignments, org-scoped uploads | document upload/download round-trip |
| 4 | Trips: CRUD + deliveries + snapshots + duplicate detection + remap/reprice (+dry-run) | reprice diff audited; 409 collision payload matches spec |
| 5 | Fuel + maintenance suites | petroapp SQL contract validated against schema (integration test with its queries) |
| 6 | Financial suite: payroll workflow, PDF, loans/expenses/credit ledger, vendors | payslip totals parity tests vs legacy formulas |
| 7 | Reports: statistics engine, driver analytics, exports, msgpack | apex-rust parity fixture tests |
| 8 | Migration bin + verification report | dry-run on a legacy DB snapshot: verification green |
| 9 | Satellite integration: petroapp env/SQL adjustments, etit-proxy JWT check, nginx staging config | end-to-end: petroapp sync writes a fuel event into nolon; dashboard token accepted by etit-proxy |
| 10 | `nolon-dashboard` (can start after Phase 2 stabilizes pricing APIs; pricing center first) | feature-parity checklist vs legacy dashboard for v1 scope |
| 11 | Cutover: freeze, migrate, verify, switch | verification report green; legacy archived |

---

## 13. Open items (small, decide during build)

- Arabic PDF payslips: confirm chosen PDF crate handles RTL/shaping, else render via HTML→(headless) print or embed shaped text — spike early in Phase 6.
- Exact `CalcStrategy` variant list falls out of the apex-rust port (§8.2) — document each formula in code comments with a worked example.
- `viewer` financial masking: implement as a `reports:financials` permission consulted by report handlers (mirrors apex-rust's `permission < 3` behavior).
- Trademark/brand diligence on "Nolon" before public launch (owner's task).
