# api-recon — Reference Manual

Grep recipes, `config.json` templates, and troubleshooting. Run all grep commands against the `js/` directory. For a single-line bundle, first use `js-beautify` or `sed 's/}/}\n/g'` if needed; raw grep with a context window is usually enough.

## Script Notes

All files in `scripts/` are **reference templates**. Adapt them to the target site before running them. Typical changes include:

| Script | Common adjustments |
|---|---|
| `harvest_static.py` | Endpoint regexes, webpack/Vite manifest parsing, microfrontend publicPath, retries/concurrency |
| `runtime_harvest.js` | Neutralization field names and success values, stub matching rules and body structure, route sources, WS recording, `waitUntil`/`routeTimeout`/`proxy` |
| `preload.js` | `loginPathRe`, L1 stubs, `neutralize.fields`, `apiPattern`, whether to enable L3, `recordDetail`, `observe.*`, `neutralizeVueRouter` |
| `spider_mpa.py` | Destructive links for `--exclude`, cookies, depth/max, same-domain filtering |
| `extract_route_map.py` | `routeMap` / `routeLink` regexes, KEY naming patterns |
| `build_perm_tree.py` | `userRouteAuth` parsing, `ROOTS`/`PREFIX_PARENT` hierarchy heuristics, top-level stub field names |
| `config.json` | Single entry point for all site-specific parameters above |

Put adapted files in the task working directory (for example, `recon/`), and document the specific changes from the reference scripts in the report.

---

## A. Three Reverse Engineering Gates

### A1. Rendering Gate — "How is login status determined?"

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

Trace `isLogin = f(getUser())` → `getUser = decode(storage.read(KEY))` to determine the **storage key**, **storage location** (Cookie vs localStorage), and **encoding**:

| Encoding | How to forge it in config |
|---|---|
| Plain string / `"1"` / token | `"value": "anything-truthy"` |
| `JSON.parse(x)` | `"value": "json:{\"id\":1,\"username\":\"admin\"}"` |
| `JSON.parse(atob(x))` | `"value": "b64json:{\"id\":1,\"username\":\"admin\"}"` |
| JWT | Unsigned/`alg:none` JWT, or a signature using a key found in the bundle |
| Encryption (SM2/AES/RSA) | Look for a hard-coded key; forge it if the rendering gate only needs a decodable blob; otherwise fall back to static analysis |

→ Write it to `cookies` / `localStorage`.

### A2. Interceptor Gate — "What triggers a redirect to /login?"

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
grep -rhoaE '.{0,40}(未登录|请重新登录|登录已过期|unauthorized|登录失效|授权|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

Determine the **field name**, **success value** (usually `0` or `200`), and **failure values that trigger a redirect**. Verify with a junk session:

```bash
curl -sk -X POST -H 'Cookie: <fakekey>=junk' https://target/api/<protected> -d '{}' -H 'Content-Type: application/json'
```

→ Set `neutralize.fields` + `neutralize.success`.

### A3. Content Gate — "Where do menus and permissions come from?"

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

**Two data layers** (common in enterprise admin systems):

| API | Typical payload | Consumer |
|---|---|---|
| `.../role_permissions` | `{ permissions: string[], role_type }` | Route guards and button-level ACL |
| `.../permissions/all` | `tree[{ code, position, children }]` | Sidebar menu rendering |
| `userRouteAuth` in the bundle | `{ CODE: { url, name? } }` | Code → frontend path |
| `routeMap` in the bundle | `{ KEY: { name, link } }` | Alias resolution (webpack `o.DASHBOARD`) |

Read the consumer code to confirm how `getResultTree(tree, permissions)` filters data and which fields `v-if` / `hasAuth(code)` checks.

**Manual forgery** (small sites): build a permissive payload → `stubs`.

**Full permission tree reconstruction** (large sites where the sidebar/submodules remain empty): see **Section I**.

---

## B. config.json Template

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": true,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

Field notes:
- `runtimeMode`: `depth` (Puppeteer), `coverage` (browser MCP), or `both`
- `cookies[].value` prefixes: `b64json:` → base64(JSON); `json:` → raw JSON; no prefix → literal value
- `forward: true` forwards real requests and rewrites code fields; `false` uses fully offline stubs
- `mockTier`: enabled preload tiers in coverage mode, such as `L1+L2` or `L1+L2+L3`
- `routes` comes from `routes.txt`; after forging the menu, the harness automatically appends `<a href>` links
- `captureResponses` / `recordWs` work only in depth mode
- `waitUntil`: use `domcontentloaded` for large SPAs to avoid `networkidle2` hanging
- `routeTimeout`: per-route `page.goto` timeout in milliseconds
- `proxy`: Puppeteer `--proxy-server`; `HTTP_PROXY` / `HTTPS_PROXY` also work

### B1. Dual Stub Template (role_permissions + permissions/all)

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

The top-level field names (`response_code` / `code` / `data`) must match the A2 interceptor gate; `permissions` must include every leaf code in the tree.

---

## C. Coverage Mode: Preload Configuration

Edit the `CONFIG` object at the top of `scripts/preload.js`, or replace it before CDP injection:

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: true,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* Same as config.json stubs */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

Verify that `window.__API_RECON_PRELOAD__ === true` and the pathname remains stable.

Export the recorded results:

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

---

## D. Preload / Runtime Hook Capabilities

Browser hook capabilities and coverage built into preload (coverage) and runtime_harvest (depth):

| Hook capability | Value for API discovery | Coverage |
|---|---|---|
| Hook fetch / XHR.open | Record request URLs/methods | ✅ `recordDetail` + `__API_RECON_LOG__` |
| Hook XHR.setRequestHeader | Discover headers such as Authorization | ✅ `observe.xhrHeaders` |
| Hook localStorage/cookie reads | Confirm session key names | ⚠️ Optional `observe.storageReads/cookieReads` |
| Capture Vue routes | Complete frontendRoutes | ✅ `__API_RECON_ROUTES__` (loaded routes) |
| Neutralize Vue route guards / block login redirects | Open modules to trigger APIs | ✅ `neutralizeVueRouter` + native redirect neutralization |
| Capture React routes | Fill in routes | ⚠️ Static analysis + clicks; no dedicated hook |
| Block page redirects (login path) | Keep the page open for analysis | ⚠️ Only blocks the login path to preserve normal navigation |
| Hook encryption libraries (CryptoJS/SM, etc.) | Encrypted parameters → plaintext API body | ❌ Manually hook encryption function inputs; document findings in config |
| Bypass anti-debugging | Otherwise runtime may record no APIs | ❌ Handle manually; static analysis remains available |

---

## E. Endpoint Extraction Regexes (When Static Results Are Sparse)

Relax the regexes in `harvest_static.py`'s `extract_endpoints`, or search manually:

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

---

## F. Troubleshooting

| Symptom | Cause → remedy |
|---|---|
| Few static APIs | Endpoint pattern mismatch → relax the regexes (Section E) |
| Chunk count ≪ manifest count | CSS-only or undeployed chunks; 404s have already been retried |
| Runtime still shows the login page | Rendering gate failed → recheck A1: key name, storage location, encoding, domain |
| Shell loads but modules are blank | Content gate → forge the menu (A3); `routes` paths may be wrong |
| Only bootstrap/locale on each route | Incomplete permission codes → reconstruct the permission tree in Section I; check both `role_permissions` and `permissions/all` stubs |
| Sidebar has items but subpages are blank | Tree lacks intermediate nodes, or codes do not match `userRouteAuth` |
| Every API redirects to login | Interceptor gate → check `neutralize`; nested fields require expanded walk logic |
| Zero WS frames | Subscription may require user interaction; increase `perRouteMs` |
| Empty response bodies | Real responses are available only with `forward: true` |
| Chromium missing | Install Chromium or set `config.chromium` / `CHROMIUM` |
| Many mocks but still redirected to login | Hook runs too late or lacks a `location.href` setter → document-start + preload |
| All lists are empty | Empty L3 arrays are expected; continue clicking tabs, settings, and details |
| Redux actions mistaken for routes | Filter internal paths containing get/set/change/clear/toggle/upload |
| Vue still redirects to login | Preload did not run at document-start → change injection timing; if `neutralizeVueRouter: false`, clear guards manually |
| Response contains a URL but it is absent from the log | Enable `extractUrlsFromResponse`, or extract it manually from `__API_RECON_DETAIL__` |
| Authorization header name is unknown | Enable `observe.xhrHeaders` or inspect request headers in DevTools |
| Runtime is very slow or times out | Set `waitUntil: domcontentloaded`; lower `routeTimeout`; avoid `networkidle2` |
| Proxy connection fails | Check `proxy` / environment variables; use the same proxy port for Puppeteer and curl |

---

## G. Hardened Targets

When the server validates the session at each step (a signed cookie that cannot be forged, or a server-rendered menu that cannot be stubbed), runtime may stall at the shell. Expected behavior:

- **Static analysis is enough for endpoint enumeration** — module paths are in the code
- If authorized, run the same harness with a **real session**: use `forward: true` without neutralization to capture real methods/parameters/responses

---

## H. Single-Task Checklist

1. Confirm the authorized scope.
2. **Read** `scripts/harvest_static.py` → adapt it to the target → run it → review `api_static.txt` and `routes.txt`.
3. **Phase 1b**: widen the context around path anchors and trace the binding layer → `param_candidates.json` (Section J).
4. Reverse engineer A1/A2/A3 → write a site-specific `config.json`.
5. **Read and adapt** `runtime_harvest.js` / `preload.js` before running them.
6. `runtimeMode=depth`: run `npm install` → run the adapted harvest script.
7. `runtimeMode=coverage/both`: inject the adapted preload at document-start → use browser MCP for dynamic enumeration + the **parameter trigger matrix**.
8. If modules do not render → **reconstruct the permission tree in Section I** → patch stubs → rerun.
9. Compare multiple parameter samples and infer from errors → `params_merged.json`.
10. Merge into `site_map.json` + `api_merged.txt`; accurately report coverage, gaps, and script changes.

---

## I. Permission Tree Reconstruction (Phase 4 Deep Dive)

Use this when a simple forged `menus: [{ path, show: true }]` does not work and submodules still do not mount.

### I1. Locate the Auth Module

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

Record the **permission API path**, **response field names**, and **consuming chunk filename**.

### I2. Extract routeMap

```bash
python3 scripts/extract_route_map.py recon/js recon/
# Produces recon/route_map.json
```

If you see `[!] no routeMap pattern found`, relax the regex in `extract_route_map.py` or grep manually:

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. Build the Permission Tree + Stubs

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Script logic:
1. Parse `userRouteAuth={MONITOR:{url:...},...}` (including webpack aliases such as `He=o.DASHBOARD`).
2. Use `route_map.json` to resolve aliases → actual paths.
3. Infer parents from code prefixes (`MONITOR_ALERT` → `MONITOR`).
4. Write `permissions_tree.json`, `permissions_all_stub.json`, and `role_permissions_stub.json`.
5. With `--config`, automatically write `stubs` to `config.json` and extend `routes`.

**Adapt to the target** (at the top of the script):
- `DEFAULT_ROOTS`: list of top-level module codes
- `DEFAULT_PREFIX_PARENT`: mapping from `PREFIX_` to parent
- `DEFAULT_EXTRA_PARENT`: orphan nodes without a prefix relationship

### I4. Validate Stub Consistency

```bash
# Permission count should be approximately the userRouteAuth entry count
wc -l recon/perm_codes_all.txt
# Routes should cover every link in route_map
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. Rerun Runtime and Compare

```bash
node recon/runtime_harvest.js recon/config.json
# Compare runtime_api.json counts before and after forgery; check whether module APIs appear for /attack, /asset, etc.
```

| Before forgery | After successful forgery |
|---|---|
| The same 3–5 bootstrap requests on every route | Different routes trigger different module APIs |
| Only `/api/locale/language` | Module endpoints under `/api/web/...` appear |
| Single-digit route count in `routes.txt` | `routes` contains 80–110+ entries from route_map |

### I6. If It Still Fails

- **Coverage mode**: click the sidebar and tabs; permission checks may send requests only after interaction.
- **Stub fields**: compare the nesting of a real API response (curl + real session) with the stub.
- **Additional guards**: grep for button-level checks such as `hasPermission|checkRole|func.`, then extend `role_permissions.permissions`.
- **Static fallback**: module API paths remain in `api_static.txt`; runtime only adds METHOD/body. Keep parameters in `param_candidates.json` along with recorded samples.

---

## J. Parameter Reverse Engineering (Phase 1b / 5b / 5c)

**A method, not a universal script.** Use regexes to find paths; find parameters by widening context around anchors, tracing UI bindings, comparing multiple samples, and inferring from errors.

### J1. Widen Anchor Context — Find the Payload Builder from a Path

```bash
# Use a known Phase 1 path as the anchor
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. Wrappers and Transport Shapes

```bash
# axios / shared request wrapper
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

# Path parameters
grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. Validation Gate — Required / Format / Enum

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. Binding Layer — Form → API

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

For runtime evidence, use DevTools → Network → request → **Initiator** (call stack), then trace upward from `fetch`/`send` to the payload builder.

### J5. Encrypted Parameters

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

**Do not guess fields from ciphertext.** Hook the **inputs** to the encryption function and record the plaintext payload before encryption; document findings in `config.json` / `param_candidates.json`.

### J6. Parameter Trigger Matrix (Required in Phase 3)

Record each operation once per module, then compare request bodies and queries:

| Operation | Focus |
|---|---|
| Initial list view | Default pagination values |
| Search | Keywords, filters |
| Advanced filters | Optional fields |
| Create/edit | Complete entity |
| Batch actions/export | `ids[]`, `exportType` |
| Sort/page change | `sortField`, `order` |

Write `param_samples.json`: `[{ "path", "method", "action": "search", "body", "query", "headers" }]`

### J7. Confidence Rules

| Confidence | Condition |
|---|---|
| **High** | Static callsite and at least two matching runtime samples |
| **Medium** | Static evidence only, or only one runtime sample |
| **Low** | Inferred from a response/error without a second validation |
| **Not yet triggered** | Field known statically, but the UI/permissions have not reached it |

### J8. Quick Recipes by Scenario

| Scenario | Sequence |
|---|---|
| REST list page | J1 payload builder → J6 four comparisons → J3 validation rules |
| Create/edit form | J3 form name → J4 submit chain → submit at runtime and deliberately leave fields blank to inspect a 400 response |
| GraphQL | J2 variable declarations → record variables for each runtime operation |
| Encrypted body | J5 hook inputs → fields before encryption are the real parameters |

### J9. Mapping to api-recon Phases

| api-recon | Parameter recon |
|---|---|
| Phase 1 static | J1 widen anchor context |
| Phase 2 A2 interceptor | Globally injected fields (`tenantId`, `sign`) |
| Phase 3 runtime | J6 trigger matrix + `param_samples.json` |
| Phase 4 permission tree | Forms differ by module → sufficient permissions are needed to trigger every field |
| Phase 5 merge | `params_merged.json` + confidence; do not mark a field required from one sample |

### J10. Troubleshooting

| Symptom | Remedy |
|---|---|
| Static field name never appears at runtime | Mark it "not yet triggered"; complete the permission tree, click advanced filters, or try each linked select option |
| Same path has different body shapes | Expected — record separate entries by `action`; do not force them into one schema |
| Stub responses are fake but parameters are needed | **Inspect outbound requests** and their bodies/headers; do not infer parameters from stub responses |
| 400 mentions a nested field | Check outer wrappers such as `data`/`bizData`/`variables` |
| GraphQL shows only the operation name | Expand the `variables` JSON; find `$var: Type` statically |

---
