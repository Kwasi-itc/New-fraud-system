# Design Guidelines

## Product Direction

This frontend should feel like an enterprise-grade fraud, AML, and investigation platform.
The UI should read as operational software, not a marketing site.

Core qualities:

- Clean
- Professional
- Calm
- Structured
- Fast to scan
- Minimal but not empty

## Brand and Tone

- Use `Inter` as the primary typeface.
- Prefer black, white, slate, and cool gray as the base palette.
- Use blue accents, not purple accents.
- Accent color should feel controlled and product-oriented, not playful.
- IT Consortium branding should use the real logo asset from `public/` where appropriate.

## Color System

Base direction:

- Backgrounds: white or very light blue-gray
- Main text: near-black or dark slate
- Borders: soft gray
- Active states: light blue background with strong blue text/icon
- Hover states: subtle gray or blue-tinted fills

Preferred accent behavior:

- Primary accent: blue spectrum
- Use blue for active nav states, focused controls, highlighted tabs, and important actions
- Avoid violet or purple unless explicitly requested

Current token direction is defined in:

- [src/app/globals.css](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\frontend\src\app\globals.css)

## Layout Principles

- The authenticated sidebar should feel attached to the left wall.
- Avoid floating app-shell cards when the interface should feel structural.
- Main content can breathe, but the shell should feel anchored.
- Use route groups to separate app areas clearly.
- Let individual pages own their own headings rather than forcing a global page-title bar.

Current route structure direction:

- `(auth)` for sign-in and access screens
- `(authenticated)` for logged-in product areas

## Border Radius and Surfaces

- Do not overuse large rounded corners.
- Prefer restrained radii for enterprise surfaces.
- Reserve softer rounding for cards, pills, and action tiles where it adds clarity.
- Sidebar and major layout surfaces should be less rounded than marketing-style UI.

## Sidebar Pattern

The sidebar should follow these rules:

- Top: logo/brand
- Primary navigation directly beneath logo
- Secondary navigation pinned toward the bottom
- Support collapsed and expanded states
- Active item uses a light blue background and blue icon/text
- Labels should be short and operational

Current preferred navigation language:

- `Detection`
- `Case Manager`
- `Customer hub`
- `Your Data Model`
- `Settings`
- `My Account`

## Page Design Pattern

Pages should generally follow one of these modes:

1. Operational dashboard
2. Workspace/detail view
3. Builder/configuration page

Guidelines:

- Dashboards can use stronger contrast and denser information.
- Builder/configuration pages should feel lighter and cleaner.
- Settings and account pages should be calm and minimal.
- Avoid reusing the same card treatment everywhere without adjusting density and tone.

## Forms and Auth

- Login should feel compact and controlled.
- Avoid oversized hero typography.
- Use simple centered layouts with clear hierarchy.
- Prefer real brand assets over placeholder marks.
- Keep form spacing tight but not cramped.

## Tabs and Toggles

- Tabs must be interactive, not static visual pills.
- Active tabs should use the blue accent.
- Inactive tabs can use blue text on a pale background or neutral background with blue hover.
- Use tabs for mode switching only when content actually changes.

## Data/Builder Screens

For pages like `Your Data Model`:

- Use compact titles
- Use pill-style mode switchers
- Use light builder-style surfaces
- Use dashed borders for creation/import actions when appropriate
- Keep the layout airy and modular

Reference direction already established in:

- [src/app/(authenticated)/your-data/page.tsx](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\frontend\src\app\(authenticated)\your-data\page.tsx)

## Components

The current UI layer is a lightweight shadcn-style base, not a full generated registry setup.

Existing base components live in:

- [src/components/ui/button.tsx](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\frontend\src\components\ui\button.tsx)
- [src/components/ui/card.tsx](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\frontend\src\components\ui\card.tsx)
- [src/components/ui/input.tsx](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\frontend\src\components\ui\input.tsx)
- [src/components/ui/badge.tsx](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\frontend\src\components\ui\badge.tsx)

When extending components:

- Keep APIs simple
- Avoid decorative complexity
- Prefer consistency over novelty
- Use comments sparingly

## What To Avoid

- Purple-forward accents
- Overly rounded surfaces everywhere
- Oversized headers that feel like a landing page
- Unanchored floating layout shells
- Generic startup-style gradients and glassmorphism
- Decorative UI that reduces operational clarity

## Working Rule

When a new screen is added, match the established product language first:

- enterprise before trendy
- clarity before flair
- structure before decoration
- blue before purple
