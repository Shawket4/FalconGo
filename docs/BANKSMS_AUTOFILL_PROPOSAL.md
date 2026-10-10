# Proposal: bank-SMS auto-fill (classification + details) with a learning loop

**Date:** 2026-10-10 · **Status:** draft for owner review — no code written.
Builds on [BANKSMS_REBUILD_PROPOSAL.md](BANKSMS_REBUILD_PROPOSAL.md). Target codebase: `apex-rust`
(`src/parser/`, `src/ingest/poller.rs`, `src/api/transactions.rs`).

---

## 1. Problem

The parser solves *extraction* (direction, amount, date, account, counterparty, reference) at
100/100 on the corpus. It does nothing for *classification*: `category`, the party
(`driver_id` / `employee_id`), the vehicle (`car_id` / `car_no_plate`), `company`, `description`.

The rebuild audit already measured this as the real gap: **only 3 of 92 whatsapp transactions
ever got a category** (2026-08-12). Categorization is manual, so it mostly doesn't happen — and
the loans/advances flow (`posting_kind`) never gets input.

Goal: every new transaction arrives with these fields pre-filled from patterns in the DB, and
every human correction makes the next suggestion better — without weakening the parser's
"never guess money" guarantees.

## 2. Non-goals

- **Extraction stays as is.** Templates remain the only source of direction / amount / date /
  account / counterparty / reference. Nothing in this proposal writes those fields.
- **No auto-splitting.** `split` stays a human action.
- **No auto-registration of money flows.** See §6.
- PetroApp-suppressed and ignored messages never reach the filler.

## 3. Fields in scope

| Field | Typical signal |
|---|---|
| `category` | counterparty, template, account, amount range |
| `driver_id` / `employee_id` | counterparty name (IPN transfers name the person) |
| `car_id` / `car_no_plate` | driver → usual car; counterparty (workshop / parts shop) |
| `company` | account (which bank account pays for which company) |
| `description` | last human description for the same counterparty |
| `payment_method` | template (card vs IPN vs wallet) — fully deterministic |

## 4. Architecture: three layers, first confident answer wins

Runs **per field**, right after `parser::parse` returns `Verdict::Matched`, inside the same
per-page DB transaction in `ingest/poller.rs` (and in `backfill`).

```text
Matched transaction
   │
   ├─ L1  fill_rules          deterministic, human-owned or promoted
   │        hit → value, confidence = 1.0, source = 'rule'
   │
   ├─ L2  history             deterministic SQL over human-confirmed rows
   │        support ≥ min_support AND share ≥ min_share → value, source = 'history'
   │
   └─ L3  decision model      optional, behind a flag (Jev or alternative)
            only for fields L1/L2 left empty; source = 'model'
   │
   ▼
fill_suggestions rows (one per field)  →  apply policy (§6)  →  transaction columns
```

### L1 — `fill_rules` (new table, mirrors `parse_templates`)

Explicit "when X, set Y" rows. Same philosophy as templates: in the DB, live within one poll
cycle, validated on write.

- **Match** (all optional, AND-ed): `counterparty_pattern` (regex, post-normalization form —
  reuse `parser::normalize`), `account`, `template`, `direction`, `amount_min` / `amount_max`.
- **Set:** a JSONB of field → value (e.g. `{"category":"Parts","company":"Apex"}`).
- `priority`, `enabled`, `origin` (`manual` | `promoted`), `hits`, `overrides`, `sample_txn_id`.
- Write-time validation: regex compiles; every value in `set` resolves (category key exists and
  is enabled, driver/car ids exist); the rule matches its `sample_txn_id`.

### L2 — history (no new table)

For an unseen field, look at **human-confirmed** transactions with the same normalized
counterparty (fallback keys: account + template, then template alone):

```sql
SELECT final_value, count(*) AS n
FROM banksms.fill_suggestions s
JOIN banksms.transactions t ON t.id = s.transaction_id
WHERE s.field = $1 AND s.confirmed AND t.deleted_at IS NULL
  AND s.key_counterparty = $2
GROUP BY final_value ORDER BY n DESC LIMIT 2;
```

Accept the top value only if `n ≥ min_support` and `n / total ≥ min_share`. Confidence =
share. Ties or thin support → no answer (fall through to L3 or leave empty).

Cold start: the 223 imported rows and the few categorized whatsapp rows seed history via a
one-off backfill of `fill_suggestions` with `source = 'import'`, `confirmed = true`.

### L3 — decision model (optional)

Only for fields L1/L2 couldn't fill — in practice, first-time counterparties.

- One request per transaction, several questions sharing one state:
  `category` (choice over enabled categories, label descriptions built from category labels +
  top counterparties seen per category), `party` (choice over active drivers/employees + "none").
- State = normalized body + extracted fields. The body is **untrusted text** (prompt-injection
  risk noted in TypeSafe's own jev-1.13 notes) — model output is only ever a *suggestion*.
- No fine-tuning exists for Jev; its "learning" is the label descriptions regenerated from
  history each cycle. Real adaptation lives in L1/L2.
- Provider behind a trait so Jev / Clef / GLiDE / a local model can be swapped; disabled when
  no key configured. Pin the model version (`jev-1.13.0`, not `jev-latest`) so suggestions are
  reproducible.

## 5. Feedback loop (the "self-improving" part)

### 5.1 `fill_suggestions` (new table)

| column | notes |
|---|---|
| `transaction_id`, `field` | PK pair |
| `suggested_value`, `source` (`rule`/`history`/`model`/`import`), `rule_id`, `confidence` | what the system proposed |
| `applied` | whether policy (§6) wrote it into the transaction |
| `final_value`, `confirmed`, `decided_by`, `decided_at` | what a human settled on |
| `key_counterparty`, `key_account`, `key_template` | denormalized lookup keys for L2 |

> **Schema decision this reverses:** the v2 migration states *"no parsed_* twins, no overrides
> ledger"*. That was right for extraction (deterministic, recomputable). Classification is not
> recomputable from the body, so the suggestion/decision pair has to be stored to learn from.
> Scope is limited to classification fields; extraction stays ledger-free.

### 5.2 Capturing decisions

- `PATCH /transactions/{id}` (`src/api/transactions.rs`, the `edited_by` update): for every
  in-scope field in the patch, set `final_value`, `confirmed = true`, `decided_by`, `decided_at`.
  Changed value = **override**; same value = **acceptance**.
- New lightweight `POST /transactions/{id}/confirm` — accepts all current suggestions in one tap
  (acceptances are as valuable as overrides; without them history only learns from mistakes).
- Manual transactions (`source = 'manual'`) and split children also write `confirmed` rows —
  they are labelled data.

### 5.3 Promotion and demotion (runs each poll cycle, cheap)

- **Promote:** same key → same final value, ≥ `promote_n` confirmations, zero contrary
  confirmations → insert `fill_rules` row with `origin = 'promoted'`. Mode is a decision (§8).
- **Demote:** a rule whose suggestions are overridden ≥ `demote_n` times (or override rate
  above threshold over a window) → `enabled = false`, logged at ERROR and notified — same
  "loud, never silent" pattern as `templates::boot_check`.
- **Drift:** history naturally follows recent behavior if L2 weights or windows recent rows
  (e.g. last 180 days); see §8.

## 6. Apply policy — what gets written vs only suggested

| Case | Behavior |
|---|---|
| Category has `posting_kind` (Advance, Loan) | **Suggest only, never auto-apply** — applying registers a `public.loans` row |
| Category `required_party` ≠ none, party not confidently filled | Suggest only |
| `source = 'rule'` | Auto-apply |
| `source = 'history'`, confidence ≥ `auto_apply_threshold` | Auto-apply |
| `source = 'model'` | Suggest only (until its measured precision earns more — §7) |
| Anything else | Suggest only |

"Suggest only" = the transaction columns stay empty; the UI shows the suggestion pre-filled
with a source badge (rule / history n-of-m / model %) and one-tap confirm.

Auto-applied values remain normal user-owned columns — editable, versioned (`version`,
If-Match) exactly as today; the edit is recorded as an override.

## 7. Measurement

From `fill_suggestions` alone, per field and source:

- coverage (share of transactions with any suggestion),
- precision (confirmed value == suggested value),
- override rate per rule (feeds demotion).

Expose as a small stats endpoint next to `/statistics`. This is also the gate for letting the
model layer auto-apply, and the only honest way to compare Jev with alternatives on this data.

## 8. Open decisions (owner)

- **D1 Promotion mode:** auto-create enabled rules · auto-create disabled rules for review · propose only (no row).
- **D2 Thresholds:** `min_support`, `min_share`, `auto_apply_threshold`, `promote_n`, `demote_n` — fixed constants vs a config table.
- **D3 Model layer:** ship L1+L2 first and add L3 later · ship all three with L3 off by default · skip L3.
- **D4 Model provider:** Jev (TypeSafe / Vercel / Cloudflare gateway) · Clef · GLiDE · self-hosted Jev-compatible (Kev / SemIf).
- **D5 History window:** all-time · last N days · recency-weighted.
- **D6 Re-fill on rule change:** apply new/changed rules to past *unconfirmed* rows · new rows only.
- **D7 Counterparty normalization key:** raw normalized string · additionally strip masking (`A........`) and honorifics · a manual alias table mapping counterparty variants to one party.

## 9. Build order

1. Migration: `fill_rules`, `fill_suggestions` (+ indexes on keys). Backfill history from
   imported + already-categorized rows.
2. `src/fill/` module: `rules.rs` (load/validate, like `parser/templates.rs`), `history.rs`,
   `policy.rs`, `mod.rs` (orchestrates per field). Pure functions over loaded data where
   possible, so they test like `parser` does.
3. Hook into `ingest/poller.rs` after `Verdict::Matched` (and `backfill`).
4. Decision capture in `transactions::patch`, new `confirm` route, manual + split paths.
5. Rules admin routes mirroring `templates_admin`.
6. Promotion/demotion pass in the poll cycle; stats endpoint.
7. Frontend (apex-react): suggestion badges, confirm button, rules screen.
8. Optional L3 behind config + provider trait.

## 10. Risks

- **Thin labels at start** — 3 categorized SMS rows. L2 is weak until imports are backfilled and
  confirmations accumulate; the confirm-in-one-tap path matters more than any algorithm.
- **Wrong rule promoted early** feeds itself if auto-applied values are never reviewed —
  mitigated by counting only `confirmed` rows as history and by demotion.
- **Counterparty masking** (`AHMED A........ A...`) collapses different people to one key —
  why D7 matters before any party auto-fill.
- **Loans side-effects** — fully avoided by §6 (posting categories never auto-apply).
