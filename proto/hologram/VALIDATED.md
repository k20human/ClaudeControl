# Hologram parameters — validated by eye, 2026-08-25

Throwaway prototype. These numbers carry over to the real `hologram` module;
the code does not.

| Parameter | Value | Exposed as |
|---|---|---|
| flow speed | 0.18 | slider, 0.02 – 1.00 |
| trail persistence | 0.90 | slider, 0.50 – 0.99 |
| density | auto: 1 particle per 22 dots of disc area | slider as a multiplier, 0.25× – 3× |
| global rotation | 0.09 rad/s | slider |
| breathing period | 9 s | slider, 0 disables |
| renderer | braille (2x4 dots/cell) | picker: braille / half-block |

Density must stay a function of pane area, never a constant: a fixed particle
count saturates a small pane and looks empty in a large one.
