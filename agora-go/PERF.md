# Performance — mesures

Ce document consigne les mesures qui justifient les choix de performance de la réécriture Go. Les campagnes sont reproductibles avec les outils de `parity/` :
- `parity/seed` (option `-scale`) pour les données ;
- `parity/perf/bench_queries.sh` pour le SQL ;
- `parity/loadtest` et `parity/cgroup.sh` pour la charge HTTP.

## 1. Index Postgres (migration `0001_hot_path_indexes`)

### Jeu de données
`parity/seed -scale 5000`. Le volume visé par le plan est de 1 à 5 M de soutiens. Volumes obtenus :

| Table | Lignes |
|---|---|
| `agora_users` | 200 017 |
| `qags` | 150 005 (82 255 acceptées) |
| `supports_qag` | 1 789 468 |
| `reponses_consultation` | 2 055 021 |
| `user_answered_consultation` | ≈ 375 000 |
| `notifications` | 110 736 |

### Méthode
- Le SQL est copié **tel quel** depuis les repositories Kotlin (`parity/perf/hot_queries.txt`).
- Chaque requête passe 5 fois sous `EXPLAIN ANALYZE` ; on retient la médiane du temps d'exécution.
- Mesure faite après `--migrate=down` puis après `--migrate=up`, un `ANALYZE` précédant chaque mesure.
- Environnement : PostgreSQL 16, conteneur de développement. Les valeurs absolues dépendent de la machine ; seul le rapport avant/après est significatif.

| Requête (route) | Avant (ms) | Après (ms) | Gain |
|---|---:|---:|---:|
| Q6 QaG soutenues par l'utilisateur (chaque page de liste) | 47.1 | 0.22 | ×214 |
| Q5 nombre de QaG soutenues (onglet « soutenues ») | 49.6 | 0.24 | ×207 |
| Q3 liste « soutenues », page 1 | 1 167.9 | 335.4 | ×3.5 |
| Q9c détail d'une QaG avec son nombre de soutiens | 40.1 | 0.29 | ×138 |
| Q14 la QaG est-elle soutenue ? (soutien / retrait) | 46.2 | 0.07 | ×660 |
| Q12 dernière QaG de l'utilisateur (`ask_status`, `POST /qags`) | 17.5 | 0.08 | ×218 |
| Q10 réponses du gouvernement (`/qags/responses`) | 114.6 | 0.28 | ×409 |
| Q17 historique d'inscription (détection d'utilisateur suspect) | 23.4 | 0.20 | ×117 |
| C1 consultations répondues (`/consultations`) | 15.8 | 0.13 | ×122 |
| C2 l'utilisateur a-t-il répondu ? (détail de consultation) | 18.4 | 0.12 | ×153 |
| C4 nombre de consultations répondues | 15.4 | 0.13 | ×118 |
| C3 nombre de participants (cache manqué) | 97.0 | 24.0 | ×4 |
| C5 résultats d'une consultation en cours (cache manqué après chaque réponse) | 107.2 | 46.1 | ×2.3 |
| N1 notifications d'un utilisateur | 10.6 | 0.08 | ×133 |
| Q4a nombre de QaG acceptées | 21.2 | 11.3 | ×1.9 |
| Q4b nombre de QaG acceptées d'une thématique | 20.1 | 2.3 | ×8.7 |
| Q1 liste « top », page 1 | 1 469.4 | 1 155.0 | ×1.3 |
| Q1t « top » d'une thématique | 380.8 | 282.9 | ×1.3 |
| Q2 liste « latest », page 1 | 1 712.7 | 1 220.6 | ×1.4 |
| Q13 trending (cache manqué) | 423.2 | 339.7 | ×1.2 |

### Lecture
- **Les requêtes par utilisateur, exécutées à chaque requête HTTP, passent de 15–110 ms à moins de 0,3 ms.** Ce sont des index-only scans. C'est là que la base de données Kotlin passe son temps aujourd'hui.
- **Les agrégations `top`, `latest` et `trending` restent coûteuses.** Le SQL Kotlin (`GROUP BY qags.id` sur une jointure gauche, puis `ORDER BY` et `LIMIT`) agrège toutes les QaG acceptées avant de trier. Aucun index ne permet de s'arrêter tôt sans changer le SQL. Le jeu de test compte 82 000 QaG acceptées, bien plus que la production.
  - Côté Go, ces pages sont partagées par tous les utilisateurs : elles passent par le micro-cache (≤ 5 s, décision n° 2). Le coût base de données ne dépend donc plus du trafic : au plus une exécution toutes les 5 s par instance et par page.
  - La partie propre à l'utilisateur (Q6) est superposée ensuite.
- **Coût en écriture :** 11 index, dont 2 sur `supports_qag`. Un soutien ou un retrait met à jour 2 index B-tree de plus, sans impact sur un INSERT unitaire. L'index `users_data` est partiel (`event_type = 'signup'`) : les connexions, qui insèrent une ligne chacune, ne le paient pas.
- **Taille :** ≈ 300 Mo pour ce jeu, dont 186 Mo pour les deux index de `supports_qag`.
- **Construction :** 8 s pour les 11 index sur ce jeu, en `CONCURRENTLY`, donc sans verrou d'écriture. Une construction interrompue laisse un index INVALID, que la commande `up` suivante supprime puis reconstruit.

### Application en production
```
scalingo --app <app-go> run 'bin/agora --migrate=status'
scalingo --app <app-go> run 'bin/agora --migrate=up'
```
La migration est compatible avec le backend Kotlin. Hibernate (`ddl-auto=update`) ignore les index qu'il ne connaît pas. Elle peut donc être appliquée **avant** la bascule, et le Kotlin en profite aussi. Retour arrière : `--migrate=down`.

## 2. Charge HTTP (Kotlin contre Go)

### 2.1 Premières mesures : routes des tranches S0 et S1 (7 oct.)

**Conditions :**
- chaque backend est confiné à **1 CPU et 1 Go de RAM** (cgroup, `parity/cgroup.sh`) et testé seul, l'un après l'autre ;
- même Postgres 16, même Redis, même faux Strapi ;
- seed `-scale 50` (2 000 utilisateurs) ;
- 200 utilisateurs virtuels, 15 s de chauffe (JIT), 30 s mesurées ;
- profil `ported` de `parity/loadtest` : `/thematiques` 30 %, `/theme_hebdo` 20 %, `/referentiels/regions-et-departements` 10 %, `GET /profile` authentifié 40 %.

La machine de test (4 CPU) faisait aussi tourner d'autres environnements de parité : ce sont des ordres de grandeur, pas une mesure de référence.

| | Kotlin | Go | Rapport |
|---|---:|---:|---:|
| Débit total | 371 req/s | 14 271 req/s | **×38** |
| p50 | 102 ms | 2,5 ms | |
| p90 | 1 883 ms | 55,8 ms | |
| p99 | 4 395 ms | 75,5 ms | |
| `/thematiques` p99 | 3 307 ms | 50 ms | |
| `GET /profile` p99 | 5 297 ms | 79 ms | |

Les 500 de `/profile` sont identiques des deux côtés : environ 20 % des utilisateurs de ce seed ont un `last_connection_date` NULL. C'est une particularité Kotlin, reproduite telle quelle (voir `parity/ledger/S1.md`).

**Commande :**
```
parity/loadtest -target http://localhost:<port> -profile ported -c 200 -d 30s -warmup 15s -users 1900 -jwt-secret "$JWT_SECRET"
```

### 2.2 Listes de QaG (tranche S3, 8 oct.)

**Conditions :**
- même machine, même PostgreSQL ;
- seed `-scale 50` ;
- les 11 index de `--migrate=up` appliqués **aux deux bases** ;
- pool de 5 connexions de chaque côté, sans limite CPU ;
- profil `app-open` de `parity/loadtest`, 50 utilisateurs virtuels, 30 s mesurées, caches froids au départ.

| Route | Kotlin | Go |
|---|---:|---:|
| `GET /v2/qags?filterType=top` | 42 req/s, p50 278 ms | 1 212 req/s, p50 10,8 ms |
| `GET /v2/qags?filterType=trending` | 42 req/s, p50 267 ms | 1 201 req/s, p50 11,3 ms |
| `GET /v2/qags?filterType=supporting` (SQL personnel, non cachable) | 10 req/s, p50 252 ms | 302 req/s, p50 33,7 ms |
| `GET /qags/responses` | 11 req/s, p50 240 ms | 304 req/s, p50 22,7 ms |
| **Mélange complet (11 routes)** | **209 req/s, p99 933 ms** | **6 035 req/s, p99 40 ms (×29)** |

**Points à retenir :**
- Sans les index, le Go est 4 fois plus lent sur ce mélange (1 564 req/s). **Les index doivent donc être appliqués avant la bascule.**
- Le détail par route est dans `parity/ledger/S3.md`.

### 2.3 Campagne complète (12 oct.)

**Conditions** (script `parity/loadtest/campaign.sh`, rejouable) :
- chaque backend est seul dans un cgroup de **1 CPU et 1 Go**, la taille d'un conteneur Scalingo. Le JVM tourne avec les options du buildpack Java Scalingo pour 1 Go (`-Xmx671m -Xss512k -XX:CICompilerCount=2`) : avec `-Xmx768m`, il était tué par le cgroup dès la première charge ;
- base `-scale 5000` : 200 000 utilisateurs, 150 000 QaG, 1,8 M de soutiens, 2 M de réponses ;
- état « milieu de semaine » : les QaG acceptées depuis plus de 7 jours sont archivées par la requête de la tâche hebdomadaire, il en reste 18 679 acceptées ;
- les 11 index de `--migrate=up` sont appliqués **aux deux bases** ;
- pool de 5 connexions des deux côtés (`DATABASE_MAX_POOL_SIZE`, défaut commun) ;
- utilisateurs tirés parmi les comptes actifs (dernière connexion connue, non bannis). Les réponses aux consultations sont écrites par des utilisateurs qui n'y ont pas encore répondu : ce sont de vraies insertions ;
- chaque profil part d'un processus neuf et de caches vides : 30 s de chauffe, puis 60 s mesurées ;
- même machine pour PostgreSQL, Redis, le faux Strapi et le générateur de charge (4 CPU).
- **Profils :**
  - `app-open` : 11 routes de l'ouverture de l'app (listes de QaG top, trending, latest et soutenues, consultations, thématiques, thème hebdo, actualités, réponses, notifications, profil) ;
  - `consultation` : détail, questions, résultats, réponse, liste ;
  - `support` : soutien, retrait et détail d'une QaG populaire ;
  - `web` : pages publiques du site.

**Débit total (req/s), p99 entre parenthèses :**

| Profil | Kotlin, 20 VU | Go, 20 VU | Rapport | Kotlin, 200 VU | Go, 200 VU | Rapport |
|---|---:|---:|---:|---:|---:|---:|
| `app-open` | 5,2 (17,7 s) | **559** (274 ms) | **×107** | 0,9 (28 s, 396 timeouts) | **596** (1,2 s) | effondrement Kotlin |
| `consultation` | 50 (1,1 s) | **2 474** (61 ms) | **×49** | 58 (11,8 s) | **2 463** (490 ms) | ×42 |
| `support` | 94 (587 ms) | **1 528** (30 ms) | **×16** | 94 (7,2 s) | **1 561** (233 ms) | ×17 |
| `web` | 156 (476 ms) | **7 984** (34 ms) | **×51** | 153 (8,8 s) | **8 010** (151 ms) | ×52 |

**Mémoire au pic :** Kotlin de 392 à 963 Mo, Go de 73 à 250 Mo.

Avec un pool de 20 connexions pour l'app Go (variable propre à son app Scalingo), à 200 VU : `app-open` 762 req/s, `consultation` 2 540, `support` 2 172, `web` 7 779.

Les 400 de `support` sont identiques des deux côtés : un soutien déjà donné, ou un retrait sans soutien, tirés au hasard. Le détail par route est dans `perf/load-2026-10-12/` (`c20`, `c200`, `c200-pool20` ; fichiers `.txt` et `.json` du générateur).

**Lecture :**
- **Kotlin ne tient pas l'ouverture de l'app sur ces volumes, même à 20 utilisateurs simultanés.**
  - Le classement trending (score et tri des 18 679 QaG de la semaine) est recalculé à chaque requête. À cela s'ajoutent le SQL des pages top et latest sans cache et les appels Strapi synchrones.
  - À 200 VU, les threads Tomcat s'accumulent, le tas sature (963 Mo) et le JVM ne répond plus. Il ne revient pas avant d'avoir purgé sa file : c'est la raison du redémarrage avant chaque profil.
- **Go est au moins 16 fois plus rapide sur chaque profil, par conteneur et à configuration égale. Le p99 reste sous 300 ms à 20 VU** (critère du plan : ×10 au p99 SLO).
- **Côté Go, la limite restante est la base de données, plus le CPU :** à 200 VU, le CPU est utilisé à moins de 30 % sur `app-open`. La requête la plus coûteuse est la page « soutenues » d'un utilisateur, environ 85 ms. C'est le SQL Kotlin copié tel quel : un parcours complet de `qags`, car `OR … IN (sous-requête)` empêche l'usage d'un index. Elle coûte autant aux deux backends.
- **Les optimisations qui ont produit ces chiffres ne changent aucune sortie :**
  - ordre du trending mémorisé tant qu'aucune QaG ne change d'heure, avec des tests de propriété contre le tri d'origine ;
  - micro-caches de 5 s (décision n° 2) ;
  - lecture de l'utilisateur en L1 ;
  - index.

  Le premier profilage sous charge a fait passer `app-open` de 75 à 612 req/s.

**Dimensionnement indicatif.**
- **Débit par conteneur :** une ouverture d'app enchaîne une dizaine de requêtes. Un conteneur Go de 1 CPU sert environ 560 req/s sous 300 ms au p99, soit environ 50 ouvertures d'app par seconde. Avec un utilisateur actif qui fait une requête toutes les 10 s en moyenne, cela représente environ 5 000 utilisateurs simultanés par conteneur. Kotlin, dans les mêmes conditions, n'en sert que quelques dizaines.
- **Montée en charge :** les conteneurs Go sont sans état (L1 + Redis + invalidation pub/sub), on en ajoute autant que nécessaire. La limite devient alors la base de données, à surveiller : CPU du plan PostgreSQL et `max_connections` (`conteneurs × DATABASE_MAX_POOL_SIZE`).

**Commande** (environ 20 min par passe) :
```
SCALE=5000 C=200 D=60s W=30s ./parity/loadtest/campaign.sh      # RESEED=0 pour réutiliser la base
GO_EXTRA_ENV=pool20.env SIDES=go ./parity/loadtest/campaign.sh   # variante de configuration Go
```
