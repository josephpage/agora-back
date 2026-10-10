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
| N-ALLOW-ORDER | Ordre des méthodes dans `Allow` (405 et OPTIONS) quand un chemin a plusieurs méthodes. Côté Kotlin, il suit l'ordre de réflexion des méthodes du contrôleur, qui change d'un démarrage du JVM à l'autre ; Go suit l'ordre d'enregistrement | Les clients n'interprètent pas l'ordre ; le séparateur (`, ` en 405, `,` en OPTIONS) est identique |
| N-LOGS | Libellés et volume des logs ; événements Sentry | Hors contrat HTTP. Les WARN+ partent toujours dans Sentry |

## Classe B (validée)

| Id | Kotlin | Go |
|---|---|---|
| B-USERCACHE | Principal JWT relu depuis Redis `userCache` (1 h), jamais invalidé lors d'un ban ou d'un changement de niveau | Cache L1 de 60 s, invalidé à chaque écriture utilisateur (login, suppression, upgrade/downgrade, ban) ; « not found » caché 5 s |
| B-AGORAQUEUE | Une exception pendant une action laisse l'utilisateur verrouillé (400) jusqu'au redémarrage de l'instance. De plus, la file vérifie puis ajoute en deux temps : des requêtes simultanées d'un même utilisateur peuvent toutes passer (3 créations de QaG, ou 3 soutiens en double de la même QaG, sur 6 requêtes envoyées en parallèle, sur une machine lente) | Verrou libéré par `defer` ; vérification et ajout atomiques : une seule action à la fois par utilisateur, comme la règle le prévoit |
| S0-2 | `THEME_HEBDO_CACHE_ENABLED=false` : Strapi appelé à chaque `/theme_hebdo` | Liste partagée pendant `AGORA_MICROCACHE_TTL` (5 s par défaut, jamais si vide ou en erreur) ; la réponse est déjà `max-age=10` |
| S1-B1 | Le login réécrit tout l'utilisateur lu dans le cache (jusqu'à 1 h) : un ban posé par la tâche nocturne ou un changement de niveau était annulé au login suivant | Le login n'écrit que `fcm_token` et `last_connection_date`, puis évince le principal |
| S1-B2 | Changement de niveau et ban nocturne sans éviction : autorisations et ban périmés jusqu'à 1 h | Éviction du cache utilisateur Go à ces deux événements |
| S1-B3 | `profileCache` (1 h) et `demographicInfoAskDate` (5 min) dans Redis, partagés entre instances. Bug : un `POST /profile` après `POST /profile/departments` efface les départements | Mêmes TTL, en L1 par processus. Le bug reste reproduit sur une instance ; avec plusieurs instances, il ne se produit que si la même instance a servi l'écriture précédente (sinon lecture BDD, plus fraîche). En coexistence, chaque écriture Go supprime la clé Kotlin |
| B-USERFEEDBACK (S2) | Après `POST /qags/{id}/feedback`, le cache `userFeedbackQags` garde l'ANCIENNE réponse de l'utilisateur pendant 1 h (aucune après un premier avis), donc le détail de la QaG reste périmé | La nouvelle réponse est stockée et invalidée sur toutes les instances ; en coexistence, la clé Kotlin est supprimée |
| S2-B1 | Agrégat de la QaG et réponse du gouvernement (Strapi) relus à chaque détail | Partagés au plus `AGORA_MICROCACHE_TTL` (5 s), évincés par toute écriture du module ; les données propres à l'utilisateur (soutien, auteur, avis) ne passent jamais par ce cache |
| S2-B2 | `feedbackResults` et `userFeedbackQags` dans Redis | En L1, mêmes clés et TTL (1 h), invalidation pub/sub ; plafond de 5 min et suppression des clés Kotlin en coexistence |
| B-S9-1 | Pages `/content/*`, `/welcome_page/last_news` et `/participation_charter` appellent Strapi à chaque requête | Chargement Strapi partagé au plus 5 s (succès seulement ; une liste de news vide n'est jamais gardée) |
| S3-B1 | Pages `top` / `latest` de `GET /v2/qags` et nombre de QaG acceptées (`GET /qags/count`) recalculés à chaque requête (agrégation de plus d'1 s sur un gros volume) | Page (clé : onglet, offset, thématique) et nombre partagés au plus `AGORA_MICROCACHE_TTL` (5 s). Les compteurs de soutiens, l'ordre et la composition des pages partagées peuvent avoir jusqu'à 5 s de retard, y compris après le propre soutien de l'utilisateur. Ce qui lui appartient reste exact dès sa propre écriture : `isSupportedByUser`, `isAuthor`, l'onglet « soutenues » et son nombre, la recherche |
| S3-B2 | Réponses du gouvernement (`/qags/responses`, `/qags/responses/{n}`) demandées à Strapi à chaque appel | Réponses Strapi partagées au plus 5 s (jamais une réponse vide ni une erreur) ; mêmes URI Strapi |
| S3-B3 | `trendingQagCache` (5 min) partagé dans Redis | Même clé et même TTL, en L1 par processus (5 s en coexistence) : chaque instance Go recharge une fois toutes les 5 min |
| S4-B1 | Nombre de participants d'une consultation (`participantCountConsultationDetailsV2`, 5 min) réécrit à chaque lecture : figé tant que la consultation est visitée au moins toutes les 5 min | Une entrée partagée par consultation, recalculée au plus toutes les 5 min, sans réécriture à la lecture (décision « compteur de participants figé ») |
| S4-B2 | Listes Strapi des consultations en cours ou terminées (`GET /consultations`) : la lecture du cache échoue toujours pour une liste non vide (questions polymorphes), Strapi est appelé à chaque requête | Liste Strapi partagée au plus 5 s ; mêmes URI Strapi |
| S4-B3 | `latestConsultationDetailsV2` (1 h, Redis, partagé entre instances), statistiques de feedback patchées dans le cache à chaque avis | L1 par processus, 1 h (5 s en coexistence) ; l'avis patche le cache local et les autres instances rechargent |
| S4-B4 | `hasGivenFeedbackConsultationUpdateV2` (1 h, Redis) lu avant la base | Non caché : lu en base par la même requête que « a répondu » (toujours exact) |
| S4-B5 | Statistiques d'une question de feedback calculées à chaque appel | Partagées au plus 5 s, évincées sur toutes les instances à chaque avis ; le propre avis de l'utilisateur est visible immédiatement |
| *(complété par les tranches suivantes : compteur de participants figé, `userFeedbackQags`, cache en ajout seul, verrous Moderatus, entrées `RedisCacheManager` par utilisateur)* | | |

## Classe C (à valider)

| Id | Écart | Raison | Statut |
|---|---|---|---|
| C-XML-BODY | Un corps de requête `application/xml` est refusé (400). Kotlin l'aurait désérialisé avec Jackson XML | Aucun client n'envoie de XML ; évite une surface d'attaque | à valider |
| C-STRAPI-TIMEOUT | Les appels Strapi ont un timeout global de 60 s. Kotlin n'avait que 20 s de connexion, sans limite de lecture | Un Strapi lent ne doit pas épuiser le serveur | à valider |
| C-SWAGGER | `/v3/api-docs` est un snapshot figé de la référence ; `Last-Modified` des assets = démarrage du process | Les routes Go sont identiques, donc le document aussi | à valider |
| C-JSESSIONID | Sur un 401 non-JSON, Go émet toujours un nouveau `JSESSIONID`, même si le client en présente un. Kotlin réutilisait une session existante de la même instance | Session sans aucun usage fonctionnel (aucun état) | à valider |
| C-LONE-SURROGATE | Un surrogate UTF-16 isolé dans un corps JSON (échappement `"\ud800"`, octets `ED A0 80`, unité UTF-32) devient `?` dès le décodage en Go. Kotlin garde le caractère isolé : le JDBC le stocke en `?` (identique), le sanitizer OWASP le supprime (Go garde `?`), un écho JSON l'écrirait `\uD800` (Go `?`) | Une chaîne Go ne peut pas porter un surrogate isolé sans risquer un UTF-8 invalide jusqu'à Postgres (erreur 500). Seule une saisie tronquée au milieu d'un emoji par un client web peut en produire | à valider |
| C-CHARSET-EXOTIC | Un corps de requête déclaré dans un charset multi-octets historique du JVM (Shift_JIS, EUC-*, GBK, GB18030, Big5, ISO-2022-*, x-IBM93x, CESU-8, x-UTF-*-BOM… : 62 charsets) est refusé en 415. Kotlin le décodait. Les charsets Unicode (UTF-8/16/32, US-ASCII) et les 104 charsets mono-octets (ISO-8859-*, windows-125x, IBM*, Mac*, KOI8…) sont identiques, avec des tables générées depuis le JVM | Aucun client ne les utilise ; reproduire leurs décodeurs du JDK octet par octet n'apporte rien | à valider |
| C-XML-CONTROL-CHAR | Une valeur contenant un caractère de contrôle interdit en XML (U+0000–U+001F hors tabulation, LF et CR), servie avec `?mediaType=xml`. Kotlin envoie un 200 avec un XML tronqué suivi d'un corps d'erreur (Woodstox échoue après l'envoi des en-têtes) ; Go répond un 500 propre | Seul Moderatus consomme du XML, sur des données (titres de QaG) qui ne contiennent pas ces caractères | à valider |
| C-S3-SURROGATE | Texte d'une réponse du gouvernement coupé à 400 unités UTF-16 au milieu d'un emoji (`GET /qags/responses/{n}`) : Kotlin écrit le demi-caractère isolé (`\uD83D...`), Go écrit `?...` | Même famille que `C-LONE-SURROGATE` : une chaîne Go ne porte pas de surrogate isolé. Seul le 400e caractère d'un texte Strapi est concerné | à valider |
| C-JDK17-DOUBLE | `Double.toString` du JDK 17 n'est pas « shortest » (`2e23` → `1.9999999999999998E23`) ; Go utilise l'écriture la plus courte | Aucun double n'apparaît dans les réponses JSON (ratios entiers) ; le TSV passe par `NumberFormat` | sans objet tant qu'aucun double n'est sérialisé |

## Par tranche

Chaque tranche documente ses écarts éventuels dans `parity/divergences/<tranche>.md` ; ils sont reportés ici à l'intégration.
