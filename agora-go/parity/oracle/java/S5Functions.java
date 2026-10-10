import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import fr.gouv.agora.domain.Concertation;
import fr.gouv.agora.domain.ConsultationPreviewFinished;
import fr.gouv.agora.domain.Territoire;
import fr.gouv.agora.domain.Thematique;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.common.StrapiDTO;
import fr.gouv.agora.infrastructure.concertations.ConcertationMapper;
import fr.gouv.agora.infrastructure.concertations.ConcertationStrapiDTO;
import fr.gouv.agora.infrastructure.consultation.ConsultationPreviewJsonMapper;
import fr.gouv.agora.infrastructure.consultationPaginated.ConsultationPaginatedJsonMapper;
import fr.gouv.agora.infrastructure.thematique.ThematiqueJsonMapper;
import fr.gouv.agora.usecase.concertations.ConcertationJsonMapper;
import fr.gouv.agora.usecase.consultation.ConsultationPreviewFinishedMapper;
import fr.gouv.agora.usecase.consultation.repository.ConsultationWithUpdateInfo;
import fr.gouv.agora.usecase.consultationPaginated.ConsultationAnsweredPaginatedList;
import fr.gouv.agora.usecase.consultationPaginated.ConsultationFinishedPaginatedList;
import fr.gouv.agora.usecase.consultationPaginated.ConsultationsAnsweredPaginatedListUseCase;
import fr.gouv.agora.usecase.consultationPaginated.ConsultationsFinishedPaginatedListUseCase;
import fr.gouv.agora.usecase.consultationPaginated.repository.ConsultationAnsweredPaginatedListCacheRepository;
import fr.gouv.agora.usecase.consultationPaginated.repository.ConsultationFinishedPaginatedListCacheRepository;
import fr.gouv.agora.usecase.consultationPaginated.repository.ConsultationPreviewAnsweredRepository;
import fr.gouv.agora.usecase.consultationPaginated.repository.ConsultationPreviewFinishedRepository;
import fr.gouv.agora.usecase.thematique.repository.ThematiqueRepository;
import kotlin.collections.CollectionsKt;

import java.time.Clock;
import java.time.Instant;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Map;

/**
 * Oracle functions of slice S5 (consultation lists, concertations). They run the REAL Kotlin mappers and use cases of
 * the reference jar.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S5Functions {
    private S5Functions() {
    }

    private static List<Thematique> thematiques(JsonNode a) {
        List<Thematique> out = new ArrayList<>();
        for (JsonNode t : a.get("thematiques")) {
            out.add(new Thematique(t.get("id").textValue(), t.get("label").textValue(), t.get("picto").textValue()));
        }
        return out;
    }

    private static String str(JsonNode n, String k) {
        JsonNode v = n.get(k);
        return v == null || v.isNull() ? null : v.textValue();
    }

    private static LocalDateTime ldt(JsonNode n, String k) {
        return LocalDateTime.parse(n.get(k).textValue());
    }

    static void register(Map<String, Oracle.Fn> F) {
        // The Strapi list of concertations -> ConcertationMapper.toConcertations -> GetConcertationsUseCase's sort ->
        // ConcertationJsonMapper: the JSON and XML of the response. A payload the DTO cannot read is
        // {"decodeError": true} (CmsStrapiHttpClient then answers an empty list); an exception of the mapper (a null
        // element of "data") is {"mapperError": true} (HTTP 500).
        F.put("s5Concertations", a -> {
            ObjectMapper m = Functions.appMapper();
            StrapiDTO<ConcertationStrapiDTO> dto;
            try {
                dto = m.readValue(Functions.s(a, "json"), new TypeReference<StrapiDTO<ConcertationStrapiDTO>>() {
                });
                if (dto == null) {
                    throw new NullPointerException();
                }
            } catch (Exception e) {
                return Functions.map("decodeError", true);
            }
            List<Concertation> list;
            try {
                list = new ConcertationMapper().toConcertations(dto, thematiques(a));
            } catch (Exception e) {
                return Functions.map("mapperError", true);
            }
            // sortedByDescending { it.updateDate }
            List<Concertation> sorted = CollectionsKt.sortedWith(list, (Comparator<Concertation>) (x, y) -> y.getUpdateDate().compareTo(x.getUpdateDate()));
            Object body = new ConcertationJsonMapper(new ThematiqueJsonMapper(), new DateMapper()).toConcertationJson(sorted);
            return Functions.map("json", Functions.encode(Functions.springJson(), body), "xml", Functions.encode(Functions.springXml(), body));
        });

        // ConsultationPaginatedJsonMapper: the three toJson variants on a list of ConsultationPreviewFinished.
        F.put("s5Paginated", a -> {
            Clock clock = Clock.fixed(Instant.ofEpochMilli(a.get("nowMs").longValue()), ZoneId.systemDefault());
            List<ConsultationPreviewFinished> list = new ArrayList<>();
            for (JsonNode c : a.get("list")) {
                JsonNode t = c.get("thematique");
                list.add(new ConsultationPreviewFinished(str(c, "id"), str(c, "slug"), str(c, "title"), str(c, "coverUrl"),
                        new Thematique(str(t, "id"), str(t, "label"), str(t, "picto")), str(c, "updateLabel"),
                        ldt(c, "lastUpdateDate"), ldt(c, "endDate"), str(c, "territory")));
            }
            int max = a.get("maxPageNumber").intValue();
            ConsultationPaginatedJsonMapper mapper = new ConsultationPaginatedJsonMapper(
                    new ConsultationPreviewJsonMapper(new ThematiqueJsonMapper(), new DateMapper(), clock));
            ObjectNode out = Functions.springJson().createObjectNode();
            Object[] bodies = {
                    mapper.toJson(new ConsultationFinishedPaginatedList(list, max)),
                    mapper.toJson(new ConsultationAnsweredPaginatedList(list, max)),
                    mapper.toJson(list),
            };
            String[] names = {"finished", "answered", "list"};
            for (int i = 0; i < bodies.length; i++) {
                out.put(names[i] + "Json", Functions.encode(Functions.springJson(), bodies[i]));
                out.put(names[i] + "Xml", Functions.encode(Functions.springXml(), bodies[i]));
            }
            return out;
        });

        // ConsultationsFinishedPaginatedListUseCase / ConsultationsAnsweredPaginatedListUseCase with fake repositories (empty
        // caches): the existence of the page, maxPageNumber, the ids of the page and the calls made (in order).
        F.put("s5UseCase", a -> {
            String kind = Functions.s(a, "kind");
            int count = a.get("count").intValue();
            int page = a.get("page").intValue();
            List<Thematique> themes = thematiques(a);
            List<ConsultationWithUpdateInfo> infos = new ArrayList<>();
            for (JsonNode c : a.get("infos")) {
                infos.add(new ConsultationWithUpdateInfo(str(c, "id"), str(c, "slug"), str(c, "title"), str(c, "coverUrl"), str(c, "thematiqueId"),
                        ldt(c, "endDate"), ldt(c, "updateDate"), str(c, "updateLabel"), str(c, "territory")));
            }
            List<String> calls = new ArrayList<>();
            ThematiqueRepository themeRepo = S2Functions.proxy(ThematiqueRepository.class, (p, method, args) -> {
                calls.add(method.getName());
                return method.getName().equals("getThematiqueList") ? themes : null;
            });
            ConsultationPreviewFinishedMapper mapper = new ConsultationPreviewFinishedMapper();
            ObjectNode out = Functions.springJson().createObjectNode();
            try {
                List<ConsultationPreviewFinished> result;
                int maxPage;
                if (kind.equals("finished")) {
                    ConsultationPreviewFinishedRepository repo = S2Functions.proxy(ConsultationPreviewFinishedRepository.class, (p, method, args) -> {
                        if (method.getName().equals("getConsultationFinishedCount")) {
                            calls.add("count");
                            return count;
                        }
                        if (args.length == 3) {
                            calls.add("list:" + args[0] + "/" + args[1] + "/" + ((Territoire) args[2]).getValue());
                        } else {
                            calls.add("listByTerritories");
                        }
                        return infos;
                    });
                    ConsultationFinishedPaginatedListCacheRepository cache = S2Functions.proxy(ConsultationFinishedPaginatedListCacheRepository.class, (p, method, args) -> {
                        calls.add("cache." + method.getName());
                        return null;
                    });
                    ConsultationFinishedPaginatedList r = new ConsultationsFinishedPaginatedListUseCase(repo, themeRepo, mapper, cache)
                            .getConsultationFinishedPaginatedList(page, Functions.s(a, "territory"));
                    result = r == null ? null : r.getConsultationFinishedList();
                    maxPage = r == null ? 0 : r.getMaxPageNumber();
                } else {
                    ConsultationPreviewAnsweredRepository repo = S2Functions.proxy(ConsultationPreviewAnsweredRepository.class, (p, method, args) -> {
                        if (method.getName().equals("getConsultationAnsweredCount")) {
                            calls.add("count");
                            return count;
                        }
                        calls.add("list:" + args[0] + "/" + args[1]);
                        return infos;
                    });
                    ConsultationAnsweredPaginatedListCacheRepository cache = S2Functions.proxy(ConsultationAnsweredPaginatedListCacheRepository.class, (p, method, args) -> {
                        calls.add("cache." + method.getName());
                        return null;
                    });
                    ConsultationAnsweredPaginatedList r = new ConsultationsAnsweredPaginatedListUseCase(repo, themeRepo, mapper, cache)
                            .getConsultationAnsweredPaginatedList("userId", page);
                    result = r == null ? null : r.getConsultationAnsweredList();
                    maxPage = r == null ? 0 : r.getMaxPageNumber();
                }
                if (result == null) {
                    out.putNull("result");
                } else {
                    ObjectNode r = out.putObject("result");
                    r.put("maxPageNumber", maxPage);
                    ArrayNode ids = r.putArray("ids");
                    for (ConsultationPreviewFinished c : result) {
                        ids.add(c.getId());
                    }
                }
            } catch (Exception e) {
                out.put("error", e.getClass().getSimpleName() + ": " + e.getMessage());
            }
            ArrayNode cs = out.putArray("calls");
            for (String c : calls) {
                cs.add(c);
            }
            return out;
        });
    }
}
