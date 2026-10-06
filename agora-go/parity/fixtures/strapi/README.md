# Fake Strapi fixtures

JSON content served by `parity/fakestrapi` (a fake Strapi v5 REST API) so that the
Kotlin reference backend and the Go rewrite can be pointed at the same CMS and their
outputs diffed.

```
cd agora-go
go run ./parity/fakestrapi/cmd/fakestrapi -addr :1337 -fixtures parity/fixtures/strapi   # -now 2026-10-06T12:00:00Z to pin the clock
go test ./parity/fakestrapi/                                                             # query engine + fixture checks
```

The default `-fixtures` is `../fixtures/strapi` (resolved against the cwd, the module root
and `parity/fakestrapi`), so it also works from the module root without arguments.
Restart the fake right before a parity run: `{{now...}}` is evaluated once at start
(`-now`, default the current UTC time truncated to the second).

## How the files are used

* `<model>.json` holds the model served at `GET /api/<model>`.
  * JSON **array** = collection type -> `{"data":[...],"meta":{"pagination":{page,pageSize,pageCount,total}}}`
  * JSON **object** = single type -> `{"data":{...},"meta":{}}`
* Documents are stored **fully populated**; the `populate*` parameters are accepted and ignored
  (so list endpoints also return sections, which the Kotlin list code simply ignores).
* Only `*.json` files at the root of this directory are loaded. `variants/` and `tools/` are ignored by the loader.
* Key order of the fixtures is preserved in responses; documents are returned in file order unless `sort[...]` is given
  (ties keep file order).
* `publishedAt`: a document whose `publishedAt` is `null` is a draft that only shows up with `status=draft`
  (which returns draft versions of *all* documents, published or not, unchanged). A document without the key counts as published.
  A single type with `"publishedAt": null` answers 404 unless `status=draft`.

### Query language supported (exactly what `StrapiRequestBuilder` emits, plus a few extras)

| Parameter | Behaviour |
|---|---|
| `pagination[pageSize]=N` (`[page]`, `[start]`/`[limit]`) | default 25, clamped to 100 |
| `status=draft` | see above |
| `populate...`, `fields...` | ignored |
| `filters[a][b][$in]=v` (repeated) | OR of the values; nested paths go through objects and arrays of objects |
| `filters[f][$containsi]=v` | case-insensitive substring (`$contains` case-sensitive) |
| `filters[f][$lt|$lte|$gt|$gte]=v` | dates compared as instants (zone-less = UTC, `2026-10-06T21:52:47.123456`, `...Z`, offsets, date-only), numbers numerically, otherwise as strings |
| `filters[f][$eq|$ne|$notIn|$null|$notNull|$startsWith|$endsWith]` | extras |
| `sort[0]=field:asc|desc` (`sort[1]`..., dotted paths) | dates/numbers/strings; nulls last when ascending |

Several filters are AND-ed. Values are decoded with `url.ParseQuery` (`+` and `%20` = space).

### Date templating

Any JSON string of the exact form `{{now}}`, `{{now+3d}}`, `{{now-2h}}`, `{{now-30m}}`, `{{now+10s}}`, `{{now+1w}}`
(offsets can be chained: `{{now+5d+5m}}`) is replaced at load by that instant formatted like Strapi
(`2006-01-02T15:04:05.000Z`, UTC). `{{date:now+3d}}` gives the date only (`2006-01-02`). Any other `{{...}}` string is a load error.

### Control endpoints (not recorded in the request log)

| Endpoint | Effect |
|---|---|
| `GET /__control/requests` | JSON array of `{method, uri (raw request URI as received), authorization, time, status}` |
| `DELETE /__control/requests` | clear the log |
| `POST /__control/fault` `{"model":"<model>\|*","mode":"none\|500\|malformed\|nulldata\|slow\|missingfield","delayMs":N}` | per-model (or `*`) fault. `500` Strapi error JSON; `malformed` 200 + truncated JSON; `nulldata` `{"data":null,"meta":{}}`; `slow` sleeps `delayMs` then answers normally; `missingfield` drops the first string field (fixture order, skipping id/documentId/createdAt/...) of the first data element |
| `POST /__control/override` `{"model":"theme-hebdos","fixture":"variants/theme-hebdos.libre-current.json"}` or `{"model":...,"data":[...]}` | replace a model's data at runtime (templates resolved) |
| `POST /__control/reset` | clear faults, log **and overrides** |
| `POST /__control/reload` | re-read the fixture files from disk |

Unknown model: `404 {"data":null,"error":{"status":404,"name":"NotFoundError","message":"Not Found","details":{}}}`.
`GET /api/<collection>/<documentId>` is also served.

Remember that the Kotlin reference caches Strapi results (Redis `FLUSHALL`, and a 5 min in-JVM
consultation-by-id cache): flush/restart it after changing faults or overrides.

## Shared ID plan (also used by `parity/seed/ids.go`)

| Model | Ids |
|---|---|
| `thematiques` | documentIds `th0000000000000000000001` .. `th0000000000000000000006` (Environnement, Sante, Education, Transports, Numerique, Democratie), label + pictogramme |
| `consultations` | documentIds `co0000000000000000000001` .. `...07`, slugs `consultation-1` .. `consultation-7` |
| consultation questions | component ids (JSON numbers) `c*100+q`, i.e. 101..106, 201..206, ... 701..706; `numero` = q |
| question choices | `c*1000+q*10+k` (1011,1012,1013 ...) |
| `reponse-du-gouvernements` | `questionId` = `00000000-0000-4000-8000-0000000000a1` (video), `...a2` (text), `...a3` (video + additional info) |
| consultation updates | `cu0000000000000000000001` (co1 "actualite-2", its latest published update), `cu...02` (co2 "actualite-1"), `cu...03` (co3 "fin-de-la-consultation") - the ids used by the DB seed for update feedbacks; all other updates are `up<22 digits: consultation*100+kind>` (kind 01 avant reponse, 02 apres reponse, 03 analyse, 04 reponse du commanditaire, 05.. autres, 90 a venir), e.g. `up0000000000000000000102` |
| other models | `ce...` concertations, `fi...` fiches inventaire, `he...` theme-hebdos, `ch...` chartes, `ne...` news, `qh...` headers, `cl...` clusters, `re...` reponses, `pg...` single types (24 chars: 2 letters + zero padded number) |

### Consultations

| co | start -> end (relative to now) | published | territoire | thematique | update content |
|---|---|---|---|---|---|
| 1 | -10d -> +10d (ongoing) | yes | France | th1 | 3 autres (-5d, -2d, **+3d future**), reponse du commanditaire **+30d (future)**, a venir; 8 sections in avant reponse (all 7 section types), rich-text "kitchen sink" |
| 2 | -5d -> +25d (ongoing) | yes | Nord (department) | th2 | 1 autre (-1d), analyse **+40d (future)** with pdf_analyse, a venir |
| 3 | -1d -> +3d (ongoing, ends soon) | yes | Ile-de-France (region) | th3 | none besides avant/apres; no images (url fallbacks) |
| 4 | -40d -> -3d (finished 3 days ago) | yes | France | th4 | 1 autre (-10d), analyse (-2d, no pdf -> url fallback), reponse du commanditaire (-1d) |
| 5 | -60d -> -10d (finished 10 days ago) | yes | Bretagne (region) | th5 | analyse (-8d, no flamme label), a venir |
| 6 | -2d -> +20d (ongoing) | **no (publishedAt null)** | France | th6 | 1 autre (-1d); only visible with `status=draft` |
| 7 | -90d -> -30d (finished 30 days ago, > 14 days: daily aggregation) | yes | France | th1 | 2 autres (-60d, -40d), analyse (-25d, with pdf_analyse), reponse du commanditaire (-20d) |

Each consultation has the same six questions:

| q | numero | kind | next |
|---|---|---|---|
| 1 | 1 | unique choice (3 choices), has `popup_explication` | null -> q2 |
| 2 | 2 | multiple choices (3 choices, `nombre_maximum_de_choix` 2) | null -> q3 |
| 3 | 3 | open question | null -> q4 (explicit 4 on even consultations) |
| 4 | 4 | conditional: choice 1 -> numero 5, choices 2 and 3 -> numero 6 | per choice |
| 5 | 5 | description / chapter (rich text; picture and url fallback variants) | 6 |
| 6 | 6 | unique choice, choice 3 has an open text field | 999 (end of form) |

Cover/content pictures vary on purpose: `formats.medium` present, `formats` without `medium`, `formats: null`, picture `null`
(Kotlin falls back to `url_image_*` / `url`). `nombre_de_questions` is 5 (the chapter is not a question).

### Other models

* `theme-hebdos` (7): two past (one of them a free week), **one current week** (-2d -> +5d, no `periode` so it is
  computed), four future in non chronological order (one of them `est_theme_libre`). The variant
  `variants/theme-hebdos.libre-current.json` makes the current week a "theme libre" (this enables the trending cluster
  filtering of `/v2/qags` and the `cluster-semaine-libres` request); activate it with
  `POST /__control/override {"model":"theme-hebdos","fixture":"variants/theme-hebdos.libre-current.json"}`
  (`{"model":"theme-hebdos","data":[]}` gives "no current theme", i.e. the Kotlin defaults). `POST /__control/reset` restores.
* `cluster-semaine-libres` (5): 3 usable clusters, one with empty and one with null `keywords` (skipped with a warning).
* `qa-g-headers-onglets` (12): for each of `top|latest|supporting|trending` an old, a current and a future header.
* `charte-participations` (3): old, current (kitchen-sink rich text) and future version.
* `welcome-page-news` (3): old, current (latest started), future (must not be returned).
* `concertations` (5): one with an unknown thematique (dropped by the mapper), two with the same date, picture variants.
* `fiche-inventaires` (5): varied `etape` (Lancement/Analyse/Suivi), `condition_participation`, `modalite_participation`,
  `annee_de_lancement` (2022-2025), thematiques, `debut` (sorted desc), picture variants.
* `reponse-du-gouvernements` (3): see ids above; dates -2d / -6d / -12d; portrait picture present / null / without medium;
  video with / without `video` media, with / without additional information.
* Single types: `page-reponse-aux-questions-au-gouvernement`, `page-poser-ma-question`, `page-questions-au-gouvernement`,
  `site-vitrine-accueil`, `site-vitrine-conditions-generales-d-utilisation`, `site-vitrine-consultation`,
  `site-vitrine-declaration-d-accessibilite`, `site-vitrine-mentions-legale`, `site-vitrine-politique-de-confidentialite`,
  `site-vitrine-question-au-gouvernement`.

### Coverage helpers

* Every rich-text node type is present (text with bold/italic/underline/strikethrough/code, all combined and explicit
  `false`; link; ordered/unordered list incl. nested; list-item; headings 1-6, an invalid level 7 and a missing level;
  paragraph incl. empty; quote; unknown block types `code` and `image`) - see `site-vitrine-mentions-legale`, `charte-participations`
  (current), `page-poser-ma-question`, `consultations` (co1 presentation / chapter) and `reponse-du-gouvernements` (a2).
* Every consultation section type (titre, texte-riche, citation, image, video, chiffre, accordeon), with and without the
  Strapi media relation, and every question component and response component.
* Text containing `&`, `<`, `>`, quotes, backslashes, accents and emoji to exercise HTML/JSON escaping parity
  (the Kotlin `toHtml` does not escape).

## Regenerating

All JSON files and the variant are produced by `tools/gen_fixtures.py` (`python3 tools/gen_fixtures.py` from this
directory); edit the script rather than the JSON. `go test ./parity/fakestrapi/` checks the ID plan, the presence of
every model/variant and that every non-nullable Kotlin DTO field is present.
