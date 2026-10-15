---
name: alpinejs
description: Use when writing, modifying, or debugging Alpine.js in this project — x-data/x-show/x-for/x-model/x-bind/x-on directives, Alpine.data and Alpine.store, magic properties, or wiring Alpine into templ and htmx pages. Covers passing Go data into x-data and Tailwind/daisyUI class handling.
---

# Alpine.js

Alpine is a reactive layer driven by attributes in the markup. State lives in the DOM, not in a
component class. htmx moves HTML; Alpine reacts to it.

Pinned at **3.17.4**, loaded from `web/static/js/alpine.min.js` (the `dist/cdn.min.js` build) with
`defer` in `web/templates/index.templ`. The CDN build starts itself — never call `Alpine.start()`.

## x-data is the root of everything

Most directives do nothing without an ancestor `x-data`. It is a plain JS object; state, methods and
getters all belong there.

```templ
<div x-data="{ open: false, toggle() { this.open = !this.open } }">
	<button @click="toggle()">Toggle</button>
	<div x-show="open">Content</div>
</div>
```

- Children inherit it, including through nested `x-data`. The nearest matching property wins.
- `this` inside the object refers to the object — that is how methods mutate state.
- `x-data` with no value is valid (`<button x-data @click="...">`) when you need behavior, not state.

## Directives worth knowing

| Directive                                       | Notes                                                                               |
| ----------------------------------------------- | ----------------------------------------------------------------------------------- |
| `x-show`                                        | toggles `display`; the element stays in the DOM                                     |
| `x-if`                                          | on a `<template>`; adds/removes the element entirely                                |
| `x-for`                                         | **must** be on a `<template>`; that template must have **exactly one root element** |
| `x-model`                                       | two-way bind to form inputs                                                         |
| `x-bind` (`:`)                                  | set attributes from expressions                                                     |
| `x-on` (`@`)                                    | listen to DOM events                                                                |
| `x-text` / `x-html`                             | set textContent / innerHTML                                                         |
| `x-init`                                        | run once on init; supports `await`                                                  |
| `x-cloak`                                       | hide until Alpine loads — needs `[x-cloak]{display:none !important}` in CSS         |
| `x-ref` + `$refs`                               | grab an element instead of `querySelector`                                          |
| `x-ignore`                                      | skip a subtree                                                                      |
| `x-effect`, `x-id`, `x-teleport`, `x-modelable` | advanced; see docs                                                                  |

`x-cloak` is already handled: `web/app.css` has the rule. An alternative for one-off cases is
`<template x-if="true">`, which hides content until Alpine renders it.

## `:class` has special behaviour

`x-bind:class` **merges** rather than overwrites the `class` attribute — except in object syntax,
where it replaces the whole attribute:

```templ
<div class="opacity-50" :class="hide && 'hidden'">      // keeps opacity-50, adds/removes hidden
<div class="hidden" :class="{ 'hidden': !show }">       // Alpine owns the attribute entirely
```

That second form is the only way to have a class present before Alpine loads _and_ toggle it from
Alpine.

## Events

```templ
<button @click="$event.target.remove()">Remove</button>
<form @submit.prevent="save()">                             // .prevent, .stop, .once, .outside, .window, .document
<input @keyup.shift.enter="submit()">                       // key modifiers chain; kebab-case any KeyboardEvent.key
<div @foo.window="console.log('foo')">                      // listen on the window for cross-component chatter
<button @click="$dispatch('foo')">                          // dispatch a DOM event; anything can listen
```

Modifiers chain left to right. `$dispatch` sends a real DOM event, so htmx events can be observed
the same way: `@htmx:after-swap="..."`.

## Reuse: templ first, then Alpine.data

The docs' first recommendation for reusable markup is a backend template partial — **that is a
`templ` component here**, so extract a `templ` function before reaching for JS. When you need
reusable _behavior_ instead, register it once:

```js
document.addEventListener("alpine:init", () => {
  Alpine.data("dropdown", () => ({
    open: false,
    toggle() {
      this.open = !this.open;
    },
  }));
});
```

Then `x-data="dropdown"` anywhere. Global state uses `Alpine.store(...)` the same way, read through
`$store`:

```js
Alpine.store("uploads", {
  files: [],
  add(f) {
    this.files.push(f);
  },
});
```

Store objects are reactive; mutating `this.files` re-renders every consumer.

## Magic properties

`$el`, `$refs`, `$store`, `$dispatch`, `$nextTick`, `$watch`, `$root`, `$data`, `$id`, and
`$event` (inside `x-on` handlers).

## With templ

templ passes unknown attribute names through verbatim, so the shorthands work as written:

```templ
<button @click="open = !open">Toggle</button>       // fine
<button x-on:click="open = !open">Toggle</button>   // fine, same thing
<input :class="{ 'input-error': bad }"/>            // fine
```

The real trap is data _from Go_. A quoted attribute value is a literal string, so interpolate with a
templ expression rather than building JSON by hand:

```templ
<div x-data={ templ.JSONString(data) }>
```

`templ.JSONString` encodes any Go value and escapes it for attribute context. Do not `fmt.Sprintf`
the JSON — an unescaped quote in the data breaks the attribute.

## With htmx

Alpine starts a `MutationObserver` on `document` and calls `initTree` on every added node, so
markup swapped in by htmx **is initialised automatically** — no manual `Alpine.initTree` needed.

What does _not_ survive is state: if htmx replaces the element holding the `x-data`, Alpine destroys
that tree and its state with it. Two fixes:

- Keep `x-data` on an element **outside** the swapped fragment, and let the fragment read from it.
- Or use htmx's [alpine-morph](https://github.com/bigskysoftware/htmx-extensions/blob/main/src/alpine-morph/README.md)
  extension, which morphs instead of replacing and preserves Alpine state.

## Tailwind / daisyUI

- Static classes are fine anywhere; Tailwind scans `web/templates` (`@source` in `web/app.css`).
- For classes that only exist at runtime, keep the literals in the source:

```templ
<div :class="active ? 'btn-primary' : 'btn-ghost'">   // safe — both names appear in source
<div :class="'btn-' + variant">                        // broken — Tailwind never sees the name
```

- Prefer daisyUI component classes (`btn`, `input`, `card`, `modal`, `list`) over raw utility soup.

## Pitfalls

- **Missing `x-data`**: silently does nothing. Walk up the tree before debugging anything else.
- **`x-for` with multiple roots in the `<template>`**: Alpine throws. One root element only.
- **`x-for` without `:key`** when reordering: DOM nodes get reused against the wrong data.
- **Dynamic class strings**: see above — Tailwind drops them.
- **CSP**: the CDN build needs `unsafe-eval`. If you add a Content-Security-Policy, switch to
  `@alpinejs/csp` (pin it) and note that some inline expressions are unsupported.
- **`defer`**: without it you get a blank page and an "Unable to initialize" warning.

## Sources

- https://alpinejs.dev/start-here
- https://alpinejs.dev/essentials/state · /events · /installation
- https://alpinejs.dev/directives/data · /bind · /for · /on · /cloak
- https://alpinejs.dev/globals/alpine-store · /alpine-data
- https://alpinejs.dev/advanced/csp
- Source of truth for the htmx/observer claims: `packages/alpinejs/src/mutation.js` and
  `lifecycle.js` in https://github.com/alpinejs/alpine
