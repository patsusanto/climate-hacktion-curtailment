# Household page style

Use this for new pages in `site/`. The arena app in `frontend/` is a different theme. Do not mix the two.

## Fonts

Load from Google Fonts: `Trocchi` for headlines, `DM Sans` for everything else.

Headlines are weight 400, not bold. Body is 16px, line-height 1.45.

```css
--serif: "Trocchi", Georgia, serif;
--sans: "DM Sans", Georgia, sans-serif;
```

## Color

Use only these tokens. Do not invent a purple gradient or a dark hero.

| Token | Hex | Use |
|---|---|---|
| `--field` | `#f4edfc` | page background |
| `--ink` | `#0a0416` | text, borders, rules |
| `--kiwi` | `#ccff67` | the one loud fill: selected chip, winning action, primary button |
| `--purple` | `#d3abff` | offset shadow only, never a fill |
| `--grey` | `#736c82` | eyebrows, captions, axis labels |
| `--blue` | `#93ffff` | bright-roof chip |
| `--yellow` | `#ffff9a` | spike chip |
| `--pink` | `#f888f5` | import |
| `--card` | `#f9f9f9` | panels on the lavender |

## Type

- Eyebrow: DM Sans, 12px, uppercase, letter-spacing `0.14em`, color `--grey`.
- `h1`: Trocchi, `clamp(40px, 6vw, 64px)`, letter-spacing `-0.035em`, line-height `1.02`.
- `h2`: Trocchi, `clamp(32px, 4.2vw, 48px)`, letter-spacing `-0.03em`, line-height `1.05`, max-width about 20 characters.
- Body: DM Sans, 18px for a lede, 15px for notes. Notes use `--grey`.

## Controls

A primary chip is `--kiwi` fill, 1px `--ink` border, shadow `4px 4px 0 #d3abff`, padding `0.4rem 0.75rem`, no border-radius.

A selected small control uses the same colors with shadow `2px 2px 0 #d3abff` and a full pill radius.

Cards are `--card` with a 1px border at 14% ink. Buttons and inputs inherit the body font. No blue focus rings, no drop shadows, no glass.

## Layout

Content padding is `3.5rem 7vw 2rem`. On a viewport under 900px wide, stack columns and drop the padding to `2.2rem 1.1rem 2.5rem`.

If `prefers-reduced-motion: reduce`, show the finished state and do not animate charts.

## Charts

Axis lines are ink at 12% opacity. Tick labels are 11px DM Sans in `--grey`. The real price is `--ink`. The estimate band is `--grey`. The rolling savings line is `--kiwi` drawn as a stroke, not a fill.
