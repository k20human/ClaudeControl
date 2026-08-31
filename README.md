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
    keep_running: false      # do services outlive the application, default false
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

**A service that dies stays dead**, showing how it died. Resurrecting it would
hide the failure behind a row reading `running` while nothing works. A death by
signal is named — `SIGTERM` rather than `code 143` — because a number in that
range reads like a failure when it means that something stopped the service.

**And it says what the service left behind.** `npm run dev` is a launcher, not
the server; kill the launcher and the server can keep its port. The scan
records what each service has started while it is still there to be asked, so a
row that ends can report `SIGTERM · 1 left · 2m ago` — "exited" being true of
the process and false of the service, and only this saying which.

**A service that ended and is running again is adopted**, which it was not:
the scan skipped anything holding a session, and a service that ends keeps its
session so its output stays readable. The row then claimed `exited` for as long
as the pane was open and no amount of scanning could correct it.

**Services already running are adopted.** The module walks `/proc` every five
seconds, and on demand with `⟲ scan`, and takes over any process whose working
directory *and* command line both match — the directory alone would not do,
since a project can run a front end and a back end at once. A service you
started in another terminal can then be watched, stopped and restarted from
here. Its **output cannot be read**: that output went wherever it was going
before the module found it. Restarting brings the service under supervision,
and its output with it.

**What a run printed survives the next one.** You restart a service because of
something it said, and losing that at the moment you act on it is the worst
possible time to lose it — so the last four hundred lines come back on the new
run's screen, dimmed, under a rule reading `previous run`. The same record is
written to `~/.local/state/claudecontrol/services/` when the application
closes, so it is there tomorrow as well. A **stopped** service shows that text
in its place, under a line saying it is not live. It is kept as plain text: the
colours and the cursor moves are dropped, because a record that could still
move the cursor is not a record.

**`keep_running: true` makes services outlive the application**, and come back
with their output when you open it again.

It is not a matter of declining to stop them. A service left writing into a
terminal nobody is draining does not die — it **blocks**: the buffer fills,
about seventy thousand lines on this machine, and the next write waits for
ever. The service then holds its port, answers nothing, and says nothing about
it, which is worse than having been stopped.

So a small process stays behind. `claudecontrol --relay` stands in its own
session, owns the terminal the service writes to, and empties it into a file.
The service sees a terminal like any other — colours, progress bars, the width
of your pane — and the file holds those bytes exactly as they arrived, escape
sequences included. The pane pours that file back through its emulator, which
is why the colours are still there.

Opening the application again finds each relay and attaches to it: the row
reads `running`, the pid is the one that was already there, and the log is the
same log. That is the one thing a service adopted from a bare process cannot
offer.

A kept service cannot be typed into — there is no path from your keyboard to a
terminal somebody else owns — and its log is capped at 8 MB, kept from the end.

**Stopping signals the whole process tree, never the process group.**
`npm run dev` is a launcher whose real server is one of its children, and
signalling only the child leaves that server holding its port. A service
adopted from a shell also shares that shell's process group, so signalling the
group would kill the terminal you are sitting in.

In the pane: click a name for its output, or a checkbox to tick it. **Drag
across a log to select it**, then `alt+c` — a log is text you read, and text you
read is text you copy. **The wheel reaches the log's history**, which is how you get to the lines a restart
brought back. From the keyboard, `↑ ↓` moves, `space` ticks, `a` ticks
everything or nothing, `enter` opens the output, and `s` `r` `x` start, restart
and stop the ticked ones.

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

**All three styles speak the same vocabulary**, in whatever shapes they have.
The `ring` breathes deeply at rest and shallowly at work, is swept by one
bright arc while a session waits on you, and comes apart into gaps when a
session dies. The `avatar` cannot rotate or scatter without becoming a
different face, so it reads only the parts that survive being a face: the glow
travels furthest while waiting, barely at rest, and the eyes blink — a face
that never blinks is the one thing that reads as dead. Pick a style for the
room you have, not for what it will tell you.

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

### When a conversation ends

The pane goes back to being a shell, in the directory the conversation was
running in, with a line saying what became of it:

```
— conversation ended (0) · shell in ~/DEV · alt+a opens another session —
```

A shell rather than another conversation, because ending one is a decision and
starting the next is a different decision. **A `claude` typed in that shell is
not one this application manages** — the hooks travel on the command line at
launch, so a session started by hand has no working mark, no usage figures and
nothing to resume tomorrow. `alt+a` opens one that has all three.

A conversation that has ended is not offered for tomorrow either: ending it was
the decision, and bringing it back because the pane is still open would undo
that decision on your behalf.

## `tabs` — several modules in one pane

A tab opened at runtime — the `+`, or `alt+a` — starts **where the tab on
screen is working**, not where the application was launched from. A pane can
say otherwise with its own `dir:`, and a directory asked for by name beats
both. The same rule opens a new pane with `alt+n`.

For a window that is not wide enough to split again. Splitting is better when
there is room — you see two things at once — so this is what you reach for when
there is not.

```yaml
- module: tabs
  options:
    dir: ~/projects           # where a tab opened here starts; defaults to
                              # wherever the tab on screen is working
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

## Being told, and going there

Three marks and a title only help while you are looking at the screen, and the
reason to run several conversations is that you are not.

**When a conversation starts waiting on you**, the terminal bell rings — most
terminals turn that into an urgent mark on their tab — and a desktop
notification is posted naming the conversation. Once each, on the way in: hooks
arrive whenever Claude Code has something to report, and a bell on every one of
them would be an alarm rather than a notice. A conversation that goes back to
work and stops again announces itself again, because the second wait is as much
news as the first.

```yaml
alerts:
  bell: true               # BEL to your terminal, default true
  desktop: true            # notify-send or kdialog, default true
```

Both default to on. If nothing on the machine can post a notification the bar
says so at startup, rather than at the moment one is missed — on Debian and
Ubuntu the program is in `libnotify-bin`.

**`alt+i` goes to whatever is waiting**, and so does clicking the `N waiting`
count in the bar. The count already said that something needs you; the only
question it left was where. The pane is focused and, if it is a pane of tabs,
the tab holding that conversation is brought to the front. Pressed again it
moves to the next one, so several are visited in turn rather than the same one
twice.

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
| `alt+:` | search your Claude conversations |
| `alt+c` | copy the selection |
| `alt+i` | go to what is waiting on you |
| `alt+space` / `alt+,` / `alt+/` | sessions, settings, command palette |
| `alt+g` / `alt+q` | help, quit |

Every one of these is a button in the bottom bar as well. **Rest the pointer on
a button** and a line above the bar says what it does and which key does the
same — an icon is legible to the person who chose it and nobody else. A narrow
window drops the least important buttons rather than overlapping them; the
keyboard and the help panel still reach everything.

With the mouse: click a pane to focus it — that first click is **swallowed**, so
moving to a pane can never trigger something inside it. Drag a divider to
resize. `alt`-drag a pane to move it: dropping it on the middle of another
**swaps** the two, dropping it on a side **inserts** it there. Everything else
goes to the guest, translated into the coordinates it expects.

**Drag a tab by its label** to move it. Dropped on the middle of another pane
of tabs it joins that pane; on the middle of a pane that is not one, the target
is wrapped in a pane of tabs holding both; on an **edge** it becomes a pane of
its own, split off that side. Dragged along its own strip it is reordered.
Escape gives up on the move, and a press that never leaves the label is what it
has always been — the tab being selected.

What moves is the running module, not a copy, and nothing is re-initialised: a
Claude module's identity is a uuid made when it starts, and that uuid names its
transcript, reports its hooks and keys its state. A moved conversation is the
same conversation. Taking the last tab out of a pane takes the pane with it,
its share going back to its neighbours the way a closed pane's does.

**Right-click** opens a small menu on the pane: copy, paste, find here, new
session, close, zoom, move. It exists because turning mouse reporting on takes
the right button away from the terminal, and with it the menu the terminal
would have shown — having taken it, the application owes one back.

## Pasting

`ctrl+shift+v` works and always has: the terminal sends the clipboard as a
bracketed paste, which is forwarded whole to the focused pane and never scanned
for shortcuts.

**Right-click → paste** needs the same clipboard tool as copy, for the same
reason: a terminal application has no way of its own to reach the clipboard.
See *Selecting and copying* below.

## Searching your conversations

`alt+:` or the `⌕ find` button in the bar opens a search across **every Claude
conversation on this machine** — not the pane in front of you. What you
remember saying is usually in a conversation you closed days ago, which is the
one case a search of the visible screen cannot help with.

It reads the transcripts Claude Code writes under `~/.claude/projects`
(`CLAUDE_CONFIG_DIR` is honoured, as Claude Code honours it). Four hundred
megabytes across three hundred conversations takes about a fifth of a second on
a normal machine, so the search runs off the drawing loop, one at a time, with
the newest query winning: type quickly and you pay for one search, not one per
letter.

Case is ignored, **accents included**: `ÉLÉPHANT` is found by `éléphant` and
the other way round. It is not accent-*blind* — `elephant` finds neither, which
would be a different feature and a slower one.

Only **what was said** is searched — your prompts and Claude's replies. Tool
results, the contents of files that were read, and thinking are skipped. A word
that is common in code would otherwise match every conversation that ever
opened a source file, which is the opposite of finding something.

Each result gives the conversation's name, the directory it ran in, how long
ago, how many times the word appears, and a line of what was actually said
around the first one — that last line is what tells you whether it is the
conversation you meant. `↑` `↓` or the wheel move, `⏎` or a click **opens it in
a new tab** of the focused pane, resumed where it left off. Escape closes the
panel, and so does a click anywhere outside it.

Results are ordered newest first rather than by how often the word appears: you
are usually looking for something recent, and a conversation from a month ago
that repeated a word forty times is not more relevant for having done so.

## Finding something in a pane

**Right-click → find here**. The click already says which pane you mean, which
is why this one has no shortcut and the bar's button goes to the conversations
instead. It works on a Claude session, a terminal, a **service's log** and a
**task's output** — the last two being where a search is wanted most, and where
it used to refuse to open. The menu entry says `nothing to search` when there
is nothing: a service that is down, a task never run.

Type and the matches appear as you go, counted on the right (`3/17`) or told
plainly that there are `none` — a search that looked and found nothing must
not look like a search that never ran.

`enter` and `↓` move to the next match, `↑` to the previous, both wrapping.
Each brings its match half a screen down so you can see what surrounds it.
Matching lines are underlined and the one you are on is bold, which leaves the
inverted video to the selection. Escape closes the search **where it left
you**: you found what you wanted, and being thrown back to the bottom would
undo that.

While the bar is open every key belongs to it. A query is text, and a letter
that reached the guest would be a letter missing from what you meant to find.

`alt+:` was chosen for three reasons, each checked: it is absent from the
Claude Code binary, it means nothing as an escape sequence, and it is not a
readline binding — `alt+b` and `alt+f` move by word in every shell this can
host.

## Selecting and copying

Drag across a pane to select, then `alt+c` — or right-click and choose
**copy**. A guest that
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
none of them is one Claude Code consumes. Each was then sent through a hosting
emulator to see whether it arrives at all, which rules out more than you would
think: `alt+=` is DECKPAM, `alt+[` is CSI itself and `alt+-` designates a
character set, so an emulator between you and the application eats all three
and the key never lands.

That test is a round trip, not a reading of the standard. `ESC c` is a full
reset **on the way out** to a terminal and nothing at all on the way in, so
`alt+c` arrives intact and is free to mean copy. The cost is that a shell
running in a pane no longer sees it: readline binds it to capitalize-word,
which is the one thing given up for the letter everybody associates with
copying.

---

# Keeping the arrangement you left

Panes get split, tabs get moved, dividers get dragged. Closing offers to keep
all of it — a `[x] save this layout` in the quit panel, ticked to begin with.
Space toggles it, so does clicking it, and `y` or `enter` quits honouring it.

It is written beside the sessions in `~/.local/state/claudecontrol/state.json`,
in the same shape the configuration file uses, and it holds the ratios as well
as the structure: a divider dragged where you wanted it is part of the
arrangement.

**Your configuration wins if you have edited it since.** The saved arrangement
starts the next run unless `config.yaml` is newer than it, so adding a pane
there is a pane you see rather than an edit that appears to do nothing. If a
saved arrangement will not rebuild — a module that no longer exists, options
that no longer parse — the configuration is used and the bar says why.

A pane is written down as what it was built from, updated by what it can say
about itself. That distinction matters: a pane of tabs reports the tabs it
holds *now*, which is the point after an afternoon of moving them, while a
supervisor cannot describe itself and would otherwise come back with no
services at all.

---

# What it deliberately does not do

- **No detaching, unless a service asks for it.** Closing the application ends
  the processes it started, and adopted services survive it. A supervisor
  configured with `keep_running: true` is the exception: its services run under
  a relay that outlives the application, and are found again on the next run.
  Conversations are not detachable at all — they come back on the next run (see
  below), but as new processes reading the same transcript rather than the ones
  you left running.
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
