# Rebuild proposal: bank-SMS transactions

**Date:** 2026-08-12 · **Status:** decisions D1–D10 resolved with the owner — ready to build
on go-ahead. No code written.
Companion to [BANKSMS_REBUILD_BRIEF.md](BANKSMS_REBUILD_BRIEF.md). Facts below were verified
today against production and the existing source. This document survived an adversarial review
pass (5 independent critics: simplicity, migration integrity, hard constraints, UX mandate,
regex-by-execution) and an owner review round; the two biggest corrections were the cutover
mechanics (§7) and the loan/advance registration flow (§2.2/§5/§6 — the owner kept it; the
"delete postings" recommendation was wrong, see D2).

---

## 1. What the audit added to the brief

Production, measured 2026-08-12:

| | |
|---|---|
| Live transactions | **315** = 92 whatsapp + 223 import · **Σ 2,921,440.35 EGP** (+6 soft-deleted whatsapp rows) |
| Raw messages | **815** = 96 parsed + 2 partial + 717 ignored |
| Chat corpus (re-pulled via API) | 815 messages = **100 bank SMS** + 461 chatter + 254 media |
| Bank SMS rate, last 30 days | 3.3/day |
| `transaction_postings` | 0 rows — but this measures *categorization friction*, not disuse: only 3 of 92 SMS rows ever got a category, so the posting path never had input. The loans ecosystem is live in FalconGo (`Loan.Kind` advance/loan/salary added recently, `Employee.CreditBalance`), and the flow stays (D2) |
| `tags`, `transaction_tags`, `noise_skeletons` | 0 rows each |
| `transaction_overrides` | 15 rows on 3 transactions (224/276/284); override amounts equal parsed amounts; `occurred_at` was the field actually corrected |
| Categorized whatsapp transactions | **3 of 92** — categorization is the real workflow gap |
| Zero-amount rows | **0** (min amount 10.00) — the new `CHECK (amount > 0)` is safe against real data |
| `car_no_plate` | 46 live rows carry a plate; **3 don't resolve** to `public.cars` — the column earns its place |
| `version` column | values up to 8 in production — must copy verbatim (If-Match clients depend on it) |
| `public.fleet_expenses` | still exists (223 rows), already migrated; legacy `/api/v1/fleet-expenses*` routes still mounted at permission 3 and read it |
| sqlx | apex-rust is already on **0.8** (RECON's 0.7 note is stale); migrations bookkeeping reaches `banksms._sqlx_migrations` via a search_path mechanism in `main.rs`, **9 versions applied** |

**The 6 templates cover 98 of 100 bank SMS.** The two misses are the two `partial`-stuck
messages, and both are new *variants* of `instant_transfer` (hotline 19666):

1. **Instant purchase** — `تم تنفيذ عملية شراء لحظي من حسابك ... بمبلغ 135.72 جم من Mobile Recharge ...`
2. **Inbound instant transfer** — `تم تنفيذ تحويل لحظي بمبلغ 5700.00 جم إلى حسابك ... من احمد ...` (a credit)

With two added templates, **coverage is 100/100 with zero bank-ish strays** — measured against
the full re-pulled corpus, not a sample, and re-verified by executing the proposed patterns
against the live message bodies (§2.3). This is the empirical case for dropping the triage
scorer entirely (brief §9, question 1).

Other audit facts that shaped this proposal:

- The **cursor-advance logic is already correct** (advance once per cycle, after all pages commit,
  `GREATEST`-guarded) and is carried over as-is, with its kill-mid-batch test.
- The **wallet_transfer year-less date is `%m-%d` (month-first)** — verified against both live
  messages (arrived Aug 9/10, bodies say `08-09`/`08-10`). The brief's `31-12` example implies
  day-first; the brief is wrong, production is right.
- `parse_yearless` (leap-probe, candidate years `[ref, ref−1]`, accept if ≤ ref+7d) and
  `parse_datetime` (per-template formats, Cairo DST via chrono-tz, `.earliest()`) are the
  hardest-won code in the module — salvaged verbatim.
- The NFKC step in normalization is **a stub that does nothing** (`normalize.rs:89-91`). No
  compatibility forms ever appeared in the corpus. The rebuild deletes the stub (honestly).
- The dashboard never calls: tags (4 routes), `/transactions/{id}/history`, `/accounts`,
  `/summary`, `GET /raw`, all `/admin/*`, all legacy `/fleet-expenses*`. The union list's
  `include_fuel` / `include_loans` toggles **are used** (visible switches, default on), and so
  are the `company` / `payment_method` filters — all four survive.
- The Excel export walks up to 25×200 rows client-side; trips export already moved server-side
  in FalconGo — the rebuild does the same here.

---

## 2. Schema — 5 tables

New tables are created fresh in schema `banksms` after the old schema is renamed to
`banksms_legacy` by the cutover routine (§7 — **not** by a sqlx migration; the critics
established that ordering is impossible). After cutover, `sqlx::migrate!` owns *future* schema
changes through the existing search_path mechanism, against a fresh `banksms._sqlx_migrations`
stamped by the cutover itself. apex-petroapp's `public._sqlx_migrations` is never touched.

### 2.1 `raw_messages` — every message, verbatim, forever

```sql
CREATE TABLE banksms.raw_messages (
  id            BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
  wa_message_id TEXT NOT NULL UNIQUE,          -- the dedup key, non-negotiable
  chat_jid      TEXT NOT NULL,
  sender        TEXT,
  is_from_me    BOOLEAN NOT NULL DEFAULT FALSE,
  wa_timestamp  TIMESTAMPTZ NOT NULL,           -- envelope, UTC
  body          TEXT NOT NULL,                  -- original, never normalized
  media_type    TEXT,
  status        TEXT NOT NULL CHECK (status IN ('matched','ignored','suppressed')),
  template      TEXT,                           -- which template matched (matched/suppressed)
  ingested_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON banksms.raw_messages (status, wa_timestamp DESC);
```

`status` records the **parser's verdict only**, and the parser is deterministic, so this is
recomputable at any time — and §3's reparse sweep is the compute path that makes that claim
real. Human action never touches it: recording an ignored message as a transaction creates a
`manual` transaction linked by `raw_message_id`. That kills the review loop by construction —
no queue, no `pending`, no `reviewed_at`, nothing a human must drain. Dropped columns vs today:
`parse_status` (6 states → 3), `triage_score`, `skeleton_hash`, `parser_version`,
`parse_error`, `reviewed_at/by`, `created_at/updated_at`.

`GENERATED BY DEFAULT` (not `ALWAYS`) so the cutover copy can preserve production ids without
`OVERRIDING SYSTEM VALUE` gymnastics; the sequence is restarted past max(id) in §7 step 6.

### 2.2 `transactions` — one column set, user-owned after creation

```sql
CREATE TABLE banksms.transactions (
  id               BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
  source           TEXT NOT NULL CHECK (source IN ('whatsapp','import','manual')),
  raw_message_id   BIGINT UNIQUE REFERENCES banksms.raw_messages(id),
  import_source_id BIGINT UNIQUE,               -- legacy fleet_expenses.id, idempotent re-import
  direction        TEXT NOT NULL CHECK (direction IN ('in','out')),
  amount           NUMERIC(18,4) NOT NULL CHECK (amount > 0),   -- 0 zero-amount rows in prod
  currency         TEXT NOT NULL DEFAULT 'EGP',
  occurred_at      TIMESTAMPTZ NOT NULL,        -- SMS body time (Cairo→UTC) or expense date
  account          TEXT,                        -- shown on row meta + edit screen
  counterparty     TEXT,
  reference        TEXT,
  category         TEXT,                        -- key into categories, verbatim text
  description      TEXT,
  payment_method   TEXT,
  company          TEXT,
  car_id           BIGINT REFERENCES public.cars(id),
  car_no_plate     TEXT,                        -- 3 legacy plates resolve to no cars row
  driver_id        BIGINT REFERENCES public.drivers(id),
  employee_id      BIGINT REFERENCES public.employees(id),
  loan_id          BIGINT UNIQUE,               -- the public.loans row this registered (D2)
  paid_by          TEXT,                        -- legacy import free text; new rows use party ids
  version          INTEGER NOT NULL DEFAULT 1,  -- If-Match concurrency; copied verbatim (prod has up to 8)
  created_by       TEXT,
  edited_by        TEXT,                        -- NULL until a human edits
  edited_at        TIMESTAMPTZ,
  deleted_at       TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT whatsapp_has_raw CHECK (source <> 'whatsapp' OR raw_message_id IS NOT NULL),
  CONSTRAINT import_has_id    CHECK ((source = 'import') = (import_source_id IS NOT NULL)),
  CONSTRAINT single_party     CHECK (driver_id IS NULL OR employee_id IS NULL)
);
CREATE INDEX ON banksms.transactions (occurred_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX ON banksms.transactions (category)         WHERE deleted_at IS NULL;
```

**The `parsed_*` twin column set and the `transaction_overrides` table are gone.** The parser
writes these columns once at creation; edits update them in place and stamp
`edited_by`/`edited_at` (brief §5 mandates exactly this). "What did the parser originally say"
stays answerable because the verbatim body sits one join away and the parser is deterministic.
The 0.1% IPN fee split stays **derived at read time** (`derive_fee`, salvaged), never stored.

Dropped vs the first draft after critique: `template` (lives on `raw_messages`, one join away,
which is where the UI reads it) and `balance_after` (no consumer anywhere; the balance text
survives inside the verbatim body). Dropped vs production: `attributes` JSONB (never
referenced), `confidence`, `parse_method`, `parser_version`. **Kept after owner review:**
`driver_id`/`employee_id` (the loan/advance party, D2 — id-only on purpose: the PartyPicker
comment documents how `loans.method` free text decayed into five spellings of one person) and
**`loan_id`**, which replaces the `transaction_postings` link table: the link is strictly 1:1
(the old table's CHECK admitted only `'loans'` as a target), so a UNIQUE nullable column
preserves the posting invariant — *apex-rust only ever touches loans rows it created, and
`loan_id` records exactly which those are* — with one less table.

On `car_id`: `public.cars` is FalconGo/GORM-owned. The FK is carried over from production
(it already exists there) and is safe because FalconGo only ever soft-deletes cars and
AutoMigrate never drops tables — recorded here so a future FalconGo cleanup knows where the
constraint lives.

### 2.3 `parse_templates` — the six regexes plus two, no versioning

```sql
CREATE TABLE banksms.parse_templates (
  id            BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
  name          TEXT NOT NULL UNIQUE,
  pattern       TEXT NOT NULL,                  -- post-normalization form, named groups
  date_formats  JSONB NOT NULL,                 -- ordered chrono format list, per template
  direction_map JSONB NOT NULL DEFAULT '{}',
  sample        TEXT NOT NULL,                  -- a real message this pattern must match
  priority      INTEGER NOT NULL,
  enabled       BOOLEAN NOT NULL DEFAULT TRUE,
  notes         TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Stays in the DB so a new bank format needs no deploy (brief §5) — managed via psql/CLI, **no
HTTP CRUD** (the admin endpoints were curl-only and templates changed twice in the system's
life; keeping 3 routes + cache-invalidation machinery for that failed the mandate's own test).
The parser reloads the 8 rows from the DB each poll cycle — 60s staleness on an 8-row table
instead of a cache and its invalidation paths. Adding a format = one INSERT + `apex-rust
reparse` (§3).

Dropped: `version` (never read), `created_by`. **New: `sample`.** Validation — enforced by a
CHECK-constraint-shaped Rust path used by both the seed and any future insert helper, and
re-asserted at boot: pattern compiles, defines `amount` + `date` groups, **matches its own
sample post-normalization, and the captured action word resolves through `direction_map` to
`in` or `out`** (the last clause was added after a critic showed a template could pass every
other check and still produce uninsertable NULL-direction rows). A boot failure logs at ERROR,
fires an ntfy alert, and disables the template — never a silent drain.

Seed = the six production rows verbatim (dumped today) **plus two** — both patterns were
**executed** against the live gap messages and the full 815-message corpus during review: every
field extracts correctly, zero cross-matches against other buckets or chatter, and the existing
`instant_transfer` provably cannot match the inbound message:

| name | priority | direction_map | date_formats | notes |
|---|---|---|---|---|
| `instant_purchase` | 13 | `{"لحظي": "out"}` | `["%d-%m-%Y"]` | `شراء لحظي`, merchant after `من`, optional balance |
| `instant_transfer_in` | 14 | `{"لحظي": "in"}` | `["%d-%m-%Y"]` | `الي حسابك ... من <sender>`, has counterparty |

```
instant_purchase:
تم\s+تنفيذ\s+عمليه\s+شراء\s+(?P<action_ar>لحظي)\s+من\s+حسابك\s+المنتهي\s+ب\s*\**(?P<account>\d{3,6})\s+بمبلغ\s+(?P<amount>[\d,]+\.\d{2})\s+(?P<currency>جم|ج\.م|[A-Z]{3})\s+من\s+(?P<counterparty>.+?)\s+برقم\s+مرجعي\s+(?P<reference>[A-Za-z0-9]+)\s+بتاريخ\s+(?P<date>\d{2}-\d{2}-\d{4})\s+(?P<time>\d{2}:\d{2})(?:.*?الرصيد\s+المتاح\s+(?:[A-Z]{3}\s+)?(?P<balance>[\d,]+\.\d{2}))?

instant_transfer_in:
تم\s+تنفيذ\s+تحويل\s+(?P<action_ar>لحظي)\s+بمبلغ\s+(?P<amount>[\d,]+\.\d{2})\s+(?P<currency>جم|ج\.م|[A-Z]{3})\s+الي\s+حسابك\s+المنتهي\s+ب\s*\**(?P<account>\d{3,6})\s+من\s+(?P<counterparty>.+?)\s+برقم\s+مرجعي\s+(?P<reference>[A-Za-z0-9]+)\s+بتاريخ\s+(?P<date>\d{2}-\d{2}-\d{4})\s+(?P<time>\d{2}:\d{2})
```

(The purchase pattern's balance clause tolerates an optional ISO-code prefix — the style
`cib_card` already exhibits — extracting `85635.98` from the live message either way.)

### 2.4 `categories` — labels plus the two columns that drive the money flow

```sql
CREATE TABLE banksms.categories (
  id             BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
  key            TEXT NOT NULL UNIQUE,
  label          TEXT NOT NULL,
  label_ar       TEXT NOT NULL,
  posting_kind   TEXT CHECK (posting_kind IN ('advance','loan','salary')),  -- NULL = no posting
  required_party TEXT NOT NULL DEFAULT 'none'
                 CHECK (required_party IN ('none','driver','employee','either')),
  sort_order     INTEGER NOT NULL DEFAULT 100,
  enabled        BOOLEAN NOT NULL DEFAULT TRUE
);
```

Revised answer to brief §9 question 3 (owner review): categories are labels **plus posting
behavior**, because the behavior is the product: marking a payment as Advance/Loan/Part-of-salary
against a driver or employee is what makes it reach FalconGo's loans. `posting_kind` collapses
the old `posting_target` + `posting_kind` pair (non-NULL = posts, and says what kind —
matching `loans.kind`'s advance/loan/salary CHECK). `required_party` stays a separate column
because production proves it's independent: the `Salary` category requires a party but posts
nothing. Dropped: `posting_target`, `field_spec` (zod-parsed as `z.unknown()`, never rendered),
`created_by`, timestamps. Writes via psql (12 rows). `transactions.category` stays TEXT.
Seed: the 12 existing rows mapped onto the new columns (Advance→advance, Loan→loan,
SalaryPortion→salary, each `required_party='either'`; Salary keeps `'either'` with NULL kind).

### 2.5 `ingest_cursor` — unchanged

The existing singleton design is correct; carried over as-is.

### Deleted outright

`transaction_overrides` (folded, §7), `transaction_postings` (0 rows — D2), `tags`,
`transaction_tags` (0 rows), `noise_skeletons` (0 rows), plus `public.fleet_expenses` and the
legacy permission-3 `/api/v1/fleet-expenses*` stack (~1,100 lines) that still reads it.

---

## 3. Parse flow — one path, three verdicts

```
message → normalize (salvaged verbatim, NFKC stub deleted)
        → try templates in priority order, first match wins
            match + PetroApp counterparty/body  → status 'suppressed' (recognized, then excluded)
            match                               → status 'matched' + INSERT transaction
            no match                            → status 'ignored'
```

- **No triage scorer, no tier 2 extractors, no skeletons, no partial, no review queue.**
  Empirical basis: 8 templates = 100/100 corpus coverage; the scorer's strangest production bug
  (score 105 in isolation stored as 0) dies with it. The Ignored view (§6) is the safety net.
- PetroApp check moves **after** template matching (brief §4: "recognise the format, then
  exclude it"): a template-matched message whose text matches `(?i)petroapp|بترو\s*اب` is
  `suppressed` with its template recorded. A PetroApp keyword with **no** template match is just
  `ignored` — visible in the Ignored view as a gap to chase.
- Transaction insert is `ON CONFLICT (raw_message_id) DO NOTHING` — re-parsing a message that
  already has a transaction (including a human-promoted one) never duplicates and never
  overwrites an edit.
- **`apex-rust reparse`** — a CLI one-shot (~30 lines, same shared parse path) that re-runs the
  parser over all raw messages without a transaction, updating their status. This is what makes
  "status is recomputable" true in practice and what applies a newly seeded template to the
  backlog. Idempotent by the conflict clause above; it never touches rows that already produced
  a transaction.
- Salvaged verbatim: the normalization character table, `normalize_currency`
  (`جم/ج.م/جنيه → EGP`), `parse_money`, `parse_datetime` + `parse_yearless`, `derive_fee`, the
  direction vocabularies, and all six production regexes.
- Money is `rust_decimal::Decimal` end to end (already true today; stays true).

## 4. Ingest — the poller design that already works, minus the machinery

Carried over: poll every 60s; `start_time = last_wa_timestamp − 300s`; page `limit=100` until a
short page (never trust `pagination.total`); blind `INSERT ... ON CONFLICT (wa_message_id) DO
NOTHING` per page; **cursor advances once per cycle, after all pages commit,
`GREATEST`-guarded** — the exact semantics that fixed the silent-skip bug, plus the
kill-mid-batch test that caught it. Backfill stays a CLI one-shot through the same insert path,
never touching the cursor.

Dropped: the webhook receiver (D5), the advisory lock (single-instance deploy), the Prometheus
counter module, the skeleton alarm, `INGEST_ENABLED`, the unused API token.

Notifications (ntfy) carried over behaviorally unchanged: ASCII-sanitized `X-Title`, Arabic in
the UTF-8 body, `X-Click` deep link to `/fleet-expenses/{id}/edit`, one summary push when a
cycle produces >5 transactions.

**Timezone rule, stated once and normatively:** all date boundaries are **Africa/Cairo** —
the month/range filters resolve to Cairo-midnight instants (converted server-side with
chrono-tz), `/statistics` `by_date` buckets and the ledger's day groups are the Cairo calendar
day of `occurred_at`, and the XLSX export renders dates in Cairo time. (Constraint 4: a
UTC-day bucketing would silently shift any 00:00–03:00 Cairo transaction into the previous
day — and across month ends, the previous month.)

## 5. API — 16 routes instead of ~30

All under `/api/v1`, permission 4 enforced in one middleware place; health at root. Amounts
cross the wire as strings (unchanged). **No hand-numbered SQL anywhere** — dynamic filters go
through `sqlx::QueryBuilder`; static queries are compile-checked macros.

| Route | Notes |
|---|---|
| `GET /transactions` | Keyset-paginated list. Filters: `from,to,category,company,payment_method,q,source,include_fuel,include_loans,cursor,limit` (`company`/`payment_method` kept — they're live UI filters today; dead `direction`/`account`/`min_amount`/`max_amount` filters dropped). Still a 3-branch UNION (bank + `fuel_events` + `loans`, read-only branches, synthetic id namespaces) because the toggles are used — but each branch is now a plain SELECT built by QueryBuilder. |
| `GET /transactions/statistics` | Same filters. Returns: totals (in/out/net/fees), `by_date` (feeds the daily chart and the per-day group totals), `by_category` (feeds the pie and the breakdown list), and `by_party` — grouped on `driver_id`/`employee_id` with real names (legacy `paid_by` text as fallback) over posting categories: the exact "who owes what" answer (D2). `by_car`/`by_company`/`by_source`/`by_account`/`by_payment_method` are dropped with the ranked lists that consumed them (D10). |
| `GET /transactions/export` | Server-side XLSX of the full filtered set; dates in Cairo; sheet `rightToLeft` when the requesting locale is Arabic; Arabic string in the export test fixture. |
| `POST /transactions` | Manual create. Optional `raw_message_id` = "record this ignored message" — the promotion path, one shared handler. Runs `validate` + `reconcile` (below). |
| `GET /transactions/{id}` | Detail incl. linked raw body verbatim (fixes "can't see the SMS from the transaction") and loan registration status. |
| `PATCH /transactions/{id}` | If-Match on `version`; edits columns in place, stamps `edited_by/at`. Also the path inline category-set uses. Runs `validate` + `reconcile`. |
| `DELETE /transactions/{id}` | Soft delete, If-Match; cascades to the registered loan (soft delete) so no orphan deduction survives. |
| `GET /parties` | Restored (D2): merged read of `public.drivers` + `public.employees` for the picker. Read-only — employee creation stays on FalconGo's own `POST /api/employees`, which the frontend already calls, so FalconGo remains the single writer of its table. |
| `GET /templates` · `POST /templates` · `PATCH /templates/{id}` | Kept per owner review (D9): sample-validated writes (§2.3) so a new bank format needs neither a deploy nor SSH. No UI screen; curl is the client. |
| `GET /messages` | `status` filter chips, `q` search, keyset pagination, **`exclude_media=true` by default** (254 empty-body cards would otherwise drown the Ignored view). Returns `transaction_id` when linked, so matched cards deep-link. |
| `GET /messages/{id}` | Single message — keeps the promotion form working across refresh/direct entry. |
| `GET /categories` | Picker data. Writes via psql (12 rows). |
| `GET /healthz` · `GET /readyz` | Root; readyz pings the WhatsApp API. |

Gone beyond the first draft: categories writes and `/ingest/status` (no widget consumed it;
cursor health is visible in logs and `readyz`), plus everything the first draft already
deleted: tags, history, `/accounts`, `/summary`, reclassify/reparse endpoints, admin routes,
the webhook, and the legacy `/fleet-expenses*` five.

**Loan registration (the D2 flow, salvaged from `postings.rs` with its tests):** on every
create/patch, `validate` rejects a posting category without its required party (immediate,
visible — not a silent no-op later), then `reconcile` brings the registered state in line:
should-post-and-isn't → INSERT into `public.loans` (`amount`, `method='banksms'`, `date` =
Cairo `YYYY-MM-DD`, `kind` = category's `posting_kind`, `is_paid=false`) and record `loan_id`;
shouldn't-and-is → soft-delete the loan, clear `loan_id`; already correct → no-op. One
deliberate behavior change from today: an edit to an already-registered transaction **updates
the linked loan while `is_paid=false`** and returns 409 with a clear message if the loan is
already settled — today's code never refreshes the amount, which was a silent-divergence
hazard once the "verified" gate it assumed was removed. Direction stays one-way: transaction →
loan; loans edited in FalconGo are never read back as truth.

## 6. UI — three screens, phone-first

Same stack (React 19, TanStack Query, Tailwind, i18next RTL), rewritten feature folders. Routes
and the ntfy deep link **keep their URLs**: `/fleet-expenses`, `/fleet-expenses/new`,
`/fleet-expenses/{id}/edit`; `/fleet-expenses/review` redirects to `/fleet-expenses/messages`.

A clickable mockup of all three screens (real production data, working sticky headers, light +
dark) accompanies this document as the "Bank ledger rebuild — UI proposal" artifact.

**Ledger (`/fleet-expenses`).** Month picker as the default control, with a range escape hatch
(presets: last month / quarter / year / all, plus custom from–to; `from`/`to` URL params stay
honored so shared links and arbitrary-range exports keep working — a critic caught that a
month-only picker would silently lose today's date-range capability). Summary strip (spent,
received, fees, uncategorized) from `/statistics` — **the uncategorized tile is a one-tap
filter to the uncategorized rows**, which turns the 89-row backlog into a workable queue.
Under the strip (D10, per owner review): the **daily spending chart** (area, from
`by_date`) and the **category pie** survive; the by-car/company/source ranked lists are
dropped, replaced by a compact **by-category list** (label · count · total) — the rendered
answer to "on what". When posting-category rows exist in range, an **"Advances & loans by
person"** rollup (grouped on `driver_id`/`employee_id` with real names — not free text)
renders beneath it: the "who owes what" answer (D2).

Transactions grouped by **day, with sticky day headers**. Structure specified because
stickiness silently no-ops under the wrong ancestors: the page body is the scroll container;
day headers are `position: sticky; top: <app-bar height>`; **no ancestor between header and
scroll root may have non-visible overflow** — so no `overflow-hidden` month wrappers (today's
bug), and the desktop table gets width containment below the header level. Per-day totals come
from `/statistics.by_date`, not client float math; amounts remain strings client-side
(`Intl.NumberFormat`, no `z.coerce.number()`).

Mobile (≤1024px): cards — amount owns the right edge, counterparty/description `dir="auto"`,
references `overflow-wrap: anywhere`; no horizontal scroll at 375px. **Inline category set** on
every uncategorized row: a native `<select>` (styled, ≥44px target) on desktop and a bottom
sheet on mobile, PATCHing through the same If-Match path as the edit form. **When the chosen
category posts (Advance / Loan / Part of salary), the same sheet flows to a second step —
"to whom?" — the PartyPicker (below); saving registers the loan in one gesture.** Export
button → server XLSX. A quiet "N ignored" link (non-media count) into Messages replaces the
amber badge.

**The PartyPicker is salvaged, not rebuilt** — it's one of the best pieces of the current UI:
a searchable combobox over `/parties` (drivers marked with a truck icon, employees with a
person), **typing a new name offers "create employee ‹name›"** inline (via FalconGo's own
`POST /api/employees`), and the duplicate-spelling guard selects "Shady" when you type
"shady " instead of minting a second person — the comment in the source documents exactly why
(free-text `loans.method` decayed into five spellings of one human). Drivers are deliberately
not creatable here — they're onboarded elsewhere with licences. The category's
`required_party` filters which kinds appear.

**Messages (`/fleet-expenses/messages`).** Filter chips **Ignored / Suppressed / Matched**
(+ a "media" chip for the 254 empty-body messages, hidden by default), search, real pagination.
Each card: verbatim body (`dir="auto"`, `whitespace-pre-wrap`, `overflow-wrap: anywhere` — a
bare-hex reference must wrap at 375px), timestamp, status pill with template name, "PetroApp"
badge + a one-line *why* on suppressed cards. Matched cards deep-link to their transaction.
Ignored cards get one action: **"Record as transaction"** → `/fleet-expenses/new?raw_message_id=N`
(fetches via `GET /messages/{id}`, so refresh survives) with the body pinned alongside the form.
**Prefills: date from the envelope timestamp, direction `out`, currency `EGP` — all visibly
editable. Amount is never prefilled: with the extractors gone there is nothing parser-grade to
prefill from, and guessing money is the one thing this system refuses to do.** Tap-to-copy on
the body panel so amounts/references are copied, not retyped. Saving links the transaction to
the message; the card thereafter shows "recorded → #id" and leaves the Ignored filter. Terminal
and visible — nothing can re-queue.

**Edit (`/fleet-expenses/{id}/edit` — the ntfy deep-link target, unchanged).** Two panels: the
editable form (amount, date, direction, category, **party — rendered when the category's
`required_party` says so**, counterparty, reference, description, payment_method, company,
vehicle — every free-text input `dir="auto"`) and, for SMS rows, the **original message
verbatim**. When a transaction is registered, a status chip says so in words: *"Registered as
advance to عماد — unpaid"*, and it links nowhere the user can break (one-way flow). If-Match
kept; 409 → reload toast. "Edited by X on Y" shown when set. Save returns via `history.back()`
to the preserved list (query cache + scroll restoration); deep-link entries navigate to the
ledger. Plain dropdowns are native `<select>` elements styled with CSS — the Radix
`onValueChange("")` bug class (three stacked workarounds today) exits the codebase; the
PartyPicker's Popover+cmdk combobox is unaffected by that bug class and stays. Amount input:
`inputmode="decimal"` text, submitted as a string.

## 7. Cutover — a CLI one-shot, not a boot migration

The adversarial review killed the first draft's "first sqlx migration renames the schema"
design three independent ways (sqlx 0.8 has no `set_migration_table`; the migrator validates
the *old* 9-version history against the new source and dies with `VersionMissing` before
anything runs; the rename would move the bookkeeping table out from under the migrator
mid-transaction). The redesign:

**`apex-rust cutover`** — a CLI subcommand (precedent: the existing `backfill` subcommand),
run **while the old binary is stopped, before the new binary starts serving**. One DB
transaction, in Rust (it needs the real parser — Postgres regex has no named groups):

1. Preflight: assert `banksms.transaction_overrides` exists (old schema present) and
   `banksms_legacy` doesn't.
2. `ALTER SCHEMA banksms RENAME TO banksms_legacy`.
3. Create the new `banksms` schema + tables (§2), stamp a fresh `banksms._sqlx_migrations`
   with the baseline version so `sqlx::migrate!` (unchanged search_path mechanism) owns only
   future migrations. Seed 8 templates + 12 categories (with their `posting_kind` /
   `required_party` mappings, §2.4).
4. **Copy raw messages** (815, ids preserved — `GENERATED BY DEFAULT` allows it): columns map
   1:1; `status`/`template` recomputed by the new parser over every body.
5. **Copy transactions** (all 321, ids + `version` + timestamps preserved): effective values =
   **all seven overridable fields folded generically** from active overrides
   (`superseded_at IS NULL AND NOT is_cleared`) over `parsed_*` — not just the fields believed
   to differ; `edited_by`/`edited_at` stamped from the latest override's actor/set_at so edit
   provenance survives. `driver_id`/`employee_id` copy trivially (0 rows carry them) and
   `loan_id` starts NULL everywhere (`transaction_postings` has no rows to fold — verified).
   The 2 direction-less rows (295, 305) are instead **re-parsed by the two new templates into
   complete rows** keeping their ids and amounts (135.72 + 5,700.00).
6. Restart both identity sequences past `max(id)`.
7. **Verify, abort on any mismatch** (a failure rolls back the whole transaction — the old
   schema is untouched and restarting the old binary is the entire recovery):
   - live count 315, Σ **2,921,440.35**; whatsapp 92 / import 223; import Σ 2,488,598.00
   - per-category counts equal today's dump; raw count 815 with status split **98 / 2 / 715**
     (matched / suppressed / ignored)
   - id preservation probed directly: 224/276/284 exist with their pre-migration `version`
     values and **all five overridden fields equal to today's dumped effective values**;
     295/305 are complete (direction `out`/`in`, currency EGP)
   - every matched raw has exactly one transaction **counting soft-deleted ones** — precisely
     6 matched raws link only to soft-deleted rows, 92 to live ones
   - a probe insert on each table receives an id greater than the copied maximum

Then the new binary starts. Total downtime: minutes, and a verification failure leaves nothing
half-applied. Legacy code (`handlers/expense.rs`, `db/expense_queries.rs`, legacy routes,
webhook, tiers, postings, tags) is deleted in the same release.

**Soak & rollback.** Two weeks with `banksms_legacy` intact. Rollback sequence (stated
precisely because rename-back collides): stop new binary → `ALTER SCHEMA banksms RENAME TO
banksms_failed` → `ALTER SCHEMA banksms_legacy RENAME TO banksms` → start old binary → refill
the message gap with the old backfill CLI. **Rollback discards soak-period human work**
(category assignments, edits, promotions) — before renaming, dump
`transactions WHERE edited_at IS NOT NULL OR source='manual' OR category IS NOT NULL` for
manual re-entry, and note the old parser will re-queue the two variant messages as `partial`.
After the soak, a follow-up drops `banksms_legacy` **and** `public.fleet_expenses` (safe: no Go
model, no GORM registration).

## 8. Testing — assert values, not vibes

Integration tests against real Postgres (dockerized in CI, local DB in dev), written per phase:

1. **Resumption**: kill the ingester between page commits; restart; assert zero duplicates AND
   zero skips (the test that caught the real bug — carried over).
2. **Per-template dates**: each of the 8 templates' fixture resolves to the correct calendar
   day under its own format list; `abk`'s month-first asserted against a day>12 counterexample.
3. **Year boundary**: wallet fixture stamped 2 Jan reporting `12-31` resolves to 31 Dec of the
   prior year (`%m-%d`, month-first — matching production data, not the brief's example).
4. **Idempotency**: every write path run twice; second run is a no-op (ingest, parse, import,
   promote, reparse sweep, cutover re-run refusal).
5. **API-shape round-trip**: the poller parsed against a recorded v9.0.0 response fixture, so a
   WhatsApp version bump fails a test, not production.
6. **Corpus acceptance**: the full 815-message fixture set (cut today from live) → exactly
   98 matched + 2 suppressed + 715 ignored, and every matched message's **extracted amount,
   date, direction, account, reference** equals the recorded expected values.
7. **Cutover rehearsal**: run `apex-rust cutover` against a fresh prod dump **including the old
   `banksms._sqlx_migrations`**; assert §7's numbers; run it twice, assert the second run
   refuses at preflight.
8. **Normalization goldens** + boot-time template/sample/direction assertion test.
9. Frontend: 375px check for horizontal overflow, a **scroll-and-assert** check that day
   headers actually stick (a static snapshot cannot detect a non-sticking header), promotion
   flow, If-Match 409, and an Arabic string in the export fixture.

## 9. Size budget

| | Today | Target |
|---|---|---|
| Backend (banksms modules) | ~6,600 | **~2,100** (ingest+backfill 300 · parser 500 · api incl. XLSX export 700 · loan registration 150 · notify 100 · cutover 250 · glue 100) |
| Tables | 11 | **5** |
| Routes | ~30 | 16 |
| Frontend feature code | ~3,200 | **~2,000** (incl. the salvaged PartyPicker and two charts) |

The first draft said ~1,400; the critics showed that number ignored the salvage-verbatim
floors (normalize.rs alone is 162 lines) and the Rust cutover the redesign forces; the owner
review added back the loan-registration flow, `/parties`, template routes, and two charts.
Per the mandate's own rule, the requirements forcing ~2,100 are: DB-resident templates with
sample+direction validation and HTTP CRUD (D9), the D4 three-branch union, server-side XLSX
export, loan/advance registration into `public.loans` (D2), and a verify-or-abort cutover in
Rust. Still less than a third of today's code for strictly more capability than the user
actually uses.

---

## 10. Decisions — all resolved with the owner, 2026-08-12

**D1 — RESOLVED: triage is gone.** "Does any template match?" is the whole gate; 100/100
measured coverage; misses land in the visible, searchable Ignored view.

**D2 — RESOLVED (owner corrected the recommendation): loan/advance registration stays.**
The first-draft recommendation to delete it over-read the zero-rows evidence — the zero was
categorization friction, not disuse, and the loans ecosystem is live in FalconGo. What ships:
categorizing a payment as **Advance / Loan / Part of salary** asks "to whom?" (PartyPicker over
real drivers/employees, inline employee creation via FalconGo's endpoint, duplicate-spelling
guard) and registers a `public.loans` row (`kind` = advance/loan/salary, `method='banksms'`,
Cairo date, unpaid) linked via `transactions.loan_id`. The posting invariant survives: only
rows we created are ever touched; delete cascades as soft-delete; edits sync while unpaid and
409 once paid. What's still deleted: the `transaction_postings` table (replaced by `loan_id`),
`posting_target`, `field_spec`, and the "verified" gate that never came back.

**D3 — RESOLVED, amended by D2: categories are labels + `posting_kind` + `required_party`.**
Still psql-managed, still no behavior JSONB.

**D4 — RESOLVED: keep fuel/loans blending, as toggles** (`include_fuel`/`include_loans`,
URL-persisted, default on — same semantics as today, clean QueryBuilder branches underneath).

**D5 — RESOLVED: webhook dropped.** 60s poller only.

**D6 — RESOLVED: all calendar boundaries Africa/Cairo.**

**D7 — RESOLVED: two new templates seeded** (`instant_purchase`, `instant_transfer_in`),
execution-verified patterns and explicit direction_maps.

**D8 — RESOLVED: sqlx stays on 0.8.**

**D9 — RESOLVED (owner chose the alternative): template routes stay.**
GET/POST/PATCH `/templates` with sample+direction validation — a new bank format needs neither
a deploy nor SSH. Categories remain psql-managed; no template UI screen.

**D10 — RESOLVED (owner chose which): daily spending chart + category pie survive**;
by-car/company/source ranked lists are dropped, replaced by the by-category list and the
advances-by-person rollup.
