import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import fr.gouv.agora.domain.Notification;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.notification.NotificationPaginatedJsonMapper;
import fr.gouv.agora.infrastructure.notification.TypeNotification;
import fr.gouv.agora.infrastructure.notification.dto.NotificationMessageDTO;
import fr.gouv.agora.infrastructure.notification.repository.NotificationMessageRepositoryImpl;
import fr.gouv.agora.usecase.notification.NotificationListAndHasMoreNotificationsFlag;

import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Map;

/** Slice S8 (notifications): the real Kotlin mappers, the enum and the texts of the QaG notifications. */
final class S8Functions {
    private S8Functions() {
    }

    static void register(Map<String, Oracle.Fn> F) {
        // NotificationPaginatedJsonMapper.toJson: args {"hasMore": bool, "notifications": [{title, description, type, millis}]}
        F.put("s8PaginatedJson", a -> {
            List<Notification> list = new ArrayList<>();
            for (JsonNode n : a.get("notifications")) {
                list.add(new Notification(n.get("title").textValue(), n.get("description").textValue(),
                        TypeNotification.valueOf(n.get("type").textValue()), new Date(n.get("millis").longValue()), "user"));
            }
            NotificationListAndHasMoreNotificationsFlag flag = new NotificationListAndHasMoreNotificationsFlag(list, a.get("hasMore").booleanValue());
            Object body = new NotificationPaginatedJsonMapper(new DateMapper()).toJson(flag);
            ObjectNode out = Functions.springJson().createObjectNode();
            out.put("json", Functions.encode(Functions.springJson(), body));
            out.put("xml", Functions.encode(Functions.springXml(), body));
            return out;
        });

        // DateMapper.toFormattedDate(Date) of the process zone: args {"millis": long}
        F.put("s8FormatDate", a -> new DateMapper().toFormattedDate(new Date(a.get("millis").longValue())));

        // TypeNotification: the constants in declaration order (name and ordinal)
        F.put("s8Types", a -> {
            ArrayNode out = Functions.springJson().createArrayNode();
            for (TypeNotification t : TypeNotification.values()) {
                ObjectNode o = out.addObject();
                o.put("name", t.name());
                o.put("ordinal", t.ordinal());
            }
            return out;
        });

        // NotificationMessageRepositoryImpl: the texts (the accepted set keeps its declaration order)
        F.put("s8Messages", a -> {
            NotificationMessageRepositoryImpl repo = new NotificationMessageRepositoryImpl();
            ObjectNode out = Functions.springJson().createObjectNode();
            ArrayNode accepted = out.putArray("accepted");
            for (NotificationMessageDTO m : repo.findAllByStatusAccepted()) {
                ObjectNode o = accepted.addObject();
                o.put("title", m.getTitle());
                o.put("message", m.getMessage());
            }
            ObjectNode rejected = out.putObject("rejected");
            rejected.put("title", repo.getQagRejected().getTitle());
            rejected.put("message", repo.getQagRejected().getMessage());
            ObjectNode after = out.putObject("afterReject");
            after.put("title", repo.getQagAcceptedAfterReject().getTitle());
            after.put("message", repo.getQagAcceptedAfterReject().getMessage());
            return out;
        });
    }
}
