# Noticeboard — Design

Noticeboard is a web application that displays world clocks on a public board and lets an administrator manage the clocks and the board's appearance.

- **Language:** Go
- **Framework:** PocketBase (data storage, authentication, migrations, admin tooling)
- **Web UI:** HTMX with server-rendered HTML fragments
- **Executable:** `noticeboard`
- **Tooling:** mise (environment and tasks)

Reference mock-up: [`docs/time-display.png`](time-display.png)

---

## 1. Goals and scope

**In scope**

- A public home page showing a configurable grid of world clocks, in 24-hour `HH:MM` format.
- The UTC clock is shown in red; all other clocks are shown in green.
- An administration dashboard, protected by login, for managing:
  - the clocks (add, edit, enable/disable, reorder, delete);
  - the board settings (title, colours, refresh interval, columns).
- Run as a web server, or as a desktop-style app window with `serve --gui`.

**Out of scope (for now)**

- Notices or messages other than clocks. The project README mentions the original GSM_Display application; this version is clocks only.
- Localisation. The UI is English only.
- Functional (end-to-end) tests. The markup is made Playwright-friendly, but no Playwright tests are written in this phase.

## 2. Architecture

A single Go binary built on PocketBase, used as an embedded framework.

- **PocketBase** provides the SQLite database (in `pb_data/`), the superuser authentication, the migration system, the built-in admin dashboard at `/_/`, and the HTTP server.
- **Custom routes** are registered on PocketBase's router in `OnServe`. Handlers render `html/template` fragments, and HTMX swaps them into the page.
- **Embedded assets:** templates, CSS, `htmx.min.js` and the DSEG7 font are embedded with `go:embed`, so the binary is self-contained.
- **Pure SQLite:** PocketBase uses a pure-Go SQLite driver, so no C toolchain is needed.

### 2.1 Layout

```
noticeboard/
├── main.go                 # bootstrap PocketBase, register routes, serve --gui
├── gui.go                  # launchGUI: browser app-mode (adapted from htmx-todos)
├── internal/
│   ├── clock/              # pure time logic: format time, colour class (no I/O)
│   ├── zones/              # timezones records: repository and validation
│   ├── settings/           # settings singleton: read, validate, update
│   └── web/                # HTTP handlers and rendering
│       ├── board.go        # GET /, GET /board
│       ├── admin_auth.go   # login, logout, superuser middleware
│       ├── admin_zones.go  # zone CRUD, toggle, move
│       └── admin_settings.go
├── migrations/             # collections, settings seed, zone seed
├── templates/              # html/template files (embedded)
├── static/
│   ├── fonts/              # DSEG7Classic-Bold.woff2, DSEG-LICENSE.txt
│   ├── css/app.css
│   └── js/htmx.min.js
├── docs/
│   ├── DESIGN.md
│   └── time-display.png
├── mise.toml
└── README.md
```

## 3. Data model

PocketBase collections, created by Go migrations. **All API rules are set to "superuser only"**. The public board reads the data server-side, so the REST API never exposes these collections to anonymous users.

### 3.1 `timezones`

One record per clock.

| Field     | Type   | Constraints                                                 |
|-----------|--------|-------------------------------------------------------------|
| `label`   | text   | required, max 60 characters                                 |
| `zone`    | text   | required, IANA zone ID (e.g. `Australia/Sydney`), validated with `time.LoadLocation` |
| `enabled` | bool   | default `true`                                              |
| `sort`    | number | integer; defines display order (ascending)                  |

Disabled clocks are hidden on the public board but remain in the admin list. Deleting a clock is a hard delete.

### 3.2 `settings`

A singleton: exactly one record, created by the migration. The application reads and updates it. It is never created or deleted through the UI.

| Field               | Type   | Default     | Constraints                     |
|---------------------|--------|-------------|---------------------------------|
| `title`             | text   | `Noticeboard` | max 80; blank hides the header |
| `columns`           | number | `3`         | integer, 1–6                    |
| `refresh_seconds`   | number | `5`         | integer, 1–60                   |
| `colour_background` | text   | `#d9d9d9`   | `#rrggbb`                       |
| `colour_label`      | text   | `#4169e1`   | `#rrggbb`                       |
| `colour_clock`      | text   | `#228b22`   | `#rrggbb` (non-UTC clocks)      |
| `colour_utc`        | text   | `#b22222`   | `#rrggbb` (UTC clock)           |

The colour defaults are approximations taken by eye from the mock-up and must be checked on the real display.

Constraints are enforced in the PocketBase field definitions, so they hold even when a record is edited through `/_/`. The application also validates input to give friendlier error messages.

### 3.3 Seed data

The migration creates:

- the `settings` record with the defaults above;
- nine clocks matching the mock-up:

| Order | Label            | Zone                 |
|-------|------------------|----------------------|
| 1     | UTC              | `UTC`                |
| 2     | New Zealand      | `Pacific/Auckland`   |
| 3     | India            | `Asia/Kolkata`       |
| 4     | Melbourne/Sydney | `Australia/Sydney`   |
| 5     | China            | `Asia/Shanghai`      |
| 6     | Canada           | `America/Vancouver`  |
| 7     | Philippines      | `Asia/Manila`        |
| 8     | Russia           | `Europe/Moscow`      |
| 9     | Ukraine          | `Europe/Kyiv`        |

The mock-up labels the red clock "G.M.T.". This design uses "UTC" to match the requirement. The Canada zone is an assumption: the mock-up's time matches UTC−7, and Vancouver is one zone with that offset.

## 4. Administration and authentication

### 4.1 Superuser account

- The dashboard uses PocketBase's **superuser** (`_superusers` collection) account. No account is seeded.
- The operator creates the first superuser from the command line:

  ```
  noticeboard superuser upsert <email> <password>
  ```

  The email address is chosen by the operator. It is the login identifier.
- While no superuser exists, the login page shows a hint with this command.

**Consequences of using a superuser:**

- The account can use PocketBase's built-in dashboard at `/_/`.
- The account bypasses API rules. Since this is a single-operator application, there is no separate limited dashboard account.

### 4.2 Login and session

1. `GET /admin/login` renders the form (email and password).
2. `POST /admin/login` checks the credentials against `_superusers`. On success it issues a PocketBase auth token.
3. The token is stored in a cookie with `HttpOnly`, `SameSite=Strict`, `Path=/admin`, and `Secure` when served over HTTPS.
4. `POST /admin/logout` clears the cookie and redirects to the login page.
5. Token lifetime uses PocketBase's default. It can be changed in the `_superusers` collection options.

**Middleware** reads the cookie on every `/admin` request, resolves the token, and accepts it only if it belongs to a `_superusers` record. Otherwise it redirects to the login page. The exact PocketBase Go call is confirmed during implementation.

**CSRF:** Admin mutating requests (`POST`, `DELETE`) must include the `HX-Request` header. HTMX sends it automatically; a cross-site form cannot set it. Combined with `SameSite=Strict`, this blocks cross-site form submission.

## 5. Routes

### 5.1 Public

| Method | Path      | Purpose                                                    |
|--------|-----------|------------------------------------------------------------|
| GET    | `/`       | Full page: HTML shell, styles, font, HTMX; embeds the board |
| GET    | `/board`  | Board fragment: title, grid of clocks, polling attributes   |

### 5.2 Administration

| Method | Path                            | Purpose                                 |
|--------|---------------------------------|-----------------------------------------|
| GET    | `/admin/login`                  | Login form                              |
| POST   | `/admin/login`                  | Authenticate and set cookie             |
| POST   | `/admin/logout`                 | Clear cookie                            |
| GET    | `/admin`                        | Dashboard: clocks list and settings form |
| GET    | `/admin/zones/new`              | New clock form                          |
| POST   | `/admin/zones`                  | Create clock                            |
| GET    | `/admin/zones/{id}/edit`        | Edit clock form                         |
| POST   | `/admin/zones/{id}`             | Update clock                            |
| POST   | `/admin/zones/{id}/toggle`      | Enable or disable clock                 |
| POST   | `/admin/zones/{id}/move?dir=up\|down` | Change display order              |
| DELETE | `/admin/zones/{id}`             | Delete clock (with confirmation)        |
| POST   | `/admin/settings`               | Update settings                         |

Validation failures return HTTP 422 with an HTML fragment containing the error messages.

## 6. Board display

### 6.1 Live updates

The whole board is one wrapper element containing the title, the grid, and the polling attributes:

```html
<div id="board"
     hx-get="/board"
     hx-trigger="every {{.RefreshSeconds}}s"
     hx-swap="outerHTML">
  ...
</div>
```

Each poll replaces the entire wrapper. A change to the title, colours, columns or refresh interval is therefore picked up within one interval, with no page reload.

### 6.2 Styling

- Settings are passed to the template as CSS custom properties on the wrapper: `--columns`, `--colour-background`, `--colour-label`, `--colour-clock`, `--colour-utc`.
- The grid uses `grid-template-columns: repeat(var(--columns), 1fr)`.
- Values are validated before they reach the template, so inlining them is safe.
- The `DSEG7 Classic` font is loaded with `@font-face` from `/static/fonts/DSEG7Classic-Bold.woff2`.

### 6.3 Clock tiles

- Each enabled clock renders as a label bar and a value.
- The value is `time.In(loc).Format("15:04")`, in 24-hour `HH:MM`.
- The UTC clock gets the UTC colour. All other clocks get the clock colour.
- The clock colour class is decided in `internal/clock`, which is pure Go and easy to test.

### 6.4 Playwright-friendly markup

- `data-testid="board"` on the wrapper.
- `data-testid="clock"` on each tile, with `data-zone="<IANA ID>"` and `data-utc="true|false"`.
- `<time datetime="HH:MM">` around each value, so the time is machine-readable.
- Labelled form fields and stable element IDs, so tests can select elements without relying on layout.

## 7. Font

- **Font:** DSEG7 Classic, Bold weight.
- **Source:** the official DSEG release, using the WOFF2 file rather than a converted TTF.
- **Files:** `static/fonts/DSEG7Classic-Bold.woff2` and `static/fonts/DSEG-LICENSE.txt`.
- **Licence:** SIL Open Font License 1.1. The font is unmodified, so the reserved name "DSEG" is not affected. The OFL text ships with the font. The font is not sold by itself.
- **Usage:** upright, rendered directly from the `HH:MM` string. No unlit ("ghost") segments, to match the mock-up.
- The weight will be confirmed by a side-by-side render against the mock-up during implementation.

## 8. Configuration

The board is configured entirely from the admin dashboard. There are no environment variables or config files beyond the standard PocketBase flags, such as `serve --http`.

- **Clocks:** the `timezones` collection.
- **Board settings:** the `settings` singleton (section 3.2).

## 9. Desktop mode

- `noticeboard serve --gui` starts the PocketBase server and opens the board in a browser app-mode window.
- The launcher is adapted from `htmx-todos/gui.go`. It tries, in order, Edge or Chrome in app mode on Windows; Chrome, Chromium, Brave or `xdg-open` on Linux; and Chrome or `open` on macOS.
- It requires no CGO and no webview library.

## 10. Build, run and tasks

Tools are managed by [mise](https://mise.jdx.dev/) (`mise.toml`).

| Task       | Purpose                                                      |
|------------|--------------------------------------------------------------|
| `run`      | Development server with reload on `.go` and `templates/` changes (wgo) |
| `run-gui`  | As `run`, launching the GUI window                           |
| `build`    | `go build -o noticeboard .`                                  |
| `test`     | `go test ./...`                                              |
| `fmt`      | `go fmt ./...`                                               |
| `clean`    | `go mod verify && go mod tidy`                               |

The Go version is pinned in the `[tools]` section of `mise.toml`.

## 11. Testing

Per the project guidelines:

- **Unit tests** use Go's built-in `testing` package with table-driven tests.
  - `internal/clock`: time formatting and colour-class selection, using an injected `now`.
  - `internal/settings`: validation rules.
- **Integration tests** use `httptest` against the routes, with PocketBase's test app and a temporary data directory.
  - Login, logout and the superuser middleware.
  - Zone CRUD, toggle, move and validation responses.
  - Board rendering with the current settings.
- **testify** is used only where it clearly simplifies an assertion.
- **Functional tests** are not written in this phase. The markup in section 6.4 is designed so they can be added later with Playwright.

## 12. Implementation plan

1. **Skeleton:** `go mod init`, `mise.toml`, PocketBase bootstrap with `serve`, embedded assets, placeholder `/`, `build` task.
2. **Migrations:** `timezones` and `settings` collections with constraints and superuser-only API rules; the settings seed; the zone seed.
3. **`internal/clock`:** time formatting and colour-class logic, test-first.
4. **`internal/settings`:** read, validate and update the singleton, with tests.
5. **Board:** layout, `/board` fragment, polling wrapper, CSS variables, font embedding, `httptest` rendering tests.
6. **Admin authentication:** login against `_superusers`, cookie handling, middleware, logout, `HX-Request` check, login hint when no superuser exists, tests with the PocketBase test app.
7. **Clock management:** list, add, edit, enable/disable, move, delete with confirmation, validation messages.
8. **Settings UI:** form with colour pickers, number inputs and validation messages.
9. **Desktop mode:** `serve --gui` with the launcher adapted from `htmx-todos/gui.go`.
10. **Polish:** test IDs, README run instructions, mise tasks, the DSEG7 side-by-side check.

## 13. Decisions and assumptions

| Topic | Decision |
|---|---|
| Admin account | PocketBase superuser; no seeded account; created with `noticeboard superuser upsert` |
| Login identifier | Superuser email, chosen by the operator |
| Session token lifetime | PocketBase default |
| Configurable board settings | Title, columns, refresh interval, and four colours (background, label, clock, UTC) |
| Clock ordering | Manual, via the `sort` field and move buttons |
| Clock identity | Free-text label plus IANA zone ID |
| Live updates | HTMX polling of the whole board wrapper |
| Desktop mode | Browser app-mode via `serve --gui`; no CGO |
| Font | DSEG7 Classic Bold, self-hosted WOFF2, OFL |
| Scope | Clocks only; English only |
| UTC label | "UTC" (mock-up says "G.M.T.") |
| Canada zone | `America/Vancouver` (matches the mock-up's offset) |
