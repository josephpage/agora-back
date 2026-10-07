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

À compléter quand les tranches des listes seront portées (S2, S3, S4, S5). Les profils de `parity/loadtest` (ouverture d'app, consultation, rafale de soutiens, pages web) seront lancés sur les deux backends, avec les mêmes limites CPU et mémoire (`parity/cgroup.sh`), la même base et le même Redis.
