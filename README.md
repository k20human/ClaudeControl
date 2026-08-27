# ClaudeControl

A terminal control centre for [Claude Code](https://claude.com/claude-code):
several live sessions in one window, beside the things you need while you work
— the services your stack runs on, what your account has spent, and a panel
that shows what Claude is doing.

It is a multiplexer specialised for one job rather than a general one. There is
no tmux underneath: panes, sessions, mouse and keyboard are its own.

```
┌ claude ─────────────────────────────┬─────────────────────────────┐
│ > refactor the session pool         │      ⢠ ⣀        ▸ following │
│                                     │   ⣀     ⠁  ⠐⠂     the thread│
│                                     │                             │
│                                     ├ services ───────────────────┤
│                                     │ ▸ start  ⟳ restart  ■ stop  │
│                                     │ [x] api      running  pid … │
│                                     │ [x] web      running  pid … │
│                                     │ [ ] mailhog  stopped        │
└─────────────────────────────────────┴─────────────────────────────┘
  + new  × close  ◫ list  ? help    usage 5h 42% · 7d 15%    ⏻ quit
```

## Requirements

Go 1.26 or later, and Linux. The service supervisor reads `/proc`, so that part
is Linux-only by construction; the rest would probably port, but has not been
tried elsewhere.

## Build and run

```sh
git clone https://github.com/k20human/ClaudeControl.git
cd ClaudeControl
go build -o bin/claudecontrol ./cmd/claudecontrol

./bin/claudecontrol -config examples/control-centre.yaml
```

With no `-config`, it reads `$XDG_CONFIG_HOME/claudecontrol/config.yaml` —
`~/.config/claudecontrol/config.yaml` unless you have said otherwise. There are
ready-made files in [`examples/`](examples) to copy from:

| File | What it shows |
|---|---|
| `control-centre.yaml` | Everything at once |
| `local-stack.yaml` | A session beside the services it works on |
| `two-claude.yaml` | Two Claude sessions side by side |
| `shells.yaml` | Plain terminals, no Claude |
| `tabs.yaml` | Two panes of tabs, for a narrow window |
| `hologram.yaml` | The animated panel on its own |

```sh
go test ./... -race
```

---

# The configuration file

One YAML file describes the whole window. It has a single required key,
`layout`, which is a tree: either a **pane** running one module, or a **split**
holding other nodes.

## A pane

```yaml
layout:
  module: claude          # which module fills the pane
  options:                # what that module accepts — see below
    dir: ~/projects/api
```

`options` is optional, and its contents belong entirely to the module. An
unknown module name is an error that names the ones that exist, rather than an
empty pane you have to work out for yourself.

## A split

```yaml
layout:
  split: horizontal       # horizontal = side by side, vertical = stacked
  ratios: [3, 2]          # relative shares, one per child
  children:
    - module: claude
    - module: hologram
```

`ratios` may be omitted, in which case the children share the space equally. It
must otherwise have exactly one entry per child. Splits nest, so a column of
panes beside another pane is a split whose second child is itself a split:

```yaml
layout:
  split: horizontal
  ratios: [3, 2]
  children:
    - module: claude
      options: { dir: ~/projects/api }

    - split: vertical
      ratios: [2, 3]
      children:
        - module: hologram
        - module: supervisor
          options:
            services:
              - { name: api, cmd: [npm, run, dev], dir: ~/projects/api }
```

A pane is never smaller than 8 columns by 4 rows — one of those rows is its
title. A split that cannot give every child that much is not laid out at all,
and the previous arrangement is kept.

## Paths

Every `dir` accepts `~` and environment variables, because a configuration file
is written by hand: `~/projects/$USER/api` resolves the way you would expect.
Everything else is taken literally.

## Editing it while it runs

`alt+,` opens the settings menu, built from what the module in the focused pane
publishes — nothing in that menu is written per module. Saving writes the
values back into this file **through its syntax tree**, so comments, ordering
and formatting all survive. A file you have commented is still your file after
the application has written to it.

---

# The modules

## `claude` — a Claude Code session

```yaml
- module: claude
  options:
    dir: ~/projects/api      # working directory
    bin: claude              # the executable, if it is not `claude` on the PATH
    resume: <session-id>     # resume an existing session instead of starting one
    args: [--model, opus]    # extra arguments, appended
```

**Conversations come back.** Every run records the Claude sessions that were
open, in the order their panes appear, and the next run hands them out in that
order — each pane starts with `--resume` on the conversation it had. A session
that was opened and never spoken to has no transcript and is quietly dropped:
resuming it would fail with an error about something you did not do.

What is recorded is the conversation **Claude Code is running now**, which is
not always the one the pane started with. Resuming — from here or with
`/resume` inside the session — makes Claude Code write a new transcript under
a new id, and the id we handed it stops describing anything. The hooks say
which pane they came from on their own command line, so the pane's identity
never moves while Claude Code's does, and everything keyed on it — the state
mark, the token counts, the conversation to bring back — keeps describing
what is actually there. Rearrange
the panes between runs and a conversation lands in a different one, which is
predictable and far better than losing it. A `resume` written here by hand
always wins: it was put there on purpose.

The session is given a stable identity, so closing its pane does not end it and
reopening finds it again. Hooks are wired automatically, through `--settings`
on that session alone — **your global `settings.json` is not touched**. That is
how the rest of the interface knows when a session is thinking, waiting for
you, or done, and how the bar comes to say `N waiting`.

The pane is named after the session: Claude Code gives each one a title, and
that title is what appears above the pane rather than the directory.

## `term` — any other command

```yaml
- module: term
  options:
    cmd: [zsh]               # defaults to your $SHELL
    dir: ~/projects
```

A plain hosted terminal, for a shell beside a session or anything that is not
Claude.

## `supervisor` — the processes your stack runs on

Starts, stops and restarts long-lived services, and shows what each one is
doing. Three modules touch processes and are worth telling apart: `tasks` runs
a command **once**, `services` asks whether something already running
**answers**, and this one **owns** the processes.

```yaml
- module: supervisor
  options:
    stop_grace: 5            # seconds before SIGKILL, default 5
    services:
      - name: api            # required
        cmd: [npm, run, dev] # required
        dir: ~/projects/api  # where to run it
        env: [PORT=3000]     # added to the environment
        autostart: false     # start it when the pane appears, default false
        optional: false      # leave it unticked, default false
```

**Nothing starts on its own** unless `autostart: true`. Press the start button,
or `s`, and every ticked service goes up together.

**`optional: true`** leaves a service listed but unticked, so "start" means your
usual set rather than everything that exists. Declaring it alongside
`autostart` is refused rather than resolved quietly — one says launch it now,
the other says leave it out.

**A service that dies stays dead**, showing its exit code. Resurrecting it
would hide the failure behind a row reading `running` while nothing works.

**Services already running are adopted.** The module walks `/proc` every five
seconds, and on demand with `⟲ scan`, and takes over any process whose working
directory *and* command line both match — the directory alone would not do,
since a project can run a front end and a back end at once. A service you
started in another terminal can then be watched, stopped and restarted from
here. Its **output cannot be read**: that output went wherever it was going
before the module found it. Restarting brings the service under supervision,
and its output with it.

**Stopping signals the whole process tree, never the process group.**
`npm run dev` is a launcher whose real server is one of its children, and
signalling only the child leaves that server holding its port. A service
adopted from a shell also shares that shell's process group, so signalling the
group would kill the terminal you are sitting in.

In the pane: click a name for its output, or a checkbox to tick it. From the
keyboard, `↑ ↓` moves, `space` ticks, `a` ticks everything or nothing, `enter`
opens the output, and `s` `r` `x` start, restart and stop the ticked ones.

## `services` — is it up?

Health checks for things you did not start. Each check is a command, an HTTP
URL or a TCP address.

```yaml
- module: services
  options:
    interval: 5              # seconds between rounds
    checks:
      - { name: network, cmd: [sh, -c, "ip route | grep -q default"] }
      - { name: docker,  cmd: [sh, -c, "test -S /var/run/docker.sock"] }
      - { name: api,     http: http://localhost:3000/health }
      - { name: redis,   tcp: localhost:6379 }
```

Every check in a round runs concurrently with its own deadline, so one endpoint
that hangs does not decide how long the round takes.

## `tasks` — commands you run now and then

```yaml
- module: tasks
  options:
    tasks:
      - { name: test,  cmd: [go, test, ./...], dir: ~/projects/api }
      - { name: build, cmd: [go, build, ./...], dir: ~/projects/api }
```

Select with `↑ ↓`, run with `enter`. The task takes over the pane while it
runs, and it is interactive: a command that asks a question can be answered.

## `stats` — tokens and account budgets

```yaml
- module: stats
  options:
    interval: 180            # seconds between readings, default 180
    account: true            # ask for the account budgets at all, default true
    endpoint: ""             # override the endpoint, for testing
    credentials: ""          # override the path to the OAuth credentials
```

Per-session figures — model, context, share served from cache, thinking tokens
— come from the transcripts on disk and cost nothing. The pane title shows the
model and the context; the rest stays in the pane, where there is room to say
what it means.

The five-hour and weekly budgets are **the only thing in this application that
reaches the network**. They are not on disk: they are read with the OAuth token
Claude Code stores, from the endpoint Claude Code itself calls. That endpoint is
internal and carries no compatibility promise, so a missing field, an unexpected
shape or a refused request is reported as unavailable **with its reason** —
never filled in with a number nobody can check, because a blank reading and a
zero reading must never look alike. **`account: false` removes that request
entirely** and keeps everything local.

While a stats pane exists, the status bar reprises the two shares, in the widest
form that fits without costing you the buttons.

## `hologram` — the panel that shows what Claude is doing

```yaml
- module: hologram
  options:
    style: sphere            # sphere, ring or avatar
    readout: right           # right, left or off — the text column
    speed: 0.18              # flow speed
    trail: 0.90              # how much of a dot survives each frame
    density: 1               # multiplier over the automatic particle count
    rotation: 0.09           # radians per second
    breath: 9                # seconds for one brightness cycle, 0 to disable
```

A particle sphere drawn in braille. Its **motion changes with what the sessions
are doing**: resting drifts slow and deep, thinking is quick and coherent,
waiting on you is nearly still and swept by a band of light, and a session that
has ended loses its coherence. Transitions ease over about a second, so a
change of state is something you watch happen. Each turn of conversation fires
a spark along a great circle — nothing fires on a timer, so a quiet panel means
quiet sessions.

The text column carries an ambient line and then what actually happened: a
session appearing, a state changing, a turn landing. **No conversation content
ever reaches it.** The ambient line is decoration, and decoration on a control
panel has one rule: it must never be mistakable for a statement about what the
machine is doing. Each line is drawn from the pool of the state the sessions are
really in, and none cites a figure or names an operation.

In the bottom corner sits a second line, on a much slower clock and unrelated
to the state. Where the ambient line reports a disposition and could in
principle be wrong about it, this one asserts nothing at all — no activity, no
figure, no state — which is what makes it safe to show whatever is happening.
It is dropped whole on a narrow pane: an aphorism cut in half is not a shorter
aphorism.

The column disappears below 44 columns of pane and the sphere takes the whole
width. The five numbers above are also sliders in the settings menu.

## `tabs` — several modules in one pane

For a window that is not wide enough to split again. Splitting is better when
there is room — you see two things at once — so this is what you reach for when
there is not.

```yaml
- module: tabs
  options:
    tabs:
      - title: brain          # optional, defaults to the module name
        module: hologram
        options: { style: sphere }
      - title: services
        module: supervisor
        options:
          services:
            - { name: api, cmd: [npm, run, dev], dir: ~/projects/api }
      - { title: usage, module: stats }
```

**Every module in the pane runs**, whether or not it is the one on screen: a
supervisor still supervises from a hidden tab and a Claude session still
answers. Only drawing is skipped, and every tab is resized with the pane so
switching never shows one laid out for a size it no longer has.

A pane of tabs stands between the application and what it holds, and passes
through everything the application looks for on a pane: the account budgets
for the status bar — asked of **every** tab, since a stats module you are not
looking at is still reading — the session behind the tab on screen, how far
back it is scrolled, and the pane's own contents for writing back to the
configuration. That last one includes tabs you opened while working: saving a
file that omitted them would lose them.

The strip **replaces the pane title** rather than adding a row — the point of
tabs is that space is short. A label that does not fit is dropped whole and
counted (`+2`), so the strip never lies about how many tabs there are.

Tabs are opened and closed as you work, not only declared here. `alt+a` opens
one — a session, the same thing a new pane holds — and `alt+x` closes the one
in front of you, falling through to closing the pane when it was the last tab.
The strip carries a `+` at its right, reserved before anything else is laid
out so a full strip still lets you open a tab, and a `×` on the tab you are
looking at and no other: it saves the width of one on every tab, and a stray
click cannot close something you were not reading.

What closing does to whatever the tab held is the module's own business, and
the same distinction closing a pane makes: a Claude session **detaches and
keeps running**, reachable from `alt+space`, while a shell ends.

`alt+;` and `alt+'` move between tabs, and clicking a label picks one directly
— though a click on an unfocused pane takes the focus and goes no further, so
from another pane it takes two. A tab is named after what it holds, numbered
when two would read alike.

## `sessions`

The session list, reached with `alt+space`. It takes no options and is not
normally placed in the layout: it is an overlay you consult and dismiss, not a
pane that would rearrange the window every time.

---

# Knowing what a session is doing

Claude Code writes a mark into its terminal's title while it works — hosting it
takes that away, since the title it writes reaches an emulator rather than your
terminal. The mark is put back, in the same shape in all three places it can
appear:

| | |
|---|---|
| `✳ ✻ ✽` turning | working |
| `◐` | waiting for you |
| `✕` | the session has ended |
| `·` | idle |

- **Above a pane**, before its name.
- **In a tab strip**, before each label, which is what makes a hidden tab
  bearable: a session asking a question from a tab you are not looking at
  colours its own label.
- **In the title of the terminal running the application**, summarised by
  whichever session most wants you — `◐ 1 waiting — ClaudeControl`. That title
  is the one thing you can see when the window is behind something else, which
  is exactly when a question would otherwise go unnoticed.

The mark comes from the session state the hooks report, not from the title
Claude Code writes for itself. That title says the same thing, but as a string
this application would have to guess at; the state arrives typed. Only the
working mark turns, and the interface redraws for it **only while something is
actually working** — an idle window stays idle.

---

# Keys and mouse

| | |
|---|---|
| `alt+h` `alt+j` `alt+k` `alt+l` | move the focus left, down, up, right |
| ``alt+` `` | previous pane |
| `alt+n` / `alt+x` | new pane / close the tab, or the pane |
| `alt+a` | open a tab in this pane |
| `alt+z` | zoom the focused pane, and back |
| `alt+m` / `alt+s` | flip a split / reset every split to equal shares |
| `alt+r` | pick this pane up, then click where it lands |
| `alt+;` / `alt+'` | previous tab, next tab, in a pane of tabs |
| `alt+space` / `alt+,` / `alt+/` | sessions, settings, command palette |
| `alt+g` / `alt+q` | help, quit |

Every one of these is a button in the bottom bar as well. A narrow window drops
the least important buttons rather than overlapping them; the keyboard and the
help panel still reach everything.

With the mouse: click a pane to focus it — that first click is **swallowed**, so
moving to a pane can never trigger something inside it. Drag a divider to
resize. `alt`-drag a pane to move it: dropping it on the middle of another
**swaps** the two, dropping it on a side **inserts** it there. Everything else
goes to the guest, translated into the coordinates it expects.

**Right-click** opens a small menu on the pane: copy, paste, new session,
close, zoom, move. It exists because turning mouse reporting on takes the right button
away from the terminal, and with it the menu the terminal would have shown —
having taken it, the application owes one back.

## Pasting

`ctrl+shift+v` works and always has: the terminal sends the clipboard as a
bracketed paste, which is forwarded whole to the focused pane and never scanned
for shortcuts.

**Right-click → paste** needs the same clipboard tool as copy, for the same
reason: a terminal application has no way of its own to reach the clipboard.
See *Selecting and copying* below.

## Selecting and copying

Drag across a pane to select, right-click and choose **copy**. A guest that
asked only for button events — Claude Code does — cannot see a drag at all: it
arrives as a press and a release with nothing in between. The gesture is
therefore free, and it is the one people expect to select text with.

The press is still forwarded, so a click reaches the guest as a click; only
once the pointer moves does the drag become a selection, and the guest is sent
a release so it is not left believing the button is still down. A guest that
**does** follow the pointer — an editor with the mouse enabled — keeps its
drag, since it is doing something with it.

**Copying needs a clipboard tool.** Both directions go through the same two
paths in the same order: a helper program if one is installed, and otherwise
the terminal itself over OSC 52 — which most terminals refuse in both
directions, since one lets a program read what you copied and the other lets
it overwrite it. gnome-terminal is among them.

```sh
sudo apt install wl-clipboard      # Wayland; xclip or xsel under X11
```

Without one, the `copy` and `paste` entries in the menu say `no clipboard
tool` **before** you press them — finding out afterwards, from a message about
a copy that did not happen, is finding out too late.

## Scrolling

The wheel reaches a pane's history, three lines a notch. A guest on the normal
screen does not scroll — it prints, and what it printed goes into the
scrollback; in a terminal you reach that with the terminal's own scrollbar, and
hosting the guest takes that away. The pane title says how far back you are
(`↑ 24`), and typing anything returns to the bottom, the way a terminal jumps
back to the prompt.

A guest that has taken the **whole screen** keeps the wheel: a pager, an
editor or a full-screen picker is drawing its own view and has no history
behind it, since nothing has scrolled off. Forwarding is the only thing that
could be right there, and the alternate screen is how the two are told apart.

Every `alt+` combination above was checked against the Claude Code binary, and
none of them is one Claude Code consumes. They were also checked against the
ANSI standard, which rules out more than you would think: `alt+=` is DECKPAM,
`alt+[` is CSI itself, `alt+c` is a full terminal reset, and `alt+-` designates
a character set. A decoder reads all four as sequences, and the key never
arrives.

---

# What it deliberately does not do

- **No detaching.** Closing the application ends the processes it started.
  Adopted services survive it; supervised services do not. Conversations come
  back on the next run (see below), but as new processes reading the same
  transcript rather than the ones you left running.
- **No monetary cost.** Token counts are shown as absolute numbers, never
  converted to money, and never as a percentage of a context window whose
  published value goes stale without saying so.
- **No automatic restart** of a service that crashed. A crash is information.

# Repository

| Path | What it is |
|---|---|
| `cmd/claudecontrol/` | The entry point |
| `internal/` | Layout, sessions, the bus, rendering, the hologram maths |
| `modules/` | One directory per module |
| `examples/` | Configurations to copy from |
| `proto/hologram/` | A throwaway prototype, kept only because it is what validated the hologram by eye. Not part of the product. |

# Licence

MIT — see [LICENSE](LICENSE).
