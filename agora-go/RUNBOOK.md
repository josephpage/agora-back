# Runbook — bascule Kotlin → Go

Ce runbook décrit le passage du backend Kotlin (app Scalingo actuelle) au backend Go (`agora-go/`). Les deux apps partagent **la même base Postgres et le même Redis** pendant toute la transition. Le Kotlin reste déployable et prêt à reprendre 100 % du trafic à tout moment.

Références :
- [`DIVERGENCES.md`](DIVERGENCES.md) : les écarts de classe C doivent être validés avant l'étape 3 ;
- [`PERF.md`](PERF.md) ;
- l'ADR `architecture-decisions/2026-10-07-reecriture-go-du-backend.md`.

## 0. Prérequis (avant toute bascule)

- [ ] **Harnais de parité vert** sur la branche à déployer :
  - commande : `parity/run.sh up && parity/run.sh test` ;
  - attendu : 0 diff, seuls les scénarios `skip` documentés tolérés ;
  - la CI `agora-go.yml` doit être verte.
- [ ] **Écarts de classe C validés** par le produit (liste dans `DIVERGENCES.md`).
- [ ] **Valeurs de prod relevées**, et identiques dans la nouvelle app :
  - `TZ` et `JAVA_OPTS` : fuseau et locale de la JVM ;
  - `JWT_SECRET` et les `LOGIN_TOKEN_*` : les JWT et loginTokens existants doivent rester valides ;
  - `REMOTE_ADDRESS_*` : sinon les hash d'IP changent ;
  - `ALLOWED_ORIGINS`, `CMS_*`, les `IS_*_ENABLED`, `REQUIRED_*_VERSION`, `ERROR_TEXT_*`, Firebase, Sentry, ACME.
- [ ] **Index appliqués en production** (sans verrou, compatibles Kotlin). Le Kotlin en profite immédiatement :
  ```
  scalingo --app <app-go> run 'bin/agora --migrate=status'
  scalingo --app <app-go> run 'bin/agora --migrate=up'
  ```
- [ ] **Charge de la base vérifiée** : `max_connections` du plan Postgres ≥ `(conteneurs Kotlin × pool) + (conteneurs Go × DATABASE_MAX_POOL_SIZE) + crons + outils (Metabase…)`, avec une marge de 20 %.

## 1. Création de l'app Go (aucun trafic)

1. Créer l'app `agora-back-go` (même région) liée au dépôt, avec `PROJECT_DIR=agora-go`. Les buildpacks Go et nginx, `Procfile`, `cron.json` et `servers.conf.erb` sont dans `agora-go/`.
2. Copier **toutes** les variables d'environnement de l'app Kotlin, puis :
   - `DATABASE_URL` et `REDIS_URL` : les URL des addons **de l'app Kotlin**, à copier car les addons ne sont pas partagés automatiquement ;
   - `AGORA_COEXISTENCE=true` : chaque écriture Go supprime aussi les clés de cache Kotlin concernées, et les caches Go des données modifiables sont plafonnés à 5 s ;
   - `AGORA_CRON_DISABLED=true` : les crons de l'app Go s'arrêtent immédiatement, tant que le Kotlin les exécute ;
   - `ACME_CRON_ENABLED=false` : le renouvellement ACME reste côté Kotlin ;
   - `NGINX_API_ACCESS_RULES` : même valeur que l'app Kotlin.
3. Déployer, puis vérifier :
   - `GET /thematiques` et `GET /v3/api-docs` répondent ;
   - aucun log ERROR au démarrage ;
   - Sentry reçoit l'environnement attendu.
4. **Smoke test**, avec l'app mobile en flavor sandbox et le front de staging pointés sur l'URL de l'app Go : connexion, liste des QaG, soutien, consultation (réponse puis résultats), notifications, profil.

## 2. Shadow (optionnel, recommandé)

Le nginx de l'app Kotlin duplique (`mirror`) 1 à 5 % des GET sans effet de bord vers l'app Go. La réponse de l'app Go est ignorée par nginx.

1. Ajouter un identifiant de requête commun (`$request_id`) aux logs d'accès des deux apps.
2. Pendant 24 à 48 h, comparer pour chaque identifiant le statut et la taille de réponse, et suivre les erreurs Sentry côté Go.

Tout écart non listé dans `DIVERGENCES.md` est un bug à corriger avant l'étape 3 : le reproduire dans un scénario du harnais, puis le corriger.

## 3. Bascule progressive (Cloudflare)

Avant le premier palier, lancer un `FLUSHDB` côté Kotlin (`POST /admin/cache/clear`) pour repartir de caches propres.

Les règles d'origine Cloudflare (Origin Rules, Worker ou Load Balancer pondéré) routent par famille de chemins, dans cet ordre :

| Palier | Chemins | Durée minimale |
|---|---|---|
| 1 | Contenus publics : `/thematiques`, `/theme_hebdo`, `/content/**`, `/welcome_page/**`, `/participation_charter`, `/fiches_inventaire/**`, `/referentiels/**`, `/api/public/**` | 24 h |
| 2 | Lectures QaG et consultations (GET `/v2/qags`, `/qags/**`, `/consultations/**`, `/v2/consultations/**`, `/notifications/**`, `/profile`) | 48 h |
| 3 | Écritures (`POST`/`DELETE` QaG, soutiens, feedbacks, réponses aux consultations, profil) | 48 h |
| 4 | `/signup`, `/login` | 48 h |
| 5 | `/admin/**`, `/moderate/**`, `/moderatus/**`, `/stub/**`, `/.well-known/**` | — |

**À surveiller à chaque palier**, en comparant les deux apps :
- taux de 5xx et de 4xx par route ;
- p50 et p99 ;
- événements Sentry ;
- CPU et connexions de la base ;
- opérations par seconde et mémoire du Redis.

**Critère d'arrêt :** toute hausse d'erreurs ou tout comportement client anormal entraîne un retour au palier précédent (voir § 5).

Alternative sans Cloudflare : un `split_clients` dans le nginx de l'app Kotlin, collant par hash du JWT, qui proxifie vers l'app Go.

## 4. 100 % Go

1. Tout le trafic passe par l'app Go.
2. Transférer les crons :
   - `AGORA_CRON_DISABLED=false` et `ACME_CRON_ENABLED=true` côté Go ;
   - désactiver `cron.json` côté Kotlin (ou le vider), dans le même créneau, **jamais les deux actifs**.
3. Après 24 h sans incident, passer `AGORA_COEXISTENCE=false` : les caches Go retrouvent leurs TTL Kotlin, et la suppression des clés Kotlin s'arrête.
4. Garder l'app Kotlin déployée, à 0 trafic, pendant 2 semaines.

## 5. Rollback (à répéter en staging avant l'étape 3)

1. Remettre la règle Cloudflare (ou le `split_clients`) vers l'app Kotlin. Le schéma n'a pas changé, les clés Redis partagées (rate limits, compteurs d'inscription, feature flags) ont le même format, et les JWT et loginTokens sont interopérables.
2. Si l'app Go a écrit seule pendant longtemps, avec la coexistence désactivée : lancer `POST /admin/cache/clear` côté Kotlin pour vider les caches Kotlin potentiellement périmés.
3. Si les crons ont déjà été transférés, les réactiver côté Kotlin et les désactiver côté Go (`AGORA_CRON_DISABLED=true`).
4. Les index peuvent rester en place. Ils ne gênent pas le Kotlin et l'accélèrent. Retrait possible avec `bin/agora --migrate=down`.

## 6. Décommission

Après 2 semaines à 100 % Go sans rollback :
- une PR séparée supprime le code Kotlin et le mode coexistence, et fait de `agora-go/` la racine ;
- les évolutions de schéma passent sous la responsabilité du Go : nouvelles migrations dans `internal/store/migrations` ;
- l'app Kotlin et ses éventuels addons dédiés sont supprimés.
