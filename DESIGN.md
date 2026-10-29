---
name: File Uploader
description: Server-rendered file storage app on daisyUI defaults (light/dark)
colors:
  base-100: "oklch(100% 0 0)"
  base-200: "oklch(98% 0 0)"
  base-300: "oklch(95% 0 0)"
  base-content: "oklch(21% .006 285.885)"
  primary: "oklch(45% .24 277.023)"
  primary-content: "oklch(93% .034 272.788)"
  secondary: "oklch(65% .241 354.308)"
  secondary-content: "oklch(94% .028 342.258)"
  accent: "oklch(77% .152 181.912)"
  accent-content: "oklch(38% .063 188.416)"
  neutral: "oklch(14% .005 285.823)"
  neutral-content: "oklch(92% .004 286.32)"
  info: "oklch(74% .16 232.661)"
  info-content: "oklch(29% .066 243.157)"
  success: "oklch(76% .177 163.223)"
  success-content: "oklch(37% .077 168.94)"
  warning: "oklch(82% .189 84.429)"
  warning-content: "oklch(41% .112 45.904)"
  error: "oklch(71% .194 13.428)"
  error-content: "oklch(27% .105 12.094)"
typography:
  display:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "2.25rem"
    fontWeight: 700
    lineHeight: 1.2
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "1.875rem"
    fontWeight: 700
    lineHeight: 1.25
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.35
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 500
    lineHeight: 1.4
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  selector: "0.5rem"
  field: "0.25rem"
  box: "0.5rem"
spacing:
  xs: "0.25rem"
  sm: "0.5rem"
  md: "1rem"
  lg: "1.5rem"
  xl: "2rem"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-content}"
    rounded: "{rounded.field}"
    padding: "0.5rem 1rem"
  button-primary-hover:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-content}"
    rounded: "{rounded.field}"
    padding: "0.5rem 1rem"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.base-content}"
    rounded: "{rounded.field}"
    padding: "0.5rem 1rem"
  button-error-outline:
    backgroundColor: "transparent"
    textColor: "{colors.error}"
    rounded: "{rounded.field}"
    padding: "0.25rem 0.75rem"
  card:
    backgroundColor: "{colors.base-100}"
    textColor: "{colors.base-content}"
    rounded: "{rounded.box}"
    padding: "1.5rem"
  input:
    backgroundColor: "{colors.base-100}"
    textColor: "{colors.base-content}"
    rounded: "{rounded.field}"
    padding: "0.5rem 0.75rem"
  select:
    backgroundColor: "{colors.base-100}"
    textColor: "{colors.base-content}"
    rounded: "{rounded.field}"
    padding: "0.5rem 0.75rem"
  badge-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-content}"
    rounded: "{rounded.selector}"
    padding: "0.25rem 0.5rem"
  badge-neutral:
    backgroundColor: "{colors.neutral}"
    textColor: "{colors.neutral-content}"
    rounded: "{rounded.selector}"
    padding: "0.25rem 0.5rem"
---

# Design System: File Uploader

## Overview

**Creative North Star: "The Stock Workbench"**

This system is deliberately unbranded: it ships daisyUI's default light and dark themes with no custom palette, no custom font, and no bespoke component styling. The visual identity is the framework's defaults, because the project is a learning codebase and clarity of structure matters more than a distinctive look. Density is moderate and utilitarian: cards on a base-200 page background, 0.5rem box radius, 0.25rem field radius, and Tailwind's default 0.25rem spacing unit. Dark mode follows `prefers-color-scheme` and swaps the base surfaces to deep slate while keeping the same accent hues. The only custom CSS is the sidebar drawer animation and the top navigation progress bar.

**Key Characteristics:**
- daisyUI default light theme on white surfaces; dark theme on deep slate surfaces.
- Single system-font stack; no web font is loaded.
- Flat by default: elevation appears only on cards and the sticky navbar.
- Accent (primary indigo) used sparingly on primary actions and active states.
- 0.5rem box radius / 0.25rem field radius; 0.5rem selector radius.

## Colors

The palette is daisyUI's default: a deep indigo primary, a vivid pink secondary, a bright teal accent, and a charcoal neutral, plus semantic success/warning/error/info and a three-step neutral surface scale.

### Primary
- **Deep Indigo** (oklch(45% .24 277.023)): Primary actions, active sort links, primary badges, progress bar. Used on ≤1 button per form.

### Secondary (available, rarely used)
- **Vivid Pink** (oklch(65% .241 354.308)): Reserved for secondary emphasis; no current template uses it directly.

### Tertiary (available, rarely used)
- **Bright Teal** (oklch(77% .152 181.912)): Accent roles; no current template uses it directly.

### Neutral
- **Charcoal** (oklch(14% .005 285.823)): Neutral badges (role "user"), neutral surfaces in dark mode.
- **Pure White** (oklch(100% 0 0)): Base surface (light mode), card background.
- **Off White** (oklch(98% 0 0)): Page background (light mode).
- **Light Gray** (oklch(95% 0 0)): Borders, dividers, zebra rows.
- **Near Black** (oklch(21% .006 285.885)): Body text (light mode).

### Semantic
- **Sky Blue** (oklch(74% .16 232.661)): Info states.
- **Mint Green** (oklch(76% .177 163.223)): Success alerts.
- **Amber** (oklch(82% .189 84.429)): Warning icons (folder glyph).
- **Coral Red** (oklch(71% .194 13.428)): Errors, destructive actions, error text.

### Named Rules
**The One Accent Rule.** Primary indigo is reserved for the single primary action per surface; supporting actions use ghost or outline variants. Its rarity is the point.

## Typography

**Display Font:** System UI stack (-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif)
**Body Font:** Same system stack
**Label/Mono Font:** ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace (for code and metadata)

**Character:** Neutral, native, and fast: no web font is downloaded, so text renders in the visitor's OS face. Hierarchy comes from size and weight only, not from a second family.

### Hierarchy
- **Display** (700, 2.25rem, 1.2): Page titles on marketing-style screens (h1 text-4xl).
- **Headline** (700, 1.875rem, 1.25): Section titles (h1 text-3xl, h2 text-2xl).
- **Title** (600, 1.25rem, 1.35): Drawer headings, card titles.
- **Body** (400, 1rem, 1.5): Paragraphs and table cells; max line length ~65ch via max-w-prose.
- **Label** (500, 0.875rem, 1.4): Form labels, metadata, captions.
- **Mono** (400, 0.875rem, 1.5): Code and technical values.

### Named Rules
**The Native Face Rule.** No custom web font is loaded; hierarchy is carried by size and weight alone, so the UI matches the visitor's OS.

## Layout

The shell is a daisyUI drawer: a collapsible sidebar (w-14 collapsed, w-64 expanded) plus a sticky top navbar, with the page body in a `<main id="page-content">`. Content pages choose their own width: reading pages use a centered max-w-3xl column; admin and drive pages take the full remaining width. Spacing follows Tailwind's default 0.25rem unit; common rhythms are gap-4 (1rem), p-4 (1rem), py-10 (2.5rem). Breakpoints follow Tailwind defaults (sm 640px, md 768px, lg 1024px); the sidebar auto-opens at lg. Tables use fixed layout with explicit column widths so deep content never reflows the page. The admin account table requires a minimum width of 56rem and scrolls horizontally below that.

## Elevation & Depth

Flat by default. Depth comes from tonal layering: base-200 page background behind base-100 cards, with a 1px base-300 border on dashed empty states. Shadows are reserved for two roles: `shadow` (1px 3px) on cards and `shadow-sm` on the sticky navbar. No blur, glass, or backdrop-filter is used. Hover states change background (base-200/50), never shadow.

### Shadow Vocabulary
- **Card** (`box-shadow: 0 1px 3px 0 rgba(0,0,0,0.1), 0 1px 2px -1px rgba(0,0,0,0.1)`): Resting elevation of cards and modals.
- **Navbar** (`box-shadow: 0 1px 2px 0 rgba(0,0,0,0.05)`): Sticky top bar only.

### Named Rules
**The Flat-By-Default Rule.** Surfaces are flat at rest; a shadow appears only on cards and the sticky navbar. Hover and focus change color, never elevation.

## Shapes

Form language is gently rounded: 0.5rem on boxes (cards, badges, selectors) and 0.25rem on fields (inputs, buttons, selects). Borders are 1px base-300. Empty states use a dashed 1px border on a rounded-box. No clipping, no custom silhouettes, no pill shapes except badges via selector radius. Icons are 24px stroke icons from Lucide-style paths.

## Components

### Buttons
- **Shape:** 0.25rem radius (rounded-field), 1px border via daisyUI.
- **Primary:** Deep Indigo background, primary-content text, 0.5rem 1rem padding; used for the single primary action per form.
- **Hover / Focus:** Background darkens via daisyUI color-mix; focus ring 2px solid.
- **Ghost / Outline:** Transparent background with base-content or error text; used for cancel and destructive actions.
- **Error Outline:** Coral Red text on transparent, used for delete actions.

### Cards / Containers
- **Corner Style:** 0.5rem radius (rounded-box).
- **Background:** base-100 on a base-200 page.
- **Shadow Strategy:** Resting card shadow only.
- **Border:** None at rest; dashed base-300 border on empty states.
- **Internal Padding:** 1.5rem (card-body).

### Inputs / Fields
- **Style:** base-100 background, 0.25rem radius, 1px base-300 border (input-bordered).
- **Focus:** Border shifts to base-content; no glow.
- **Error / Disabled:** Error text below field in Coral Red; disabled dims to 20% opacity.

### Selects
- **Style:** Same as inputs, 0.25rem radius, bordered.

### Badges
- **Style:** 0.5rem radius (selector), solid primary or neutral background, small text.
- **State:** Primary for admin role, neutral for user role.

### Navigation
- **Style:** daisyUI menu in a collapsible drawer; icons only when collapsed with tooltips. Active state uses base-200/50 hover background. Admin section is a collapsible group that opens when the route is under /admin.

### Tables
- **Style:** Zebra-striped, fixed layout, small text. Header sort links show ▲/▼/↕.

### Alerts
- **Style:** Success alert (Mint Green) for save confirmations; error text (Coral Red) for form errors.

## Do's and Don'ts

### Do:
- **Do** use daisyUI semantic classes (btn, card, input, badge) rather than hand-rolled components.
- **Do** keep one primary button per form; use ghost/outline for the rest.
- **Do** use base-200/50 for hover backgrounds.
- **Do** keep the sidebar collapsed-state tooltips (data-tip) in sync with icons.
- **Do** support both light and dark via prefers-color-scheme.

### Don't:
- **Don't** introduce a custom color outside the daisyUI theme without updating this file.
- **Don't** load a web font; the system stack is the design.
- **Don't** add shadows to hover or focus states.
- **Don't** widen table columns with content; use fixed layout with truncation and tooltips.
- **Don't** remove the fake-latency behavior in production without checking the loading states.