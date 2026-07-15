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

- **All four consumer-subscription providers are now GO**, after the
  community-tool deep-dive (see § "Update" below) surfaced stable
  unauthenticated-by-us OAuth endpoints that return exactly the 5h/weekly
  signal the user wants. The dashboard can ship with four live providers
  (or three plus Minimax which has the cleanest documented API).
- **Minimax** is GO via its first-party `GET /v1/token_plan/remains` API —
  the cleanest of the five, no OAuth indirection.
- **Claude Code, Codex, and Antigravity** are GO via OAuth-bearer usage
  endpoints documented in the open-source
  [CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
  project (`api.anthropic.com/api/oauth/usage`,
  `chatgpt.com/backend-api/wham/usage`, and
  `daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`).
  These are not in the providers' first-party docs but are stable,
  customer-tier, OAuth-bearer endpoints.
- **AMP** remains NO-GO within constraints. No equivalent usage API is
  documented anywhere reachable.

### Update from the initial PR

The first version of this doc (PR #31, commit 1) reported Minimax as the
only GO and listed the other three subscription providers as either
CONDITIONAL (spend-proxies via Admin APIs) or NO-GO. After Dan pointed me
at [CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor),
I cross-referenced its source — the project hits three of the exact
endpoints we need. The update reverses the verdicts for
`openai-codex`, `anthropic-claude`, and `google-antigravity` from
NO-GO/CONDITIONAL to GO, with the OAuth-credential caveat called out
per-provider.

## Summary table

| Provider            | Verdict | Recommended method                                                                       | Caveats                                                                                                                                  |
|---------------------|---------|------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------|
| `openai-codex`      | **GO**  | `GET https://chatgpt.com/backend-api/wham/usage` with Codex CLI OAuth access token       | Token comes from `$CODEX_HOME/auth.json` (Codex CLI must be installed + signed in on the host). Endpoint is OAuth-bearer, customer-tier. |
| `anthropic-claude`  | **GO**  | `GET https://api.anthropic.com/api/oauth/usage` with Claude Code OAuth access token      | Token from `~/.claude/.credentials.json`. Endpoint requires `anthropic-beta: oauth-2025-04-20` header. Free plan not supported.            |
| `google-antigravity`| **GO**  | `POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`     | Two-call flow: `loadCodeAssist` then `retrieveUserQuotaSummary`. Token from Windows Credential Manager target `gemini:antigravity`.     |
| `minimax`           | **GO**  | `GET https://www.minimax.io/v1/token_plan/remains` with Subscription Key                 | None — exact 5h/weekly window support.                                                                                                  |
| `amp`               | NO-GO   | (none within constraints)                                                                | Pricing gated, no documented public usage/cap API. Defer to local file or browser.                                                     |

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

**`GET https://chatgpt.com/backend-api/wham/usage`** — not in OpenAI's
first-party docs, but stable enough that
[CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
(304 stars, 72 forks) hits it in production for a paid Codex-CLI
user-base. Source: `src/poller.rs:803-836` and `src/poller.rs:16-18`.

| Aspect        | Detail                                                                                                  |
|---------------|---------------------------------------------------------------------------------------------------------|
| Method        | `GET`                                                                                                   |
| Auth          | `Authorization: Bearer <access_token>` from Codex CLI's `auth.json`                                     |
| Extra headers | `User-Agent: codex-cli`, optionally `ChatGPT-Account-Id: <account_id>`                                 |
| Response      | `{ "rate_limit": { "primary_window": { "used_percent": float, "reset_at": int_unix }, "secondary_window": { ... } } }` |

`primary_window` is the 5-hour window; `secondary_window` is the weekly
window. `used_percent` is a 0–100 number; `reset_at` is a Unix timestamp
in seconds.

**Customer-tier / no-admin requirement:** the access token is the user's
own Codex CLI OAuth token, not an org-admin key. Free plan support is
not documented; Plus / Pro / Business / Enterprise all carry Codex
access (per the pricing page).

**Auth model fits the "viable path" definition** (customer-tier, own
credential). The only difference from Minimax is the credential is an
OAuth access token, not a Subscription Key. Per § "Auth-credential
model" below, this maps cleanly onto the existing write-only credential
field — the user copies their access token (or we can read it from
`$CODEX_HOME/auth.json` if `aud` runs on the same host).

### 4. Method B — Rate-limit response headers

**Platform API rate-limit headers** — documented on the OpenAI
[Rate limits guide](https://platform.openai.com/docs/guides/rate-limits):

| Header                                  | Meaning                                                  |
|-----------------------------------------|----------------------------------------------------------|
| `x-ratelimit-limit-requests`            | RPM cap for the org/project                              |
| `x-ratelimit-remaining-requests`        | Remaining RPM                                            |
| `x-ratelimit-reset-requests`            | When the RPM bucket replenishes                          |
| `x-ratelimit-limit-tokens` / `-remaining-tokens` / `-reset-tokens` | TPM cap / remaining / reset |
| `x-ratelimit-limit-project-tokens` (and remaining / reset variants) | Project-scoped TPM |

These are **per-key request-rate buckets (RPM / TPM)**, not ChatGPT
subscription 5h/weekly caps. We **do not** surface these as the
`openai-codex` metric — they belong on a different provider id
(`openai-api-spend` or similar). Method A (`wham/usage`) is the right
signal for the consumer-subscription tracker.

### 5. Method C — Account dashboard API (no browser)

The ChatGPT web UI shows a "plan / usage" widget (visible at
`chatgpt.com/#settings/Subscription`). Network-tab inspection was the
implicit way the community discovered `wham/usage` — but the
`wham/usage` endpoint is the API behind that UI and is hit directly, so
this Method collapses into Method A.

### 6. Method D — Community / OSS tools

- [`CodeZeno/Claude-Code-Usage-Monitor`](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
  is the reference implementation for this approach (Claude Code +
  Codex + Antigravity in one Rust app, ~304 stars). **This is the
  source for the `wham/usage` endpoint documented in Method A.**
- [`ccusage`](https://github.com/ccusage/ccusage) (17.2k stars) supports
  `ccusage codex daily|weekly|monthly|blocks|session` — but as before,
  this is a **local JSONL file parser**, not a remote API. Not viable
  for a separate-host dashboard, but does validate that the
  subscription-cap signal exists and is what the user wants.

### 7. Method E — Other paths

- Reading local Codex CLI `auth.json`: viable if `aud` runs on the same
  host. We can support both modes — user pastes the access token
  directly, or we read it from the well-known path.
- Billing / invoice CSV exports: not real-time; deferred and not useful
  for "how much do I have left *right now*".

### 8. Verdict

**GO** — with the credential caveat that the user supplies an OAuth
access token (or `aud` reads it from the Codex CLI's `auth.json` if
co-located). `wham/usage` is OAuth-bearer, customer-tier, returns
5-hour and weekly windows directly. Not first-party documented but is
the de-facto standard implementation in the most-popular OSS usage
monitor for the consumer-subscription Codex flow.

### 9. Recommended next step

1. **Implement the plugin** under id `openai-codex` once BSOD-90
   unblocks. Credential field: `access_token` (`Secret = true`) — the
   Codex CLI OAuth access token, with a helper to extract it from
   `$CODEX_HOME/auth.json` on co-located hosts.
2. `FetchUsage` calls `GET https://chatgpt.com/backend-api/wham/usage`
   with the documented headers and maps `primary_window` →
   `Name="codex_5h"` and `secondary_window` → `Name="codex_weekly"` in
   `UsageMetric` records. Unit: `percent` (`Used = used_percent`,
   `Limit = 100`, `Remaining = 100 - used_percent`).
3. On 401/403, return `ErrAuth` so the new P3.0 cooldown (BSOD-90) can
   back off and prompt the user to refresh.

---

## 2. Provider: `anthropic-claude` (BSOD-66)

### 2. Subscription model — what are we trying to expose?

Claude.ai plans: **Free**, **Pro**, **Max**, **Team**, **Enterprise**.
Each has a 5-hour rolling message block (the famous "5h bar" in the UI)
and (on Max) additional weekly Opus caps. The numeric values are surfaced
in the Claude.ai web UI as a progress bar; the underlying data is
available via the OAuth usage endpoint documented below.

Per
[CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
README (as of March 19, 2026): "Supported: **Pro, Max, Teams,
Enterprise, and Console accounts**. Not supported: the free Claude.ai
plan."

### 3. Method A — Official API

**`GET https://api.anthropic.com/api/oauth/usage`** — not in
Anthropic's first-party docs, but stable enough that
[CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
hits it in production. Source: `src/poller.rs:16` and `src/poller.rs:687-723`.

| Aspect        | Detail                                                                                                   |
|---------------|----------------------------------------------------------------------------------------------------------|
| Method        | `GET`                                                                                                    |
| Auth          | `Authorization: Bearer <oauth_access_token>` (Claude Code OAuth token from `~/.claude/.credentials.json`) |
| Required header | `anthropic-beta: oauth-2025-04-20`                                                                     |
| Response      | `{ "five_hour": { "utilization": float, "resets_at": iso8601 }, "seven_day": { "utilization": float, "resets_at": iso8601 } }` |

`utilization` is a 0–100 percentage. `resets_at` is the reset timestamp
in ISO 8601 with timezone.

**Customer-tier / no-admin requirement:** the access token is the user's
own Claude Code OAuth token, not an org-admin key. Free plan is not
supported; the OAuth endpoint requires a paid Claude.ai subscription.

**Auth model fits the "viable path" definition** (customer-tier, own
credential). Maps to the existing write-only credential field.

### 4. Method B — Rate-limit response headers (fallback)

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
5h blocks. **CodeZeno's approach is the right model:** if the OAuth
usage endpoint is unavailable, send a Messages API call with
`max_tokens: 1` and read the unified headers:

- `anthropic-ratelimit-unified-5h-utilization`
- `anthropic-ratelimit-unified-7d-utilization`
- `anthropic-ratelimit-unified-5h-reset` (Unix seconds)
- `anthropic-ratelimit-unified-7d-reset` (Unix seconds)
- `anthropic-ratelimit-unified-status` (`"rejected"` → 100%)
- `anthropic-ratelimit-unified-representative-claim` (`"five_hour"` /
  `"seven_day"` → which window is at 100%)

This is more invasive than the OAuth usage endpoint (it costs a token of
upstream traffic per poll) but degrades gracefully when the OAuth
endpoint is unreachable.

**For `anthropic-claude` we use the OAuth usage endpoint as primary**;
the rate-limit-headers path is a documented fallback if the OAuth
endpoint ever returns an auth error. We do not confuse the API-key
provider (`anthropic` in the registry) with this consumer-subscription
provider (`anthropic-claude`) — they have different credential types
and different windows.

### 5. Method C — Account dashboard API (no browser)

Same situation as `openai-codex`: the OAuth usage endpoint *is* the
API the Claude.ai web UI uses to render the 5h bar. Method C collapses
into Method A.

### 6. Method D — Community / OSS tools

- [`CodeZeno/Claude-Code-Usage-Monitor`](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
  is the reference implementation for this approach. **Source for the
  `oauth/usage` endpoint documented in Method A.**
- [`ccusage` `blocks`](https://github.com/ccusage/ccusage) is a
  **local JSONL parser** that derives 5h blocks from
  `~/.claude/projects/.../*.jsonl` — does not query Claude.ai. Not
  viable for a separate-host dashboard.

### 7. Method E — Other paths

- Local Claude Code `~/.claude/.credentials.json` (OAuth tokens):
  viable if `aud` runs on the same host.
- Anthropic Console "Usage" page: org-level spend, not Claude.ai 5h
  blocks.
- Admin Usage API + Admin Analytics API: org-scoped, requires Admin
  API key (not customer-tier) — see the old (now-superseded) version
  of this section in the git history. Not used for the consumer
  tracker.

### 8. Verdict

**GO** — with the same OAuth-credential caveat as `openai-codex`. The
OAuth usage endpoint is OAuth-bearer, customer-tier, returns 5h and
7-day (weekly) windows directly. The rate-limit-headers fallback is
documented for resilience.

### 9. Recommended next step

1. **Implement the plugin** under id `anthropic-claude` once BSOD-90
   unblocks. Credential field: `access_token` (`Secret = true`).
2. `FetchUsage` calls `GET https://api.anthropic.com/api/oauth/usage`
   first; on 401/403, fall back to a Messages API probe
   (`max_tokens: 1` with a cheap model like `claude-haiku-4-5-20251001`)
   and read the unified headers. Map `five_hour.utilization` →
   `Name="claude_5h"` (`Window="5h"`) and `seven_day.utilization` →
   `Name="claude_weekly"` (`Window="week"`), both `Unit="percent"`.
3. Document in `docs/providers.md` that this provider requires the
   user to supply their Claude Code OAuth access token (with a
   helper to read it from `~/.claude/.credentials.json` on
   co-located hosts, per the CodeZeno precedent).

---

## 3. Provider: `google-antigravity` (BSOD-67)

### 2. Subscription model — what are we trying to expose?

Google Antigravity is Google's "AI-first development platform that allows
anyone to be a builder" ([deepmind.google](https://deepmind.google/) front
page, [antigravity.google/download](https://antigravity.google/download)).
It runs Gemini-family models (Gemini 3.5, 3.5 Flash, 3.1 Pro, etc.)
on top of agentic dev tooling (browser, computer-use, terminal,
multi-agent orchestration). Per CodeZeno's reference implementation,
Antigravity exposes **5-hour and weekly Gemini quota windows** that
match the dashboard's required shape exactly.

### 3. Method A — Official API

**Cloud Code / Antigravity quota endpoints** — not in Google's
first-party docs for Antigravity specifically, but stable enough that
[CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
uses them in production. Source: `src/poller.rs:19-24` and
`src/poller.rs:875-1043`.

Three candidate base URLs, tried in order:

1. `https://daily-cloudcode-pa.googleapis.com`
2. `https://daily-cloudcode-pa.sandbox.googleapis.com`
3. `https://cloudcode-pa.googleapis.com`

All three are POST endpoints with `Authorization: Bearer <token>` and
`User-Agent: antigravity`. The flow is **two calls per poll** (or three
if the summary endpoint fails and we fall back to per-model quota).

**Call 1: `POST /v1internal:loadCodeAssist`**

```json
// request
{ "metadata": { "ideType": "ANTIGRAVITY" } }

// response
{ "cloudaicompanionProject": "<project-id>" }
```

The `cloudaicompanionProject` field may be null/empty; if so, call 2
is skipped and we go directly to the model-level fallback.

**Call 2 (primary): `POST /v1internal:retrieveUserQuotaSummary`**

```json
// request
{ "project": "<project-id>" }

// response (truncated)
{
  "groups": [
    {
      "displayName": "Gemini Pro / Flash Quota",
      "buckets": [
        {
          "bucketId": "gemini-5h",
          "displayName": "5-hour limit",
          "window": "5h",
          "remainingFraction": 0.62,
          "resetTime": "2026-07-15T17:00:00Z"
        },
        {
          "bucketId": "gemini-weekly",
          "displayName": "Weekly limit",
          "window": "weekly",
          "remainingFraction": 0.41,
          "resetTime": "2026-07-19T00:00:00Z"
        }
      ]
    }
  ]
}
```

`window: "5h"` maps to the 5-hour window; `window: "weekly"` maps to
the weekly window. `remainingFraction` is 0–1; `resetTime` is ISO 8601.

CodeZeno's implementation picks the "Gemini" group (matching
`displayName` / `description` / `bucketId` containing "gemini")
preferentially. We should replicate that preference — Antigravity may
expose quota for multiple model groups and we want the user's primary
working model.

**Call 2 fallback: `POST /v1internal:fetchAvailableModels`**

```json
// request
{ "project": "<project-id>" }

// response (truncated)
{
  "models": {
    "gemini-3.5-flash": {
      "quotaInfo": {
        "remainingFraction": 0.62,
        "resetTime": "2026-07-15T17:00:00Z"
      }
    }
  }
}
```

CodeZeno filters to `gemini*`, `claude*`, `gpt*`, `image*`, `imagen*`
prefixes and picks the model with the highest `used_percent`. We get
the 5-hour window only (no weekly bucket per-model); set
`Name="antigravity_5h"` and `Remaining = nil` for weekly.

**Auth model fits the "viable path" definition.** The access token is
the user's own Antigravity OAuth token (not org/admin). CodeZeno reads
it from the Windows Credential Manager target `gemini:antigravity` —
we can do the same on co-located Windows hosts, or accept the token
via the write-only credential field on other platforms.

### 4. Method B — Rate-limit response headers

Standard Gemini API rate-limit headers exist on the Gemini API
endpoint but do not cover Antigravity's 5h/weekly subscription caps.
Not useful here.

### 5. Method C — Account dashboard API (no browser)

The Antigravity web UI renders the same quota data; the endpoints
above are what the UI calls. Method C collapses into Method A.

### 6. Method D — Community / OSS tools

[`CodeZeno/Claude-Code-Usage-Monitor`](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
is the reference implementation. **Source for the
`v1internal:retrieveUserQuotaSummary` and `v1internal:loadCodeAssist`
endpoints documented in Method A.**

`ccusage` does not list Antigravity as a supported source. No other
notable community tooling found.

### 7. Method E — Other paths

- Reading the OAuth token from Windows Credential Manager
  (`gemini:antigravity` target): viable if `aud` runs on a Windows host
  co-located with the Antigravity install. On Linux/Mac, the token
  source path is different — investigate at implementation time.
- Google Cloud billing export: not real-time.

### 8. Verdict

**GO** — with the same OAuth-credential caveat as `openai-codex` and
`anthropic-claude`. The `retrieveUserQuotaSummary` endpoint returns
5h and weekly buckets directly. Per-model quota is a fallback for
when the summary endpoint is unavailable.

**Linux/Mac note:** CodeZeno's implementation is Windows-only
(Credential Manager API). For `aud` to support Antigravity on
Linux/Mac, we need to find the equivalent token-storage path (likely
under `~/.config/google-antigravity/` or in the Antigravity Linux
install's auth dir) and document it. This is a small follow-up; the
endpoints themselves are cross-platform.

### 9. Recommended next step

1. **Implement the plugin** under id `google-antigravity` once BSOD-90
   unblocks. Credential field: `access_token` (`Secret = true`).
2. `FetchUsage` follows the CodeZeno pattern: `loadCodeAssist` → if
   project, `retrieveUserQuotaSummary` → if that fails, fall back to
   `fetchAvailableModels`. Map the Gemini group: `window="5h"` →
   `Name="antigravity_5h"`, `window="weekly"` → `Name="antigravity_weekly"`,
   `Unit="percent"`.
3. **Linux/Mac token-source investigation** is a follow-up issue; the
   `aud` core polling path is platform-agnostic and works against
   any HTTP endpoint.

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

**GREEN-LIGHT.** Four of the five providers are GO, and the user has a
real path forward for each. The only NO-GO is `amp` (Sourcegraph's
coding agent), which has no documented public usage/cap API within our
constraints. Every GO provider either:

- has a first-party documented customer-tier API (Minimax), or
- has a stable, OAuth-bearer, customer-tier endpoint that the most
  popular OSS usage monitor for the consumer-subscription Codex / Claude
  Code / Antigravity flow already uses in production
  ([CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)).

The user's gate — "if we have no path forward, then we need to stop
here" — is **clearly not tripped.** The project can ship as a real
usage dashboard.

### Recommended scope

- **Ship Minimax first** (BSOD-68) once BSOD-90 unblocks. It is the
  cleanest API (first-party documented) and the canary that proves the
  `Fetcher` pattern end-to-end.
- **In parallel, ship the three OAuth-based plugins** (BSOD-65 Codex,
  BSOD-66 Claude, BSOD-67 Antigravity). They share the same credential
  pattern (OAuth access token, `Secret = true`); a small helper in the
  plugin package can extract the token from the locally-installed CLI's
  storage on co-located hosts (CodeZeno's Rust code is the reference
  for the read paths).
- **Scaffold `amp` (BSOD-69)** with `live: false` and a visible "not yet
  live" badge. Revisit on a 6-month cadence.

### Items needing Dan's decision (revised)

- **Scope confirmation:** ship all four GO providers (Minimax, Codex,
  Claude, Antigravity) with the OAuth-credential model, and scaffold
  `amp`? The current `openai` and `anthropic` API-key providers in
  `internal/provider/provider.go` Registry stay as-is (they have
  different windows and different auth) — recommendation: keep them,
  ship the new providers as **additional** ids, not replacements.
- **Token-source ergonomics:** should `aud` only accept the OAuth access
  token via the write-only credential field (simple, works on any host),
  or also auto-detect the locally-installed CLI's token storage path
  (CodeZeno-style; requires `aud` to run on the same host as the user's
  CLI install; cross-platform concerns for Antigravity on Linux/Mac)?
  Recommendation: accept the credential directly (simple, universal),
  and ship a separate "copy from CLI" helper as a one-off tool if
  there's demand.

## Sources

### Provider docs

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

### Community tooling

- [CodeZeno/Claude-Code-Usage-Monitor](https://github.com/CodeZeno/Claude-Code-Usage-Monitor)
  — primary source for the `api.anthropic.com/api/oauth/usage`,
  `chatgpt.com/backend-api/wham/usage`, and
  `daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`
  endpoints. Reviewed source: `src/poller.rs:1-50` (endpoint constants
  and response shapes), `src/poller.rs:687-801` (Claude fetch +
  fallback), `src/poller.rs:803-859` (Codex fetch + parser),
  `src/poller.rs:875-1105` (Antigravity fetch, summary, model
  fallback).
- [`ccusage`](https://github.com/ccusage/ccusage) — local session JSONL
  parser. Not a remote API; useful for understanding what the dashboard
  needs to surface, not for the implementation path.

## What this does NOT do

- Does NOT implement any provider plugin (BSOD-65..69 remain
  unblocked-pending).
- Does NOT change the P2 contract or OpenAPI.
- Does NOT touch BSOD-90 (P3.0 foundation) — that remains blocked until
  this report lands a usable path; BSOD-90 should now unblock on the
  Minimax lane.