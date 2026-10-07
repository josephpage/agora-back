# agora-go — réécriture Go du backend Agora

Réécriture du backend Kotlin/Spring Boot (`../src/main/kotlin/fr/gouv/agora`, référence figée au commit `2898584`). Le contrat avec ses clients est inchangé :
- l'app mobile (agora-app) ;
- le site (agora-front) ;
- Moderatus ;
- les crons.

**Cela couvre :** mêmes routes, statuts, en-têtes, octets JSON/XML, écritures en base et appels Strapi. Seuls les écarts listés dans [`DIVERGENCES.md`](DIVERGENCES.md) diffèrent.

**Objectif :** tenir des centaines de milliers d'utilisateurs simultanés sur le même hébergement Scalingo.

| Document | Contenu |
|---|---|
| [`CONVENTIONS.md`](CONVENTIONS.md) | Règles de portage (à lire avant de toucher un module) |
| [`DIVERGENCES.md`](DIVERGENCES.md) | Registre des écarts Kotlin → Go (classes A/B/C/N) |
| [`PERF.md`](PERF.md) | Mesures : index, charge HTTP |
| [`RUNBOOK.md`](RUNBOOK.md) | Bascule progressive et retour arrière |
| `../architecture-decisions/2026-10-07-reecriture-go-du-backend.md` | ADR |
| `parity/LEDGER.md` | Chaque fichier Kotlin → son équivalent Go, ses tests et ses scénarios |

## Lancer

Pré-requis : Go (version de `go.mod`), PostgreSQL 16, Redis ≥ 5.

```sh
go build -o bin/agora ./cmd/agora

# serveur HTTP (AGORA_PORT, sinon PORT)
DATABASE_URL=postgres://… REDIS_URL=redis://… JWT_SECRET=… bin/agora

# tâches planifiées (cron.json), mêmes arguments que le Kotlin
bin/agora --run-custom-command=dailyTasks
bin/agora --run-custom-command=weeklyTasks --force_question_selection=true
bin/agora --run-custom-command=acmeCertificateRenewalTasks

# index Postgres (CREATE INDEX CONCURRENTLY, compatibles avec le Kotlin)
bin/agora --migrate=status|up|down
```

## Configuration

Les variables d'environnement sont **celles du Kotlin**, avec les mêmes défauts et les mêmes erreurs au démarrage : `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`, `LOGIN_TOKEN_*`, `REMOTE_ADDRESS_*`, `CMS_*`, `ALLOWED_ORIGINS`, `REQUIRED_*_VERSION`, `FIREBASE_CREDENTIALS_JSON`, `SENTRY_*`, etc. Elles sont toutes lues dans `internal/config`.

Variables propres au Go :

| Variable | Défaut | Rôle |
|---|---|---|
| `AGORA_COEXISTENCE` | `false` | Pendant la bascule, quand le Kotlin sert encore du trafic sur la même base. Chaque écriture Go supprime aussi les clés de cache Kotlin concernées. Les caches Go de données modifiables par le Kotlin sont plafonnés à 5 s |
| `AGORA_MICROCACHE_TTL` | `5s` | Durée du micro-cache des données partagées non cachées par le Kotlin. Bornée à 5 s ; `0` le désactive |
| `AGORA_CRON_DISABLED` | `false` | Les commandes `--run-custom-command` se terminent sans rien faire. Pour une app parallèle qui ne doit pas exécuter les crons |
| `AGORA_REDIS_POOL_SIZE` | `50` | Connexions Redis par conteneur (timeouts de 500 ms, jamais d'attente infinie) |
| `AGORA_BOOTSTRAP_SCHEMA` | `false` | Crée les tables manquantes au démarrage (base vide de test ou de review app). Ne jamais activer en production : le schéma reste celui d'Hibernate |
| `DATABASE_MAX_POOL_SIZE` | `5` | Même variable et même défaut que le Kotlin. À dimensionner selon `max_connections` (voir le runbook) |

## Déploiement Scalingo

L'app Go est une app Scalingo distincte, avec `PROJECT_DIR=agora-go`. Les fichiers de déploiement vivent dans ce répertoire :
- `.buildpacks` : Go puis nginx ;
- `Procfile` : nginx et `bin/agora` ;
- `servers.conf.erb` : upstream keepalive ;
- `cron.json` : mêmes horaires que le Kotlin ;
- `scalingo.json`.

La procédure complète est dans [`RUNBOOK.md`](RUNBOOK.md).

## Organisation

```
cmd/agora/            point d'entrée : serveur, tâches, migrations
internal/
  config/             variables d'environnement (sémantique Kotlin)
  httpx/              pipeline HTTP au comportement Tomcat / Spring Security / Spring MVC
  jsonjava/ xmljava/  sérialisation et désérialisation Jackson (JSON) et Jackson XML / Woodstox, octet par octet
  javacompat/         sémantique Java/Kotlin (UTF-16, UUID, URLEncoder, charsets, décodeur UTF-8…)
  auth/               JWT (règles jjwt), loginToken, hash d'IP
  cache/              L1 + singleflight, invalidation pub/sub entre instances, coexistence
  store/              pool pgx, schéma de référence, migrations d'index
  strapi/ fcm/ sanitize/ domain/ common/
  modules/<feature>/  un module par package Kotlin : handlers, use cases, repositories
  tasks/              tâches des crons
parity/               harnais différentiel Kotlin ↔ Go (voir ci-dessous)
```

## Tests et parité

```sh
gofmt -l cmd internal parity && go vet ./... && go test -race ./...
```

Le **harnais différentiel** (`parity/`) exécute la référence Kotlin et le Go côte à côte. Chacun a sa base, son Redis et son faux Strapi. Chaque scénario YAML (`parity/scenarios/`) est joué sur les deux, puis il compare :
- statuts, en-têtes et corps ;
- les lignes écrites en base ;
- les URI Strapi émises.

```sh
./parity/run.sh up                      # PG, 2 Redis, 2 faux Strapi, Kotlin :8081, Go :8082
./parity/run.sh test                    # tous les scénarios ; -run <regexp>, -tags <tranche>
./parity/run.sh restart-go              # après une modification du Go
PARITY_SLOT=1 ./parity/run.sh up        # environnement isolé (ports et bases décalés)
```

**Variables utiles :**
- `PARITY_REF_JAR` : jar de la référence, construit avec `./gradlew bootJar -x test` sur le commit figé, avec un JDK 17 ;
- `PARITY_JAVA` : binaire `java` 17 ;
- `PARITY_RUN_DIR` : répertoire de travail ;
- `PARITY_PG_EXTERNAL=1` : PostgreSQL déjà lancé (CI).

**L'oracle JVM** (`parity/oracle`) compare les implémentations Go aux classes Java de la référence : Jackson, Woodstox, sanitizer OWASP, jjwt, `URLEncoder`, décodeurs… Il est appelé par les tests Go quand `PARITY_ORACLE=1` et que `REFJAR_DIR` pointe vers le jar extrait.

La CI (`.github/workflows/agora-go.yml`) enchaîne trois étapes :
1. tests unitaires ;
2. tests oracle ;
3. harnais complet.
