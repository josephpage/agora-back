import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import fr.gouv.agora.domain.FeedbackQag;
import fr.gouv.agora.domain.FeedbackResults;
import fr.gouv.agora.domain.QagDetails;
import fr.gouv.agora.domain.QagStatus;
import fr.gouv.agora.domain.QagWithUserData;
import fr.gouv.agora.domain.ResponseQag;
import fr.gouv.agora.domain.ResponseQagAdditionalInfo;
import fr.gouv.agora.domain.ResponseQagText;
import fr.gouv.agora.domain.ResponseQagVideo;
import fr.gouv.agora.domain.Thematique;
import fr.gouv.agora.domain.AgoraFeature;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.common.StrapiDTO;
import fr.gouv.agora.infrastructure.qag.PublicQagJsonMapper;
import fr.gouv.agora.infrastructure.qag.QagJsonMapper;
import fr.gouv.agora.infrastructure.responseQag.dto.StrapiResponseQag;
import fr.gouv.agora.infrastructure.responseQag.repository.ResponseQagMapper;
import fr.gouv.agora.infrastructure.thematique.ThematiqueJsonMapper;
import fr.gouv.agora.usecase.featureFlags.repository.FeatureFlagsRepository;
import fr.gouv.agora.usecase.feedbackQag.FeedbackQagUseCase;
import fr.gouv.agora.usecase.feedbackQag.repository.FeedbackQagRepository;
import fr.gouv.agora.usecase.feedbackQag.repository.FeedbackResultsCacheRepository;
import fr.gouv.agora.usecase.feedbackQag.repository.FeedbackResultsCacheResult;
import fr.gouv.agora.usecase.feedbackQag.repository.UserFeedbackQagCacheRepository;
import fr.gouv.agora.usecase.qag.GetAskQagStatusUseCase;
import fr.gouv.agora.usecase.qag.repository.QagInfo;
import fr.gouv.agora.usecase.qag.repository.QagInfoRepository;

import java.lang.reflect.Proxy;
import java.time.Clock;
import java.time.Instant;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Map;

/**
 * Oracle functions of slice S2 (QaG details, insertion, support, feedback). They run the REAL Kotlin
 * mappers / use cases of the reference jar.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S2Functions {
    private S2Functions() {
    }

    static long l(JsonNode a, String k) {
        JsonNode n = a.get(k);
        if (n == null || !n.isIntegralNumber()) {
            throw new IllegalArgumentException("argument '" + k + "' must be an integer");
        }
        return n.longValue();
    }

    static String str(JsonNode n, String k) {
        JsonNode v = n.get(k);
        return v == null || v.isNull() ? null : v.textValue();
    }

    static Boolean bool(JsonNode n, String k) {
        JsonNode v = n.get(k);
        return v == null || v.isNull() ? null : v.booleanValue();
    }

    static ResponseQag responseFrom(JsonNode r) {
        if (r == null || r.isNull()) {
            return null;
        }
        Date date = new Date(r.get("responseDate").longValue());
        if ("video".equals(r.get("kind").textValue())) {
            JsonNode ai = r.get("additionalInfo");
            ResponseQagAdditionalInfo info = ai == null || ai.isNull() ? null
                    : new ResponseQagAdditionalInfo(ai.get("title").textValue(), ai.get("description").textValue());
            return new ResponseQagVideo(str(r, "author"), str(r, "authorPortraitUrl"), date, str(r, "feedbackQuestion"),
                    str(r, "qagId"), str(r, "authorFunction"), str(r, "authorDescription"), str(r, "videoUrl"),
                    str(r, "videoTitle"), r.get("videoWidth").intValue(), r.get("videoHeight").intValue(),
                    str(r, "transcription"), info);
        }
        return new ResponseQagText(str(r, "author"), str(r, "authorPortraitUrl"), date, str(r, "feedbackQuestion"),
                str(r, "qagId"), str(r, "authorFunction"), str(r, "responseLabel"), str(r, "responseText"));
    }

    static QagDetails detailsFrom(JsonNode d) {
        JsonNode th = d.get("thematique");
        JsonNode fr = d.get("feedbackResults");
        FeedbackResults results = fr == null || fr.isNull() ? null
                : new FeedbackResults(fr.get("positiveRatio").intValue(), fr.get("negativeRatio").intValue(), fr.get("count").intValue());
        return new QagDetails(str(d, "id"), new Thematique(str(th, "id"), str(th, "label"), str(th, "picto")),
                str(d, "title"), str(d, "description"), new Date(d.get("date").longValue()),
                QagStatus.valueOf(str(d, "status")), str(d, "username"), str(d, "userId"), d.get("supportCount").intValue(),
                responseFrom(d.get("response")), results);
    }

    static <T> T proxy(Class<T> type, java.lang.reflect.InvocationHandler h) {
        return (T) Proxy.newProxyInstance(type.getClassLoader(), new Class<?>[]{type}, h);
    }

    static void register(Map<String, Oracle.Fn> F) {
        // QagJsonMapper.toJson(QagWithUserData) / PublicQagJsonMapper.toJson(QagDetails): JSON and XML like Spring MVC
        F.put("qagJson", a -> {
            QagDetails d = detailsFrom(a.get("details"));
            DateMapper dm = new DateMapper();
            QagWithUserData u = new QagWithUserData(d, bool(a, "canShare"), bool(a, "canSupport"), bool(a, "canDelete"),
                    bool(a, "isAuthor"), bool(a, "isSupportedByUser"), bool(a, "isHelpful"));
            Object body = new QagJsonMapper(new ThematiqueJsonMapper(), dm).toJson(u);
            return Functions.map("json", Functions.encode(Functions.springJson(), body),
                    "xml", Functions.encode(Functions.springXml(), body));
        });
        F.put("publicQagJson", a -> {
            QagDetails d = detailsFrom(a.get("details"));
            Object body = new PublicQagJsonMapper(new DateMapper(), new ThematiqueJsonMapper()).toJson(d);
            return Functions.map("json", Functions.encode(Functions.springJson(), body),
                    "xml", Functions.encode(Functions.springXml(), body));
        });

        // ResponseQagMapper on a decoded Strapi payload (CmsStrapiHttpClient: a payload that cannot be decoded is
        // an empty list; the mapper's exceptions are propagated). Dates are reduced to the day: the Kotlin
        // LocalDate.toDate() adds the current time of day.
        F.put("responseQagMap", a -> {
            ObjectMapper m = Functions.appMapper();
            JavaType t = m.getTypeFactory().constructParametricType(StrapiDTO.class, StrapiResponseQag.class);
            StrapiDTO<StrapiResponseQag> dto;
            try {
                dto = m.readValue(Functions.s(a, "json"), t);
                if (dto == null) {
                    throw new NullPointerException();
                }
            } catch (Exception e) {
                dto = StrapiDTO.Companion.<StrapiResponseQag>ofEmpty();
            }
            List<ResponseQag> list = new ResponseQagMapper().toDomain(dto);
            DateMapper dm = new DateMapper();
            ArrayNode out = m.createArrayNode();
            for (ResponseQag r : list) {
                ObjectNode n = m.createObjectNode();
                String day = dm.toFormattedDate(r.getResponseDate());
                n.put("responseDay", day.substring(0, day.indexOf(' ')));
                n.put("author", r.getAuthor());
                n.put("authorPortraitUrl", r.getAuthorPortraitUrl());
                n.put("feedbackQuestion", r.getFeedbackQuestion());
                n.put("qagId", r.getQagId());
                n.put("authorFunction", r.getAuthorFunction());
                if (r instanceof ResponseQagVideo v) {
                    n.put("kind", "video");
                    n.put("authorDescription", v.getAuthorDescription());
                    n.put("videoUrl", v.getVideoUrl());
                    n.put("videoTitle", v.getVideoTitle());
                    n.put("videoWidth", v.getVideoWidth());
                    n.put("videoHeight", v.getVideoHeight());
                    n.put("transcription", v.getTranscription());
                    if (v.getAdditionalInfo() == null) {
                        n.putNull("additionalInfo");
                    } else {
                        ObjectNode ai = n.putObject("additionalInfo");
                        ai.put("title", v.getAdditionalInfo().getAdditionalInfoTitle());
                        ai.put("description", v.getAdditionalInfo().getAdditionalInfoDescription());
                    }
                } else if (r instanceof ResponseQagText x) {
                    n.put("kind", "text");
                    n.put("responseLabel", x.getResponseLabel());
                    n.put("responseText", x.getResponseText());
                }
                out.add(n);
            }
            return out;
        });

        // GetAskQagStatusUseCase with a fixed clock (zone: the JVM default zone), the user's last QaG posted at postMs
        // (or none)
        F.put("askQagStatus", a -> {
            Clock clock = Clock.fixed(Instant.ofEpochMilli(l(a, "nowMs")).plusNanos(l(a, "nowExtraNanos")), ZoneId.systemDefault());
            JsonNode post = a.get("postMs");
            QagInfo last = post == null || post.isNull() ? null
                    : new QagInfo("id", "t", "title", "description", new Date(post.longValue()), QagStatus.OPEN, "u", "user");
            QagInfoRepository repo = proxy(QagInfoRepository.class, (p, method, args) -> {
                if (method.getName().equals("getUserLastQagInfo")) {
                    return last;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            return new GetAskQagStatusUseCase(repo, clock).getAskQagStatus("user").name();
        });

        // FeedbackQagUseCase.getFeedbackResults on `total` feedbacks, `helpful` of them helpful
        F.put("feedbackResults", a -> {
            int total = (int) l(a, "total");
            int helpful = (int) l(a, "helpful");
            List<FeedbackQag> list = new ArrayList<>();
            for (int i = 0; i < total; i++) {
                list.add(new FeedbackQag("q", "u" + i, i < helpful));
            }
            FeatureFlagsRepository flags = proxy(FeatureFlagsRepository.class, (p, method, args) -> {
                if (method.getName().equals("isFeatureEnabled") && args[0] == AgoraFeature.FeedbackResponseQag) {
                    return true;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            FeedbackQagRepository repo = proxy(FeedbackQagRepository.class, (p, method, args) -> {
                if (method.getName().equals("getFeedbackQagList")) {
                    return list;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            FeedbackResultsCacheRepository cache = proxy(FeedbackResultsCacheRepository.class, (p, method, args) -> {
                if (method.getName().equals("getFeedbackResults")) {
                    return FeedbackResultsCacheResult.FeedbackResultsCacheNotInitialized.INSTANCE;
                }
                return null;
            });
            UserFeedbackQagCacheRepository users = proxy(UserFeedbackQagCacheRepository.class, (p, method, args) -> {
                throw new UnsupportedOperationException(method.getName());
            });
            FeedbackResults r = new FeedbackQagUseCase(flags, repo, cache, users).getFeedbackResults("q");
            return Functions.map("positiveRatio", r.getPositiveRatio(), "negativeRatio", r.getNegativeRatio(), "count", r.getCount());
        });
    }
}
