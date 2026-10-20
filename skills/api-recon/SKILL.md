---
name: api-recon
description: Use this skill when collecting a website's API endpoints.
---

# API Recon (Frontend Endpoint Reconnaissance)

With **authorization**, discover as completely as possible: **backend APIs** (paths, methods, parameters, response bodies), **frontend routes**, and **UI feature triggers** (tabs, dialogs, table actions, etc.).

---

## Boundaries and Prohibitions (Required Reading for Agents)

This skill covers **API and parameter surface reconnaissance only**. It does not cover vulnerability research or exploitation.

### Task boundaries

| Area | Allowed | Prohibited |
|---|---|---|
| **Target** | Enumerate paths, methods, parameters, routes, and UI triggers | SQLi/XSS, authorization bypass, brute force, vulnerability fuzzing, modified-request attacks, destructive actions |
| **Authentication** | Use hooks and stubs/mocks to bypass **client-side** login gates | Ask the user for credentials or guess them; submit a real login form |
| **Runtime** | Hook APIs without credentials and use mock responses to let the SPA render its post-login shell | Workflows that require a real backend session to continue |

### Credential-free dynamic analysis (Phase 3 default)

1. Use `preload.js` / `runtime_harvest.js` to **intercept and stub** bootstrap APIs for login, permissions, menus, and similar data.
2. Return mock bodies for business-query APIs with the **correct structure, a successful business code, and optionally empty data**.
3. Let the frontend render post-login pages even without a backend or when it returns 401, so it triggers more XHR/fetch/WebSocket requests.
4. **Empty data, empty tables, and placeholder UI are expected.** Do not switch to real login or vulnerability testing because of them.

**In short:** Use mocks to expose frontend routes and mount components, then **record only outbound requests**. What the backend returns is less important than **which additional endpoints the frontend calls**.

### Strict process prohibitions

| Prohibited | Alternative |
|---|---|
| Before Phase 1 is complete, grep/curl/Read the main entry `index-*.js` to extract API paths | Run `OUTDIR/harvest_static.py` |
| Write an `extract_apis.py` or other script to replace harvest | Modify `OUTDIR/harvest_static.py`, then rerun it |
| Repeat the same grep/command after it has failed twice or more | Change strategy: read tool logs, modify harvest, or consult the reference |
| Skip Gate A/B and run the original files in `scripts/` directly | Copy them to OUTDIR and adapt them to the target |
| Use real usernames/passwords, OTP, OAuth, or other real authentication | Use stubs/mocks as described above |
| Skip stubs to obtain real data or conduct authorization-bypass/injection testing | Record only outbound requests within the recon boundary |
| Delete or export sensitive data, perform bulk writes, or take other irreversible actions | Apply the same restriction to coverage clicks |
| Claim all pages and endpoints were found without runtime analysis and dynamic enumeration | Follow the completion criteria or state the limitations |
| Claim all parameters were found without the parameter trigger matrix and diff | Complete the Phase 3b matrix and Phase 5 diff |
| Infer required/optional status from a single runtime sample | Diff multiple samples or infer from validation rules/errors |

---

## Two-Layer Model and Runtime Modes

| Layer | Output | Limitations |
|---|---|---|
| **Static** (JS bundle) | Full endpoint path list, draft routes, candidate request-building fields | No HTTP methods; parameters require Phase 1b; misses URLs assembled at runtime |
| **Runtime** (live session) | Methods, bodies, responses, dynamic URLs, WS/SSE; multi-sample diffs complete parameter coverage | A page must actually render to issue requests; one sample cannot establish required/optional status |

| Runtime mode | Engine | Use case |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, methods/parameters/response bodies, WS/SSE, reproducible batch runs |
| **coverage** | Browser + `preload.js` | Click tabs, dialogs, and tables for deeper feature coverage |
| **both** | depth, then coverage | Most complete, but takes the longest |

**Parameter method** (no universal script): use harvest/regex for paths; use **anchor-based context expansion, the UI binding chain, multi-sample diffs, and error-based inference** for parameters (grep recipes in [reference.md](reference.md), section J).

---

## Completion Criteria

Only claim recon is complete when all applicable conditions are met:

- [ ] **Static:** Phase 1 harvest produced `api_static.txt`, `routes.txt`, and `js/`.
- [ ] **Runtime:** At least one of depth or coverage ran; coverage/both requires **an active hook and the dynamic enumeration loop**.
- [ ] **Post-login shell:** Visiting a business path does not redirect to `/login` (account for hash routes).
- [ ] **Parameters:** coverage/both completed the parameter trigger matrix and `param_samples.json`; Phase 5 produced `params_merged.json`.
- [ ] **Depth** (if module pages are blank): Phase 4 reconstructed the permissions tree and Phase 3 was rerun until **module-level APIs** appeared (not just locale/bootstrap).
- [ ] **Delivery:** All Phase 5 outputs exist (see its output table); `insert_assets` wrote service and endpoint assets.

---

## Scripts and Gates

The files in `scripts/` are reference templates only. **Do not** run the originals directly and treat their output as final.

**Rule:** Read first, adapt to the target, write to `OUTDIR` (such as `recon/`), and record changes in `CHANGES.md`. If a template does not fit, rewrite it using the method and reuse only its structure.

| Gate | When | Reference script to OUTDIR copy | Common required changes |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **The default regex works for most sites**; change endpoint regex, webpack/Vite `publicPath`, or MPA exclusion/cookie only when the manifest or syntax differs |
| **B (runtime)** | After Phase 2, before depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, successful neutralization values, stubs, login regex, API prefix, hash/history routing |

**Mandatory SPA order** (do not reorder; phase numbers take precedence over “explore before scripting”):

| Step | Required | Prohibited |
|---|---|---|
| After Phase 0 | Next Bash command = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read the main entry `index-*.js` (usually >500 KB) |
| Gate A | Copy script, make small changes if needed, then **run it immediately** | Manually extract APIs first and decide later whether to harvest |
| Before Phase 1 is complete | Verify outputs with `wc -l`; on 404, modify harvest and retry | Write an extraction script; repeatedly grep URLs that were not downloaded |
| Starting in Phase 1b | grep only `OUTDIR/js/*.js` | Use the main bundle in place of harvest |

- ✅ Copy `harvest_static.py` → optionally change regex → **run immediately**.
- ❌ curl the main bundle → grep repeatedly → write a temporary extractor → harvest last.
- **MPA:** After Phase 0, next Bash command = `python3 OUTDIR/spider_mpa.py ...`.

---

## Tool and Output Constraints

| Constraint | Description |
|---|---|
| Large files | **Do not** Read/grep `index-*.js` files over 100 KB into context; process them in batches with an OUTDIR script |
| grep output | Use `\| head -20` or `-m 5`; keep only path summaries in the conversation, never bundle excerpts |
| Verification | Use `wc -l` and `ls \| wc -l`; do not Read the whole directory |
| Initial regex probe | Optional, at most once, only on an HTML file or a small chunk of at most 50 KB; harvest remains the authoritative static result |
| Reference | See [reference.md](reference.md) for recipes, templates, and troubleshooting; do not inline the whole document |

---

## Execution Roadmap

```
Phase 0 Classify + OUTDIR
  → Gate A → Phase 1 harvest (★ run immediately ★)
  → Phase 1b Parameter analysis
  → Phase 2 Three authentication gates → config.json
  → Gate B → Phase 3 Runtime + parameter matrix
  → Phase 4 Permissions tree (if needed) → rerun Phase 3
  → Phase 5 Merge/report + use insert_assets to bulk-insert every discovered service and endpoint API asset; never omit a discovered asset during insertion
```

Check off steps in order. **Do not enter the next phase until the previous one is complete.**

1. [ ] **Phase 0:** Determine SPA/MPA and create `OUTDIR` → [Phase 0](#phase-0--classification).
2. [ ] **Gate A + Phase 1:** Copy script → harvest **immediately** → verify with `wc -l` → [Phase 1](#phase-1--static-analysis).
3. [ ] **Phase 1b:** Expand context around anchors and trace bindings → `param_candidates.json` → [Phase 1b](#phase-1b--parameter-analysis).
4. [ ] **Phase 2:** Inspect the three authentication gates → `config.json` → [Phase 2](#phase-2--three-authentication-gates).
5. [ ] **Gate B:** Adapt runtime scripts → [Phase 3](#phase-3--runtime).
6. [ ] **Phase 3:** depth / coverage / both; confirm entry into the post-login shell; run the parameter trigger matrix → `param_samples.json`.
7. [ ] **Phase 4** (if needed): Reconstruct the permissions tree → patch stubs → rerun Phase 3 → [Phase 4](#phase-4--permissions-tree-reconstruction).
8. [ ] **Phase 5:** Merge outputs, write the report, and call `insert_assets` → [Phase 5](#phase-5--merge-and-report).

---

## Phase 0 — Classification

Fetch the entry HTML and **create `OUTDIR`** (do not modify this skill's `scripts/` directory):

- **SPA:** Empty shell + `<div id=app>` + chunks → Phases 1–5.
- **MPA:** SSR + `<form>`, with no endpoint bundle → after Gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Outputs: `forms.txt`, `links.txt`, `api_inline.txt`. If an SPA yields about zero forms, switch to Phase 1.

---

## Phase 1 — Static Analysis

Follow [Scripts and Gates](#scripts-and-gates) and [Tool and Output Constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

Harvest parses HTML scripts and webpack/Vite manifests, downloads all lazy chunks, and produces `js/`, `api_static.txt`, `routes.txt`, and `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Compare the chunk count to the manifest. On 404, modify harvest and retry; do not curl each chunk manually.
- If `api_static.txt` has too few entries, loosen the endpoint regex in OUTDIR and rerun (see the reference).

### Phase 1b — Parameter Analysis

Paths come from Phase 1; parameter fields require separate reconnaissance. Follow the grep rules in [Tool and Output Constraints](#tool-and-output-constraints).

**Completion standard:** For important endpoints, identify field names, transport locations, inferred types, required status, sample values, and confidence.

#### 1b.0 — Transport Formats

| Format | Parameter location | Static evidence to inspect first |
|---|---|---|
| REST JSON | Body + query | `(params\|data\|body)\s*:\s*\{` next to a path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Traditional form | URL-encoded | `<form>`, `FormData` |
| File upload | Multipart | `FormData.append` |
| Path parameters | `/user/:id` | Route table + `useParams` / `$route.params` |
| Encryption/signing | Wrapped in `sign`/`data` | Hook inputs to encryption functions (reference section D) |

Output: Annotate each endpoint with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 — Expand Context Around Anchors

Use a known path as an anchor and expand the context window to find request-building objects:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper | Parameter clues |
|---|---|
| axios instance | `data` / `params` |
| Shared request function | Globally injected fields from interceptors |
| OpenAPI client | Generated method signatures |
| React Query / SWR | Hook's second argument |
| Vue composable | Composable inputs |

Look for remaining type clues in `yup`/`zod`/rules, `Form.Item name=`, and embedded Swagger definitions.

→ `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2 — Binding Layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Method |
|---|---|
| Form submit | Trace submit → transform → API |
| Table search | Trace `getFieldsValue()` → `params` |
| Route | Inspect `:id` / `?tab=` |
| Interceptor | Inspect global `tenantId`, pagination, and sign fields |
| Enum select | Trace `options` → API enum values |

Trace up the DevTools call stack from `fetch`/`XHR.send` to the request-building function.

#### 1b.3 — Three Request-Building Questions (Distinct from Phase 2's Authentication Gates)

| Question | What to determine |
|---|---|
| **Assembly** | Where the payload is built and how it is transformed |
| **Validation** | required, pattern, enum |
| **Transport** | Path / query / body / multipart / headers |

When inspecting the interceptor gate (Phase 2), also record globally injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4 — Handoff to Phase 3

Candidate fields come from static analysis and the binding layer. **Required/optional/conditional dependencies** require the Phase 3 parameter matrix and diff, plus Phase 5 error-based inference.

---

## Phase 2 — Three Authentication Gates

grep `OUTDIR/js/` (limit output with `head`) and write findings to `config.json` (recipes in the reference):

| Gate | Question | Keywords |
|---|---|---|
| **Render gate** | How does the app determine that a user is logged in? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What causes a redirect to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do menus and permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat a localStorage key name as a credential. Confirm it from chunks and request flows.

**Exit = Gate B:** Put conclusions in `config.json` and modify `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b — API Observation (Optional)

Use `preload.js` in OUTDIR to confirm session key names, Authorization, and nested API URLs:

| Setting | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | Header observations |
| `extractUrlsFromResponse: true` | Child APIs found in responses |
| `observe.storageReads/cookieReads: true` | Values to feed back into config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Export `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, and `__API_RECON_OBSERVE__` after each coverage round.

---

## Phase 3 — Runtime

Gate B must be complete. Follow [Boundaries and Prohibitions](#boundaries-and-prohibitions-required-reading-for-agents) and the credential-free mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in the reference).

### Hooks and Stubs (Shared by depth and coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 precise | Auth/permissions/bootstrap stubs | Pass initial authentication checks |
| L2 negative correction | All JSON responses | Change unauthenticated business codes to success |
| L3 fallback | `/api` and similar requests not matched by L1 | Return an empty success body so the UI can render |

- **depth:** fake auth + `forward` to change business codes + `stubs`; traverse `routes` (hash/history); produce `runtime_api.json`.
- **coverage:** Inject `preload.js` at **document start** (CDP `addScriptToEvaluateOnNewDocument` or Userscript).

Verify that `window.__API_RECON_PRELOAD__` exists and business paths do not redirect to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b — Coverage Dynamic Enumeration (Required)

1. Main navigation/sidebar — click each item and wait 1–3 seconds for network activity.
2. Tabs — `role=tab`, `.ant-tabs-tab`.
3. Tables — view/edit/open details for the first row.
4. Toolbars — export, filter, create (**avoid irreversible deletion**).
5. After entering each module — merge APIs and routes.
6. SPA — use controlled `pushState` for uncovered paths in `routes.txt` (prohibited for MPA).

**Parameter trigger matrix** (required): Record at least one sample of each operation type per module, then **diff multiple samples**:

| Operation | Common additional parameters |
|---|---|
| Initial list view | Pagination + default filters |
| Search | keyword, filter |
| Advanced filtering | More optional fields |
| Create/edit | Complete entity |
| Bulk actions/export/sort | `ids[]`, `exportType`, `sortField` |

**Outbound bodies and headers remain real even with stubs.** Trust the requests. Save recordings to `scan_raw.json`, `param_samples.json`, and `api_detail.json`.

- **Vue:** `neutralizeVueRouter: true` + document-start preload.
- **React:** `routes.txt` + sidebar clicks + `pushState`.
- **both:** Run 3a depth, then 3b coverage.

---

## Phase 4 — Permissions Tree Reconstruction

**Trigger:** A module page is blank or each route calls only bootstrap APIs (such as locale). The content gate has not been passed.

| Observation | Meaning |
|---|---|
| Post-login shell renders | Render and interceptor gates have passed |
| Sidebar entries are missing or clicks lead to blank pages | Stub shape or permission codes are incomplete |
| Each route calls the same small set of APIs | `v-if permission` has not passed |
| `routes.txt` has far fewer routes than the bundle | Add routes from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) → `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Check stubs: The outer `response_code` matches the interceptor gate; flat codes align with the tree; `routes` covers every link in `route_map`.

After updating `config.json`, **rerun Phase 3**. For large SPAs, adjust `waitUntil`, `routeTimeout`, and `perRouteMs` (reference sections A3/I).

---

## Phase 5 — Merge and Report

### Output Table

| File | Phase | Contents |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Candidate static parameter fields |
| `config.json` | 2 | Three gates and runtime configuration |
| `runtime_api.json` | 3a | Detailed depth recording (including WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click log, details |
| `route_map.json`, etc. | 4 | Intermediate permissions tree files (if run) |
| `params_merged.json` | 5 | Merged parameter fields and confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, parameters, features, limitations |
| **insert_assets** | 5 | Write all service and endpoint assets to the asset database |

### 5b — Merge Parameters

Diff `param_samples.json`; **there is no universal merge script**. See reference section J7 for confidence rules (high/medium/low/not yet triggered).

### 5c — Infer from Errors

Within the authorized scope, incomplete requests may be sent to inspect 400 responses (**parameter reconnaissance, not vulnerability testing**): `field 'x' is required`, enum errors, etc. Account for `data` wrappers, `variables`, and `bizData` before encryption.

The report must state runtimeMode, static/runtime API counts, parameter confidence, uncovered modules, and a summary of `CHANGES.md` relative to the reference scripts.

Suggested `site_map.json` structure:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

See [reference.md](reference.md) for more fields and grep recipes.

---

## General Notes

- **Framework-independent:** The same lazy-loading method applies to webpack/Vite/Angular.
- **Transports:** REST/JSON, GraphQL, WebSocket, SSE; gRPC-web is out of scope.
- **SSR:** Client fetch calls can be recorded; RSC/Server Actions cannot be fully enumerated.
- **Blind spots:** Strong JSVMP, WASM, or HMAC/mTLS checks → rely on static analysis and state the limitations.
- **Parameter blind spots:** Conditional fields, hidden parameters, WASM request building → mark as “not yet triggered”/“unreachable.”
- **Static analysis is a safety net:** Endpoints can still be enumerated statically when runtime analysis is blocked.

---

## Additional Resources

- Grep recipes, a `config.json` template, troubleshooting, hooks, parameter analysis in section J, and a site_map template: **[reference.md](reference.md)**.
- See the table in [Scripts and Gates](#scripts-and-gates) for reference script paths.
