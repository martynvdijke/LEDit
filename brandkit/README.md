# LEDit Brand Kit

LEDit is a tiny broadcast terminal for live data. The identity treats every update like a signal on a classic LED matrix: crisp, bright, useful, and a little bit nerdy.

## Logo Files

The reusable source assets live in [`web/static/brand`](../web/static/brand/).

| Asset | Use |
| --- | --- |
| [`ledit-mark.svg`](../web/static/brand/ledit-mark.svg) | Square app icon, avatar, favicon, or compact signature |
| [`ledit-lockup-dark.svg`](../web/static/brand/ledit-lockup-dark.svg) | Primary lockup on dark panels and terminal-style surfaces |
| [`ledit-lockup-light.svg`](../web/static/brand/ledit-lockup-light.svg) | Lockup on light documentation and print surfaces |
| [`ledit-pattern.svg`](../web/static/brand/ledit-pattern.svg) | Quiet matrix texture for large backgrounds |
| [`brand.css`](../web/static/brand/brand.css) | Shared CSS tokens and application brand helpers |

The mark is an `L` rendered as lit matrix pixels. The cyan pixels read as a serial signal or terminal prompt; the amber diode is the single warm status light in an otherwise phosphor-green system.

## Palette

| Name | Hex | Role |
| --- | --- | --- |
| Ink | `#050806` | Deepest background and high-contrast text |
| Panel | `#08130D` | Main dark surface |
| Phosphor | `#B7FF35` | Primary action, active state, and LED glow |
| Phosphor Dim | `#6DA51D` | Borders, secondary emphasis, and light-surface accent |
| Signal Cyan | `#4DE8FF` | Links, data traces, and secondary status |
| Status Amber | `#FFB000` | Warnings, attention, and indicator lights |
| Paper | `#F4F7E8` | Warm light text on dark surfaces |
| Mist | `#84A28A` | Supporting text on dark surfaces |

Use Ink, Panel, and Paper as the structural colors. Phosphor is the hero color; Cyan and Amber are signals, not competing brand colors. Do not use all three as equal-sized fills.

## Type

Use a monospace face for display labels, headings, data, and the wordmark: `Space Mono`, `Courier New`, or the platform's `ui-monospace` fallback. Use the existing UI sans-serif for long-form copy and controls. Keep display text tight, uppercase when it behaves like a label, and sentence case for instructions.

## Spacing And Size

- Keep clear space around the mark equal to at least one LED pixel, or 8% of the mark width.
- Do not render the standalone mark below `24px` wide.
- Do not render the lockup below `160px` wide; use the mark alone when space is tighter.
- Keep the logo optically aligned to the LED grid rather than centered against a text baseline.

## Usage

- Use the dark lockup on `#050806`, `#08130D`, and other dark surfaces.
- Use the light lockup on white or pale documentation surfaces.
- Use the pattern at low opacity and at least 2x its source tile size; it should feel like hardware texture, not wallpaper.
- Keep the phosphor glow restrained. Bright pixels should signal state or focus, not decorate every surface.

Avoid stretching, rotating, recoloring the matrix pixels, adding drop shadows to the lockup, or placing the dark lockup directly on a busy image. Never redraw the `L` with a smooth font; the pixel construction is the recognizable part of the mark.

## Tone

Write like a well-labeled control panel: direct, curious, and lightly playful. Prefer `SIGNAL CONNECTED`, `NEXT FRAME`, and `DISPLAY ONLINE` over generic marketing language. Geeky is good; faux-hacker jargon and forced nostalgia are not.
