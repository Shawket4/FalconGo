# Rebuild brief: bank-SMS transactions

You are rebuilding a working-but-overbuilt system from scratch. Read this whole
brief before writing code. It contains facts that took a full day to establish —
do not re-derive them, verify them cheaply and move on.

---

## 1. What this actually is

A person forwards their bank's SMS notifications into one WhatsApp chat. The
business wants those to become a searchable, categorised ledger of company
spending, and wants advances paid to drivers and staff to reach the payroll
system.

That is the whole product. Everything else is implementation.

**Scale, measured in production today:**

| | |
|---|---|
| Bank SMS successfully parsed, all time | **95** |
| Messages in the source chat | ~820 |
| Of those, ordinary human conversation | **~87%** |
| Legacy expense rows migrated in | 223 |
| New bank transactions per day | roughly 3–8 |

**This is a small-data problem.** Under a hundred real records. Any design that
cannot be held in one person's head is wrong for this size.

---

## 2. Why you are rebuilding — read this honestly

The existing implementation works, passes 114 tests, and is deployed. It is also
**~6,600 lines of Rust, ~3,200 lines of TypeScript, and 11 database tables** to
manage 95 records. That ratio is the whole problem.

It was built incrementally against a detailed spec, and each addition was
defensible on its own. The result is not.

### Concrete failures, so you can avoid repeating them

**Silent skips.** The WhatsApp API returns messages newest-first. The poller
advanced its cursor per page while paging *backwards* into older messages, so a
crash mid-cycle left the cursor in the future and permanently skipped everything
older. Caught only by deliberately killing the process mid-batch.

**An infinite review loop.** Messages that parsed only partially sat in a review
queue. Clicking "this IS a transaction" re-queued them for the parser, which is
deterministic, so it reached the same verdict and the message returned
immediately — forever. The queue's exit condition was "the parser is now happy",
which no human action could bring about.

**Hand-numbered SQL parameters, twice.** A three-branch UNION with fixed `$1..$N`
slots. Removing one filter shifted every later parameter; the outer query still
read `$16` and every list request died with `cannot cast type bigint to timestamp
with time zone` — an error naming a cast, pointing nowhere near the filter that
moved. Shipped to production. Fixed, then the *same class* of bug recurred in a
second query.

**A feature added and removed within a day.** A `verified` flag was introduced to
gate posting to payroll, then removed entirely once it was clear that a
template match already establishes correctness and the click confirmed nothing.

**Frontend fighting its own components.** Radix `Select` emits
`onValueChange("")` when its controlled value matches no *mounted* item — and
items only mount when the dropdown opens. A category restored from the server was
silently wiped before the user touched anything. Diagnosed only after
instrumenting the component.

**Migration checksums.** A template was appended to an already-applied migration,
which would have broken every environment's next boot.

The pattern: **too many moving parts, each individually reasonable.** Your job is
to build something where these bugs have nowhere to live.

---

## 3. Verified environment facts — do not re-derive

All of this is confirmed against production. Spend ten minutes checking, not a
day discovering.

### The host
Single VPS, `ssh root@<vps-ip>`. Everything below runs on it.

| Service | Port | Notes |
|---|---|---|
| WhatsApp Go API | 127.0.0.1:3000 | `aldinokemal/go-whatsapp-web-multidevice` **v9.0.0**. Unauthenticated by design, bound to loopback via a CLI flag. **Off-the-shelf — do not modify.** |
| FalconGo | 127.0.0.1:3001 | Go/Fiber monolith. Issues the JWTs. |
| apex-rust | 127.0.0.1:3002 | Where this system currently lives. |
| PostgreSQL | 127.0.0.1:5432 | Database `apex`, PG 17.10. |

### Authentication
FalconGo signs tokens with **HS256**, secret from its `JWT_SECRET` env var.

```go
type CustomClaims struct {
    UserType   string `json:"user_type"`  // "admin_user" | "driver"
    UserID     uint   `json:"user_id"`    // NUMBER on the wire
    DriverID   uint   `json:"driver_id"`
    Permission int    `json:"permission"` // 1 viewer .. 4 admin
    jwt.RegisteredClaims                  // exp + iss only
}
```

- **There is no `sub` claim.** Identity is `user_id`, always a JSON number.
- `iss` is the user's own id as a *string* — never validate it, it differs per user.
- `aud` is never emitted — never require it.
- Tokens arrive as a `jwt` **cookie** *or* an `Authorization: Bearer` header. The
  dashboard uses the cookie. Support both.
- Pin the algorithm explicitly. Allow ~60s clock skew.

### The source chat
`TARGET_CHAT_JID = 20XXXXXXXXXX@s.whatsapp.net`

**This is a 1:1 conversation with a real person**, not a bank feed. The same JID,
with `is_from_me: false`, sends both forwarded bank SMS *and* ordinary chat. There
is no forwarder prefix. `is_from_me: true` is our own replies.

Consequence: **content inspection is the only way to tell a bank SMS from
chatter, and ~87% of the chat is chatter.** Any design that assumes otherwise is
wrong.

### WhatsApp API shape
- `GET /chat/{jid}/messages?limit=&offset=&start_time=&search=`
- `limit` caps at **100**.
- **`start_time` is honoured** (verified). Use it — do not page blindly.
- `pagination.total` is the chat's *unfiltered* total. It cannot tell you whether
  more pages match your filter. Page until you get an empty batch.
- Message fields: `id` (32-char uppercase hex, the dedup key), `chat_jid`,
  `sender_jid`, `content`, `timestamp` (**RFC 3339, always UTC**), `is_from_me`,
  `media_type`.
- **Media messages have `content: ""`** — about a third of the chat. Not errors.
- Webhooks are supported: `--webhook <url> --webhook-secret <secret>`. Near-instant
  delivery, but **no delivery guarantee** — a push sent while the service restarts
  is simply lost. Keep a poller as the guarantee; the webhook is latency only.

### Time
Message envelopes are UTC. Times *inside* the SMS bodies are **Africa/Cairo**.
Egypt observes DST — use the tz database, never a fixed +2/+3.

---

## 4. What to keep — this part is genuinely validated

### Normalization pre-pass
Normalize a **copy** for matching; store the original verbatim. In order:

1. Arabic-Indic (`٠-٩`) and Persian (`۰-۹`) digits → ASCII
2. Strip bidi marks `U+200E/200F/061C/202A-202E` and zero-width `U+200B-200D`
3. NBSP → space, strip tatweel `U+0640`, strip diacritics `U+064B-0652`
4. Unify alef `أإآٱ → ا`, `ى → ي`, `ة → ه`
5. Arabic decimal `٫ → .`, thousands `٬ → ,`
6. Collapse whitespace runs, then NFKC

**Patterns must be written in post-normalization form.** A pattern containing
`إلى` compiles fine, inserts fine, and can never match, because the haystack
contains `الي`. This cost real debugging time. Add a startup assertion that every
pattern matches at least one fixture.

### The six real formats
All validated against live traffic; each has its **own** date format. A shared
date parser silently produces wrong dates rather than erroring — `8/6/26` is
valid under both `%m/%d/%y` and `%d/%m/%y` with different meanings.

| Format | Hotline | Date | Notes |
|---|---|---|---|
| `abk` | 19322 | `%m/%d/%y` | **Month-first — the only one.** Amounts have 1–2 decimals. |
| `arabic_ipn` | 19666 | `%d-%m-%Y` | Masked account `********9276`. |
| `ref_balance` | 19555 | `%d/%m/%Y` | Dashed account `6001-01`, reports running balance. |
| `cib_card` | — | `%d/%m/%y` | Card mask, merchant instead of counterparty, **no reference at all**. |
| `instant_transfer` | 19666 | `%d-%m-%Y` | Currency written **`جم`**, not an ISO code. Reference is bare lowercase hex. No counterparty. |
| `wallet_transfer` | 19623 | `%m-%d` | **No year in the date.** Has a counterparty. Usually PetroApp. |

The exact regexes are in `banksms.parse_templates` in production — read them out,
they are the tested versions.

Two traps they encode:
- **`جم` is Egyptian pounds.** Normalize it to `EGP` or your currency column
  splits the same money across two codes.
- **A year-less date must be resolved against the message's own arrival
  timestamp**, with year-boundary correction (a message arriving 2 January
  reporting `31-12` means *last* year). Guessing "this year" is right until
  something is reparsed.

### Business rules that are correct and must survive
- **PetroApp transfers must not enter the ledger.** That fuel already arrives as
  `fuel_events` from the PetroApp sync; recording the bank notification too
  double-counts it. Recognise the format, then exclude it — an unrecognised
  message is a gap to chase, a suppressed one is a decision.
- **A 0.1% IPN fee exists.** Some banks send fee-inclusive amounts
  (`2002.00` = 2000 + 2), others deduct separately. Both appear in the same chat.
  Derive the split for display; never store a guess.
- Money is `NUMERIC` and a decimal type end to end. Never `f64`, never a JS
  float. Amounts cross the wire as strings.

---

## 5. What to throw away

The existing `banksms` schema has **11 tables**. Most of them exist to support
machinery that does not earn its keep at this scale:

| Table | Verdict |
|---|---|
| `raw_messages` | **Keep.** Store raw first, parse second, never lose a message. |
| `transactions` | **Keep**, much simpler. |
| `parse_templates` | Probably keep in DB — new bank formats shouldn't need a deploy. But drop the versioning and `field_spec`. |
| `transaction_overrides` | **Delete.** A per-field append-only override ledger for a table with 95 rows. Just let people edit the row and keep an `edited_at`/`edited_by`. |
| `transaction_postings` | **Reconsider.** It existed to link a transaction to a payroll `loans` row. Payroll has since been deprecated — check whether this coupling is needed at all any more. |
| `categories` | Simplify hard. It grew `posting_target`, `required_party`, `posting_kind`, `field_spec` JSONB. |
| `tags` / `transaction_tags` | **Delete.** Built, never used, no UI. |
| `noise_skeletons` | **Delete.** A feedback mechanism for a triage gate that should be simpler. |
| `ingest_cursor` | Keep — one row. |

Also delete: the four-tier parse pipeline (templates → field extractors →
skeleton mining → review queue), parser versioning, confidence tiering, and
skeleton hashing. **Tier 1 handles every real message.** The lower tiers exist to
handle formats you should instead just add a template for.

---

## 6. Your mandate

### Simplicity
Target roughly **1,500 lines of backend and 4–5 tables**, not 6,600 and 11. If you
exceed that, you should be able to name the specific requirement forcing it.

Two rules that would have prevented most of the bugs above:

1. **No hand-numbered SQL parameters.** Use a query builder, or named binds, or
   compile-time-checked queries. Two production outages came from this.
2. **One code path per operation.** Ingest, parse, and write should each have
   exactly one implementation that every caller shares.

### Testing
Write tests as you go, not at the end. Integration tests against a real Postgres.
The bugs that actually happened were:

- **Ordering/resumption** — kill the ingester mid-batch, restart, assert zero
  duplicates *and zero skips*. This found a real silent-skip bug.
- **Per-template date resolution** — assert each format's sample resolves to the
  correct calendar day under its own format list.
- **Year-boundary inference** for the year-less format.
- **Idempotency** — run every write twice, assert the second is a no-op.
- **Round-trip through the real API shape**, so a WhatsApp version bump fails a
  test instead of production.

A test asserting "the parse succeeded" is nearly worthless here. Assert the
*extracted values*.

### Intuitiveness — this is a first-class requirement, not polish

The current UI is the weakest part and the user has said so directly. Problems to
solve, not reproduce:

- **A review queue the user could not empty.** If you keep a queue at all, a human
  decision must be terminal and visible.
- **Nowhere obvious to see what was rejected.** ~87% of messages get ignored. When
  the gate is wrong, the user must be able to find and fix that in one click.
- **The list is undifferentiated.** The user explicitly asked for grouping by date
  with sticky date headers inside each month, and for a layout that works on a
  phone — this is used on mobile.
- **The mobile experience.** Not a shrunken table. Cards, real tap targets, no
  horizontal scrolling ever. Verify at 375px.
- **Arabic.** Counterparties, plates and descriptions are Arabic. `dir="auto"`,
  and long unbroken tokens (references, URLs) must wrap.

The user should be able to answer "what did we spend this month, on what, and
who owes what" without being taught the data model.

### Notifications
ntfy push per new transaction with a deep link straight to that row's edit
screen — `https://apextransport.ddns.net/fleet-expenses/224/edit`. This works well
and should survive. Two things learned: ntfy reads title/tags from **HTTP
headers**, so Arabic must go in the UTF-8 body or the request is rejected
outright; and a bulk run must collapse into one summary rather than firing fifty
pushes.

---

## 7. Hard constraints

1. **Do not modify the WhatsApp Go API.** Off-the-shelf. Configuration only.
2. **Do not break FalconGo.** It AutoMigrates its own tables on every boot — a
   table you drop will silently reappear empty. `public.expenses` and
   `public.loans` are its, and `public.employees` has CRUD there.
3. **Do not lose data.** 223 migrated legacy expense rows and 97 parsed bank
   transactions exist in production. Any new schema must carry them across with
   a verified row count and money total.
4. **Money correctness beats convenience.** A wrong date or amount that nothing
   flags is the worst outcome available. Prefer refusing to guess.
5. Whole module is **permission level 4 only**, backend and UI.

---

## 8. Where to start

1. **Audit before designing.** Read the existing implementation in
   `/Users/shawket/Rust/apex/src/{parser,ingest,api}` and the dashboard in
   `apex-react/src/{entities,pages,widgets}/…fleet-expenses…`. Take the template
   regexes and the normalization rules; be sceptical of everything else.
2. **Pull real data.** Fetch the actual chat history through the API and the
   existing rows from `banksms.*`. Design against real messages, not the spec.
3. **Propose the schema and the UI before building either**, and stop for review.
   The two most expensive mistakes here were schema shape and a UI flow that
   could not be completed — both are cheap to fix on paper.

Then build it, test it as you go, and migrate the data with verification.

---

## 9. Things worth asking about before you commit to them

- Is a triage score still the right mechanism, or is "does any template match?"
  sufficient now that six templates cover real traffic? The scoring gate is the
  most fragile part of the current system and the source of its strangest bug: a
  message that scores 105 in isolation was stored with a score of 0 in
  production, and the cause was never fully established.
- Does the payroll coupling still need to exist, given payroll is deprecated?
- Should categories carry behaviour at all, or just be labels?
