import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import fr.gouv.agora.domain.Territoire;
import fr.gouv.agora.domain.Thematique;
import fr.gouv.agora.domain.ThemeHebdo;
import fr.gouv.agora.infrastructure.common.StrapiDTO;
import fr.gouv.agora.infrastructure.referentiel.TerritoiresJsonMapper;
import fr.gouv.agora.infrastructure.thematique.ThematiqueJsonMapper;
import fr.gouv.agora.infrastructure.themeHebdo.ThemeHebdoJsonMapper;
import fr.gouv.agora.infrastructure.themeHebdo.repository.ThemeHebdoMapper;
import fr.gouv.agora.infrastructure.themeHebdo.repository.ThemeHebdoStrapiDTO;
import fr.gouv.agora.usecase.thematique.ListThematiqueUseCase;
import fr.gouv.agora.usecase.thematique.repository.ThematiqueRepository;
import fr.gouv.agora.usecase.themeHebdo.GetThemeHebdoUseCase;
import fr.gouv.agora.usecase.themeHebdo.IsThemeHebdoTransitionUseCase;
import fr.gouv.agora.usecase.themeHebdo.repository.CurrentThemeHebdoCacheRepository;
import fr.gouv.agora.usecase.themeHebdo.repository.CurrentThemeHebdoCacheResult;
import fr.gouv.agora.usecase.themeHebdo.repository.ThemeHebdoRepository;

import java.time.Clock;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * Oracle functions of slice S0 (thematiques, theme hebdo, referentiels). They run the REAL Kotlin
 * mappers / use cases of the reference jar.
 *
 * <p>A domain ThemeHebdo travels as JSON: {titre, sousTitre, periode, theme, avatarUrl, nom, fonction,
 * prochainsThemes, titreCompteur, dateDebutTheme (epoch ms | null), dateFinTheme, estThemeLibre};
 * missing properties take the Kotlin default values (decoded with the application ObjectMapper).
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S0Functions {
    private S0Functions() {
    }

    static ThemeHebdo themeFrom(JsonNode n) throws Exception {
        return Functions.appMapper().treeToValue(n, ThemeHebdo.class);
    }

    static List<ThemeHebdo> themesFrom(JsonNode a) throws Exception {
        List<ThemeHebdo> out = new ArrayList<>();
        JsonNode arr = a.get("themes");
        if (arr != null && arr.isArray()) {
            for (JsonNode n : arr) {
                out.add(themeFrom(n));
            }
        }
        return out;
    }

    static JsonNode tree(Object v) {
        return Functions.appMapper().valueToTree(v);
    }

    static long l(JsonNode a, String k) {
        JsonNode n = a.get(k);
        if (n == null || !n.isIntegralNumber()) {
            throw new IllegalArgumentException("argument '" + k + "' must be an integer");
        }
        return n.longValue();
    }

    static ThemeHebdoRepository repo(List<ThemeHebdo> themes) {
        return () -> themes;
    }

    static void register(Map<String, Oracle.Fn> F) {
        // ThemeHebdoMapper.toDomain on a decoded Strapi payload. Like CmsStrapiHttpClient.request, a payload that
        // cannot be decoded gives an empty list; an exception of the mapper (bad date...) is propagated.
        F.put("themeHebdoMap", a -> {
            ObjectMapper m = Functions.appMapper();
            JavaType t = m.getTypeFactory().constructParametricType(StrapiDTO.class, ThemeHebdoStrapiDTO.class);
            StrapiDTO<ThemeHebdoStrapiDTO> dto;
            try {
                dto = m.readValue(Functions.s(a, "json"), t);
                if (dto == null) {
                    throw new NullPointerException();
                }
            } catch (Exception e) {
                dto = StrapiDTO.Companion.<ThemeHebdoStrapiDTO>ofEmpty();
            }
            return tree(new ThemeHebdoMapper().toDomain(dto));
        });

        // ThemeHebdoJsonMapper.toJson, serialized like Spring MVC (JSON string)
        F.put("themeHebdoJson", a -> {
            ThemeHebdo th = themeFrom(a.get("theme"));
            return Functions.encode(Functions.springJson(), new ThemeHebdoJsonMapper().toJson(th));
        });

        // GetThemeHebdoUseCase.getThemeHebdo with a fixed clock, then the controller's JSON body
        F.put("getThemeHebdo", a -> {
            Clock clock = Clock.fixed(Instant.ofEpochMilli(l(a, "nowMs")), ZoneOffset.UTC);
            CurrentThemeHebdoCacheRepository cache = new CurrentThemeHebdoCacheRepository() {
                public CurrentThemeHebdoCacheResult getCurrentThemeHebdo() {
                    return CurrentThemeHebdoCacheResult.CacheNotInitialized.INSTANCE;
                }

                public void insertCurrentThemeHebdo(ThemeHebdo themeHebdo) {
                }
            };
            ThemeHebdo r = new GetThemeHebdoUseCase(repo(themesFrom(a)), cache, clock).getThemeHebdo();
            Map<String, Object> out = Functions.map("theme", tree(r));
            try {
                out.put("json", Functions.encode(Functions.springJson(), new ThemeHebdoJsonMapper().toJson(r)));
            } catch (Throwable t) {
                out.put("jsonError", t.getClass().getSimpleName());
            }
            return out;
        });

        F.put("isThemeHebdoTransition", a -> {
            Clock clock = Clock.fixed(Instant.ofEpochMilli(l(a, "nowMs")), ZoneOffset.UTC);
            return new IsThemeHebdoTransitionUseCase(repo(themesFrom(a)), clock).isInTransition();
        });

        // ListThematiqueUseCase (sort) + ThematiqueJsonMapper, serialized like Spring MVC (JSON string)
        F.put("thematiqueList", a -> {
            List<Thematique> list = new ArrayList<>();
            for (JsonNode n : a.get("thematiques")) {
                list.add(new Thematique(Functions.s(n, "id"), Functions.s(n, "label"), Functions.s(n, "picto")));
            }
            ThematiqueRepository repo = new ThematiqueRepository() {
                public List<Thematique> getThematiqueList() {
                    return list;
                }

                public Thematique getThematique(String id) {
                    return null;
                }
            };
            List<Thematique> sorted = new ListThematiqueUseCase(repo).getThematiqueList();
            return Functions.encode(Functions.springJson(), new ThematiqueJsonMapper().toJson(sorted));
        });

        // ReferentielController body: JSON and XML
        F.put("referentielBody", a -> {
            Object body = new TerritoiresJsonMapper().toJson(Territoire.Region.values(), Territoire.Pays.values());
            return Functions.map(
                    "json", Functions.encode(Functions.springJson(), body),
                    "xml", Functions.encode(Functions.springXml(), body));
        });

        // String.uppercase() (toUpperCase(Locale.ROOT)) of each string of "list"
        F.put("javaUpperCase", a -> {
            ArrayNode out = Functions.appMapper().createArrayNode();
            for (JsonNode n : a.get("list")) {
                out.add(n.textValue().toUpperCase(Locale.ROOT));
            }
            return out;
        });
    }
}
