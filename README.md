# ClaudeControl

Un centre de contrôle en terminal pour Claude Code.

Plusieurs sessions Claude Code vivantes dans une seule fenêtre, pilotées à la
souris ou au clavier, entourées de modules configurables — hologramme, liste
des sessions, sondes de service, lanceur de tâches.

Écrit en Go. Aucune dépendance à tmux.

## Démarrer

```sh
go build -o bin/claudecontrol ./cmd/claudecontrol
./bin/claudecontrol                                     # une session, dossier courant
./bin/claudecontrol -config examples/control-centre.yaml # tout
```

Sans fichier de configuration, une seule session Claude Code s'ouvre dans le
dossier courant. Pour une configuration permanente :

```sh
mkdir -p ~/.config/claudecontrol
cp examples/control-centre.yaml ~/.config/claudecontrol/config.yaml
```

## Raccourcis

| Touche | Action |
|---|---|
| `Alt+h j k l` | Déplacer le focus |
| `` Alt+` `` | Panneau précédent |
| `Alt+n` / `Alt+x` | Nouvelle session / fermer le panneau |
| `Alt+z` | Plein écran, et retour |
| `Alt+m` / `Alt+s` | Pivoter la scission / parts égales |
| `Alt+Espace` | Liste des sessions |
| `Alt+,` | Réglages du panneau focalisé |
| `Alt+/` | Palette de commandes |
| `Alt+g` | Aide |
| `Alt+q` | Quitter, avec confirmation |

À la souris : cliquer focalise un panneau, recliquer transmet le clic à
l'invité, tirer un séparateur redimensionne. La barre du bas est entièrement
cliquable.

## Modules

| Module | Rôle |
|---|---|
| `claude` | Une session Claude Code, avec identité imposée et remontée d'état |
| `term` | N'importe quelle commande dans un panneau |
| `sessions` | Toutes les sessions, attachées ou non, et laquelle vous attend |
| `hologram` | Sphère à particules, jauge circulaire ou avatar d'état |
| `services` | Sondes de service : commande, HTTP, port TCP |
| `tasks` | Vos commandes fréquentes, et leur sortie |

Chaque module publie ses réglages ; le menu `Alt+,` se construit à partir de
ces descriptions. Enregistrer réécrit la configuration **en préservant vos
commentaires**.

## Voyant d'attente

Les sessions Claude Code sont lancées avec nos propres hooks, injectés par
`--settings` — votre `settings.json` global n'est pas touché. Quand l'une
d'elles demande une permission, la barre du bas affiche `N waiting`, sans que
vous ayez à regarder ce panneau-là.

## Limites assumées

- **Fermer l'application arrête les sessions.** Pas de démon détaché comme
  tmux. Les identifiants sont enregistrés, une reprise par `--resume` reste à
  câbler au démarrage.
- **Pas de coût monétaire.** Il exigerait une table de prix par modèle qui
  devient fausse en silence.
- **Pas de glisser-déposer** d'un panneau vers un autre endroit de l'arbre.

Ces choix sont argumentés dans la spec, section 11.

## Contenu

| Chemin | Rôle |
|---|---|
| `docs/superpowers/specs/` | La conception, et tous les défauts trouvés en route |
| `docs/superpowers/plans/` | Les cinq plans d'implémentation |
| `examples/` | Configurations prêtes à l'emploi |
| `proto/hologram/` | Prototype jetable — a servi à valider le rendu à l'œil, n'est pas dans le produit |

## Tests

```sh
go test ./... -race
```
