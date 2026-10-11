import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import fr.gouv.agora.domain.AgeRange;
import fr.gouv.agora.domain.ChoixPossibleConditional;
import fr.gouv.agora.domain.ChoixPossibleDefault;
import fr.gouv.agora.domain.CityType;
import fr.gouv.agora.domain.ConsultationResultAggregated;
import fr.gouv.agora.domain.ConsultationResults;
import fr.gouv.agora.domain.Department;
import fr.gouv.agora.domain.DemographicInfoCount;
import fr.gouv.agora.domain.DemographicInfoCountByChoices;
import fr.gouv.agora.domain.Frequency;
import fr.gouv.agora.domain.Gender;
import fr.gouv.agora.domain.JobCategory;
import fr.gouv.agora.domain.Question;
import fr.gouv.agora.domain.QuestionChapter;
import fr.gouv.agora.domain.QuestionConditional;
import fr.gouv.agora.domain.QuestionMultipleChoices;
import fr.gouv.agora.domain.QuestionOpen;
import fr.gouv.agora.domain.QuestionUniqueChoice;
import fr.gouv.agora.domain.Questions;
import fr.gouv.agora.domain.ReponseConsultationInserting;
import fr.gouv.agora.domain.ResponseConsultationCount;
import fr.gouv.agora.domain.Thematique;
import fr.gouv.agora.domain.UserAnsweredConsultation;
import fr.gouv.agora.infrastructure.consultationResponse.repository.ReponseConsultationMapper;
import fr.gouv.agora.infrastructure.consultationResults.ConsultationResultJsonMapper;
import fr.gouv.agora.infrastructure.consultationResults.ConsultationResultWithDemographicInfoTsvMapper;
import fr.gouv.agora.infrastructure.consultationResults.dto.DemographicInfoCountByChoiceDTO;
import fr.gouv.agora.infrastructure.consultationResults.dto.DemographicInfoCountDTO;
import fr.gouv.agora.usecase.consultation.repository.ConsultationDetailsV2CacheRepository;
import fr.gouv.agora.usecase.consultation.repository.ConsultationInfo;
import fr.gouv.agora.usecase.consultation.repository.ConsultationInfoRepository;
import fr.gouv.agora.usecase.consultationAggregate.repository.ConsultationResultAggregatedRepository;
import fr.gouv.agora.usecase.consultationPaginated.repository.ConsultationAnsweredPaginatedListCacheRepository;
import fr.gouv.agora.usecase.consultationResponse.ControlResponseConsultationUseCase;
import fr.gouv.agora.usecase.consultationResponse.GetConsultationResultsWithDemographicRatiosUseCase;
import fr.gouv.agora.usecase.consultationResponse.InsertConsultationResponseParametersMapper;
import fr.gouv.agora.usecase.consultationResponse.InsertReponseConsultationUseCase;
import fr.gouv.agora.usecase.consultationResponse.QuestionMultipleChoicesValidator;
import fr.gouv.agora.usecase.consultationResponse.QuestionOpenValidator;
import fr.gouv.agora.usecase.consultationResponse.QuestionUniqueChoiceAndConditionalValidator;
import fr.gouv.agora.usecase.consultationResponse.repository.GetConsultationResponseRepository;
import fr.gouv.agora.usecase.consultationResponse.repository.InsertReponseConsultationRepository;
import fr.gouv.agora.usecase.consultationResponse.repository.UserAnsweredConsultationRepository;
import fr.gouv.agora.usecase.consultationResponse.repository.UserAnsweredConsultationResult;
import fr.gouv.agora.usecase.consultationResults.ConsultationResultsUseCase;
import fr.gouv.agora.usecase.consultationResults.QuestionNoResponseMapper;
import fr.gouv.agora.usecase.consultationResults.repository.ConsultationResultsCacheRepository;
import fr.gouv.agora.usecase.consultationResults.repository.ConsultationResultsCacheResult;
import fr.gouv.agora.usecase.qag.ContentSanitizer;
import fr.gouv.agora.usecase.question.repository.QuestionRepository;

import java.lang.reflect.Method;
import java.time.Clock;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

/**
 * Oracle functions of slice S6 (consultation responses and results). They run the REAL Kotlin validators, use cases and
 * mappers of the reference jar, with the repositories replaced by proxies that answer from the arguments.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S6Functions {
    private S6Functions() {
    }

    private static String str(JsonNode n, String k) {
        JsonNode v = n.get(k);
        return v == null || v.isNull() ? null : v.textValue();
    }

    private static int intOf(JsonNode n, String k) {
        return n.get(k).intValue();
    }

    private static boolean boolOf(JsonNode n, String k) {
        return n.get(k).booleanValue();
    }

    // -------------------------------------------------------------------------------------------------------------
    // Domain objects from JSON

    private static Question question(JsonNode q) {
        String kind = str(q, "kind");
        String id = str(q, "id"), title = str(q, "title"), popup = str(q, "popup"), next = str(q, "nextQuestionId"), cid = str(q, "consultationId");
        int order = intOf(q, "order");
        switch (kind) {
            case "unique": {
                List<ChoixPossibleDefault> cs = new ArrayList<>();
                for (JsonNode c : q.get("choices")) {
                    cs.add(new ChoixPossibleDefault(str(c, "id"), str(c, "label"), intOf(c, "ordre"), str(c, "questionId"), boolOf(c, "hasOpenTextField")));
                }
                return new QuestionUniqueChoice(id, title, popup, order, next, cid, cs);
            }
            case "multiple": {
                List<ChoixPossibleDefault> cs = new ArrayList<>();
                for (JsonNode c : q.get("choices")) {
                    cs.add(new ChoixPossibleDefault(str(c, "id"), str(c, "label"), intOf(c, "ordre"), str(c, "questionId"), boolOf(c, "hasOpenTextField")));
                }
                return new QuestionMultipleChoices(id, title, popup, order, next, cid, cs, intOf(q, "maxChoices"));
            }
            case "conditional": {
                List<ChoixPossibleConditional> cs = new ArrayList<>();
                for (JsonNode c : q.get("choices")) {
                    cs.add(new ChoixPossibleConditional(str(c, "id"), str(c, "label"), intOf(c, "ordre"), str(c, "questionId"), boolOf(c, "hasOpenTextField"), str(c, "nextQuestionId")));
                }
                return new QuestionConditional(id, title, popup, order, next, cid, cs);
            }
            case "open":
                return new QuestionOpen(id, title, popup, order, next, cid);
            default:
                return new QuestionChapter(id, title, popup, order, next, cid, str(q, "urlImage"), str(q, "description"), str(q, "transcriptionImage"));
        }
    }

    private static List<Question> questions(JsonNode a) {
        List<Question> out = new ArrayList<>();
        for (JsonNode q : a.get("questions")) {
            out.add(question(q));
        }
        return out;
    }

    private static List<ReponseConsultationInserting> responses(JsonNode a) {
        List<ReponseConsultationInserting> out = new ArrayList<>();
        for (JsonNode r : a.get("responses")) {
            List<String> ids = null;
            if (r.hasNonNull("choiceIds")) {
                ids = new ArrayList<>();
                for (JsonNode c : r.get("choiceIds")) {
                    ids.add(c.isNull() ? null : c.textValue());
                }
            }
            out.add(new ReponseConsultationInserting(str(r, "questionId"), ids, str(r, "responseText")));
        }
        return out;
    }

    private static QuestionRepository questionRepo(List<Question> list) {
        return S2Functions.proxy(QuestionRepository.class, (p, m, args) -> new Questions(list.size(), list));
    }

    private static ConsultationInfo info(JsonNode i) {
        if (i == null || i.isNull()) {
            return null;
        }
        return new ConsultationInfo(str(i, "id"), str(i, "title"), str(i, "slug"), str(i, "coverUrl"), "details", LocalDateTime.parse("2020-01-01T00:00:00"),
                LocalDateTime.parse(str(i, "endDate")), "5 questions", "10 minutes", 1000, new Thematique("t", "Thématique", "T"), "France", "Titre web", "Sous-titre");
    }

    private static ConsultationInfoRepository infoRepo(ConsultationInfo info) {
        return S2Functions.proxy(ConsultationInfoRepository.class, (p, m, args) -> {
            if (m.getName().equals("getConsultation")) {
                return info;
            }
            throw new UnsupportedOperationException(m.getName());
        });
    }

    private static Object text16Of(String s) {
        return Functions.text16(s);
    }

    // -------------------------------------------------------------------------------------------------------------
    // Demographic maps

    private static <E extends Enum<E>> Map<E, Integer> enumMap(JsonNode list, Class<E> type) {
        Map<E, Integer> m = new LinkedHashMap<>();
        for (JsonNode e : list) {
            JsonNode k = e.get(0);
            m.put(k.isNull() ? null : Enum.valueOf(type, k.textValue()), e.get(1).intValue());
        }
        return m;
    }

    private static DemographicInfoCount demographic(JsonNode d) {
        return new DemographicInfoCount(enumMap(d.get(0), Gender.class), enumMap(d.get(1), AgeRange.class), enumMap(d.get(2), Department.class),
                enumMap(d.get(3), CityType.class), enumMap(d.get(4), JobCategory.class), enumMap(d.get(5), Frequency.class),
                enumMap(d.get(6), Frequency.class), enumMap(d.get(7), Frequency.class));
    }

    private static ArrayNode keyed(Map<? extends Enum, Integer> m) {
        ArrayNode out = Functions.springJson().createArrayNode();
        for (Map.Entry<? extends Enum, Integer> e : m.entrySet()) {
            ArrayNode pair = out.addArray();
            if (e.getKey() == null) {
                pair.addNull();
            } else {
                pair.add(e.getKey().name());
            }
            pair.add(e.getValue());
        }
        return out;
    }

    private static ArrayNode demographicOut(DemographicInfoCount d) {
        ArrayNode out = Functions.springJson().createArrayNode();
        out.add(keyed(d.getGenderCount()));
        out.add(keyed(d.getAgeRangeCount()));
        out.add(keyed(d.getDepartmentCount()));
        out.add(keyed(d.getCityTypeCount()));
        out.add(keyed(d.getJobCategoryCount()));
        out.add(keyed(d.getVoteFrequencyCount()));
        out.add(keyed(d.getPublicMeetingFrequencyCount()));
        out.add(keyed(d.getConsultationFrequencyCount()));
        return out;
    }

    private static DemographicInfoCountDTO dto(JsonNode e) {
        String key = e.get(0).isNull() ? null : e.get(0).textValue();
        int count = e.get(1).intValue();
        return S2Functions.proxy(DemographicInfoCountDTO.class, (p, m, args) -> m.getName().equals("getKey") ? key : count);
    }

    private static DemographicInfoCountByChoiceDTO dtoByChoice(JsonNode e) {
        String choice = e.get(0).textValue();
        String key = e.get(1).isNull() ? null : e.get(1).textValue();
        int count = e.get(2).intValue();
        return S2Functions.proxy(DemographicInfoCountByChoiceDTO.class, (p, m, args) -> {
            switch (m.getName()) {
                case "getChoiceId":
                    return choice;
                case "getKey":
                    return key;
                default:
                    return count;
            }
        });
    }

    private static Method mapperMethod(Class<?> returnType) {
        for (Method m : ReponseConsultationMapper.class.getDeclaredMethods()) {
            if (m.getName().equals("toDomain") && m.getParameterCount() == 8 && m.getReturnType() == returnType) {
                return m;
            }
        }
        throw new IllegalStateException("toDomain -> " + returnType);
    }

    static void register(Map<String, Oracle.Fn> F) {
        // ControlResponseConsultationUseCase with the real validators: is this list of answers valid?
        F.put("s6Control", a -> {
            ControlResponseConsultationUseCase uc = new ControlResponseConsultationUseCase(questionRepo(questions(a)), new QuestionMultipleChoicesValidator(),
                    new QuestionUniqueChoiceAndConditionalValidator(), new QuestionOpenValidator(intOf(a, "openMax")));
            return uc.isResponseConsultationValid(str(a, "consultationId"), responses(a));
        });

        // InsertReponseConsultationUseCase: the answers handed to the repository (completed and sanitized) and which collaborators were called
        F.put("s6Insert", a -> {
            ConsultationInfo info = info(a.get("info"));
            List<String> calls = new ArrayList<>();
            List<ReponseConsultationInserting> inserted = new ArrayList<>();
            boolean[] insertedCalled = {false};
            LocalDateTime now = LocalDateTime.parse(str(a, "now"));
            Clock clock = Clock.fixed(now.atZone(ZoneId.systemDefault()).toInstant(), ZoneId.systemDefault());
            InsertReponseConsultationRepository repo = S2Functions.proxy(InsertReponseConsultationRepository.class, (p, m, args) -> {
                insertedCalled[0] = true;
                inserted.addAll((List<ReponseConsultationInserting>) args[1]);
                return "FAILURE".equals(str(a, "insertResult")) ? InsertReponseConsultationRepository.InsertResult.INSERT_FAILURE : InsertReponseConsultationRepository.InsertResult.INSERT_SUCCESS;
            });
            UserAnsweredConsultationRepository answered = S2Functions.proxy(UserAnsweredConsultationRepository.class, (p, m, args) -> {
                calls.add("answered." + m.getName());
                if (m.getName().equals("hasAnsweredConsultation")) {
                    return boolOf(a, "hasAnswered");
                }
                if (m.getName().equals("insertUserAnsweredConsultation")) {
                    return UserAnsweredConsultationResult.SUCCESS;
                }
                throw new UnsupportedOperationException(m.getName());
            });
            ConsultationDetailsV2CacheRepository details = S2Functions.proxy(ConsultationDetailsV2CacheRepository.class, (p, m, args) -> {
                calls.add("details." + m.getName());
                return null;
            });
            ConsultationAnsweredPaginatedListCacheRepository paginated = S2Functions.proxy(ConsultationAnsweredPaginatedListCacheRepository.class, (p, m, args) -> {
                calls.add("paginated." + m.getName());
                return null;
            });
            ConsultationResultsCacheRepository results = S2Functions.proxy(ConsultationResultsCacheRepository.class, (p, m, args) -> {
                calls.add("results." + m.getName());
                return null;
            });
            InsertReponseConsultationUseCase uc = new InsertReponseConsultationUseCase(clock, new ContentSanitizer(), repo, answered, questionRepo(questions(a)),
                    new InsertConsultationResponseParametersMapper(), infoRepo(info), details, paginated, results, intOf(a, "openMax"));
            ObjectNode out = Functions.springJson().createObjectNode();
            try {
                out.put("result", uc.insertReponseConsultation(str(a, "consultationId"), str(a, "userId"), responses(a)).name());
            } catch (Exception e) {
                out.put("error", e.getClass().getSimpleName());
            }
            out.put("repositoryCalled", insertedCalled[0]);
            ArrayNode ins = out.putArray("inserted");
            for (ReponseConsultationInserting r : inserted) {
                ObjectNode o = ins.addObject();
                o.put("questionId", r.getQuestionId());
                if (r.getChoiceIds() == null) {
                    o.putNull("choiceIds");
                } else {
                    ArrayNode ids = o.putArray("choiceIds");
                    for (String c : r.getChoiceIds()) {
                        if (c == null) {
                            ids.addNull();
                        } else {
                            ids.add(c);
                        }
                    }
                }
                o.putPOJO("text", Functions.text16(r.getResponseText()));
            }
            ArrayNode cs = out.putArray("calls");
            for (String c : calls) {
                cs.add(c);
            }
            return out;
        });

        // ConsultationResultsUseCase (no cache) + ConsultationResultJsonMapper: the JSON and XML of the results, and the repository calls
        F.put("s6Results", a -> {
            List<String> calls = new ArrayList<>();
            ConsultationInfo info = info(a.get("info"));
            List<ResponseConsultationCount> counts = new ArrayList<>();
            for (JsonNode c : a.get("counts")) {
                counts.add(new ResponseConsultationCount(str(c, "questionId"), str(c, "choiceId"), intOf(c, "responseCount")));
            }
            List<ConsultationResultAggregated> aggregated = new ArrayList<>();
            for (JsonNode c : a.get("aggregated")) {
                aggregated.add(new ConsultationResultAggregated(str(a, "consultationId"), str(c, "questionId"), str(c, "choiceId"), intOf(c, "responseCount")));
            }
            GetConsultationResponseRepository responseRepo = S2Functions.proxy(GetConsultationResponseRepository.class, (p, m, args) -> {
                calls.add("responses." + m.getName());
                if (m.getName().equals("getConsultationResponsesCount")) {
                    return counts;
                }
                throw new UnsupportedOperationException(m.getName());
            });
            ConsultationResultAggregatedRepository aggregatedRepo = S2Functions.proxy(ConsultationResultAggregatedRepository.class, (p, m, args) -> {
                calls.add("aggregated." + m.getName());
                if (m.getName().equals("getConsultationResultAggregated")) {
                    return aggregated;
                }
                throw new UnsupportedOperationException(m.getName());
            });
            UserAnsweredConsultationRepository answered = S2Functions.proxy(UserAnsweredConsultationRepository.class, (p, m, args) -> {
                calls.add("answered." + m.getName());
                return intOf(a, "participantCount");
            });
            ConsultationResultsCacheRepository cache = S2Functions.proxy(ConsultationResultsCacheRepository.class, (p, m, args) ->
                    m.getName().equals("getConsultationResults") ? ConsultationResultsCacheResult.ConsultationResultsCacheNotInitialized.INSTANCE : null);
            ConsultationResultsUseCase uc = new ConsultationResultsUseCase(infoRepo(info), questionRepo(questions(a)), responseRepo, aggregatedRepo, answered,
                    new QuestionNoResponseMapper(), cache);
            ObjectNode out = Functions.springJson().createObjectNode();
            ConsultationResults r = uc.getConsultationResults(str(a, "consultationId"));
            if (r == null) {
                out.putNull("results");
            } else {
                Object body = new ConsultationResultJsonMapper().toJson(r);
                ObjectNode o = out.putObject("results");
                o.put("json", Functions.encode(Functions.springJson(), body));
                o.put("xml", Functions.encode(Functions.springXml(), body));
            }
            ArrayNode cs = out.putArray("calls");
            for (String c : calls) {
                cs.add(c);
            }
            return out;
        });

        // GetConsultationResultsWithDemographicRatiosUseCase + the TSV mapper (default locale of the JVM)
        F.put("s6Tsv", a -> {
            ConsultationInfo info = info(a.get("info"));
            List<ResponseConsultationCount> counts = new ArrayList<>();
            for (JsonNode c : a.get("counts")) {
                counts.add(new ResponseConsultationCount(str(c, "questionId"), str(c, "choiceId"), intOf(c, "responseCount")));
            }
            DemographicInfoCount global = demographic(a.get("global"));
            Map<String, Map<String, DemographicInfoCount>> byQuestion = new LinkedHashMap<>();
            a.get("byChoices").fields().forEachRemaining(q -> {
                Map<String, DemographicInfoCount> m = new LinkedHashMap<>();
                q.getValue().fields().forEachRemaining(c -> m.put(c.getKey(), demographic(c.getValue())));
                byQuestion.put(q.getKey(), m);
            });
            GetConsultationResponseRepository responseRepo = S2Functions.proxy(GetConsultationResponseRepository.class, (p, m, args) -> {
                switch (m.getName()) {
                    case "getConsultationResponsesCount":
                        return counts;
                    case "getParticipantDemographicInfo":
                        return global;
                    case "getParticipantDemographicInfoByChoices":
                        return new DemographicInfoCountByChoices(byQuestion.getOrDefault((String) args[0], new LinkedHashMap<>()));
                    default:
                        throw new UnsupportedOperationException(m.getName());
                }
            });
            UserAnsweredConsultationRepository answered = S2Functions.proxy(UserAnsweredConsultationRepository.class, (p, m, args) -> intOf(a, "participantCount"));
            GetConsultationResultsWithDemographicRatiosUseCase uc = new GetConsultationResultsWithDemographicRatiosUseCase(infoRepo(info), questionRepo(questions(a)),
                    responseRepo, answered, new QuestionNoResponseMapper());
            var r = uc.getConsultationResults(str(a, "consultationId"));
            if (r == null) {
                return Functions.map("tsv", null);
            }
            return Functions.map("tsv", Functions.text16(new ConsultationResultWithDemographicInfoTsvMapper().buildTsvBody(r)));
        });

        // NumberFormat.getInstance(locale) with maximumFractionDigits = 4, like the TSV mapper
        F.put("s6NumberFormat", a -> {
            java.text.NumberFormat nf = java.text.NumberFormat.getInstance(java.util.Locale.forLanguageTag(Functions.s(a, "locale")));
            nf.setMaximumFractionDigits(4);
            return Functions.text16(nf.format(Functions.dbl(a)));
        });

        // ReponseConsultationMapper.toDomain (the 8 lists of a consultation / the 8 lists of the choices of a question)
        F.put("s6DemographicMapper", a -> {
            ReponseConsultationMapper mapper = new ReponseConsultationMapper();
            ObjectNode out = Functions.springJson().createObjectNode();
            if (a.get("global") != null) {
                Object[] lists = new Object[8];
                for (int i = 0; i < 8; i++) {
                    List<DemographicInfoCountDTO> l = new ArrayList<>();
                    for (JsonNode e : a.get("global").get(i)) {
                        l.add(dto(e));
                    }
                    lists[i] = l;
                }
                DemographicInfoCount d = (DemographicInfoCount) mapperMethod(DemographicInfoCount.class).invoke(mapper, lists);
                out.set("global", demographicOut(d));
            }
            if (a.get("byChoices") != null) {
                Object[] lists = new Object[8];
                for (int i = 0; i < 8; i++) {
                    List<DemographicInfoCountByChoiceDTO> l = new ArrayList<>();
                    for (JsonNode e : a.get("byChoices").get(i)) {
                        l.add(dtoByChoice(e));
                    }
                    lists[i] = l;
                }
                DemographicInfoCountByChoices d = (DemographicInfoCountByChoices) mapperMethod(DemographicInfoCountByChoices.class).invoke(mapper, lists);
                ObjectNode m = out.putObject("byChoices");
                for (Map.Entry<String, DemographicInfoCount> e : new TreeMap<>(d.getChoiceDemographicInfoMap()).entrySet()) {
                    m.set(e.getKey(), demographicOut(e.getValue()));
                }
            }
            return out;
        });
    }
}
