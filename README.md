# ClaudeControl

Un centre de contrôle en terminal pour Claude Code.

Plusieurs sessions Claude Code vivantes dans une seule fenêtre, pilotées à la
souris ou au clavier, accompagnées de modules d'information configurables —
hologramme, état des sessions, sondes de service, lanceur de tâches.

Écrit en Go. Aucune dépendance à tmux.

## État

En conception. La spec est dans
[`docs/superpowers/specs/`](docs/superpowers/specs/).

## Contenu

| Chemin | Rôle |
|---|---|
| `docs/superpowers/specs/` | Conception validée |
| `proto/hologram/` | Prototype jetable — valide le rendu de l'hologramme, n'entrera pas dans le produit |

## Prototype de l'hologramme

```sh
cd proto/hologram && go build -o holo . && ./holo
```

Touches : `b` braille/demi-blocs · `g` halo · `+`/`-` densité · `[`/`]` vitesse ·
`,`/`.` traînées · `r` nouveau champ · `q` quitter.
