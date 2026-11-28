# Noticeboard — Design

Noticeboard is a web application that displays world clocks on a public clocks page and lets an administrator manage the clocks and the page's appearance.

- **Language:** Go
- **HTTP:** standard library `net/http` (no web framework)
- **Database:** SQLite via `database/sql` and the pure-Go `modernc.org/sqlite` driver
- **Web UI:** HTMX with server-rendered HTML fragments
- **Executable:** `noticeboard`
- **Tooling:** mise (environment and tasks)
- **Deployment:** on a VPN (section 4.3)

Reference mock-up: [`docs/time-display.png`](time-display.png)

---

## 1. Goals and scope

**In scope**

- A public home page showing a fixed 3×3 grid of world clocks (at most nine enabled clocks), in 24-hour `HH:MM` format.
- The UTC clock is shown in red; all other clocks are shown in green.
- An administration dashboard, protected by login, for managing:
  - the clocks (add, edit, enable/disable, reorder, delete);
  - the clock display settings (title, colours, refresh interval) and the table page size;
  - the `timezones` table through a paginated explorer with its raw columns, Edit and Delete per row, built on a generic table layer (section 5.3).
- Run as a web server, or as a desktop-style app window with `serve --launch`; `open <address>` opens an already-running clocks page without starting a server.

**Out of scope (for now)**

- Notices or messages other than clocks. The project README mentions the original GSM_Display application; this version is clocks only.
- Localisation. The UI is English only.
- Functional (end-to-end) tests. The markup is made Playwright-friendly, but no Playwright tests are written in this phase.
- TLS, login throttling and multiple users. The app runs on a VPN (section 4.3).

## 2. Architecture

A single Go binary using only the standard library for HTTP and `database/sql` for storage. There is no web framework and no PocketBase.

- **Routing:** the standard `ServeMux` (Go 1.22 method patterns, such as `POST /admin/zones/{id}`) serves all routes. Handlers render `html/template` fragments, and HTMX swaps them into the page.
- **SQLite:** `database/sql` with `modernc.org/sqlite`, a pure-Go translation of SQLite. The driver is imported blank and opened as `"sqlite"`. The data file is `./noticeboard.db` by default (`--db`), opened with the rollback journal (`journal_mode=DELETE`, not WAL) and a 5-second busy timeout so that `admin init` can run while the server is running.
- **No CGO:** no C compiler is needed. The binary builds with `CGO_ENABLED=0`, and cross-compiles with `GOOS`/`GOARCH` alone.
- **Schema migrations:** numbered SQL files in `migrations/`, embedded and applied in order, each in its own transaction. The applied version is stored in `PRAGMA user_version`. Migrations are forward-only: after a deployment, a schema change goes in a new numbered file. Before the first deployment, earlier migrations may be folded into `0001_init.sql`; that has happened here, so `0001_init.sql` is currently the only migration. Startup refuses a database whose `user_version` is higher than the newest migration the binary knows.
- **Authentication:** one admin account in the database, passwords hashed with bcrypt, and server-side sessions (section 4).
- **Logging:** an audit log of logins and mutating admin requests, in a text file (section 4.4).
- **Embedded assets:** templates, CSS, JavaScript, `htmx.min.js` and the DSEG7 font are embedded with `go:embed`, so the binary is self-contained.
- **Time zones:** the IANA database is embedded with `import _ "time/tzdata"`, so zone lookups work without a system zoneinfo (Windows has none).
- **Dependencies (runtime):** `modernc.org/sqlite`, `golang.org/x/crypto/bcrypt` and `golang.org/x/term` (no-echo password prompt). Tests may also use testify (section 11).

### 2.1 Layout

```
noticeboard/
├── main.go                 # subcommands: serve [--addr --db --log --launch], open <address>, admin init [--db]; --help
├── launch.go               # desktop mode: browser app-mode
├── assets.go               # go:embed for templates/ and static/
├── internal/
│   ├── db/                 # open SQLite (modernc.org/sqlite), run migrations
│   ├── clock/              # pure time logic: format time, colour class (no I/O)
│   ├── zones/              # timezones table: repository and validation
│   ├── settings/           # settings singleton: read, validate, update
│   ├── paging/             # page arithmetic: Clamp, LastPage (no I/O)
│   ├── admintable/         # generic table pages: contract, registry, adapters
│   │   ├── table.go        # Table interface and its types
│   │   ├── errors.go       # ErrNotFound, ErrUnknownAction, ErrBadDirection, FieldErrors, Refused
│   │   ├── region.go       # zone drop-down grouped by region
│   │   ├── registry.go     # All(db): the registered tables
│   │   └── timezones.go    # the timezones adapter over internal/zones
│   ├── auth/               # admin account, password hashing, sessions
│   ├── auditlog/           # text audit log, capped at 1000 lines
│   └── web/                # HTTP handlers and rendering
│       ├── clocks.go       # GET /clocks (page or poll fragment), GET / (redirect)
│       ├── admin_auth.go   # login, logout, session middleware
│       ├── admin_zones.go  # zone CRUD, toggle, move (dashboard clock list)
│       ├── admin_table.go  # generic table pages: /admin/tables/{table}
│       ├── admin_settings.go
│       └── middleware.go   # security headers, body size limit, audit logging
├── migrations/             # numbered schema migrations; migrations.go embeds them
├── templates/              # html/template files (embedded), including tables.html
├── static/
│   ├── fonts/              # DSEG7Classic-Bold.woff2, DSEG-LICENSE.txt
│   ├── css/app.css
│   └── js/
│       ├── htmx.min.js
│       └── app.js          # 422 swap rule, stale indicator
├── docs/
│   ├── DESIGN.md
│   └── time-display.png
├── .gitignore
├── mise.toml
└── README.md
```

### 2.2 Server behaviour

- **Startup:** open the database (creating and migrating it if absent) and the audit log, then listen. Once listening, delete every session, so an admin login from a previous run does not survive a restart. A listen failure (for example, port in use) exits with status 1 and leaves the running server's sessions alone.
- **Shutdown:** on SIGINT or SIGTERM, stop accepting connections, let in-flight requests finish for up to 5 seconds, then exit with status 0.
- **Timeouts:** `http.Server` sets `ReadHeaderTimeout` to 10 seconds.
- **Body size:** request bodies are limited to 64 KiB with `http.MaxBytesReader`.
- **Response headers:** every response carries `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'`. The inline style is needed only for the CSS custom properties (section 6.2). No inline scripts are used.
- **Clock:** the clocks page shows the host's time. The host clock must be kept accurate, for example with NTP.

## 3. Data model

SQLite tables, created by the embedded migrations (section 2.1). There is **no REST API**: all data is read and written by server-side handlers, so nothing is exposed to anonymous clients beyond the rendered clocks page.

### 3.1 `timezones`

One record per clock.

| Field     | Type    | Constraints                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`      | INTEGER | primary key, autoincrement (used in routes)                                                                                                                                                                                                                                                                                                                                                                                                  |
| `label`   | TEXT    | required, trimmed, 1–60 characters, unique (`CHECK`, `UNIQUE`)                                                                                                                                                                                                                                                                                                                                                                               |
| `zone`    | TEXT    | required, unique, IANA zone ID (e.g. `Australia/Sydney`); checked in Go with `time.LoadLocation`, which must not return an error. The empty string and `Local` are rejected. SQLite cannot validate zone names. The clock form offers a drop-down of the zones in the embedded database (`internal/zones/zonenames.txt`), grouped by region. Legacy aliases (the IANA `backward` links, such as `Asia/Calcutta`) are left out; `UTC` is kept |
| `enabled` | INTEGER | `0` or `1`, default `1` (`CHECK`)                                                                                                                                                                                                                                                                                                                                                                                                            |
| `sort`    | INTEGER | required, unique; defines display order (ascending); new clocks take max + 1                                                                                                                                                                                                                                                                                                                                                                 |

- Disabled clocks are hidden on the public clocks page but remain in the admin list.
- The clocks page is a fixed 3×3 grid, so at most nine clocks may be enabled. Creating a clock, or enabling a disabled one, is refused when nine are already enabled. SQLite cannot express this rule, so the application enforces it. Disabled clocks have no limit.
- Deleting a clock is a hard delete.
- Moving a clock swaps `sort` with its neighbour (the adjacent clock in the list, enabled or disabled) in one transaction. A temporary value is needed because `sort` is unique. Moving the first clock up, or the last clock down, does nothing.

### 3.2 `settings`

A singleton: exactly one row, enforced by `CHECK (id = 1)` and created by the initial migration. The application reads and updates it. It is never created or deleted through the UI.

| Field               | Type    | Default        | Constraints                                                |
| ------------------- | ------- | -------------- | ---------------------------------------------------------- |
| `id`                | INTEGER | `1`            | always `1`                                                 |
| `title`             | TEXT    | `World Clocks` | trimmed, max 80 characters; blank hides the header         |
| `refresh_seconds`   | INTEGER | `5`            | 1–60 (`CHECK`)                                             |
| `colour_background` | TEXT    | `#d9d9d9`      | `#rrggbb` (`CHECK` with `GLOB`)                            |
| `colour_label`      | TEXT    | `#4169e1`      | `#rrggbb`; label bar background, and the title text colour |
| `colour_clock`      | TEXT    | `#228b22`      | `#rrggbb` (non-UTC clocks)                                 |
| `colour_utc`        | TEXT    | `#b22222`      | `#rrggbb` (UTC clock)                                      |
| `table_page_size`   | INTEGER | `20`           | 5–100 (`CHECK`); rows per page in the administration tables |
| `login_timeout`     | REAL    | `15`           | 0.1–1440 (`CHECK`); minutes without user activity before the admin pages sign out |

- `table_page_size` applies to every administration table page (section 5.3). It is edited on the dashboard, in the settings form's "Tables" fieldset.
- `login_timeout` is the idle log-out period of the admin pages (section 4.2). It is edited on the dashboard, in the settings form's "General" fieldset, and may be fractional, such as `1.5` minutes. The pages receive it in whole seconds as `data-idle-timeout`.
- Colours are accepted in either case and stored in lowercase.
- Label text is fixed white. It is not a setting.
- The colour defaults are approximations taken by eye from the mock-up and must be checked on the real display.
- The `CHECK` constraints hold even if rows are edited with an external SQLite client. The application also validates input to give friendlier error messages.

### 3.3 `admins`

The single administrator account (section 4.1). `CHECK (id = 1)` limits the table to one row. It is created or replaced only by the `noticeboard admin init` command, never through the UI.

| Field           | Type    | Constraints                                                                                                                            |
| --------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `id`            | INTEGER | always `1`                                                                                                                             |
| `username`      | TEXT    | required; one or more printable characters with no white space; trimmed and lower-cased before storing and before lookup (section 4.1) |
| `password_hash` | TEXT    | bcrypt hash; plaintext passwords are never stored                                                                                      |

### 3.4 `sessions`

Server-side login sessions (section 4.2).

| Field        | Type | Constraints                                                                                   |
| ------------ | ---- | --------------------------------------------------------------------------------------------- |
| `token_hash` | TEXT | primary key; hex-encoded SHA-256 of the session token, so the database holds no usable tokens |

Sessions have no expiry column: a row exists only while the session is live (section 4.2).

### 3.5 Seed data

The initial migration (`0001_init.sql`) creates:

- the `settings` record with the defaults above;
- nine clocks matching the mock-up, all enabled:

| Order | Label            | Zone                |
| ----- | ---------------- | ------------------- |
| 1     | UTC              | `UTC`               |
| 2     | New Zealand      | `Pacific/Auckland`  |
| 3     | India            | `Asia/Kolkata`      |
| 4     | Melbourne/Sydney | `Australia/Sydney`  |
| 5     | China            | `Asia/Shanghai`     |
| 6     | Canada           | `America/Vancouver` |
| 7     | Philippines      | `Asia/Manila`       |
| 8     | Russia           | `Europe/Moscow`     |
| 9     | Ukraine          | `Europe/Kyiv`       |

The mock-up labels the red clock "G.M.T.". This design uses "UTC" to match the requirement.

The mock-up is a single moment, not a specification. Zones are IANA IDs, so daylight saving applies automatically. The mock-up matches Vancouver only during PDT (UTC−7) and Kyiv only during summer time (UTC+3). New Zealand (+12) and Sydney (+10) are on standard time. The values therefore correspond to a date between about April and September. The Canada zone is an assumption.

## 4. Administration and authentication

### 4.1 Admin account

- The dashboard has one administrator account, stored in the `admins` table (section 3.3). No account is seeded.
- The operator creates or replaces it from the command line:

  ```
  noticeboard admin init <username>
  ```

  The username is the login identifier, and the operator chooses it. A valid username is a string of printable characters with no white space: one or more characters, each one printable, and none of them a space, tab, line break or other white-space character. Non-ASCII letters are allowed, and so is an email address such as `joe@example.com`. Surrounding white space is trimmed, and the username is lower-cased, before it is stored. The login form normalises input the same way, so usernames are not case-sensitive. `admin init` rejects an invalid username before it asks for the password.

- `admin init` creates the database file and runs migrations if the file does not exist (section 8).
- Replacing the account deletes all of its sessions, so existing logins end.
- The command prompts for the password. Input is not echoed (`golang.org/x/term`), and the password must be entered twice to confirm. If the two entries differ, the command exits with status 1 and changes nothing. The password is never a command-line argument, so it does not appear in shell history.
- The password must not be empty. It is hashed with bcrypt (`golang.org/x/crypto/bcrypt`) at `bcrypt.DefaultCost`. bcrypt uses only the first 72 bytes, so the command rejects passwords longer than 72 bytes (bytes, not characters).
- While no admin exists, the login page shows a hint with this command.

**Consequences:**

- There is no built-in database dashboard. The operator can inspect the SQLite file with any SQLite client.
- There are no other users or roles.

### 4.2 Login and session

1. `GET /admin/login` renders the form (username and password) with a Cancel link beneath Log in that opens the clocks page `/clocks`. Like all admin forms, it is an HTMX form (see CSRF below).
2. `POST /admin/login` normalises the username (section 4.1), looks up the admin and checks the password with `bcrypt.CompareHashAndPassword`. If the username does not match, the server still runs the comparison against a dummy bcrypt hash, so the response time does not reveal which usernames exist. A failure returns HTTP 422 with the message "Username or password is incorrect.", for both a wrong username and a wrong password. A username that contains white space cannot match, so it gets the same message.
3. On success the server generates 32 random bytes (`crypto/rand`) and sends them base64url-encoded in a cookie named `noticeboard_session`. It stores the hex SHA-256 of the token in `sessions`. The cookie has no `Max-Age`, so the browser drops it when its session ends; it has `HttpOnly`, `SameSite=Strict` and `Path=/admin`, and `Secure` when the request arrived over TLS (`r.TLS != nil`). The response carries `HX-Redirect: /admin`. Several sessions may be active at once.
4. `POST /admin/logout` deletes the session row, expires the cookie (`MaxAge=-1`) and responds with `HX-Redirect: /admin/login`.
5. Sessions have no server-side expiry. A session ends on logout (point 4), when the admin account is replaced, and when the server starts, which deletes every session row so a restart requires a fresh login.
6. The admin pages sign out a browser after a period without user activity. The period is the `login_timeout` setting in minutes (default 15, section 3.2), passed to both pages in whole seconds as the `data-idle-timeout` attribute on their `<body>`; `app.js` then sends `POST /admin/logout` with `HX-Request` and opens `/admin/login`. The server session is ended first because `/admin/login` redirects a signed-in visitor to `/admin`, so a plain redirect would not log anyone out.

**Middleware** applies to all `/admin` routes except `/admin/login`. It reads the cookie, hashes the token, and accepts it only if a `sessions` row exists. Otherwise:

- an HTMX request receives `401` with `HX-Redirect: /admin/login`, so a session lost during use sends the browser to the login page;
- any other request receives `303` to `/admin/login`.

A request to `/admin/login` with a valid session is redirected to `/admin`.

**CSRF:** Admin mutating requests (`POST`, `DELETE`) must include the `HX-Request` header. HTMX sends it automatically, and a cross-site form cannot set it. Every admin form is an HTMX form, including login and logout, so the rule applies uniformly. Combined with `SameSite=Strict`, this blocks cross-site form submission. The admin pages therefore require JavaScript.

### 4.3 Deployment and security scope

The application runs on a VPN, so the design relies on the network for protection:

- Plain HTTP only; there is no TLS. `--addr` should be set to an address reachable only over the VPN. The default is loopback.
- No login throttling or lockout.
- Hardening is limited to section 2.2.

### 4.4 Audit log

- `internal/auditlog` appends to the file given by `serve --log` (default `./noticeboard.log`).
- One line is written for each login attempt, successful or not, and for each `POST` or `DELETE` request under `/admin`, including failed ones and logout. `GET` requests are not logged. Passwords are never logged.
- Each line has the form `<RFC 3339 UTC time> <client IP> <METHOD> <path> <status> [detail]`. The client IP is the host part of `r.RemoteAddr`; proxy headers are not trusted. Login lines carry the detail `login ok <username>` or `login failed <username>`.
- The file holds at most 1000 lines. When a new line would exceed that, the oldest lines are removed.
- If the log cannot be written, the error is printed to stderr and the request is still handled.

## 5. Routes

### 5.1 Public

| Method | Path       | Purpose                                                               |
| ------ | ---------- | --------------------------------------------------------------------- |
| GET    | `/`        | Redirects to `/clocks`                                                |
| GET    | `/clocks`  | Full page: HTML shell, styles, font, HTMX, `app.js`; embeds the clock grid. An `HX-Request` poll gets just the clock grid fragment |
| GET    | `/favicon.ico` | Empty `204 No Content`, so the browser's automatic icon request is not an error |
| GET    | `/static/` | Embedded CSS, JavaScript and font files                               |

### 5.2 Administration

| Method | Path                                  | Purpose                                  |
| ------ | ------------------------------------- | ---------------------------------------- |
| GET    | `/admin/login`                        | Login form                               |
| POST   | `/admin/login`                        | Authenticate and set cookie              |
| POST   | `/admin/logout`                       | End session and clear cookie             |
| GET    | `/admin`                              | Dashboard: clocks list and settings form |
| GET    | `/admin/zones/new`                    | New clock form                           |
| POST   | `/admin/zones`                        | Create clock                             |
| GET    | `/admin/zones/{id}/edit`              | Edit clock form                          |
| POST   | `/admin/zones/{id}`                   | Update clock                             |
| POST   | `/admin/zones/{id}/toggle`            | Flip enabled/disabled                    |
| POST   | `/admin/zones/{id}/move?dir=up\|down` | Change display order                     |
| DELETE | `/admin/zones/{id}`                   | Delete clock (after `hx-confirm`)        |
| POST   | `/admin/settings`                     | Update settings                          |

**Responses**

- **Success:** create, edit, toggle, move and delete return the updated clock list (`#zones`) and an empty form region, as out-of-band swaps. A settings update returns the settings form with the stored values and the outcome after the status line's "Status: " label, under the Save settings button: a green "Settings saved" after a successful save, or a red "Failed to save settings" when the form comes back with 422. The server renders both the text and the colour class, so no script is involved. The message has no timeout: it stays until the form is next replaced.
- **Validation failure:** HTTP 422 with the form re-rendered, containing the error messages, in its own region. HTMX does not swap 4xx responses by default, so `app.js` enables the swap for 422 (section 6.5).
- **Unknown clock `{id}`:** 404. Invalid `dir`: 400.
- **Login and logout:** as section 4.2.

The dashboard is a full page. Its regions are `#zones` (clock list), the form region, and `#settings`.

### 5.3 Administration table pages

The table pages are generic. `{table}` is a registered slug (`admintable.All`), and `timezones` is the one registered now. An unknown slug gives 404. The slug is never used to build SQL.

| Method | Path                                               | Purpose                                                    |
| ------ | -------------------------------------------------- | ---------------------------------------------------------- |
| GET    | `/admin/tables/{table}`                            | Page: full page, or grid fragment with `HX-Request`        |
| GET    | `/admin/tables/{table}/new`                        | New-record form                                            |
| POST   | `/admin/tables/{table}`                            | Create (shows the last page)                               |
| GET    | `/admin/tables/{table}/{id}/edit?page=N`           | Edit form                                                  |
| POST   | `/admin/tables/{table}/{id}?page=N`                | Update every editable field                                |
| DELETE | `/admin/tables/{table}/{id}?page=N`                | Delete (after `hx-confirm`)                                |

- **Grid:** the table's own columns, in schema order, with raw values, and an Actions column holding Edit and Delete only. For `timezones` that is `id`, `label`, `zone`, `enabled` (`1`/`0`), `sort`. The adapter's `List` decides the order; `timezones` uses `sort` ascending.
- **Search:** a search box sits to the left of Add record; pressing Enter applies the search. `q` keeps only the records with a value containing the text in any column (`LIKE`, so without case for ASCII; `%` and `_` are literal). The total, pages and clamping use the filtered set, and `q` is kept in the pager and Edit/Delete URLs. The box is an `<input type="search">`, so the browser's own clear (x) removes the filter and reloads the grid unfiltered. No match shows "No records found.".
- **Form:** every column except the ID, so `enabled` and `sort` are edited here as well as `label` and `zone`. A new record is prefilled from the adapter's defaults; for `timezones` that is `enabled = 1` and `sort = max + 1`. `sort` must be unique, and enabling a tenth clock is refused, both with 422 messages.
- **Paging:** `page` is 1-based; missing means page 1; anything else that is not a positive whole number gives 400; a page past the end shows the last page. The count and rows are read in one transaction, and the clamping rule is `internal/paging.Clamp`. `List(ctx, pg, search)` applies the search before counting.
- **Regions:** `#table-grid` and `#table-form`. Responses are out-of-band swaps of the grid and the form, as for the clock list.
- **Errors:** a refusal gives 422 with the message in the form region, and a stale `{id}` gives 404 with the refreshed grid and "That record no longer exists.".
- **Sidebar menu:** both admin pages (`/admin` and `/admin/tables/{table}`) share a fixed vertical menu in a left sidebar, which does not scroll with the page: `Administration` (`/admin`), `Clocks` (`/clocks`), `Tables` (a heading, not a link) with the indented `timezones` link (`/admin/tables/timezones`) and `Log out` (the `POST /admin/logout` form). All items are bold dark grey and look the same; the current page's item also carries `aria-current="page"` for assistive technology.
- **Adding a table:** an adapter in `internal/admintable` plus one line in `All`. No handler, route or template changes.

## 6. Clock display

### 6.1 Live updates

The whole clock grid is one wrapper element containing the title, the grid, and the polling attributes:

```html
<div
  id="clocks"
  hx-get="/clocks"
  hx-trigger="every {{.RefreshSeconds}}s"
  hx-swap="outerHTML"
>
  ...
</div>
```

Each poll replaces the entire wrapper. A change to the title, colours or refresh interval is therefore picked up within one interval, with no page reload.

**Known issue:** when a browser restores `/clocks` as its selected tab at startup, the restored document can be painted without its subresources: no stylesheet and no scripts, so the page is unstyled and the clocks do not tick. DevTools shows `document.styleSheets.length` of 0 and no failed requests, and a reload fixes it. This is browser tab-restore/freeze behaviour rather than a server fault, and it is accepted as is: no client-side workaround is attempted (the page's own scripts never ran in that state). Opening the page with `serve --launch`, or reloading it, avoids the problem.

### 6.2 Styling

- Colour settings are passed to the template as CSS custom properties on the wrapper: `--colour-background`, `--colour-label`, `--colour-clock`, `--colour-utc`.
- The grid is fixed at three columns and three rows, with `grid-template-columns: repeat(3, 1fr)` and equal row heights. It fills the viewport.
- Values are validated before they reach the template. `html/template` escapes them for the style attribute.
- When no clocks are enabled, the grid area shows "No clocks are enabled."
- The `DSEG7 Classic` font is loaded with `@font-face` from `/static/fonts/DSEG7Classic-Bold.woff2`.

### 6.3 Clock tiles

- Each enabled clock renders as a label bar and a value.
- The value is `time.In(loc).Format("15:04")`, in 24-hour `HH:MM`.
- The UTC clock is the clock whose zone is exactly `UTC`. Its colour is the UTC colour. Labels are free text and are not used. Other zones, including `Etc/UTC`, `GMT` and `Europe/London`, get the clock colour. If more than one clock has zone `UTC`, all of them get the UTC colour.
- The colour class is decided in `internal/clock`, which is pure Go and easy to test.

### 6.4 Playwright-friendly markup

- `data-testid="clocks"` on the wrapper.
- `data-testid="clock"` on each tile, with `data-zone="<IANA ID>"` and `data-utc="true|false"`.
- `<time datetime="HH:MM">` around each value, so the time is machine-readable.
- `data-testid="stale"` on the stale indicator (section 6.5).
- Labelled form fields and stable element IDs, so tests can select elements without relying on layout.

### 6.5 Stale indicator

- If a poll fails (an HTTP error status, or a network failure), HTMX keeps the last good clocks display on screen.
- `app.js` then shows a small "STALE" indicator in a fixed corner. The indicator sits outside `#clocks`, so the polling replacement does not remove it.
- The indicator is hidden again after the next successful swap of `#clocks`.
- `app.js` also sets `shouldSwap = true` for `htmx:beforeSwap` responses with status 422 or 404 whose `Content-Type` is `text/html`. That is the HTMX 2 pattern for validation fragments and for the table pages' stale-record fragment. Plain-text 404s, such as `http.NotFound`, are not swapped.

## 7. Font

- **Font:** DSEG7 Classic, Bold weight.
- **Source:** the official DSEG release, using the WOFF2 file rather than a converted TTF.
- **Files:** `static/fonts/DSEG7Classic-Bold.woff2` and `static/fonts/DSEG-LICENSE.txt`.
- **Licence:** SIL Open Font License 1.1. The font is unmodified, so the reserved name "DSEG" is not affected. The OFL text ships with the font. The font is not sold by itself.
- **Usage:** upright, rendered directly from the `HH:MM` string. No unlit ("ghost") segments, to match the mock-up.
- The weight will be confirmed by a side-by-side render against the mock-up during implementation.

## 8. Configuration

The clocks page is configured entirely from the admin dashboard. There are no environment variables or config files. The command-line options are:

| Command / flag          | Default             | Purpose                                                                                           |
| ----------------------- | ------------------- | ------------------------------------------------------------------------------------------------- |
| `serve --addr`          | `127.0.0.1:8080`    | HTTP listen address. Set it to a VPN address to reach the clocks page from other machines (section 4.3) |
| `serve --db`            | `./noticeboard.db`  | SQLite file; created and migrated on start                                                        |
| `serve --log`           | `./noticeboard.log` | Audit log file (section 4.4)                                                                      |
| `serve --launch`        | off                 | Open the board in an app-mode window (section 9)                                                  |
| `open <address>`        | —                   | Open an already-running board in an app-mode window; does not start a server (section 9)          |
| `admin init <username>` | —                   | Create or replace the admin account; prompts for password (section 4.1)                           |
| `admin init --db`       | `./noticeboard.db`  | Same file as `serve --db`; created and migrated if absent                                         |

**Usage and exit codes**

- `--help` at the top level or on any subcommand prints usage and exits with status 0.
- No subcommand, an unknown subcommand, a missing argument (such as the username or the `open` address), or an unknown or invalid flag prints usage to stderr and exits with status 2.
- Runtime failures (such as a port in use, a database error, or mismatched passwords) print a message to stderr and exit with status 1.
- A normal `serve` shutdown exits with status 0 (section 2.2).

- **Clocks:** the `timezones` table.
- **Clock display settings:** the `settings` singleton (section 3.2).

## 9. Desktop mode

- `noticeboard serve --launch` starts the HTTP server, and once it is listening, opens `/clocks` (the clocks page) in a browser app-mode window.
- `noticeboard open <address>` opens an already-running clocks page at that address in a browser app-mode window. It does not start a server. The address is a host and port, or a full URL.
- The launcher tries, in order, Edge or Chrome in app mode on Windows; Chrome, Chromium, Brave or `xdg-open` on Linux; and Chrome or `open` on macOS.
- It requires no webview library. The launcher is plain Go, and the binary needs no CGO (section 2).

**Behaviour**

- The server runs until Ctrl-C or SIGTERM. Closing the window does not stop it, because the launcher does not track the browser process.
- On Linux, `xdg-open` cannot guarantee app mode. A normal browser tab is acceptable there.
- Chromium-family browsers are given a dedicated profile directory under the OS temporary directory, so app mode opens even when that browser is already running.
- If no browser is found, the URL is printed to stdout; `serve` keeps running. On Windows the search is for Edge or Chrome, on PATH or at their standard install locations, and there is no fallback to the default browser.
- The window opens only after the server is listening. A listen failure other than an address already in use exits with status 1 and no window opens.
- An address whose host is unspecified (`0.0.0.0` or `::`) is opened as `127.0.0.1`.
- If the listen address is in use, `serve` exits with status 1 and no window opens, unless `--launch` is given. With `--launch`, a message is printed and the clocks page is opened in a browser window, and the command exits with status 0. The address is assumed to be a running noticeboard. The command does not check this.

## 10. Build, run and tasks

Tools are managed by [mise](https://mise.jdx.dev/) (`mise.toml`).

| Task      | Purpose                                                                                                                       |
| --------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `run`     | Development server with reload on `.go` and `templates/` changes (wgo)                                                        |
| `run-gui` | As `run`, launching the GUI window. Each reload opens a new window; this is accepted for development                          |
| `build`   | `CGO_ENABLED=0 go build -o noticeboard .`                                                                                     |
| `test`    | `go test ./...`                                                                                                               |
| `fmt`     | `go fmt ./...`                                                                                                                |
| `tidy`    | `go mod verify && go mod tidy`                                                                                                |
| `release` | Build the Linux and Windows binaries (`dist`) and publish a GitHub release for an existing local tag with `gh release create` |

The Go version is pinned in the `[tools]` section of `mise.toml`.

**Repository housekeeping:** `.gitignore` ignores the built binary (`noticeboard`), the database and its rollback journal (`noticeboard.db`, `noticeboard.db-*`), the audit log (`noticeboard.log`), and the `sessions/` directory of pi session exports (also `pi-session-*`), plus `SCRATCH.md`.

## 11. Testing

Per the project guidelines. Every test builds its own database and server, so the non-browser tests call `t.Parallel()` and run concurrently; the browser tests run one at a time, because each launches its own Chrome.

- **Unit tests** use Go's built-in `testing` package with table-driven tests.
  - `internal/clock`: time formatting, and colour-class selection (exact zone `UTC` only), using an injected `now`.
  - `internal/settings`: validation rules (title trimming and length, colour format, refresh range, table page size).
  - `internal/zones`: label and zone trimming, uniqueness, rejection of `""` and `Local`, the nine-enabled limit, and move at the ends of the list; `ListPage` paging, clamping and totals.
  - `internal/paging`: `Clamp` at the ends, beyond the last page, the `LastPage` sentinel and an empty table.
  - `internal/admintable`: region grouping, and the timezones adapter's columns, defaults, raw cells, `Load`, create/update values and error mapping.
  - `internal/db`: migrations apply in order, once, and are idempotent; a newer `user_version` is refused.
  - `internal/auth`: password hashing, username validation and normalisation, and session storage.
  - `internal/auditlog`: the 1000-line cap removes the oldest lines.
- **Integration tests** use `httptest` against the routes, with a temporary SQLite file (`t.TempDir()`) and the real migrations applied.
  - Login (including the generic 422), logout, the session middleware, and the HTMX `401` + `HX-Redirect` path.
  - Zone CRUD, toggle, move, the 404 and 400 cases, and 422 validation responses.
  - Table pages: page parsing, the page size setting, the full page and fragment (`Vary: HX-Request`), the raw columns and Edit/Delete buttons, the prefilled new-record form, create, update, delete, validation and refusal, search (filtering, totals, literal wildcards, filter kept in pager and record URLs, no-match message), stale-record 404s, unknown slug, middleware, auditing, and a test-only second adapter proving the layer is generic.
  - Clocks page rendering with the current settings, including the empty message.
- **Browser tests** use `github.com/chromedp/chromedp` to drive headless Chrome against an `httptest` server. They skip when Chrome or Chromium is not installed (or with `-short`). They check `/clocks` (document title, the nine tiles, the clock colours that come from the stylesheet, and that the HTMX poll picks up a settings change without a reload); logging in with the login page's Log in button and logging out with the admin menu's Log out button (after which `/admin` redirects to the login page); the dashboard's settings status line (green `Settings saved`, red `Failed to save settings` with the field error, including the computed colours); every clock list action through its button (add, edit, toggle, move, and delete with the `hx-confirm` dialog accepted and dismissed); the sidebar links to the table page and back; a saved title appearing on `/clocks`; and the idle log-out, which uses the shortest login timeout twice and so takes about ten seconds. The markup in section 6.4 supports adding more later.
- **testify** is a test-only dependency, used only where it clearly simplifies an assertion.

## 12. Implementation plan

1. **Skeleton:** `go mod init`, `mise.toml`, `.gitignore`, `main.go` with `serve` and `admin` flags, `--help` and usage errors, embedded assets, placeholder `/`, `build` task.
2. **Database:** `internal/db` (open SQLite with the rollback journal, not WAL, apply migrations by `user_version`); `0001_init.sql` creating the four tables with `CHECK`, `UNIQUE` and `PRIMARY KEY` constraints, plus the settings and zone seeds.
3. **`internal/clock`:** time formatting and colour-class logic, test-first.
4. **`internal/settings`:** read, validate and update the singleton, with tests.
5. **Clocks page:** layout, clock grid fragment for polls, polling wrapper, CSS variables, font embedding, `app.js` (422 swap and stale indicator), `httptest` rendering tests.
6. **Admin authentication:** `admin init` command with bcrypt and username validation and normalisation; login against `admins` with the dummy-hash comparison; `sessions` rows and cookie handling; HTMX forms and `HX-Redirect`; middleware; logout; `HX-Request` check; login hint when no admin exists; `internal/auditlog` and request logging; tests against a temporary database.
7. **Clock management:** `internal/zones` repository and validation; list, add, edit, enable/disable, move, delete with `hx-confirm`; uniqueness, the nine-enabled limit, and validation messages.
8. **Settings UI:** form with colour pickers, number inputs and validation messages.
9. **Desktop mode:** `serve --launch` and `open <address>`.
10. **Polish:** test IDs, README run instructions (including the PocketBase reference, which is now wrong), mise tasks, the DSEG7 side-by-side check.

## 13. Decisions and assumptions

| Topic                       | Decision                                                                                                                                                   |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Web framework               | None: standard library `net/http` `ServeMux`                                                                                                               |
| Database                    | SQLite via `database/sql` and `modernc.org/sqlite` (pure Go, no CGO); file `./noticeboard.db`, rollback journal (WAL disabled), 5 s busy timeout           |
| Schema migrations           | Embedded SQL files, each in a transaction, applied in order; version in `PRAGMA user_version`; forward-only; newer database refused                        |
| Time zone data              | Embedded with `time/tzdata`                                                                                                                                |
| Admin account               | Single row in `admins`; no seeded account; created with `noticeboard admin init <username>`, which creates the database if absent and deletes all sessions |
| Login identifier            | Admin username: one or more printable characters with no white space; trimmed and lower-cased; not case-sensitive                                          |
| Password storage            | bcrypt, `DefaultCost` (`golang.org/x/crypto/bcrypt`); non-empty, at most 72 bytes                                                                          |
| Admin password entry        | Interactive no-echo prompt, entered twice (`golang.org/x/term`); never a command-line argument                                                             |
| Login failure               | Dummy-hash comparison when the username is unknown; generic 422 message                                                                                    |
| Session                     | Random 32-byte token (base64url in cookie); hex SHA-256 stored in `sessions`; no expiry, ends on logout, admin replacement, restart, or idle (the `login_timeout` setting); several allowed |
| CSRF                        | `HX-Request` required on admin `POST`/`DELETE`; all admin forms are HTMX forms, including login and logout                                                 |
| Lost session                | HTMX requests get `401` + `HX-Redirect`; others get `303` to login                                                                                         |
| Deployment                  | VPN; plain HTTP; no TLS; no login throttling                                                                                                               |
| Security headers            | `nosniff`, `X-Frame-Options: DENY`, CSP `default-src 'self'`; 64 KiB body limit; 10 s header timeout; graceful shutdown                                    |
| Audit log                   | `noticeboard.log` (`serve --log`); logins and mutating admin requests; capped at 1000 lines                                                                |
| Clock limit                 | Fixed 3×3 grid; at most nine enabled clocks; disabled clocks unlimited                                                                                     |
| Clock uniqueness            | Labels (trimmed) and zones are unique; zones `""` and `Local` rejected                                                                                     |
| Clock ordering              | Manual, via the unique `sort` field and move buttons; moving past either end is a no-op                                                                    |
| UTC clock                   | Exactly zone `UTC`; labels are not used                                                                                                                    |
| Configurable clock display  | Title, refresh interval, and four colours (background, label, clock, UTC); label text fixed white; title uses the label colour                             |
| Colour format               | `#rrggbb`, case-insensitive on input, stored lowercase                                                                                                     |
| Empty clocks page           | "No clocks are enabled."                                                                                                                                   |
| Live updates                | HTMX polling of the whole clock grid wrapper                                                                                                                    |
| Poll errors                 | Last good clocks display kept; "STALE" indicator outside `#clocks`, hidden on next successful swap                                                                   |
| Validation responses        | HTTP 422 with fragment; swap enabled in `app.js`                                                                                                           |
| Admin responses             | Success returns the clock list (`#zones`) and an out-of-band empty form region                                                                             |
| Admin table pages           | Generic `/admin/tables/{table}` routes over an `internal/admintable` adapter; unknown slug is 404 (section 5.3)                                              |
| Table grid                  | The table's own columns with raw values, and Edit and Delete per row                                                                                        |
| Table form                  | Every column except the ID; `enabled` and `sort` are edited here too; a new record uses the adapter's defaults                                              |
| Table search                | One `q` box filtering every column (contains, without case for ASCII, `%`/`_` literal), left of Add record; Enter applies it and the box's own clear (x) resets it |
| Table page size             | `settings.table_page_size`, 5–100, default 20, set on the dashboard; the count and rows are read in one transaction (section 3.2)                           |
| Stale table record          | 404 with the refreshed grid and a message; swapped because the response is `text/html`                                                                      |
| CLI                         | `--help` exits 0; usage errors exit 2; runtime errors exit 1                                                                                               |
| Desktop mode                | Browser app-mode via `serve --launch` or `open <address>`; no webview library; `serve` runs until Ctrl-C/SIGTERM                                           |
| Font                        | DSEG7 Classic Bold, self-hosted WOFF2, OFL                                                                                                                 |
| Scope                       | Clocks only; English only                                                                                                                                  |
| UTC label                   | "UTC" (mock-up says "G.M.T.")                                                                                                                              |
| Canada zone                 | `America/Vancouver` (matches the mock-up during PDT; an assumption)                                                                                        |
| Repository                  | `.gitignore` covers the binary, database and journal files, audit log, and `sessions/`                                                                     |
