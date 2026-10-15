---
name: templ
description: Use when writing, modifying, or debugging a-h/templ code — .templ files, templ generate, templ fmt, templ.Component, Go HTML rendering, fragments, or integrating templ with net/http and htmx. Covers syntax, components, attributes, CSS classes, scripts, context, and testing.
---

# templ

templ is a type-safe HTML templating language for Go. `.templ` files compile to Go code with `templ generate`; each component is a Go function returning `templ.Component`.

```templ
package views

templ Hello(name string) {
  <h1 data-testid="hello">Hello, { name }</h1>
}
```

```go
type Component interface {
	Render(ctx context.Context, w io.Writer) error
}
```

`reference.md` in this directory is the full templ documentation dump — read it for the complete CLI, IDE/LSP setup, React and Datastar integration, deployment and migration guides.

## Workflow (do this every time)

1. Edit the `.templ` file.
2. Run `templ generate` — without it the generated Go is stale, and the build fails or renders old HTML.
3. Run `templ fmt .` to format.
4. Commit the generated `*_templ.go` files alongside the `.templ` sources.
5. CI check that generation is up to date: `templ generate && git diff --exit-code`.

| Command          | Purpose                                                                                 |
| ---------------- | --------------------------------------------------------------------------------------- |
| `templ generate` | Compile `.templ` → Go. `-f header.templ` for one file, `-watch` to regenerate on change |
| `templ fmt .`    | Format templates (`-fail` exits 1 if changes were needed, for CI)                       |
| `templ lsp`      | Language server used by editor integrations                                             |
| `templ info`     | Print environment details, useful when debugging LSP issues                             |

Install (Go 1.24+): `go install github.com/a-h/templ/cmd/templ@latest`. Per-project alternative: `go get -tool github.com/a-h/templ/cmd/templ@latest`, then run `go tool templ`. Docker image: `ghcr.io/a-h/templ:latest`.

Live reload: `templ generate --watch --proxy="http://localhost:8080" --cmd="go run ."` regenerates Go on `.templ` edits, restarts the `--cmd` process on Go edits, and reloads the browser through a proxy on `:7331` (no F5 needed). External watchers (air, wgo) can trigger a reload with `templ generate --notify-proxy`. The reload script is only injected when the response is `text/html` and contains a `</body>` tag.

## File anatomy

`.templ` files are ordinary Go files (package, imports, types, `var`, `func`) plus `templ` and `script` blocks.

```templ
package views

import "fmt"

var greeting = "Hello" // ordinary Go

templ hello(name string) {
  <p>{ greeting }, { name }</p>
}
```

- Components are exported when the name starts with an uppercase letter, and are shared/imported like any Go function.
- Code-only components: `templ.ComponentFunc(func(ctx context.Context, w io.Writer) error { ... })` (escape output yourself, e.g. `templ.EscapeString`).
- Method components are allowed: `templ (d Data) Method() { ... }`.

## Elements and expressions

```templ
templ button(name, content string, enabled bool) {
  <button value={ name } disabled?={ !enabled }>{ content }</button>
  <img src="logo.png"/>
}
```

- All elements must be closed (`</a>` or self-closing `<hr/>`); void elements render without `/`.
- `{ expr }` interpolates strings, numbers, booleans, or functions returning `(value)` / `(value, error)`. Errors surface from `Render` with source location.
- Interpolated values are HTML-escaped automatically.
- templ minifies HTML output.
- `templ.Raw(trustedHTML)` bypasses escaping — trusted sources only.
- Text that starts with `if`, `for`, or `switch` is parsed as Go; to print such text, use a string expression (`{ "for sale" }`) or capitalize the word.
- HTML comments render to output; use Go comments outside templ blocks.

## Control flow

Standard Go `if`/`else`, `switch`, and `for` work inside components, plus scoped raw Go with `{{ ... }}`:

```templ
templ list(items []Item) {
  {{ count := len(items) }}
  <p>{ count } items</p>
  if count == 0 {
    <p>Empty</p>
  } else {
    <ul>
      for i, item := range items {
        <li class={ templ.KV("first", i == 0) }>{ item.Name }</li>
      }
    </ul>
  }
}
```

## Composition

```templ
templ layout(contents templ.Component) {
  <div id="wrapper">
    @header()
    @contents
  </div>
}

templ wrap() {
  <div id="wrapper">
    { children... }
  </div>
}

templ page() {
  @layout(paragraph("Hello"))
  @wrap() {
    <p>Passed in from the caller</p>
  }
}
```

- `@component(args)` renders a component; `@component` renders a `templ.Component` value.
- `{ children... }` renders children passed via `@wrapper() { ... }`; from Go use `templ.WithChildren` / `templ.GetChildren` / `templ.ClearChildren`.
- `templ.Join(a, b)` concatenates components.
- `@templ.Fragment("name") { ... }` marks a renderable subsection (see Fragments below).

## Attributes

| Form                        | Syntax                                                                  |
| --------------------------- | ----------------------------------------------------------------------- |
| Constant                    | `<p data-testid="x">`                                                   |
| Expression                  | `<p data-testid={ id }>`                                                |
| Boolean (true when present) | `<hr noshade/>`, `<hr noshade?={ false }/>`                             |
| Conditional                 | `if cond { class="active" }` inside the open tag                        |
| Dynamic key                 | `<p { "data-" + key }="value">`                                         |
| Spread                      | `<p { attrs... }>` where attrs is `templ.Attributes` (`map[string]any`) |

```templ
<hr style="padding: 10px"
  if isActive {
    class="itIsTrue"
  }
/>
```

- URL attributes (`href`, `src`, `action`) sanitize dynamic values; use `templ.SafeURL` to bypass.
- Non-standard URL attributes (e.g. htmx `hx-get`) are **not** sanitized by default — wrap with `templ.URL(...)`.
- Dynamic attribute keys skip the special handling for `href`/`on*` — they are treated as plain strings.
- JSON attribute values (htmx `hx-vals`, Alpine `x-data`) are set by marshaling to a string: `x-data={ templ.JSONString(data) }`.
- `on*` attributes expect a `script` template reference — see Scripts.

## class and style

```templ
<button class={ "button", templ.KV("is-primary", isPrimary) }>Go</button>
<div style={ templ.KV("border-color: red", hasError), "padding: 0.5em" }>Text</div>
```

- `class={ ... }` accepts strings, `templ.KV(name, bool)`, and `map[string]bool`; `templ.KV(v, cond)` emits `v` only when `cond` is true. Unsafe class names are replaced with `--templ-css-class-safe-name`; `templ.SafeClass` bypasses the check (still escaped).
- `style={ ... }` accepts strings, `templ.SafeCSS`, `map[string]string`, `map[string]templ.SafeCSSProperty`, and `KV` variants. Dynamic CSS is sanitized (unsafe parts become `zTemplUnsafeCSSPropertyName` / `zTemplUnsafeCSSPropertyValue`); wrap trusted CSS in `templ.SafeCSS` / `templ.SafeCSSProperty`.
- `css name() { ... }` elements define scoped classes usable in class expressions: `templ.KV(red(), isPrimary)`. The generated class name is unstable (autogenerated) and the `<style>` is emitted once per HTTP request.
- A plain `<style>` element renders verbatim; use a once-handle (below) to emit it only once per page.

## Context

Components receive the context passed to `Render`; inside a component it is available as the implicit `ctx` variable.

```templ
templ showTheme() {
  <div class={ GetTheme(ctx) }>Display</div>
}
```

- Prefer explicit parameters for most data; context is for cross-cutting data (current user, locale, theme).
- Provide type-safe getters (`func GetTheme(ctx context.Context) string`) instead of reading keys in templates.
- A missing key or bad type assertion panics at render time.
- Middleware pattern: put values into `r.Context()` with `context.WithValue`, then `next.ServeHTTP(w, r.WithContext(ctx))`.

## HTTP integration

```go
// Static component as handler:
http.Handle("/", templ.Handler(hello()))

// Options:
templ.Handler(notFound(), templ.WithStatus(http.StatusNotFound))
templ.Handler(comp, templ.WithContentType("text/html; charset=utf-8"))
templ.Handler(comp, templ.WithErrorHandler(func(r *http.Request, err error) http.Handler { ... }))

// Dynamic data: render directly.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    h.view(page(h.model(r))).Render(r.Context(), w)
}
```

Good components are idempotent and take data as parameters; keep database/API calls in handlers, not templates. Prefer view models that mirror the visual structure (one field per thing displayed, plus validation methods) so rendering is testable without integration setup.

Static output: `Render` to any `io.Writer` — a file or buffer — to generate HTML for static hosting, PDF conversion or email.

## Fragments (htmx-friendly)

Render a subsection of a page while the whole template still executes:

```templ
templ Page() {
  <div>Header</div>
  @templ.Fragment("results") {
    <div id="results">...</div>
  }
}
```

```go
// Handler: render only the fragment when requested.
templ.Handler(Page(), templ.WithFragments("results"))

// Outside an HTTP handler:
templ.RenderFragments(ctx, w, Page(), "results")
```

Nested fragments are included when the outer fragment is selected. Fragment identifiers may be typed keys to avoid collisions. When no fragment matches, no output is produced.

## htmx integration

templ just renders HTML — htmx attributes are ordinary attributes. For htmx behavior itself, see the **htmx-guidance** skill.

```templ
templ loginForm(err string) {
  <form hx-post="/htmx/login" hx-target="closest form" hx-swap="outerHTML">
    if err != "" {
      <p class="error">{ err }</p>
    }
    <input type="email" name="email" required/>
    <input type="password" name="password" required/>
    <button type="submit">Login</button>
  </form>
}
```

Key points:

- Endpoints for `hx-*` should return HTML components (often fragments), not JSON.
- Dynamic `hx-*` URLs: `hx-get={ templ.URL(fmt.Sprintf("/items/%s", id)) }`.
- Inline handlers: `hx-on:click="alert('Hi')"` for static JS, `hx-on:click={ templ.JSFuncCall("showMessage", msg) }` for Go data.
- `hx-select="#id"` lets a handler return a full page while htmx extracts part of it; `@templ.Fragment` is the templ-native alternative.
- Use `data-testid` attributes to keep tests stable.
- Match server status handling: e.g. return 422 with an error component and use `hx-status:422` on the client.

## Scripts and JavaScript

```templ
script showMessage(msg string) {
  alert(msg);
}

templ page(msg string) {
  <button onclick={ showMessage(msg) }>Show</button>
  <button hx-on:click={ templ.JSFuncCall("showMessage", msg) }>Show</button>
  <script>
    function showMessage(msg) { alert(msg); }
  </script>
}
```

- `script` templates are a legacy feature; prefer external JS files plus `templ.JSFuncCall`, `templ.JSONScript`, `templ.JSONString`.
- Arguments passed to script templates and `JSFuncCall` are JSON encoded. `templ.JSExpression("event")` passes JavaScript expressions like `event` or `this`; it bypasses encoding, so never use it with untrusted data.
- Pass Go data to the client in attributes with `templ.JSONString(data)` (e.g. Alpine `x-data`) or as `<script type="application/json">` with `templ.JSONScript("id", data)`.
- Inside `<script>`/`<style>` elements use `{{ expr }}` interpolation (escaped; JSON-encoded outside JS strings).
- `templ.JSUnsafeFuncCall` bypasses script sanitization — escape hatch only.

## Render once

`templ.NewOnceHandle()` + `.Once()` renders a `<script>`/`<style>`/`<link>` exactly once per render context; declare the handle once at package level.

```templ
var helloHandle = templ.NewOnceHandle()

templ hello(label, name string) {
  @helloHandle.Once() {
    <script>
      function hello(name) { alert('Hello, ' + name + '!'); }
    </script>
  }
  <input type="button" value={ label } data-name={ name }/>
}
```

Never call `templ.NewOnceHandle().Once()` inline — a new handle per call means the content renders every time. Export a component that owns the handle to share once-only scripts/styles across packages.

## Security

- Interpolated values are HTML-escaped; `templ.Raw` is the only markup bypass and must only receive trusted input.
- URL attributes sanitize protocols; `templ.SafeURL` bypasses. Non-standard URL attributes such as `hx-*` need `templ.URL(...)` explicitly.
- CSS values and class names are sanitized; bypasses (`templ.SafeCSS`, `templ.SafeCSSProperty`, `templ.SafeClass`) are for trusted, computed values.
- Content Security Policy: `templ.WithNonce(ctx, nonce)` adds a nonce to rendered script tags so inline `script` templates work under a strict CSP. Generate the nonce with a cryptographically secure RNG and send the matching `Content-Security-Policy` header yourself.

## Testing

Expectation testing with `goquery` (pipe the rendered output, assert with `data-testid` selectors):

```go
r, w := io.Pipe()
go func() {
    _ = headerTemplate("Posts").Render(context.Background(), w)
    _ = w.Close()
}()
doc, err := goquery.NewDocumentFromReader(r)
if doc.Find(`[data-testid="headerTemplate"]`).Length() == 0 {
    t.Error("expected header to render")
}
```

- HTTP handlers: `httptest.NewRecorder()` + `httptest.NewRequest()`, then parse `w.Result().Body` with goquery.
- Snapshot testing: `generator/htmldiff.Diff(component, expectedHTML)` (requires `prettier` on `PATH`).
- Add `data-testid` attributes to components to make selectors stable.
- Test rendering at the component level and wiring at the handler level; don't re-assert the same details at both.

## Interop

- `html/template` inside templ: `@templ.FromGoHTML(goTemplate, data)`.
- templ inside `html/template`: `templ.ToGoHTML(ctx, component)` returns the component as `template.HTML`.

## Common mistakes

- Forgetting `templ generate` after editing a `.templ` file.
- Starting body text with `if`/`for`/`switch` — parser error; use a string expression.
- Not closing tags — templ requires explicit closing or self-closing syntax.
- Passing dynamic `hx-*` URLs without `templ.URL` (no sanitization) or using `templ.Raw` on untrusted input.
- Reading context keys directly in templates; use type-safe getters to avoid panics.
- Calling `templ.NewOnceHandle().Once()` inline instead of using a package-level handle.
- Relying on generated `css` class names — they are unstable.
- Doing DB/API work inside components instead of handlers/view models.

## Instructions for Claude

When writing templ code:

1. Write `.templ` files (not `html/template`) when the project uses templ.
2. After any `.templ` edit, tell the user to run (or run) `templ generate`; never assume generated files are current, and never hand-edit `*_templ.go`.
3. Components are functions returning `templ.Component`; render with `@Component(args)` and pass data explicitly (or via context for cross-cutting concerns).
4. Htmx endpoints must return HTML components/fragments, not JSON.
5. Wrap dynamic `hx-*` URLs in `templ.URL(...)`; use `templ.KV` for conditional classes and `templ.WithFragments` for fragment-only responses.
6. Prefer `data-testid` + goquery for tests, and view models to keep templates logic-light.
7. Check `reference.md` in this skill directory before answering detailed questions about CLI flags, editor setup, or deployment.
