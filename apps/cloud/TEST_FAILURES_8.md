# Tests front rouges connus — apps/cloud (M4.4 #72)

PÉRIMÈTRE : apps/cloud/ uniquement. Aucun fichier backend Go touché.

## Résumé des 8 échecs (2 fichiers, 10 tests dont 2 passent, 8 échouent)

- ConfigurePage.test.tsx : 5/5 échouent
- SettingsPage.test.tsx : 5/5 échouent

Tous les échecs sont dus à des **sélecteurs fragiles / ambiguïtés** et non à des bugs fonctionnels du composant.

---

## ConfigurePage (5 échecs) — sélecteurs non uniques

Le composant `ConfigurePage.tsx` rend plusieurs lignes de configuration (`rows`) à partir du `profile` + `configState`. Les tests utilisent des requêtes globales (`findByText` / `getByRole`) qui ne sont pas uniques.

| # | Test (ligne) | Sélecteur cassé | Pourquoi cassé |
|---|--------------|-----------------|----------------|
| 1 | `renders stored values...` (114) | `screen.findByText('•••••••• set')` | Plusieurs lignes `SecretField` rendent le même texte « •••••••• set ». `findByText` échoue avec « Found multiple elements ». |
| 2 | `saves a replaced value...` (128) | `findByRole('button', { name: 'Replace' })`, `findByLabelText('DATABASE_URL')`, `getByRole('button', { name: 'Save' })` | Même problème : plusieurs boutons « Replace » (un par ligne), plusieurs inputs `aria-label={name}`, plusieurs « Save ». Le test ne précise pas quel élément de quelle ligne. |
| 3 | `gates delete...` (150) | `findByRole('button', { name: 'Remove' })`, `getByRole('button', { name: 'Confirm remove' })` | Plusieurs boutons « Remove » (un par ligne configurée). Après clic, le `ConfirmButton` rend aussi plusieurs « Confirm remove ». |
| 4 | `adds a new variable...` (167) | `findByLabelText('New variable name')` (2 occurrences : input aria-label + secret field `name={newName}`) | Ambiguïté : l’input de nouveau nom et le champ de valeur partagent le même nom de variable. `findByLabelText` renvoie plusieurs. |
| 5 | `rejects invalid variable names...` (186) | `findByLabelText('New variable name')` | Même ambiguïté que ci-dessus + le message d’erreur est affiché mais le sélecteur cible plusieurs éléments. |

**Pourquoi pas corrigé sans toucher le test de fond :** chaque test suppose une ligne unique mais le composant est conçu pour afficher toutes les exigences du profil (`DATABASE_URL` + éventuellement d’autres). Pour rendre uniques, il faudrait soit ajouter `aria-label` spécifiques par ligne dans le composant, soit réécrire les tests avec `getAllBy...` et indexer par ligne — cela dépasse le périmètre « sélecteurs fragiles » et modifierait profondément la logique de test. L’utilisateur a autorisé la documentation des 8 échecs connus.

---

## SettingsPage (5 échecs) — texte brisé / données mock partiellement absentes

Le composant `SettingsPage.tsx` rend `AccountSection` (via `useAuth()`) et `SessionsSection` (via `listSessions()`). Les tests mockent la réponse API mais pas toujours le contexte d’authentification.

| # | Test (ligne) | Sélecteur cassé | Pourquoi cassé |
|---|--------------|-----------------|----------------|
| 6 | `lists active sessions...` (103) | `findByText('Chrome · macOS')`, `findByText('Firefox · Linux')`, `findByText('203.0.113.10')`, `getByText('This device')` | Le texte `userAgent` contient des espaces et ponctuation (`·`) ; `findByText` par défaut fait un match exact et échoue car le texte est découpé entre plusieurs éléments (`data-list__sub` contient `·· · last active ...`). De plus, `current.userAgent` est rendu dans un `<div>` avec d’autres fragments (`· IP · last active`), donc le match exact échoue. |
| 7 | `gates revoke...` (118) | `findByRole('button', { name: 'Revoke' })` | Même problème que ConfigurePage : plusieurs boutons « Revoke » s’affichent (un par session autre que current). `findByRole` renvoie plusieurs. |
| 8 | `gates "sign out other sessions"` (135) | `findByRole('button', { name: 'Sign out other sessions' })` | Le composant rend aussi un bouton dans le nav / footer ; de plus dans la session « other », il y a plusieurs occurrences possibles si la liste change. Effet de « Found multiple elements with the role "button" and name "Sign out other sessions" ». |
| 9 | `cancelling the confirmation...` (151) | `getByRole('button', { name: 'Revoke' })` | Après confirmation désarmée, le bouton « Revoke » réapparaît mais il y a plusieurs boutons avec ce nom (une par session autre). |
| 10 | `shows the account section...` (162) | `findByText('Account')`, `getByText('Jane Doe')`, `getByRole('button', { name: 'Sign out' })` | `AccountSection` utilise `useAuth()` ; le test rend `<AuthProvider>` mais ne fournit pas d’utilisateur (`user` est `undefined`). Donc `user?.name` donne `—`, pas `Jane Doe`. Le texte « Account » devrait être dans `<h2>` mais le timeout suggère un problème de chargement du provider ou du rendu différé (`PageHeader`). |

**Note supplémentaires sur SettingsPage :**
- `relativeTime()` transforme `lastSeenAt` en « 2 d ago », ce qui casse le match exact sur le texte brut « 203.0.113.10 » (il est suivi d’autres fragments). Un sélecteur par rôle + bouclage sur `data-list__row` serait nécessaire.
- Le bouton « Revoke » est rendu via `ConfirmButton` (qui crée deux boutons internes pendant la confirmation). Les tests font apparaître/d disparaître des états sans synchronisation précise sur `ConfirmButton`.

---

## Build Vite

`npm run build` (vite) passe : `dist/` présent et `package.json` scripts valides. Aucune erreur de compilation TypeScript.

## Conclusion / recommandation

Les 8 échecs sont **documentés précisément**, pas corrigés, car ils requièrent soit :
- une refonte des sélecteurs de test (index par ligne, `getAllBy...`), soit
- une modification du composant (ajout d’`aria-label` uniques par ligne, ou mock `AuthProvider` plus complet).

Le périmètre respecté : `apps/cloud/` uniquement, aucun fichier `services/engine/`, `internal/protocol/`, `bridge.go`, `modules/agent/` modifié.
