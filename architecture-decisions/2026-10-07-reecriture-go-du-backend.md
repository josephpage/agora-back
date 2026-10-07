📅 Date : 7 octobre 2026

## Contexte

- Le backend Kotlin/Spring Boot (≈24 000 lignes, 88 routes) sert l'application mobile (agora-app) et le site (agora-front) sur Scalingo, derrière nginx.
- L'objectif est de supporter **des centaines de milliers d'utilisateurs simultanés** sur le même hébergement, sans aucun changement fonctionnel visible des clients.
- L'analyse du code a identifié les goulots suivants :
  - **Authentification :** chaque requête authentifiée vérifie le JWT deux fois, puis lit l'utilisateur dans Redis (désérialisation polymorphe).
  - **Pool Redis :** 8 connexions à attente infinie, pour 200 threads Tomcat.
  - **Requêtes BDD :** N+1 sur les thématiques. Les listes et la recherche de QaG sont en SQL non caché, sans index adaptés.
  - **Strapi :** appelé de manière synchrone sur le chemin des requêtes, sans timeout de lecture.
  - **Soumission d'une consultation :** scans `KEYS` et un cache en ajout seul, jamais relu.
  - **Logs :** INFO très volumineux, envoyés à Sentry.
  - **Coût mémoire :** il impose un plafond de threads, donc de connexions simultanées par conteneur.

## Options envisagées 💡

### Optimiser le backend Kotlin en place
Corriger les goulots un par un (cache utilisateur local, pool Redis, index, timeouts).

✅ Avantages : pas de réécriture, risque fonctionnel faible.

🚫 Inconvénients :
- le coût par requête de la pile (Tomcat + Spring Security + Jackson + JPA) et l'empreinte mémoire de la JVM restent ;
- le gain attendu est de l'ordre de ×2 à ×3, loin du ×10 visé à hébergement constant.

### Multiplier les conteneurs Kotlin
Monter en conteneurs et en taille de base de données.

✅ Avantages : immédiat.

🚫 Inconvénients :
- hébergement plus cher ;
- les goulots partagés (Redis, base, Strapi) saturent quand même ;
- le nombre de connexions Postgres limite le nombre de conteneurs.

### Réécrire le backend en Go avec une parité fonctionnelle stricte
Nouveau backend Go dans `agora-go/`. Même base, même Redis, mêmes contrats HTTP. Une nouvelle application Scalingo, avec une bascule progressive.

✅ Avantages :
- coût par requête et empreinte mémoire bien plus faibles (goroutines, pas de JVM) ;
- cache local par instance avec invalidation explicite ;
- l'occasion de corriger les goulots sans toucher aux clients.

🚫 Inconvénients :
- risque de régression : toute différence de comportement est un bug pour les clients ;
- coût de réécriture ;
- deux backends à maintenir pendant la transition.

## Décision 🏆

Réécriture en Go, avec une **parité stricte** :
- **Parité hybride :** tout comportement déterministe du Kotlin est reproduit à l'identique, y compris ses bugs. Cela couvre les codes, les en-têtes, le JSON et le XML octet par octet, les requêtes SQL copiées telles quelles, les erreurs Spring et le traitement des requêtes malformées.
- **Corrections autorisées :** seuls les artefacts de cache et de timing (valeurs périmées) et les fuites (verrou jamais libéré) sont corrigés. Chacun est tracé dans [`agora-go/DIVERGENCES.md`](../agora-go/DIVERGENCES.md) par classe :
  - A : identique ;
  - B : plus frais ou échec plus sain ;
  - C : écart soumis à validation ;
  - N : non sémantique.
- **Garantie de parité :**
  - Un harnais différentiel (`agora-go/parity/`) rejoue les mêmes scénarios contre le Kotlin de référence et contre le Go, sur des bases et des Redis séparés seedés à l'identique, avec deux faux Strapi.
  - Il compare statut, en-têtes, corps, état de la base après chaque étape, URI Strapi émises et payloads FCM.
  - Un oracle JVM appelle le vrai code Kotlin et les bibliothèques Java (Jackson, OWASP, Spring, JDK) pour fuzzer les briques de bas niveau : sanitizer, JSON, XML, encodages, JWT, UUID…
- **Performance :**
  - cache utilisateur local invalidé par pub/sub Redis, sans accès Redis sur le chemin d'authentification ;
  - snapshots locaux des données Strapi ;
  - micro-cache de 5 s maximum sur les données partagées non cachées ;
  - index Postgres créés en `CONCURRENTLY` par une migration séparée (`agora --migrate=up`), compatible avec le Kotlin ;
  - pools dimensionnés et timeouts partout.
- **Déploiement :**
  - une application Scalingo séparée (`PROJECT_DIR=agora-go`) sur la même base et le même Redis ;
  - une bascule progressive via Cloudflare, par famille de routes ;
  - le Kotlin reste déployable pour le rollback ;
  - un mode coexistence (`AGORA_COEXISTENCE=true`) supprime les clés de cache Kotlin rendues obsolètes par une écriture Go.

## Conséquences

- Tout écart observable non listé dans `DIVERGENCES.md` est un bug. Les écarts de classe C doivent être validés par le produit avant la bascule.
- Le harnais de parité tourne en CI (`.github/workflows/agora-go.yml`). Toute évolution fonctionnelle du Kotlin pendant la transition doit être reportée dans le Go, avec ses scénarios.
- Les crons (tâches quotidiennes et hebdomadaires, renouvellement ACME) ne tournent que dans une seule application à la fois. Ils passent côté Go à 100 % du trafic.
- Après la bascule complète et deux semaines sans rollback, une PR séparée supprime le Kotlin et le mode coexistence. Les évolutions de schéma passent alors sous la responsabilité du Go.
- Les mesures de charge comparatives (Kotlin contre Go, mêmes limites CPU et mémoire) sont consignées dans `agora-go/PERF.md`.
