import com.fasterxml.jackson.databind.JsonNode;
import fr.gouv.agora.domain.AgoraFeature;
import fr.gouv.agora.domain.ModeratusQag;
import fr.gouv.agora.domain.ModeratusQagLockResult;
import fr.gouv.agora.domain.QagLockResult;
import fr.gouv.agora.domain.QagStatus;
import fr.gouv.agora.domain.UserInfo;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.moderatus.ModerateQagOptionsMapper;
import fr.gouv.agora.infrastructure.moderatus.ModeratusQagListErrorXml;
import fr.gouv.agora.infrastructure.moderatus.ModeratusQagListXmlMapper;
import fr.gouv.agora.infrastructure.moderatus.ModeratusQagLockResultXmlMapper;
import fr.gouv.agora.infrastructure.moderatus.ModeratusQagModerateResultPageXmlMapper;
import fr.gouv.agora.infrastructure.moderatus.repository.ModeratusQagLockCacheRepository;
import fr.gouv.agora.infrastructure.moderatus.repository.ModeratusQagLockDatabaseRepository;
import fr.gouv.agora.infrastructure.moderatus.repository.ModeratusQagLockMapper;
import fr.gouv.agora.infrastructure.moderatus.repository.ModeratusQagLockRepositoryImpl;
import fr.gouv.agora.infrastructure.moderatus.dto.ModeratusQagLockDTO;
import fr.gouv.agora.infrastructure.notification.TypeNotification;
import fr.gouv.agora.infrastructure.notification.repository.NotificationMessageRepositoryImpl;
import fr.gouv.agora.usecase.featureFlags.repository.FeatureFlagsRepository;
import fr.gouv.agora.usecase.login.repository.UserRepository;
import fr.gouv.agora.usecase.moderatus.LockModeratusQagListUseCase;
import fr.gouv.agora.usecase.moderatus.ModerateModeratusQagUseCase;
import fr.gouv.agora.usecase.moderatus.ModerateQagOptions;
import fr.gouv.agora.usecase.moderatus.repository.ModeratusQagLockRepository;
import fr.gouv.agora.usecase.notification.SendQagNotificationUseCase;
import fr.gouv.agora.usecase.notification.repository.NotificationMessageRepository;
import fr.gouv.agora.usecase.notification.repository.NotificationRepository;
import fr.gouv.agora.usecase.notification.repository.NotificationResult;
import fr.gouv.agora.usecase.notification.repository.NotificationSendingRepository;
import fr.gouv.agora.usecase.qag.repository.QagInfo;
import fr.gouv.agora.usecase.qag.repository.QagInfoRepository;
import fr.gouv.agora.usecase.qag.repository.QagUpdateResult;
import fr.gouv.agora.usecase.qagArchive.AnonymizeOldQagUseCase;
import fr.gouv.agora.usecase.qagArchive.ArchiveOldQagUseCase;
import fr.gouv.agora.usecase.qagUpdates.repository.QagUpdatesRepository;
import org.springframework.cache.CacheManager;

import java.time.Clock;
import java.time.Instant;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.Date;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Oracle functions of slice S7 (Moderatus, moderation, archive). They run the REAL Kotlin mappers and use cases of
 * the reference jar, with the repositories replaced by recording proxies.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S7Functions {
    private S7Functions() {
    }

    private static String str(JsonNode n, String k) {
        JsonNode v = n.get(k);
        return v == null || v.isNull() ? null : v.textValue();
    }

    private static long lng(JsonNode n, String k) {
        return n.get(k).longValue();
    }

    /** JSON and XML like Spring MVC; an XML that Woodstox refuses to write is reported as "xmlError". */
    private static Map<String, Object> bodies(Object body) throws Exception {
        String json = Functions.encode(Functions.springJson(), body);
        try {
            return Functions.map("json", json, "xml", Functions.encode(Functions.springXml(), body));
        } catch (Exception e) {
            return Functions.map("json", json, "xmlError", e.getClass().getSimpleName() + ": " + e.getMessage());
        }
    }

    private static QagInfo qagInfo(JsonNode n) {
        return new QagInfo(str(n, "id"), "thematique", "title", "description", new Date(0), QagStatus.valueOf(str(n, "status")), "username", str(n, "userId"));
    }

    static void register(Map<String, Oracle.Fn> F) {
        // ModeratusQagListXmlMapper.toXml: JSON and XML bytes like Spring MVC
        F.put("moderatusListXml", a -> {
            List<ModeratusQag> qags = new ArrayList<>();
            for (JsonNode q : a.get("qags")) {
                qags.add(new ModeratusQag(str(q, "qagId"), str(q, "title"), str(q, "description"), new Date(lng(q, "dateMs")), str(q, "username"), str(q, "userId")));
            }
            return bodies(new ModeratusQagListXmlMapper(new DateMapper()).toXml(qags));
        });
        F.put("moderatusListErrorXml", a -> bodies(new ModeratusQagListErrorXml(0, str(a, "message"))));

        // ModeratusQagLockResultXmlMapper: the results of a lock request, or the error answer
        F.put("moderatusLockXml", a -> {
            ModeratusQagLockResultXmlMapper mapper = new ModeratusQagLockResultXmlMapper();
            if (a.has("errorMessage")) {
                List<String> ids = new ArrayList<>();
                for (JsonNode id : a.get("ids")) {
                    ids.add(id.textValue());
                }
                return bodies(mapper.toErrorXml(ids, str(a, "errorMessage")));
            }
            List<ModeratusQagLockResult> results = new ArrayList<>();
            for (JsonNode r : a.get("results")) {
                results.add(new ModeratusQagLockResult(str(r, "qagId"), QagLockResult.valueOf(str(r, "lockResult"))));
            }
            return bodies(mapper.toXml(results));
        });

        // ModeratusQagModerateResultPageXmlMapper.toXml(result, message) / toErrorXml(message)
        F.put("moderatusModerateXml", a -> {
            ModeratusQagModerateResultPageXmlMapper mapper = new ModeratusQagModerateResultPageXmlMapper();
            if (a.has("result")) {
                return bodies(mapper.toXml(fr.gouv.agora.domain.ModeratusQagModerateResult.valueOf(str(a, "result")), str(a, "message")));
            }
            return bodies(mapper.toErrorXml(str(a, "message")));
        });

        // ModerateQagOptionsMapper.toModerateQagOptions
        F.put("moderateOptions", a -> {
            JsonNode flag = a.get("shouldDeleteFlag");
            ModerateQagOptionsMapper.Result r = new ModerateQagOptionsMapper().toModerateQagOptions(
                    str(a, "qagId"), str(a, "userId"), str(a, "status"), str(a, "reason"),
                    flag == null || flag.isNull() ? null : flag.intValue(), str(a, "motifId"));
            if (r instanceof ModerateQagOptionsMapper.Result.Error) {
                return Functions.map("error", ((ModerateQagOptionsMapper.Result.Error) r).getErrorMessage());
            }
            ModerateQagOptions o = ((ModerateQagOptionsMapper.Result.Success) r).getOptions();
            return Functions.map("qagId", o.getQagId(), "userId", o.getUserId(), "isAccepted", o.isAccepted(), "reason", o.getReason(),
                    "shouldDelete", o.getShouldDelete(), "motifId", o.getMotifId());
        });

        // The messages of NotificationMessageRepositoryImpl
        F.put("qagNotificationMessages", a -> {
            NotificationMessageRepositoryImpl repo = new NotificationMessageRepositoryImpl();
            List<Object> accepted = new ArrayList<>();
            for (var m : repo.findAllByStatusAccepted()) {
                accepted.add(Functions.map("title", m.getTitle(), "message", m.getMessage()));
            }
            return Functions.map(
                    "rejected", Functions.map("title", repo.getQagRejected().getTitle(), "message", repo.getQagRejected().getMessage()),
                    "acceptedAfterReject", Functions.map("title", repo.getQagAcceptedAfterReject().getTitle(), "message", repo.getQagAcceptedAfterReject().getMessage()),
                    "accepted", accepted);
        });

        // ModerateModeratusQagUseCase with the REAL SendQagNotificationUseCase: the sequence of repository calls.
        //   qag: {id, status, userId}; qagMissing; updateFails; userFcmToken (null = user not found); lockedIds not used
        F.put("moderateFlow", a -> {
            List<String> calls = new ArrayList<>();
            JsonNode q = a.get("qag");
            boolean updateFails = a.has("updateFails") && a.get("updateFails").booleanValue();
            QagInfo info = qagInfo(q);
            Set<String> acceptedTitles = new java.util.HashSet<>();
            Set<String> acceptedMessages = new java.util.HashSet<>();
            for (var m : new NotificationMessageRepositoryImpl().findAllByStatusAccepted()) {
                acceptedTitles.add(m.getTitle());
                acceptedMessages.add(m.getMessage());
            }
            QagInfoRepository qags = S2Functions.proxy(QagInfoRepository.class, (p, method, args) -> {
                switch (method.getName()) {
                    case "getQagInfo":
                        calls.add("getQagInfo(" + args[0] + ")");
                        return args[0].equals(info.getId()) ? info : null;
                    case "updateQagStatus":
                        calls.add("updateQagStatus(" + args[0] + "," + args[1] + ")");
                        return updateFails ? QagUpdateResult.Failure.INSTANCE : new QagUpdateResult.Success(info);
                    case "updateQagMotifId":
                        calls.add("updateQagMotifId(" + args[0] + "," + args[1] + ")");
                        return null;
                    default:
                        throw new UnsupportedOperationException(method.getName());
                }
            });
            UserRepository users = S2Functions.proxy(UserRepository.class, (p, method, args) -> {
                if (method.getName().equals("getUserById")) {
                    calls.add("getUserById(" + args[0] + ")");
                    String token = str(a, "userFcmToken");
                    return token == null ? null : new UserInfo((String) args[0], token, false, new ArrayList<>());
                }
                throw new UnsupportedOperationException(method.getName());
            });
            NotificationSendingRepository sending = S2Functions.proxy(NotificationSendingRepository.class, (p, method, args) -> {
                String title = acceptedTitles.contains(args[0]) ? "<accepted>" : (String) args[0];
                String desc = acceptedMessages.contains(args[1]) ? "<accepted>" : (String) args[1];
                calls.add("sendGenericMultiNotification(" + title + "," + desc + "," + args[2] + "," + args[3] + "," + args[4] + ")");
                return NotificationResult.SUCCESS;
            });
            NotificationRepository notifications = S2Functions.proxy(NotificationRepository.class, (p, method, args) -> {
                if (method.getName().equals("insertNotifications")) {
                    fr.gouv.agora.domain.NotificationInserting n = (fr.gouv.agora.domain.NotificationInserting) args[0];
                    String title = acceptedTitles.contains(n.getTitle()) ? "<accepted>" : n.getTitle();
                    String desc = acceptedMessages.contains(n.getDescription()) ? "<accepted>" : n.getDescription();
                    calls.add("insertNotifications(" + title + "," + desc + "," + n.getType() + "," + n.getUserIds() + ")");
                    return null;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            QagUpdatesRepository updates = S2Functions.proxy(QagUpdatesRepository.class, (p, method, args) -> {
                if (method.getName().equals("insertQagUpdates")) {
                    fr.gouv.agora.domain.QagInsertingUpdates u = (fr.gouv.agora.domain.QagInsertingUpdates) args[0];
                    calls.add("insertQagUpdates(" + u.getQagId() + "," + u.getNewQagStatus() + "," + u.getUserId() + "," + u.getReason() + "," + u.getShouldDelete() + "," + u.getMotifId() + ")");
                    return null;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            ModeratusQagLockRepository locks = S2Functions.proxy(ModeratusQagLockRepository.class, (p, method, args) -> {
                if (method.getName().equals("removeLockedQagId")) {
                    calls.add("removeLockedQagId(" + args[0] + ")");
                    return null;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            SendQagNotificationUseCase send = new SendQagNotificationUseCase(users, qags, sending, new NotificationMessageRepositoryImpl(), notifications);
            ModerateModeratusQagUseCase useCase = new ModerateModeratusQagUseCase(qags, send, updates, locks);
            ModerateQagOptions options = new ModerateQagOptions(str(a, "optionsQagId"), "moderator", a.get("isAccepted").booleanValue(),
                    str(a, "reason"), a.get("shouldDelete").booleanValue(), str(a, "motifId"));
            String result;
            try {
                result = useCase.moderateQag(options).name();
            } catch (Throwable t) {
                result = "THROWS " + t.getClass().getSimpleName();
            }
            return Functions.map("result", result, "calls", calls);
        });

        // LockModeratusQagListUseCase + the REAL ModeratusQagLockRepositoryImpl (database and cache proxies, the cache is
        // never filled): the results, the qag ids saved and the sequence of repository calls.
        //   existing: {id: status}; locked: qag uuids in the table
        F.put("lockFlow", a -> {
            List<String> calls = new ArrayList<>();
            Map<String, String> existing = new LinkedHashMap<>();
            a.get("existing").fields().forEachRemaining(e -> existing.put(e.getKey(), e.getValue().textValue()));
            List<ModeratusQagLockDTO> table = new ArrayList<>();
            for (JsonNode id : a.get("locked")) {
                table.add(new ModeratusQagLockDTO(java.util.UUID.fromString(id.textValue()), new Date(0), java.util.UUID.fromString(id.textValue())));
            }
            QagInfoRepository qags = S2Functions.proxy(QagInfoRepository.class, (p, method, args) -> {
                if (method.getName().equals("getQagInfo")) {
                    calls.add("getQagInfo(" + args[0] + ")");
                    String st = existing.get(args[0]);
                    return st == null ? null : new QagInfo((String) args[0], "t", "title", "description", new Date(0), QagStatus.valueOf(st), "u", "user");
                }
                throw new UnsupportedOperationException(method.getName());
            });
            CacheManager cacheManager = S2Functions.proxy(CacheManager.class, (p, method, args) -> null);
            ModeratusQagLockDatabaseRepository db = S2Functions.proxy(ModeratusQagLockDatabaseRepository.class, (p, method, args) -> {
                switch (method.getName()) {
                    case "findAll":
                        calls.add("findAll");
                        return table;
                    case "saveAll":
                        List<ModeratusQagLockDTO> dtos = new ArrayList<ModeratusQagLockDTO>((java.util.Collection<ModeratusQagLockDTO>) args[0]);
                        List<String> saved = new ArrayList<>();
                        for (ModeratusQagLockDTO d : dtos) {
                            saved.add(d.getQagId().toString());
                        }
                        calls.add("saveAll" + saved);
                        return dtos;
                    default:
                        throw new UnsupportedOperationException(method.getName());
                }
            });
            ModeratusQagLockRepositoryImpl repo = new ModeratusQagLockRepositoryImpl(db, new ModeratusQagLockCacheRepository(cacheManager), new ModeratusQagLockMapper());
            List<String> ids = new ArrayList<>();
            for (JsonNode id : a.get("ids")) {
                ids.add(id.textValue());
            }
            List<Object> results = new ArrayList<>();
            for (ModeratusQagLockResult r : new LockModeratusQagListUseCase(qags, repo).lockQagIds(ids)) {
                results.add(Functions.map("qagId", r.getQagId(), "lockResult", r.getLockResult().name()));
            }
            return Functions.map("results", results, "calls", calls);
        });

        // RemoveLockedQagId of the real repository: whether the database delete is reached (valid UUID.fromString)
        F.put("removeLockedFlow", a -> {
            List<String> calls = new ArrayList<>();
            CacheManager cacheManager = S2Functions.proxy(CacheManager.class, (p, method, args) -> null);
            ModeratusQagLockDatabaseRepository db = S2Functions.proxy(ModeratusQagLockDatabaseRepository.class, (p, method, args) -> {
                if (method.getName().equals("deleteLockedQag")) {
                    calls.add("deleteLockedQag(" + args[0] + ")");
                    return 1;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            new ModeratusQagLockRepositoryImpl(db, new ModeratusQagLockCacheRepository(cacheManager), new ModeratusQagLockMapper()).removeLockedQagId(str(a, "qagId"));
            return calls;
        });

        // ArchiveOldQagUseCase / AnonymizeOldQagUseCase with a fixed clock (JVM default zone): the Date handed to the repository
        F.put("archiveDates", a -> {
            Clock clock = Clock.fixed(Instant.ofEpochMilli(lng(a, "nowMs")).plusNanos(lng(a, "nowExtraNanos")), ZoneId.systemDefault());
            List<Object> dates = new ArrayList<>();
            QagInfoRepository qags = S2Functions.proxy(QagInfoRepository.class, (p, method, args) -> {
                if (method.getName().equals("archiveOldQags") || method.getName().equals("anonymizeOldQags")) {
                    dates.add(Functions.map("call", method.getName(), "dateMs", ((Date) args[0]).getTime()));
                    return null;
                }
                throw new UnsupportedOperationException(method.getName());
            });
            FeatureFlagsRepository flags = S2Functions.proxy(FeatureFlagsRepository.class, (p, method, args) -> {
                AgoraFeature f = (AgoraFeature) args[0];
                return a.get("flags").get(f.name()).booleanValue();
            });
            String archive = new ArchiveOldQagUseCase(flags, qags, clock).archiveOldQag().name();
            String anonymize = new AnonymizeOldQagUseCase(flags, qags, clock).anonymizeOldQag().name();
            return Functions.map("archive", archive, "anonymize", anonymize, "calls", dates);
        });
    }
}
