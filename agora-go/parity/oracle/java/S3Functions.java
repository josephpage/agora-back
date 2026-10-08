import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import fr.gouv.agora.config.CmsStrapiHttpClient;
import fr.gouv.agora.domain.HeaderQag;
import fr.gouv.agora.domain.IncomingResponsePreview;
import fr.gouv.agora.domain.QagPreview;
import fr.gouv.agora.domain.QagStatus;
import fr.gouv.agora.domain.ResponseQag;
import fr.gouv.agora.domain.ResponseQagPreview;
import fr.gouv.agora.domain.ResponseQagPreviewWithoutOrder;
import fr.gouv.agora.domain.ResponseQagVideo;
import fr.gouv.agora.domain.Thematique;
import fr.gouv.agora.domain.ThemeHebdo;
import fr.gouv.agora.domain.TrendingCluster;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.common.StrapiDTO;
import fr.gouv.agora.infrastructure.qagHome.QagHomeJsonMapper;
import fr.gouv.agora.infrastructure.qagPaginated.QagPaginatedJsonMapper;
import fr.gouv.agora.infrastructure.qagPaginated.repository.TrendingQagCacheRepository;
import fr.gouv.agora.infrastructure.responseQag.dto.StrapiResponseQag;
import fr.gouv.agora.infrastructure.responseQag.repository.ResponseQagMapper;
import fr.gouv.agora.infrastructure.responseQag.repository.ResponseQagRepositoryImpl;
import fr.gouv.agora.infrastructure.responseQag.repository.ResponseQagStrapiRepository;
import fr.gouv.agora.infrastructure.responseQagPaginated.ResponseQagPaginatedJsonMapper;
import fr.gouv.agora.infrastructure.thematique.ThematiqueJsonMapper;
import fr.gouv.agora.usecase.qag.QagPreviewMapper;
import fr.gouv.agora.usecase.qag.repository.QagInfo;
import fr.gouv.agora.usecase.qag.repository.QagInfoRepository;
import fr.gouv.agora.usecase.qag.repository.QagInfoWithSupportCount;
import fr.gouv.agora.usecase.qagPaginated.QagPaginatedV2UseCase;
import fr.gouv.agora.usecase.qagPaginated.QagsAndMaxPageCountV2;
import fr.gouv.agora.usecase.qagPaginated.repository.HeaderQagCacheRepository;
import fr.gouv.agora.usecase.qagPaginated.repository.HeaderQagCacheResult;
import fr.gouv.agora.usecase.qagPaginated.repository.HeaderQagRepository;
import fr.gouv.agora.usecase.qagPaginated.repository.TrendingClusterRepository;
import fr.gouv.agora.usecase.responseQag.GetResponseQagPreviewPaginatedListUseCase;
import fr.gouv.agora.usecase.responseQag.QagWithResponseAndOrder;
import fr.gouv.agora.usecase.responseQag.QagWithSupportCountAndOrder;
import fr.gouv.agora.usecase.responseQag.ResponseQagPaginatedList;
import fr.gouv.agora.usecase.responseQag.ResponseQagPreviewList;
import fr.gouv.agora.usecase.responseQag.ResponseQagPreviewListMapper;
import fr.gouv.agora.usecase.responseQag.ResponseQagPreviewOrderMapper;
import fr.gouv.agora.usecase.responseQag.ResponseQagPreviewOrderResult;
import fr.gouv.agora.usecase.responseQag.repository.ResponseQagRepository;
import fr.gouv.agora.usecase.supportQag.SupportQagUseCase;
import fr.gouv.agora.usecase.supportQag.repository.GetSupportQagRepository;
import fr.gouv.agora.usecase.thematique.repository.ThematiqueRepository;
import fr.gouv.agora.usecase.themeHebdo.GetThemeHebdoUseCase;
import fr.gouv.agora.usecase.themeHebdo.repository.CurrentThemeHebdoCacheRepository;
import fr.gouv.agora.usecase.themeHebdo.repository.ThemeHebdoRepository;
import org.springframework.cache.CacheManager;

import java.lang.reflect.Field;
import java.text.SimpleDateFormat;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Date;
import java.util.List;
import java.util.Map;

/**
 * Oracle functions of slice S3 (QaG lists and responses). They run the REAL Kotlin mappers / use cases of the
 * reference jar, with fake repositories.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S3Functions {
    private S3Functions() {
    }

    // ------------------------------------------------------------------------------------------
    // Builders from JSON

    static Date date(JsonNode n, String k) {
        JsonNode v = n.get(k);
        return v == null || v.isNull() ? null : new Date(v.longValue());
    }

    static Thematique thematique(JsonNode n) {
        return new Thematique(S2Functions.str(n, "id"), S2Functions.str(n, "label"), S2Functions.str(n, "picto"));
    }

    static QagInfoWithSupportCount qagWithSupport(JsonNode q) {
        return new QagInfoWithSupportCount(S2Functions.str(q, "id"), S2Functions.str(q, "thematiqueId"), S2Functions.str(q, "title"),
                S2Functions.str(q, "description"), date(q, "date"), QagStatus.valueOf(S2Functions.str(q, "status")),
                S2Functions.str(q, "username"), S2Functions.str(q, "userId"), q.get("supportCount").intValue(), date(q, "moderatedDate"));
    }

    static List<QagInfoWithSupportCount> qagsWithSupport(JsonNode list) {
        List<QagInfoWithSupportCount> out = new ArrayList<>();
        for (JsonNode q : list) {
            out.add(qagWithSupport(q));
        }
        return out;
    }

    static List<String> strings(JsonNode list) {
        List<String> out = new ArrayList<>();
        if (list != null) {
            for (JsonNode s : list) {
                out.add(s.textValue());
            }
        }
        return out;
    }

    static HeaderQag header(JsonNode n) {
        return n == null || n.isNull() ? null
                : new HeaderQag(S2Functions.str(n, "headerId"), S2Functions.str(n, "title"), S2Functions.str(n, "message"));
    }

    static Map<String, Object> previewMap(QagPreview p) {
        return Functions.map("id", p.getId(), "thematique", p.getThematique().getId(), "title", p.getTitle(), "supportCount", p.getSupportCount(),
                "isSupportedByUser", p.isSupportedByUser(), "isAuthor", p.isAuthor(), "canShare", p.getCanShare(), "date", p.getDate().getTime());
    }

    static Map<String, Object> resultMap(QagsAndMaxPageCountV2 r) {
        if (r == null) {
            return null;
        }
        List<Object> qags = new ArrayList<>();
        for (QagPreview p : r.getQags()) {
            qags.add(previewMap(p));
        }
        HeaderQag h = r.getHeaderQag();
        return Functions.map("maxPageCount", r.getMaxPageCount(), "qags", qags,
                "header", h == null ? null : Functions.map("headerId", h.getHeaderId(), "title", h.getTitle(), "message", h.getMessage()));
    }

    static QagPreview previewFrom(JsonNode p) {
        return new QagPreview(S2Functions.str(p, "id"), thematique(p.get("thematique")), S2Functions.str(p, "title"),
                S2Functions.str(p, "description"), S2Functions.str(p, "username"), date(p, "date"), p.get("supportCount").intValue(),
                p.get("isSupportedByUser").booleanValue(), p.get("isAuthor").booleanValue(), p.get("canShare").booleanValue());
    }

    // ------------------------------------------------------------------------------------------
    // The QaG list use case with fake repositories

    /** What the fake repositories were asked. */
    static final class Calls {
        final List<Object> list = new ArrayList<>();
    }

    static QagPaginatedV2UseCase useCase(JsonNode a, Calls calls) throws Exception {
        final List<QagInfoWithSupportCount> trending = qagsWithSupport(a.get("trending"));
        final List<QagInfoWithSupportCount> page = qagsWithSupport(a.get("page"));
        final int count = a.get("count").intValue();
        final java.util.Set<String> knownThematiques = new java.util.HashSet<>(strings(a.get("thematiques")));
        final HeaderQag header = header(a.get("header"));
        final List<String> supportedIds = strings(a.get("supportedIds"));
        final int supportedCount = a.hasNonNull("supportedCount") ? a.get("supportedCount").intValue() : 0;
        final boolean estThemeLibre = a.get("estThemeLibre").booleanValue();
        final List<TrendingCluster> clusters = new ArrayList<>();
        if (a.hasNonNull("clusters")) {
            for (JsonNode c : a.get("clusters")) {
                clusters.add(new TrendingCluster(S2Functions.str(c, "id"), strings(c.get("mots"))));
            }
        }

        QagInfoRepository info = S2Functions.proxy(QagInfoRepository.class, (p, m, args) -> {
            switch (m.getName()) {
                case "getQagsCount":
                    calls.list.add("count:" + args[0]);
                    return count;
                case "getPopularQagsPaginatedV2":
                case "getLatestQagsPaginatedV2":
                    calls.list.add(m.getName() + ":" + args[0] + ":" + args[1]);
                    return page;
                case "getSupportedQagsPaginatedV2":
                    calls.list.add(m.getName() + ":" + args[0] + ":" + args[1] + ":" + args[2]);
                    return page;
                case "getTrendingQagsV3":
                    return trending;
                default:
                    throw new UnsupportedOperationException(m.getName());
            }
        });
        ThematiqueRepository themes = S2Functions.proxy(ThematiqueRepository.class, (p, m, args) -> {
            if (!m.getName().equals("getThematique")) {
                throw new UnsupportedOperationException(m.getName());
            }
            String id = (String) args[0];
            return knownThematiques.contains(id) ? new Thematique(id, "label-" + id, "picto") : null;
        });
        HeaderQagRepository headers = S2Functions.proxy(HeaderQagRepository.class, (p, m, args) -> {
            calls.list.add("header:" + args[0]);
            return header;
        });
        HeaderQagCacheRepository headerCache = S2Functions.proxy(HeaderQagCacheRepository.class, (p, m, args) -> {
            if (m.getName().equals("getHeader")) {
                return HeaderQagCacheResult.HeaderQagCacheNotInitialized.INSTANCE;
            }
            return null;
        });
        CacheManager cacheManager = S2Functions.proxy(CacheManager.class, (p, m, args) -> null);
        TrendingQagCacheRepository trendingCache = new TrendingQagCacheRepository(cacheManager) {
            @Override
            public CacheResult getTrendingQagList() {
                return CacheResult.CacheNotInitialized.INSTANCE;
            }

            @Override
            public void insertTrendingQagList(List<QagInfoWithSupportCount> qags) {
            }
        };
        GetSupportQagRepository supportRepo = S2Functions.proxy(GetSupportQagRepository.class, (p, m, args) -> {
            throw new UnsupportedOperationException(m.getName());
        });
        SupportQagUseCase supports = new SupportQagUseCase(supportRepo) {
            @Override
            public List<String> getUserSupportedQagIds(String userId) {
                return supportedIds;
            }

            @Override
            public int getSupportedQagCount(String userId, String thematiqueId) {
                calls.list.add("supportedCount:" + userId + ":" + thematiqueId);
                return supportedCount;
            }
        };
        ThemeHebdoRepository themeRepo = S2Functions.proxy(ThemeHebdoRepository.class, (p, m, args) -> {
            throw new UnsupportedOperationException(m.getName());
        });
        CurrentThemeHebdoCacheRepository currentCache = S2Functions.proxy(CurrentThemeHebdoCacheRepository.class, (p, m, args) -> {
            throw new UnsupportedOperationException(m.getName());
        });
        Clock clock = Clock.fixed(Instant.ofEpochMilli(a.get("nowMs").longValue()), ZoneId.systemDefault());
        GetThemeHebdoUseCase theme = new GetThemeHebdoUseCase(themeRepo, currentCache, clock) {
            @Override
            public ThemeHebdo getCurrentThemeHebdo() {
                calls.list.add("theme");
                return new ThemeHebdo("", "", "", "", null, null, null, Collections.emptyList(), "", null, null, estThemeLibre);
            }
        };
        TrendingClusterRepository clusterRepo = S2Functions.proxy(TrendingClusterRepository.class, (p, m, args) -> {
            calls.list.add("clusters");
            return clusters;
        });
        return new QagPaginatedV2UseCase(info, themes, headers, headerCache, trendingCache, new QagPreviewMapper(), supports, clock, theme, clusterRepo);
    }

    // ------------------------------------------------------------------------------------------
    // ResponseQagRepositoryImpl.getResponsesQag(from, pageSize, minDate) on a Strapi payload

    static ResponseQagStrapiRepository fakeStrapi(StrapiDTO<StrapiResponseQag> all) throws Exception {
        Field f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        sun.misc.Unsafe unsafe = (sun.misc.Unsafe) f.get(null);
        CmsStrapiHttpClient client = (CmsStrapiHttpClient) unsafe.allocateInstance(CmsStrapiHttpClient.class);
        return new ResponseQagStrapiRepository(client) {
            @Override
            public StrapiDTO<StrapiResponseQag> getResponsesQag() {
                return all;
            }

            @Override
            public int getResponsesCount() {
                return all.getMeta().getPagination().getTotal();
            }
        };
    }

    static void register(Map<String, Oracle.Fn> F) {
        // SimpleDateFormat("yyyy-MM-dd").apply { isLenient = false }.parse(s) of the QaG responses controllers
        F.put("simpleDateParse", a -> {
            String s = Functions.s(a, "s");
            try {
                SimpleDateFormat f = new SimpleDateFormat("yyyy-MM-dd");
                f.setLenient(false);
                return Functions.map("ok", true, "ms", f.parse(s).getTime());
            } catch (Exception e) {
                return Functions.map("ok", false, "error", e.getClass().getSimpleName());
            }
        });

        // Math.pow(x, y) for a list of bases: the raw bits of each result
        F.put("mathPow", a -> {
            double y = a.get("y").doubleValue();
            List<String> out = new ArrayList<>();
            for (JsonNode x : a.get("xs")) {
                out.add(Long.toHexString(Double.doubleToLongBits(Math.pow(x.doubleValue(), y))));
            }
            return out;
        });

        // QagPaginatedV2UseCase.getTrendingQag
        F.put("trendingQag", a -> {
            Calls calls = new Calls();
            QagsAndMaxPageCountV2 r = useCase(a, calls).getTrendingQag(S2Functions.str(a, "userId"));
            return Functions.map("result", resultMap(r), "calls", calls.list);
        });

        // QagPaginatedV2UseCase.get{Popular,Latest,Supported}QagPaginated
        F.put("qagPaginated", a -> {
            Calls calls = new Calls();
            QagPaginatedV2UseCase uc = useCase(a, calls);
            String userId = S2Functions.str(a, "userId");
            int page = a.get("pageNumber").intValue();
            String thematiqueId = S2Functions.str(a, "thematiqueId");
            QagsAndMaxPageCountV2 r;
            switch (S2Functions.str(a, "filter")) {
                case "top":
                    r = uc.getPopularQagPaginated(userId, page, thematiqueId);
                    break;
                case "latest":
                    r = uc.getLatestQagPaginated(userId, page, thematiqueId);
                    break;
                default:
                    r = uc.getSupportedQagPaginated(userId, page, thematiqueId);
            }
            return Functions.map("result", resultMap(r), "calls", calls.list);
        });

        // JSON mappers (JSON and XML like Spring MVC)
        F.put("qagPaginatedJson", a -> {
            List<QagPreview> qags = new ArrayList<>();
            for (JsonNode p : a.get("qags")) {
                qags.add(previewFrom(p));
            }
            QagsAndMaxPageCountV2 r = new QagsAndMaxPageCountV2(qags, header(a.get("header")), a.get("maxPageCount").intValue());
            Object body = new QagPaginatedJsonMapper(new ThematiqueJsonMapper(), new DateMapper()).toJson(r);
            return Functions.map("json", Functions.encode(Functions.springJson(), body), "xml", Functions.encode(Functions.springXml(), body));
        });
        F.put("qagPreviewListJson", a -> {
            List<QagPreview> qags = new ArrayList<>();
            for (JsonNode p : a.get("qags")) {
                qags.add(previewFrom(p));
            }
            Object body = new QagHomeJsonMapper(new ThematiqueJsonMapper(), new DateMapper()).toJson(qags);
            return Functions.map("json", Functions.encode(Functions.springJson(), body), "xml", Functions.encode(Functions.springXml(), body));
        });
        F.put("qagResponsesJson", a -> {
            List<IncomingResponsePreview> incoming = new ArrayList<>();
            for (JsonNode i : a.get("incoming")) {
                incoming.add(new IncomingResponsePreview(S2Functions.str(i, "id"), thematique(i.get("thematique")), S2Functions.str(i, "title"),
                        i.get("supportCount").intValue(), LocalDate.parse(S2Functions.str(i, "previous")), LocalDate.parse(S2Functions.str(i, "next")),
                        i.get("order").intValue()));
            }
            List<ResponseQagPreview> responses = new ArrayList<>();
            for (JsonNode r : a.get("responses")) {
                responses.add(new ResponseQagPreview(S2Functions.str(r, "qagId"), thematique(r.get("thematique")), S2Functions.str(r, "title"),
                        S2Functions.str(r, "author"), S2Functions.str(r, "authorPortraitUrl"), date(r, "responseDate"), r.get("order").intValue()));
            }
            Object body = new QagHomeJsonMapper(new ThematiqueJsonMapper(), new DateMapper()).toResponsesJson(new ResponseQagPreviewList(incoming, responses));
            return Functions.map("json", Functions.encode(Functions.springJson(), body), "xml", Functions.encode(Functions.springXml(), body));
        });
        F.put("responsePaginatedJson", a -> {
            List<ResponseQagPreviewWithoutOrder> list = new ArrayList<>();
            for (JsonNode r : a.get("responses")) {
                list.add(new ResponseQagPreviewWithoutOrder(S2Functions.str(r, "qagId"), thematique(r.get("thematique")), S2Functions.str(r, "title"),
                        S2Functions.str(r, "author"), S2Functions.str(r, "authorPortraitUrl"), S2Functions.str(r, "authorFunction"), date(r, "responseDate"),
                        S2Functions.str(r, "responseText"), S2Functions.str(r, "username")));
            }
            Object body = new ResponseQagPaginatedJsonMapper(new ThematiqueJsonMapper(), new DateMapper())
                    .toJson(new ResponseQagPaginatedList(list, a.get("maxPageNumber").intValue()));
            return Functions.map("json", Functions.encode(Functions.springJson(), body), "xml", Functions.encode(Functions.springXml(), body));
        });

        // ResponseQagPreviewOrderMapper.buildOrderResult
        F.put("buildOrderResult", a -> {
            List<QagInfoWithSupportCount> incoming = qagsWithSupport(a.get("incoming"));
            List<kotlin.Pair<QagInfoWithSupportCount, ResponseQag>> responses = new ArrayList<>();
            for (JsonNode r : a.get("responses")) {
                responses.add(new kotlin.Pair<>(qagWithSupport(r.get("qag")), S2Functions.responseFrom(r.get("response"))));
            }
            ResponseQagPreviewOrderResult res = new ResponseQagPreviewOrderMapper().buildOrderResult(strings(a.get("lowPriority")), incoming, responses);
            List<Object> in = new ArrayList<>();
            for (QagWithSupportCountAndOrder o : res.getIncomingResponses()) {
                in.add(Functions.map("id", o.getQagWithSupportCount().getId(), "order", o.getOrder()));
            }
            List<Object> out = new ArrayList<>();
            for (QagWithResponseAndOrder o : res.getResponses()) {
                out.add(Functions.map("id", o.getQagInfo().getId(), "order", o.getOrder()));
            }
            return Functions.map("incoming", in, "responses", out);
        });

        // ResponseQagPreviewListMapper.toIncomingResponsePreview
        F.put("incomingResponsePreview", a -> {
            IncomingResponsePreview p = new ResponseQagPreviewListMapper().toIncomingResponsePreview(
                    new QagWithSupportCountAndOrder(qagWithSupport(a.get("qag")), a.get("order").intValue()), thematique(a.get("thematique")));
            return Functions.map("previous", p.getDateLundiPrecedent().toString(), "next", p.getDateLundiSuivant().toString(), "order", p.getOrder());
        });

        // ResponseQagPreviewListMapper.toResponseQagPreviewWithoutOrder: the sanitized text
        F.put("responseTextPreview", a -> {
            QagInfo info = new QagInfo("id", "th", "title", "description", new Date(0), QagStatus.SELECTED_FOR_RESPONSE, "username", "userId");
            ResponseQagPreviewWithoutOrder p = new ResponseQagPreviewListMapper().toResponseQagPreviewWithoutOrder(
                    info, S2Functions.responseFrom(a.get("response")), thematique(a.get("thematique")));
            return Functions.text16(p.getResponseText());
        });

        // ResponseQagRepositoryImpl.getResponsesQag(from, pageSize, minDate) and getResponsesQagCount(minDate) on a Strapi payload
        F.put("responsesPage", a -> {
            ObjectMapper m = Functions.appMapper();
            JavaType t = m.getTypeFactory().constructParametricType(StrapiDTO.class, StrapiResponseQag.class);
            StrapiDTO<StrapiResponseQag> dto = m.readValue(Functions.s(a, "json"), t);
            ResponseQagRepositoryImpl repo = new ResponseQagRepositoryImpl(fakeStrapi(dto), new ResponseQagMapper());
            Date minDate = date(a, "minDate");
            List<String> ids = new ArrayList<>();
            try {
                for (ResponseQag r : repo.getResponsesQag(a.get("from").intValue(), a.get("pageSize").intValue(), minDate)) {
                    ids.add(r instanceof ResponseQagVideo ? ((ResponseQagVideo) r).getQagId() : ((fr.gouv.agora.domain.ResponseQagText) r).getQagId());
                }
            } catch (Exception e) {
                return Functions.map("error", e.getClass().getSimpleName());
            }
            return Functions.map("ids", ids, "count", repo.getResponsesQagCount(minDate));
        });

        // GetResponseQagPreviewPaginatedListUseCase: which offset is read, or null
        F.put("responsePaginatedOffsets", a -> {
            final List<Object> calls = new ArrayList<>();
            final int count = a.get("count").intValue();
            ResponseQagRepository responses = S2Functions.proxy(ResponseQagRepository.class, (p, m, args) -> {
                switch (m.getName()) {
                    case "getResponsesQagCount":
                        calls.add("count");
                        return count;
                    case "getResponsesQag":
                        calls.add("page:" + args[0] + ":" + args[1]);
                        return Collections.emptyList();
                    default:
                        throw new UnsupportedOperationException(m.getName());
                }
            });
            QagInfoRepository qags = S2Functions.proxy(QagInfoRepository.class, (p, m, args) -> Collections.emptyList());
            ThematiqueRepository themes = S2Functions.proxy(ThematiqueRepository.class, (p, m, args) -> null);
            ResponseQagPaginatedList r = new GetResponseQagPreviewPaginatedListUseCase(responses, qags, themes, new ResponseQagPreviewListMapper())
                    .getResponseQagPreviewPaginatedList(a.get("pageNumber").intValue(), null);
            return Functions.map("isNull", r == null, "maxPageNumber", r == null ? null : r.getMaxPageNumber(), "calls", calls);
        });
    }
}
