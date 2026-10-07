# Registre des divergences Kotlin → Go

Toute différence observable entre le backend Kotlin (référence figée au commit
`2898584`) et la réécriture Go est listée ici. Par défaut, le comportement est
**identique (classe A)** et vérifié par le harnais différentiel (`parity/`).

| Classe | Signification | Validation |
|---|---|---|
| **A** | Identique | Harnais : 0 diff |
| **B** | Go est seulement « plus frais » (artefact de cache Kotlin corrigé) ou échoue plus proprement (fuite corrigée) | Décision produit du 6 oct. ; tolérance dédiée dans le harnais |
| **C** | Écart assumé, à valider explicitement par le produit | En attente de validation (voir colonne « Statut ») |
| **N** | Non sémantique : invisible pour les clients HTTP | — |

## Non sémantique (N)

| Id | Écart | Pourquoi c'est invisible |
|---|---|---|
| N-HDR-ORDER | Ordre des en-têtes HTTP (Go les trie, Tomcat suit l'ordre d'écriture) | HTTP ne donne aucun sens à l'ordre des en-têtes ; nginx les relaie tels quels |
| N-REASON | Ligne de statut : Tomcat n'écrit pas de « reason phrase » (`HTTP/1.1 401 `), Go si (`401 Unauthorized`) | nginx réécrit la ligne de statut vers le client |
| N-FRAMING | `Transfer-Encoding: chunked` (Tomcat) vs `Content-Length` (Go), `Connection: close` de Tomcat sur 400/500 | Cadrage HTTP géré par nginx ; corps identique |
| N-LOGS | Libellés et volume des logs ; événements Sentry | Hors contrat HTTP. Les WARN+ partent toujours dans Sentry |

## Classe B (validée)

| Id | Kotlin | Go |
|---|---|---|
| B-USERCACHE | Principal JWT relu depuis Redis `userCache` (1 h), jamais invalidé lors d'un ban ou d'un changement de niveau | Cache L1 de 60 s, invalidé à chaque écriture utilisateur (login, suppression, upgrade/downgrade, ban) ; « not found » caché 5 s |
| B-AGORAQUEUE | Une exception pendant une action laisse l'utilisateur verrouillé (400) jusqu'au redémarrage de l'instance | Verrou libéré par `defer` |
| *(complété par les tranches : compteur de participants figé, `userFeedbackQags`, cache en ajout seul, verrous Moderatus, entrées `RedisCacheManager` par utilisateur)* | | |

## Classe C (à valider)

| Id | Écart | Raison | Statut |
|---|---|---|---|
| C-XML-BODY | Un corps de requête `application/xml` est refusé (400). Kotlin l'aurait désérialisé avec Jackson XML | Aucun client n'envoie de XML ; évite une surface d'attaque | à valider |
| C-STRAPI-TIMEOUT | Les appels Strapi ont un timeout global de 60 s. Kotlin n'avait que 20 s de connexion, sans limite de lecture | Un Strapi lent ne doit pas épuiser le serveur | à valider |
| C-SWAGGER | `/v3/api-docs` est un snapshot figé de la référence ; `Last-Modified` des assets = démarrage du process | Les routes Go sont identiques, donc le document aussi | à valider |
| C-JSESSIONID | Sur un 401 non-JSON, Go émet toujours un nouveau `JSESSIONID`, même si le client en présente un. Kotlin réutilisait une session existante de la même instance | Session sans aucun usage fonctionnel (aucun état) | à valider |
| C-JDK17-DOUBLE | `Double.toString` du JDK 17 n'est pas « shortest » (`2e23` → `1.9999999999999998E23`) ; Go utilise l'écriture la plus courte | Aucun double n'apparaît dans les réponses JSON (ratios entiers) ; le TSV passe par `NumberFormat` | sans objet tant qu'aucun double n'est sérialisé |

## Par tranche

Chaque tranche documente ses écarts éventuels dans `parity/divergences/<tranche>.md` ; ils sont reportés ici à l'intégration.
