import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import fr.gouv.agora.domain.ConsultationDetailsV2WithInfo;
import fr.gouv.agora.domain.ConsultationPreview;
import fr.gouv.agora.domain.ConsultationPreviewFinished;
import fr.gouv.agora.domain.ConsultationUpdateHistory;
import fr.gouv.agora.domain.ConsultationUpdateInfoV2;
import fr.gouv.agora.domain.FeedbackConsultationUpdateStats;
import fr.gouv.agora.domain.Question;
import fr.gouv.agora.domain.Questions;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.common.StrapiDTO;
import fr.gouv.agora.infrastructure.consultation.ConsultationDetailsV2JsonMapper;
import fr.gouv.agora.infrastructure.consultation.ConsultationPreviewJsonMapper;
import fr.gouv.agora.infrastructure.consultation.dto.strapi.ConsultationStrapiDTO;
import fr.gouv.agora.infrastructure.consultation.dto.strapi.StrapiConsultationContenuAutre;
import fr.gouv.agora.infrastructure.consultation.repository.ConsultationInfoMapper;
import fr.gouv.agora.infrastructure.consultationUpdates.repository.ConsultationUpdateHistoryMapper;
import fr.gouv.agora.infrastructure.consultationUpdates.repository.ConsultationUpdateInfoV2Mapper;
import fr.gouv.agora.infrastructure.question.QuestionJsonMapper;
import fr.gouv.agora.infrastructure.question.repository.QuestionMapper;
import fr.gouv.agora.infrastructure.question.repository.QuestionsMapper;
import fr.gouv.agora.infrastructure.thematique.ThematiqueJsonMapper;
import fr.gouv.agora.infrastructure.thematique.repository.ThematiqueMapper;
import fr.gouv.agora.usecase.consultation.repository.ConsultationInfo;

import java.time.Clock;
import java.time.Instant;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Oracle functions of slice S4 (consultation details). They run the REAL Kotlin mappers of the reference jar on
 * a Strapi consultation payload.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S4Functions {
    private S4Functions() {
    }

    static void register(Map<String, Oracle.Fn> F) {
        // Jackson's LocalDateTime deserializer as the application mapper is configured; a null (empty string) is
        // an error for the non-null Kotlin properties
        F.put("s4LocalDateTime", a -> {
            ObjectMapper m = Functions.appMapper();
            LocalDateTime v = m.readValue(Functions.s(a, "json"), LocalDateTime.class);
            if (v == null) {
                throw new IllegalStateException("null");
            }
            return new DateMapper().toFormattedDate(v);
        });

        // Everything the consultation details, the questions and the preview derive from one Strapi consultation:
        // the JSON and XML of ConsultationDetailsV2Json for every update (3 variants of user data), of QuestionsJson
        // and of ConsultationPreviewJson. A payload the DTO cannot read is {"decodeError": true}; an exception of
        // a mapper is an oracle error.
        F.put("s4Consultation", a -> {
            ObjectMapper m = Functions.appMapper();
            ConsultationStrapiDTO dto;
            try {
                dto = m.readValue(Functions.s(a, "json"), ConsultationStrapiDTO.class);
                if (dto == null) {
                    throw new NullPointerException();
                }
            } catch (Exception e) {
                return Functions.map("decodeError", true);
            }
            Clock clock = Clock.fixed(Instant.ofEpochMilli(a.get("nowMs").longValue()), ZoneId.systemDefault());
            LocalDateTime now = LocalDateTime.now(clock);
            DateMapper dateMapper = new DateMapper();
            ThematiqueJsonMapper thematiqueJsonMapper = new ThematiqueJsonMapper();
            ConsultationInfoMapper infoMapper = new ConsultationInfoMapper(new ThematiqueMapper());
            ConsultationInfo info = infoMapper.toConsultationInfo(dto);
            List<ConsultationUpdateHistory> history = new ConsultationUpdateHistoryMapper(clock).toDomain(dto);
            ConsultationUpdateInfoV2Mapper updateMapper = new ConsultationUpdateInfoV2Mapper();
            ConsultationDetailsV2JsonMapper detailsMapper = new ConsultationDetailsV2JsonMapper(dateMapper, thematiqueJsonMapper);

            List<String> kinds = new ArrayList<>();
            List<ConsultationUpdateInfoV2> updates = new ArrayList<>();
            kinds.add("avant");
            updates.add(updateMapper.toDomainUnanswered(dto));
            kinds.add("apres");
            updates.add(updateMapper.toDomainAnsweredOrEnded(dto, now));
            kinds.add("analyse");
            updates.add(updateMapper.toDomainAnalyseDesReponses(dto));
            kinds.add("commanditaire");
            updates.add(updateMapper.toDomainReponseDuCommanditaire(dto));
            int i = 0;
            for (StrapiConsultationContenuAutre autre : dto.getConsultationContenuAutres()) {
                kinds.add("autre-" + i++);
                updates.add(updateMapper.toDomainContenuAutre(dto, autre));
            }

            ObjectNode out = m.createObjectNode();
            ArrayNode views = out.putArray("views");
            for (int k = 0; k < updates.size(); k++) {
                ConsultationUpdateInfoV2 update = updates.get(k);
                if (update == null) {
                    continue;
                }
                for (int variant = 0; variant < 3; variant++) {
                    FeedbackConsultationUpdateStats stats = variant == 0 ? null : new FeedbackConsultationUpdateStats(60, 40, 5);
                    Boolean feedback = variant == 0 ? null : (variant == 1 ? Boolean.TRUE : Boolean.FALSE);
                    ConsultationDetailsV2WithInfo details = new ConsultationDetailsV2WithInfo(
                            info, update, stats, history, 17, feedback, variant == 2);
                    Object body = detailsMapper.toJson(details);
                    ObjectNode v = views.addObject();
                    v.put("kind", kinds.get(k));
                    v.put("variant", variant);
                    v.put("json", Functions.encode(Functions.springJson(), body));
                    v.put("xml", Functions.encode(Functions.springXml(), body));
                }
            }

            List<Question> questions = new QuestionsMapper(new QuestionMapper()).toDomain(dto);
            Object questionsBody = new QuestionJsonMapper().toJson(new Questions(dto.getNombreDeQuestion(), questions));
            out.put("questionsJson", Functions.encode(Functions.springJson(), questionsBody));
            out.put("questionsXml", Functions.encode(Functions.springXml(), questionsBody));

            StrapiDTO<ConsultationStrapiDTO> strapi = new StrapiDTO<>(List.of(dto), StrapiDTO.Companion.<ConsultationStrapiDTO>ofEmpty().getMeta());
            List<ConsultationPreview> ongoing = infoMapper.toConsultationPreview(strapi);
            List<ConsultationPreviewFinished> finished = infoMapper.toDomainFinished(strapi, now);
            Object previewBody = new ConsultationPreviewJsonMapper(thematiqueJsonMapper, dateMapper, clock).toJson(ongoing, finished, finished);
            out.put("previewJson", Functions.encode(Functions.springJson(), previewBody));
            out.put("previewXml", Functions.encode(Functions.springXml(), previewBody));
            return out;
        });

        // FeedbackConsultationUpdateMapper.toStats on a positive / negative count
        F.put("s4FeedbackStats", a -> {
            int positive = a.get("positive").intValue();
            int negative = a.get("negative").intValue();
            List<fr.gouv.agora.infrastructure.feedbackConsultationUpdate.dto.FeedbackConsultationUpdateStatsDTO> rows = new ArrayList<>();
            rows.add(S2Functions.proxy(fr.gouv.agora.infrastructure.feedbackConsultationUpdate.dto.FeedbackConsultationUpdateStatsDTO.class,
                    (p, method, args) -> method.getName().equals("getHasPositiveValue") ? (Object) 1 : (Object) positive));
            rows.add(S2Functions.proxy(fr.gouv.agora.infrastructure.feedbackConsultationUpdate.dto.FeedbackConsultationUpdateStatsDTO.class,
                    (p, method, args) -> method.getName().equals("getHasPositiveValue") ? (Object) 0 : (Object) negative));
            FeedbackConsultationUpdateStats s = new fr.gouv.agora.infrastructure.feedbackConsultationUpdate.repository.FeedbackConsultationUpdateMapper().toStats(rows);
            return Functions.map("positiveRatio", s.getPositiveRatio(), "negativeRatio", s.getNegativeRatio(), "responseCount", s.getResponseCount());
        });
    }
}
