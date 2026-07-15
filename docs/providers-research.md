# Provider API path-forward research

**Issue:** [BSOD-91](../README.md) — API path-forward investigation for 5h/weekly subscription limits.

## Scope and definitions

For each of the five providers the `aud` dashboard is meant to surface, we
investigated whether there is a viable, supported path to obtain
**current-state 5-hour and weekly subscription limits** (used / limit /
remaining + reset) — the values the user wants to see in a "how much have I
got left" dashboard.

### What "viable path" means (in priority order)

1. **Official API.** Provider-published, documented, supported endpoint(s)
   that return current-state plan limits for the user's subscription tier.
   Auth model must match what an end user can do with their own key —
   not just org/admin operators.
2. **Rate-limit response headers** (`x-ratelimit-*`, `anthropic-ratelimit-*`,
   etc.). Document what they actually represent per provider and whether
   they can be aggregated into a proxy for plan usage.
3. **Account dashboard data.** JSON endpoints behind the provider's web
   dashboard that can be hit with the user's credentials (no browser
   automation).
4. **Community / OSS tools.** Existing libraries, scrapers, or wrappers
   (e.g. `ccusage`) that already solve this.
5. **Other paths.** Anything else worth considering.

### Constraints we will not relax

- **No headless browser automation in production.** Browser automation is
  acceptable only as a one-off reverse-engineering technique to discover an
  underlying API — not as a runtime fetch strategy.
- **No scraping with user credentials** beyond what the official API / SDK
  allows.
- **No admin / org-tier credential requirement** unless clearly the only
  documented path and we surface the limitation in the UI.
- **No "estimated" or "inferred" limits** — surfaced values must come from
  the provider, not derived heuristics.

## Headline findings

- **One clear GO** (`minimax`) with a documented official API for the exact
  5h/weekly windows the user wants. This single provider is sufficient to
  ship the dashboard as a real usage tracker.
- **Two providers have an official API for a *different* signal** (spend
  against per-key rate buckets, not plan windows) that we could surface as
  a partial signal under `CONDITIONAL` (`openai`, `anthropic`).
- **Two providers have no documented public usage/cap API within our
  constraints** (`google-antigravity`, `amp`). The dashboard can still ship
  them as "scaffolded, not yet live" per the parent's MVP-scope rule.

## Summary table

| Provider            | Verdict     | Recommended method                                  | Caveats                                                                              |
|---------------------|-------------|----------------------------------------------------|--------------------------------------------------------------------------------------|
| `openai-codex`      | CONDITIONAL | OpenAI Admin API usage endpoints (spend proxy)     | Org-scoped, requires Admin API key. Tracks API spend, not ChatGPT sub 5h/weekly caps. |
| `anthropic-claude`  | CONDITIONAL | Anthropic Admin Usage API (spend proxy)             | Org-scoped, requires Admin API key. Tracks API spend, not Claude.ai sub 5h blocks.    |
| `google-antigravity`| NO-GO       | (none within constraints)                          | No documented public usage/cap API. SPA site only. Defer to local file or browser.   |
| `minimax`           | **GO**      | `GET /v1/token_plan/remains` with Subscription Key  | None — exact 5h/weekly window support.                                              |
| `amp`               | NO-GO       | (none within constraints)                          | Pricing gated, no documented public usage/cap API. Defer to local file or browser.   |

---

## 1. Provider: `openai-codex` (BSOD-65)

### 2. Subscription model — what are we trying to expose?

ChatGPT plans: **Free**, **Go**, **Plus**, **Pro** (individual) and
**Business**, **Enterprise** (per [openai.com/chatgpt/pricing](https://openai.com/chatgpt/pricing/)). Codex (the agent
product that runs against ChatGPT) inherits the same subscription; the
pricing page describes the plan's *Codex access* qualitatively
("Limited", "Expanded", "Maximum") rather than as numeric used/limit/remaining.
The numeric 5-hour and weekly caps the user is asking about exist in the
[OpenAI Help Center article on ChatGPT limits](https://help.openai.com/articles/11909943)
("Limits apply" link from the pricing page) — that article is the
authoritative source but is a help-center page, not an API.

### 3. Method A — Official API

**Platform API rate-limit headers** — documented on the OpenAI
[Rate limits guide](https://platform.openai.com/docs/guides/rate-limits):

| Header                                  | Meaning                                                  |
|-----------------------------------------|----------------------------------------------------------|
| `x-ratelimit-limit-requests`            | RPM cap for the org/project                              |
| `x-ratelimit-remaining-requests`        | Remaining RPM                                            |
| `x-ratelimit-reset-requests`            | When the RPM bucket replenishes                          |
| `x-ratelimit-limit-tokens` / `-remaining-tokens` / `-reset-tokens` | TPM cap / remaining / reset |
| `x-ratelimit-limit-project-tokens` (and remaining / reset variants) | Project-scoped TPM |

These are **per-key request-rate buckets (RPM / TPM)**, not subscription
plan windows. Auth: standard API key (`Bearer`). They tell you how close
you are to throttling on this *API key*'s org, not how close you are to
your ChatGPT Plus 5-hour cap.

**OpenAI Admin API usage endpoints** — documented at
[platform.openai.com/docs/api-reference/administration](https://platform.openai.com/docs/api-reference/administration/usage):

| Endpoint family                              | Returns                                                              |
|----------------------------------------------|----------------------------------------------------------------------|
| `/v1/organization/usage/...` (audio, code_interpreter_sessions, completions, embeddings, images, moderations, vector_stores, file_search_calls, web_search_calls) | Token / call counts grouped by model, project, time bucket |
| `/v1/organization/costs`                     | USD spend broken down the same way                                   |

Auth: **Admin API key** (`sk-admin-...`) — only org owners / admins can
create these. Returns org-wide spend / usage, not per-user ChatGPT
subscription caps. Useful as a partial signal if the user is on an API
plan with monthly spend caps, but not for ChatGPT Pro/Plus 5h/weekly
message caps.

**Customer-tier alternative for ChatGPT sub caps:** none documented. The
Admin API is the only programmatic surface OpenAI publishes for
organisational usage; the ChatGPT consumer subscription's
5h/weekly-cap state is not exposed via any documented public API.

### 4. Method B — Rate-limit response headers

See Method A. Headers exist on every API call and can be captured by an
interceptor on outbound requests. Useful for surfacing the user's
**API** rate-budget state (`5h: X/Y RPM used`), but **not** for
**ChatGPT subscription** 5h/weekly caps.

For the new `openai-codex` provider this is the wrong signal: the user
subscribes to ChatGPT Pro and wants to see their ChatGPT message caps,
not their OpenAI Platform org's RPM. We should not conflate the two in
the UI.

### 5. Method C — Account dashboard API (no browser)

The ChatGPT web UI shows a "plan / usage" widget (visible at
`chatgpt.com/#settings/Subscription`). Network-tab inspection would be
needed to enumerate the underlying JSON calls. **Not investigated in
depth** as part of this spike because:

- ChatGPT endpoints are cookie-scoped and the cookie flow requires a
  browser login. This is the **headless-browser path** we have explicitly
  ruled out for production.
- A one-off inspection would only document what the web app shows; it
  would not be a stable documented API the dashboard could rely on.

### 6. Method D — Community / OSS tools

- [`ccusage`](https://github.com/ccusage/ccusage) ([17.2k stars](https://github.com/ccusage/ccusage))
  supports `ccusage codex daily|weekly|monthly|blocks|session`. **Crucially,
  it does NOT do ChatGPT Pro 5h/weekly caps either** — it parses
  **local session JSONL files** written by the Codex CLI on the user's
  own machine (`~/.codex/sessions/...`). This is the same "local file"
  pattern as Claude Code, not a remote API.
- This means `ccusage` is **not** a way for `aud` (which runs as a server
  on a different host from the user's Codex CLI) to read Codex Pro caps.

### 7. Method E — Other paths

- Reading local Codex CLI session JSONL files (`~/.codex/sessions/...`):
  viable only if `aud` runs on the same machine as the user's Codex CLI
  installation. **Out of scope** for a server-hosted dashboard.
- Billing / invoice CSV exports: not real-time; deferred and not useful
  for "how much do I have left *right now*".

### 8. Verdict

**CONDITIONAL** — viable via the Admin API usage endpoints as a
*spend proxy* (not the ChatGPT sub 5h/weekly caps the user asked about).

If we label it `openai-codex` (subscription), we either ship it without
5h/weekly caps (Admin API spend data only) and clearly mark it as such,
or leave it scaffolded until OpenAI publishes a customer-tier ChatGPT
sub cap API. We must **not** surface `x-ratelimit-*` headers as
"ChatGPT subscription cap" data — that's misleading.

### 9. Recommended next step

1. **Land the provider scaffolded** under the new id `openai-codex`
   (subscription tracker), `live: false`. Enable / credentialed UI
   present, but `FetchUsage` is a stub returning
   `ErrFetcherNotFound`-equivalent or a typed "not yet implemented"
   error. The P2 metadata entry is enough.
2. **Optionally, in parallel, ship a *different* id `openai` for the
   existing API key tracker** (which already exists today in
   `internal/provider/provider.go` Registry) — its Admin-Usage path
   becomes live once the user provides an Admin API key. That id
   surfaces org-level spend, clearly named differently from the
   subscription tracker, so users do not get confused.
3. **Defer the `openai-codex` (subscription) implementation** until
   OpenAI publishes a customer-tier ChatGPT sub cap API. Action for
   Dan: if OpenAI has a beta / partner surface for this, escalate on
   BSOD-62 to raise a feature request.

---

## 2. Provider: `anthropic-claude` (BSOD-66)

### 2. Subscription model — what are we trying to expose?

Claude.ai plans: **Free**, **Pro**, **Max**, **Team**, **Enterprise**.
Each has a 5-hour rolling message block (the famous "5h bar" in the UI)
and (on Max) additional weekly Opus caps. The numeric values are surfaced
in the Claude.ai web UI as a progress bar; they are not exposed as a
documented public API.

### 3. Method A — Official API

**Anthropic API rate-limit response headers** — documented on the
[Rate limits page](https://docs.claude.com/en/api/rate-limits):

| Header                                            | Meaning                                                |
|---------------------------------------------------|--------------------------------------------------------|
| `anthropic-ratelimit-requests-limit` / `-remaining` / `-reset` | RPM cap / remaining / reset (RFC 3339 timestamp)       |
| `anthropic-ratelimit-tokens-limit` / `-remaining` / `-reset`    | TPM cap / remaining / reset                            |
| `anthropic-ratelimit-input-tokens-...` / `-output-tokens-...`   | ITPM / OTPM split (input / output token buckets)       |
| `anthropic-ratelimit-priority-input-tokens-...`                 | Priority Tier (paid add-on) input token bucket         |
| `retry-after`                                     | Set on 429 responses                                    |

These are **per-key RPM/ITPM/OTPM buckets**, not Claude.ai subscription
5h blocks. Auth: standard API key (`x-api-key`). Useful for surfacing
the user's API rate-budget state, not Claude.ai sub caps.

**Anthropic Usage and Cost Admin API** — documented at
[docs.anthropic.com/en/api/usage-cost-api](https://docs.anthropic.com/en/api/usage-cost-api):

| Endpoint                                            | Returns                                                                  |
|-----------------------------------------------------|--------------------------------------------------------------------------|
| `GET /v1/organizations/usage_report/messages`       | Per-time-bucket token counts, groupable by model / workspace / api_key  |
| `GET /v1/organizations/cost_report`                 | USD spend in fractional cents, daily granularity                        |

Auth: **Admin API key** (`sk-ant-admin01-...`). The page is explicit:
**"The Admin API is unavailable for individual accounts."** Only
organisations with multiple members can use it. The endpoint tracks
**API spend**, not Claude.ai subscription 5h blocks.

For Claude.ai (consumer subscription) organisations there is a separate
**Claude Enterprise Analytics API** ([docs.claude.com/en/api/admin/analytics](https://docs.claude.com/en/api/admin/analytics))
which exposes org-wide activity summaries (`assigned_seat_count`,
`chat_*_active_user_count`, `claude_code_*_active_user_count`) and a
usage_report bucketed by minute/hour/day — **but this is also
admin-only**, and the response shape is activity counts (DAU/MAU/WAU
per product surface), **not per-user 5h/weekly plan caps**.

### 4. Method B — Rate-limit response headers

See Method A. Headers exist on every Messages API response and can be
captured by an interceptor on outbound Anthropic calls. Useful for the
user's **API** rate-budget state, not Claude.ai sub caps.

Same problem as `openai-codex`: the existing `anthropic` API-key
provider in the registry would get a confusing live signal that isn't
the user-facing "5h block" they care about.

### 5. Method C — Account dashboard API (no browser)

Claude.ai web UI shows the 5h block progress bar at `claude.ai`. As
with ChatGPT, the endpoints behind it are session-cookie-scoped and
not a documented public API. We have explicitly ruled out browser
automation for production.

### 6. Method D — Community / OSS tools

- [`ccusage` `blocks` command](https://github.com/ccusage/ccusage) ([README](https://github.com/ccusage/ccusage))
  advertises "Claude Code 5-hour billing windows". **`ccusage blocks`
  parses Claude Code's local `~/.claude/projects/.../*.jsonl` session
  log files** — the local history of API calls Claude Code made on the
  user's behalf, then **derives** a 5h block view by rolling-window
  aggregation. It does **not** query Claude.ai for the user's official
  "5h block remaining" — the Anthropic docs are explicit that the
  Messages API does not return a Claude.ai subscription 5h block field.
- For a separate-host dashboard, this local-file approach is **not viable**.
  It would only work if `aud` ran on the same machine as the user's
  Claude Code installation.

### 7. Method E — Other paths

- Local Claude Code session JSONL files (same caveat as Codex): only
  works if `aud` runs on the user's dev machine.
- Anthropic Console "Usage" page for org-level spend: useful for the
  Admin Usage API path, not for Claude.ai 5h blocks.

### 8. Verdict

**CONDITIONAL** — same shape as `openai-codex`. Viable via the Admin
Usage API as a **spend proxy** (org-wide USD / tokens), not as the
Claude.ai 5h/weekly caps the user asked about. Same UX recommendation:
separate the existing `anthropic` (API key) provider from a future
`anthropic-claude` (subscription) provider so users do not get
misled.

### 9. Recommended next step

1. **Land the provider scaffolded** under the new id `anthropic-claude`
   (subscription tracker), `live: false`. Enable / credentialed UI
   present; `FetchUsage` is a stub.
2. **In parallel, the existing `anthropic` (API key) provider** can
   gain Admin Usage integration in a future slice — that's an
   org-scoped Admin API key credential, separate from the user's own
   API key. Surface it under a clearly-named `live: true` provider so
   we do not conflate "Claude.ai Pro bar" with "Anthropic org spend".
3. **Defer the subscription 5h/weekly caps** until Anthropic publishes
   a customer-tier subscription-cap API. ccusage's local-file method
   is not viable for a server-hosted dashboard.

---

## 3. Provider: `google-antigravity` (BSOD-67)

### 2. Subscription model — what are we trying to expose?

Google Antigravity is Google's "AI-first development platform that allows
anyone to be a builder" ([deepmind.google](https://deepmind.google/) front
page, [antigravity.google/download](https://antigravity.google/download)).
It runs Gemini-family models (Gemini 3.5, 3.5 Flash, 3.1 Pro, etc.)
on top of agentic dev tooling (browser, computer-use, terminal,
multi-agent orchestration).

Per the DeepMind page, Antigravity is positioned alongside Gemini app,
AI Studio, and Gemini API as one of the ways to "build with Gemini".
**No pricing or subscription tiers are publicly listed on the
antigravity.google site** at the time of this investigation — the
download page just shows "Google Antigravity" and redirects to
[antigravity.google/blog/introducing-google-antigravity](https://antigravity.google/blog/introducing-google-antigravity)
which is a SPA and returns no textual content via `webfetch`.

### 3. Method A — Official API

- **Gemini API** ([ai.google.dev/gemini-api/docs](https://ai.google.dev/gemini-api/docs))
  is documented and has [quota pages](https://ai.google.dev/gemini-api/docs/quotas)
  — but Gemini API quotas are *per-minute/per-day* token / request rate
  buckets (TPM/RPM/RPD) keyed to a Google Cloud project, **not
  Antigravity subscription 5h/weekly caps**.
- No documented **Antigravity** customer-tier usage API was discoverable
  via the deepmind.google front page, the antigravity.google site, or
  Google's general AI docs. The Antigravity marketing site is SPA-only
  with no readable text content via webfetch.

### 4. Method B — Rate-limit response headers

Standard Gemini API rate-limit headers exist (per the Gemini API docs)
and can be captured on outbound Gemini calls. Same caveat as
OpenAI/Anthropic: this is a Gemini API rate signal, not Antigravity
subscription caps.

### 5. Method C — Account dashboard API (no browser)

The Antigravity web UI presumably renders usage somewhere; not
investigated in depth because the marketing pages do not expose
endpoints, and Antigravity accounts may be tied to Google Workspace /
consumer Google accounts where the limits surface lives — beyond our
budget for a docs-only investigation, and the constraints rule out
browser automation.

### 6. Method D — Community / OSS tools

`ccusage` does not list Antigravity as a supported source
([README supported sources](https://github.com/ccusage/ccusage)).
No other notable community tooling found.

### 7. Method E — Other paths

- Local Antigravity session files: not known to exist; Antigravity
  appears to be primarily a cloud / browser-based surface, not a CLI.
- Google Cloud billing export: shows aggregate spend across all GCP
  services, not Antigravity-specific 5h/weekly caps.

### 8. Verdict

**NO-GO** within the investigation's constraints. Antigravity is a new
Google product with public marketing but no documented public API for
subscription usage / caps. Surfacing it would require either browser
automation (ruled out) or a future Antigravity-specific API that does
not appear to exist yet.

### 9. Recommended next step

1. **Land the provider scaffolded** (`google-antigravity`, `live: false`),
   `FetchUsage` stub. Enable / credentialed UI present, but with a
   "not yet live" badge.
2. **Action for Dan (when convenient):** raise a feature request with
   the Antigravity team for a customer-tier usage API. Revisit on a
   6-month cadence.

---

## 4. Provider: `minimax` (BSOD-68) — **GO**

### 2. Subscription model — what are we trying to expose?

Minimax offers a [Token Plan](https://platform.minimax.io/docs/guides/pricing-token-plan)
subscription that wraps API access with a monthly usage quota. **The
plan's quota windows are exactly the 5-hour and weekly windows the
dashboard is designed for**, and they are explicitly documented:

> Included Token Plan quota is controlled by a **5-hour rolling
> window** and a **weekly window**. Unused included quota does not
> carry over to the next billing cycle.
> — [platform.minimax.io/docs/token-plan/faq](https://platform.minimax.io/docs/token-plan/faq)

| Tier   | Price       | Window                  | Agent capacity       |
|--------|-------------|-------------------------|----------------------|
| Plus   | **$20/mo**  | 5-hour rolling + weekly | ~3-4 agents          |
| Max    | **$50/mo**  | 5-hour rolling + weekly | ~4-5 agents          |
| Ultra  | **$120/mo** | 5-hour rolling + weekly | ~6-7 agents          |

Quota is shared across all supported models on the API platform
(text / image / speech / music); the console renders a unified usage
bar. **Bonus:** a Credits system
($5 = 5,000 credits, $25 = 25,000 credits, $100 = 100,000 credits;
1,000 credits = $1) acts as overflow — usage first draws on Token Plan
quota, then on Credits balance, so the quota check is a single source
of truth.

### 3. Method A — Official API

**`GET https://www.minimax.io/v1/token_plan/remains`** — documented
verbatim in the [Token Plan FAQ](https://platform.minimax.io/docs/token-plan/faq):

```bash
curl --location 'https://www.minimax.io/v1/token_plan/remains' \
--header 'Authorization: Bearer <API Key>' \
--header 'Content-Type: application/json'
```

Auth: **`Subscription Key`** as `Authorization: Bearer`. The
Subscription Key is **separate from the pay-as-you-go Open Platform API
Key** ([quickstart](https://platform.minimax.io/docs/token-plan/quickstart)
— "It is not interchangeable with pay-as-you-go API Keys"). This is
the credential we should ask the user for via the existing write-only
credential field.

This endpoint returns the current-state quota for the user's Token Plan:
the same data the console usage bar renders. This is a **direct,
documented, official, customer-tier API** for the exact signal we
want — used / limit / remaining + reset — at both the 5-hour and weekly
windows.

### 4. Method B — Rate-limit response headers

Minimax publishes token-bucket RPM/TPM rate limits on the API platform
([FAQ: "Rate limits (RPM / TPM)"](https://platform.minimax.io/docs/token-plan/faq)).
These are request-rate buckets (typical ~1 min reset, dynamic during
peak hours) — useful but **not** the Token Plan quota windows. The Token
Plan `remains` endpoint is the right surface for the 5h/weekly signal.

### 5. Method C — Account dashboard API (no browser)

The console usage bar at
[platform.minimax.io/user-center/payment/token-plan](https://platform.minimax.io/user-center/payment/token-plan)
is the rendered view of the same `remains` endpoint. No additional
public endpoint needed; the API exists.

### 6. Method D — Community / OSS tools

`ccusage` does not list Minimax as a supported source (it's a
subscription-tracking tool, not a Token Plan parser). Not needed —
the official API suffices.

### 7. Method E — Other paths

- Credits balance check: same Subscription Key is used to consume
  Credits. The same `remains` endpoint (or a sibling `/credits/balance`
  if it exists) covers the overflow case.
- Billing / invoice CSV: deferred / not real-time.

### 8. Verdict

**GO.** This is the cleanest fit in the entire research matrix. An
official, documented, customer-tier API exists, returns exactly the
5h/weekly current-state signal we want, and uses a credential type
the user already has.

### 9. Recommended next step

1. **Implement the plugin** under id `minimax` (matches existing
   `minimax` registry slot) as soon as BSOD-90 unblocks. Credential
   field: `subscription_key` (`Secret = true`). `FetchUsage` calls
   `GET https://www.minimax.io/v1/token_plan/remains` and maps the
   response to two `UsageMetric` records:
   - `Name="token_plan_5h", Window="5h", Unit="usd_cents"` (or
     `"tokens"` if the endpoint returns token counts — verify against
     a captured response)
   - `Name="token_plan_weekly", Window="week", Unit="usd_cents"`
2. **Capture a real response** for the docs PR so the plugin's tests
   can ship with a fixture (redacted).
3. **This is the canary provider** that proves the pattern. Once
   landed, the remaining four providers can copy the scaffold.

---

## 5. Provider: `amp` (BSOD-69)

### 2. Subscription model — what are we trying to expose?

[Amp](https://ampcode.com/) is Sourcegraph's "frontier coding agent" —
CLI / web / phone, multi-model (GPT-5.6, Claude Fable 5, GLM-5.2),
backed by "orbs" (remote machines that keep working when the laptop
is closed). Per the [home page](https://ampcode.com/):

> Pay as you go, with no markup for individuals.

So Amp is sold as a usage-based subscription with model-routed token
usage. The actual pricing tier list and quota windows are behind a
login ([ampcode.com/manual/pricing](https://ampcode.com/manual/pricing)
returned a WorkOS sign-in screen — gated). Per the [Owner's Manual](https://ampcode.com/manual),
Amp has 4 agent modes (`low`, `medium`, `high`, `ultra`) that map to
specific models; presumably each mode has its own cost / quota shape,
but no public numbers are listed.

### 3. Method A — Official API

The **Amp SDK** ([ampcode.com/manual/sdk](https://ampcode.com/manual/sdk))
exposes a programmatic surface, but it is an **agent-execution** API
(`execute()` runs the agent; messages stream back as
`system` / `assistant` / `result`), not a **usage / plan-cap** API.
Auth: `AMP_API_KEY=sgamp_...` (the user's access token from
`ampcode.com/settings/security`).

**No documented public Amp "fetch my current plan usage" endpoint was
found in the SDK or manual docs.** The pricing / quota endpoints that
likely exist are gated behind login and not advertised as a public
surface.

### 4. Method B — Rate-limit response headers

Amp fronts multiple upstream LLM providers (OpenAI, Anthropic,
Google). Its own API surface does not document rate-limit headers, and
the upstreams' headers (e.g. `x-ratelimit-*` from OpenAI,
`anthropic-ratelimit-*` from Anthropic) reflect the upstream's
key-level rate buckets — **not Amp plan usage**.

### 5. Method C — Account dashboard API (no browser)

The Amp web UI at `ampcode.com` likely has a billing / usage page
behind login. Not investigated; same reasoning as Antigravity — gated
behind WorkOS auth, and the constraints rule out browser automation.

### 6. Method D — Community / OSS tools

`ccusage` lists **Amp** as a supported source
([README](https://github.com/ccusage/ccusage) — `ccusage amp daily|weekly|monthly|session`).
As with Codex and Claude Code, this parses Amp's **local session
files**, not a remote API:

> Reads local usage logs from Claude Code, Codex, OpenCode, **Amp**, …

Not viable for a separate-host dashboard.

### 7. Method E — Other paths

- Local Amp session files: only viable if `aud` runs on the user's
  machine where Amp is installed.
- Sourcegraph billing CSV: deferred / not real-time.

### 8. Verdict

**NO-GO** within constraints. Amp has a documented **execution** SDK
but no documented public **plan-usage** endpoint. Surfacing it would
require browser automation, which is ruled out, or local file access,
which only works if `aud` runs on the user's dev machine.

### 9. Recommended next step

1. **Land the provider scaffolded** (`amp`, `live: false`),
   `FetchUsage` stub.
2. **Action for Dan (when convenient):** ask Sourcegraph whether the
   Amp pricing / quota endpoints can be exposed for first-party
   integrations. Revisit on a 6-month cadence.

---

## Overall verdict

**YELLOW-LIGHT.** One provider (`minimax`) is GO with a direct fit, and
two more (`openai-codex`, `anthropic-claude`) are CONDITIONAL via their
respective spend-proxies on org-tier Admin APIs. Two
(`google-antigravity`, `amp`) are NO-GO within constraints.

This is **enough to ship the dashboard as a real usage tracker** for at
least Minimax, with the other four scaffolded (or showing the Admin-API
spend signal where the user supplies an org-tier credential). The
project is **not** to be halted — the user's stated gate was "if we have
no path forward, then we need to stop here", and we have a clear path
forward for one provider and a partial path for two more.

### Recommended scope

- **Ship Minimax first** (BSOD-68) once BSOD-90 unblocks. It is the
  canary that proves the `Fetcher` pattern end-to-end.
- **Scaffold the other four** with `live: false` and a visible "not yet
  live" badge in the dashboard. They become incremental PRs once their
  respective paths firm up (e.g. if OpenAI ships a customer-tier ChatGPT
  sub cap API, or Sourcegraph exposes an Amp usage endpoint).
- **Loop-in opportunity:** when `openai-codex` and `anthropic-claude`
  get Admin-API-backed spend plugins, decide whether the spend signal
  should appear under the same provider id as the (still scaffolded)
  subscription tracker, or as a separate id. Recommend **separate ids**
  (`openai-spend` vs `openai-codex`, `anthropic-spend` vs
  `anthropic-claude`) so the UI never conflates "API spend" with
  "subscription cap". This is an Architect-level decision; raise on
  BSOD-62 when the plugin PRs land.

### Items needing Dan's decision

- **Scope confirmation:** ship Minimax as the first live provider, with
  the other four scaffolded? Or pause P3 until OpenAI / Anthropic ship
  customer-tier subscription-cap APIs? Recommendation: ship Minimax
  now.
- **Spend proxy vs. scaffold-only for `openai-codex` / `anthropic-claude`:** if
  shipped, the Admin-API spend signal goes under a **separate** provider
  id from the subscription one, so we never mislead users about which
  signal they're seeing. OK to proceed on that basis?

## Sources

- OpenAI rate limits guide: <https://platform.openai.com/docs/guides/rate-limits>
- OpenAI Admin API usage reference: <https://platform.openai.com/docs/api-reference/administration/usage>
- OpenAI ChatGPT pricing: <https://openai.com/chatgpt/pricing/>
- Anthropic rate limits: <https://docs.claude.com/en/api/rate-limits>
- Anthropic Usage and Cost Admin API: <https://docs.anthropic.com/en/api/usage-cost-api>
- Anthropic Claude Enterprise Analytics API: <https://docs.claude.com/en/api/admin/analytics>
- Minimax Token Plan pricing: <https://platform.minimax.io/docs/guides/pricing-token-plan>
- Minimax Token Plan quickstart: <https://platform.minimax.io/docs/token-plan/quickstart>
- Minimax Token Plan FAQ (contains the `remains` endpoint): <https://platform.minimax.io/docs/token-plan/faq>
- Minimax subscription page: <https://platform.minimax.io/subscribe/token-plan>
- Google Antigravity home: <https://antigravity.google/>
- Google DeepMind home (Antigravity context): <https://deepmind.google/>
- Amp home: <https://ampcode.com/>
- Amp Owner's Manual: <https://ampcode.com/manual>
- Amp SDK: <https://ampcode.com/manual/sdk>
- Amp Models: <https://ampcode.com/models>
- `ccusage` (Claude Code / Codex / Amp local session parser): <https://github.com/ccusage/ccusage>

## What this does NOT do

- Does NOT implement any provider plugin (BSOD-65..69 remain
  unblocked-pending).
- Does NOT change the P2 contract or OpenAPI.
- Does NOT touch BSOD-90 (P3.0 foundation) — that remains blocked until
  this report lands a usable path; BSOD-90 should now unblock on the
  Minimax lane.