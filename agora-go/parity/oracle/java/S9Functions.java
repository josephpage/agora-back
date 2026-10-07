import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import fr.gouv.agora.config.CmsStrapiHttpClient;
import fr.gouv.agora.domain.AppFeedbackInserting;
import fr.gouv.agora.domain.FicheInventaire;
import fr.gouv.agora.infrastructure.appFeedback.AppFeedbackJson;
import fr.gouv.agora.infrastructure.appFeedback.AppFeedbackJsonMapper;
import fr.gouv.agora.infrastructure.common.DateMapper;
import fr.gouv.agora.infrastructure.content.ContentController;
import fr.gouv.agora.infrastructure.content.repository.ContentRepositoryImpl;
import fr.gouv.agora.infrastructure.content.repository.ContentStrapiRepository;
import fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireFilters;
import fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireJsonMapper;
import fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireRepositoryImpl;
import fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireStrapiRepository;
import fr.gouv.agora.infrastructure.participationCharter.ParticipationCharterController;
import fr.gouv.agora.infrastructure.participationCharter.repository.ParticipationCharterRepositoryImpl;
import fr.gouv.agora.infrastructure.participationCharter.repository.ParticipationCharterStrapiRepository;
import fr.gouv.agora.infrastructure.thematique.ThematiqueJsonMapper;
import fr.gouv.agora.infrastructure.thematique.repository.ThematiqueMapper;
import fr.gouv.agora.infrastructure.welcomePage.repository.NewsRepository;
import fr.gouv.agora.infrastructure.welcomePage.repository.NewsStrapiRepository;
import fr.gouv.agora.usecase.appFeedback.repository.AppFeedbackMapper;
import fr.gouv.agora.usecase.content.GetContentPagePoserMaQuestionUseCase;
import fr.gouv.agora.usecase.content.GetContentPageReponseAuxQuestionsAuGouvernementUseCase;
import fr.gouv.agora.usecase.content.GetContentQuestionsAuGouvernementUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrineAccueilContentUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrineConditionGeneralesContentUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrineConsultationContentUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrineDeclarationAccessibiliteContentUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrineMentionsLegalesContentUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrinePolitiqueConfidentialiteContentUseCase;
import fr.gouv.agora.usecase.content.GetSiteVitrineQuestionAuGouvernementContentUseCase;
import fr.gouv.agora.usecase.participationCharter.ParticipationCharterUseCase;
import fr.gouv.agora.usecase.qag.repository.QagInfoRepository;
import fr.gouv.agora.usecase.welcomePage.GetLastNewsUseCase;
import fr.gouv.agora.usecase.welcomePage.NewsJsonMapper;
import org.springframework.cache.Cache;
import org.springframework.cache.CacheManager;
import org.springframework.cache.concurrent.ConcurrentMapCache;
import org.springframework.http.ResponseEntity;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLParameters;
import javax.net.ssl.SSLSession;
import java.io.IOException;
import java.lang.reflect.Proxy;
import java.net.Authenticator;
import java.net.CookieHandler;
import java.net.ProxySelector;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpHeaders;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.Executor;

/**
 * Oracle functions of slice S9 (CMS content pages, welcome page news, participation charter, fiches inventaire, app
 * feedback). They run the REAL Kotlin repositories / use cases / controllers of the reference jar; the Strapi HTTP
 * client answers a canned payload and records the URI it was asked.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
final class S9Functions {
    private S9Functions() {
    }

    /** HttpClient answering the same body to every request and recording the request URIs. */
    static final class CannedClient extends HttpClient {
        final String body;
        final List<String> uris = new ArrayList<>();

        CannedClient(String body) {
            this.body = body;
        }

        @Override
        public Optional<CookieHandler> cookieHandler() {
            return Optional.empty();
        }

        @Override
        public Optional<Duration> connectTimeout() {
            return Optional.empty();
        }

        @Override
        public Redirect followRedirects() {
            return Redirect.NEVER;
        }

        @Override
        public Optional<ProxySelector> proxy() {
            return Optional.empty();
        }

        @Override
        public SSLContext sslContext() {
            return null;
        }

        @Override
        public SSLParameters sslParameters() {
            return null;
        }

        @Override
        public Optional<Authenticator> authenticator() {
            return Optional.empty();
        }

        @Override
        public Version version() {
            return Version.HTTP_1_1;
        }

        @Override
        public Optional<Executor> executor() {
            return Optional.empty();
        }

        @Override
        public <T> HttpResponse<T> send(HttpRequest request, HttpResponse.BodyHandler<T> handler) throws IOException {
            uris.add(request.uri().toString());
            final String b = body;
            return (HttpResponse<T>) new HttpResponse<String>() {
                public int statusCode() {
                    return 200;
                }

                public HttpRequest request() {
                    return request;
                }

                public Optional<HttpResponse<String>> previousResponse() {
                    return Optional.empty();
                }

                public HttpHeaders headers() {
                    return HttpHeaders.of(Collections.emptyMap(), (x, y) -> true);
                }

                public String body() {
                    return b;
                }

                public Optional<SSLSession> sslSession() {
                    return Optional.empty();
                }

                public URI uri() {
                    return request.uri();
                }

                public HttpClient.Version version() {
                    return Version.HTTP_1_1;
                }
            };
        }

        @Override
        public <T> CompletableFuture<HttpResponse<T>> sendAsync(HttpRequest request, HttpResponse.BodyHandler<T> handler) {
            throw new UnsupportedOperationException();
        }

        @Override
        public <T> CompletableFuture<HttpResponse<T>> sendAsync(HttpRequest request, HttpResponse.BodyHandler<T> handler,
                                                                HttpResponse.PushPromiseHandler<T> push) {
            throw new UnsupportedOperationException();
        }
    }

    static CmsStrapiHttpClient strapi(CannedClient c) {
        return new CmsStrapiHttpClient(c, Functions.appMapper(), false, "token", "http://strapi/api/");
    }

    static String uri(CannedClient c) {
        // the path and query after the Strapi base URL
        if (c.uris.isEmpty()) {
            return null;
        }
        String u = c.uris.get(c.uris.size() - 1);
        return u.substring("http://strapi/api/".length());
    }

    static String json(Object v) throws Exception {
        return Functions.encode(Functions.springJson(), v);
    }

    static Map<String, Object> failure(Throwable t, CannedClient c) {
        Throwable root = t;
        return Functions.map("exception", root.getClass().getSimpleName(), "message", String.valueOf(root.getMessage()), "uri", uri(c));
    }

    static List<String> strList(JsonNode a, String k) {
        JsonNode n = a.get(k);
        if (n == null || n.isNull()) {
            return null;
        }
        List<String> out = new ArrayList<>();
        for (JsonNode e : n) {
            out.add(e.isNull() ? null : e.asText());
        }
        return out;
    }

    static FicheInventaireFilters filters(JsonNode a) {
        return new FicheInventaireFilters(Functions.sOpt(a, "titre"), Functions.sOpt(a, "thematique"), strList(a, "etape"),
                strList(a, "conditionParticipation"), strList(a, "modaliteParticipation"), Functions.sOpt(a, "annee"));
    }

    /** CacheManager whose single cache records the keys it is asked for. */
    static final class RecordingCacheManager implements CacheManager {
        final List<String> keys = new ArrayList<>();
        final Cache cache = new ConcurrentMapCache("fichesInventaireCache") {
            @Override
            public <T> T get(Object key, Class<T> type) {
                keys.add(String.valueOf(key));
                return super.get(key, type);
            }
        };

        public Cache getCache(String name) {
            return cache;
        }

        public Collection<String> getCacheNames() {
            return Collections.singletonList("fichesInventaireCache");
        }
    }

    static FicheInventaireRepositoryImpl ficheRepo(CannedClient c, CacheManager cm) {
        return new FicheInventaireRepositoryImpl(new FicheInventaireStrapiRepository(strapi(c)), new ThematiqueMapper(), cm);
    }

    static ContentController contentController(CannedClient c, final int qagCount) {
        ContentRepositoryImpl repo = new ContentRepositoryImpl(new ContentStrapiRepository(strapi(c)));
        QagInfoRepository qags = (QagInfoRepository) Proxy.newProxyInstance(S9Functions.class.getClassLoader(),
                new Class<?>[]{QagInfoRepository.class}, (proxy, method, args) -> {
                    if (method.getName().equals("getQagsCount")) {
                        return qagCount;
                    }
                    throw new UnsupportedOperationException(method.getName());
                });
        return new ContentController(
                new GetContentQuestionsAuGouvernementUseCase(repo, qags),
                new GetContentPageReponseAuxQuestionsAuGouvernementUseCase(repo),
                new GetContentPagePoserMaQuestionUseCase(repo),
                new GetSiteVitrineAccueilContentUseCase(repo),
                new GetSiteVitrineConditionGeneralesContentUseCase(repo),
                new GetSiteVitrineConsultationContentUseCase(repo),
                new GetSiteVitrineDeclarationAccessibiliteContentUseCase(repo),
                new GetSiteVitrineMentionsLegalesContentUseCase(repo),
                new GetSiteVitrinePolitiqueConfidentialiteContentUseCase(repo),
                new GetSiteVitrineQuestionAuGouvernementContentUseCase(repo));
    }

    static Clock clock(JsonNode a) {
        long ns = a.get("nowNs").asLong();
        return Clock.fixed(Instant.ofEpochSecond(Math.floorDiv(ns, 1_000_000_000L), Math.floorMod(ns, 1_000_000_000L)), ZoneOffset.UTC);
    }

    static void register(Map<String, Oracle.Fn> F) {
        // jackson-datatype-jsr310 LocalDate / LocalDateTime out of a JSON text (the application ObjectMapper)
        F.put("s9LocalDate", a -> {
            try {
                LocalDate d = Functions.appMapper().readValue(Functions.s(a, "json"), LocalDate.class);
                return Functions.map("ok", d != null, "value", d == null ? null : d.toString(),
                        "formatted", d == null ? null : new DateMapper().toFormattedDate(d));
            } catch (Exception e) {
                return Functions.map("ok", false, "exception", e.getClass().getSimpleName());
            }
        });
        F.put("s9LocalDateTime", a -> {
            try {
                LocalDateTime d = Functions.appMapper().readValue(Functions.s(a, "json"), LocalDateTime.class);
                return Functions.map("ok", d != null, "value", d == null ? null : d.toString());
            } catch (Exception e) {
                return Functions.map("ok", false, "exception", e.getClass().getSimpleName());
            }
        });

        // FicheInventaireRepositoryImpl.getAll + FicheInventaireJsonMapper on a canned Strapi payload
        F.put("s9Fiches", a -> {
            CannedClient c = new CannedClient(Functions.s(a, "payload"));
            RecordingCacheManager cm = new RecordingCacheManager();
            FicheInventaireFilters f = filters(a);
            Map<String, Object> out = Functions.map("uri", null, "key", null,
                    "titre", f.getTitre(), "thematique", f.getThematique(), "etape", f.getEtape(),
                    "condition", f.getConditionParticipation(), "modalite", f.getModaliteParticipation(), "annee", f.getAnneeDeLancement());
            try {
                List<FicheInventaire> list = ficheRepo(c, cm).getAll(f);
                FicheInventaireJsonMapper m = new FicheInventaireJsonMapper(new DateMapper(), new ThematiqueJsonMapper());
                List<Object> js = new ArrayList<>();
                for (FicheInventaire fi : list) {
                    js.add(m.toFicheInventaireJson(fi));
                }
                out.put("json", json(js));
            } catch (Throwable t) {
                out.put("exception", t.getClass().getSimpleName());
                out.put("message", String.valueOf(t.getMessage()));
            }
            out.put("uri", uri(c));
            out.put("key", cm.keys.isEmpty() ? null : cm.keys.get(0));
            return out;
        });
        // FicheInventaireRepositoryImpl.get(id) (the use case throws FicheInventaireNotFound on null)
        F.put("s9Fiche", a -> {
            CannedClient c = new CannedClient(Functions.s(a, "payload"));
            try {
                FicheInventaire fi = ficheRepo(c, new RecordingCacheManager()).get(Functions.s(a, "id"));
                if (fi == null) {
                    return Functions.map("json", null, "uri", uri(c));
                }
                FicheInventaireJsonMapper m = new FicheInventaireJsonMapper(new DateMapper(), new ThematiqueJsonMapper());
                return Functions.map("json", json(m.toFicheInventaireJson(fi)), "uri", uri(c));
            } catch (Throwable t) {
                return failure(t, c);
            }
        });

        // ContentController.<page>() on a canned single-type payload
        F.put("s9Page", a -> {
            CannedClient c = new CannedClient(Functions.s(a, "payload"));
            try {
                ContentController ctl = contentController(c, a.hasNonNull("qagCount") ? a.get("qagCount").asInt() : 0);
                ResponseEntity<?> r;
                switch (Functions.s(a, "page")) {
                    case "questions":
                        r = ctl.getContentQuestionsAuGouvernementPage();
                        break;
                    case "reponses":
                        r = ctl.getContentReponsesAVenirPage();
                        break;
                    case "poser":
                        r = ctl.getContentPoserMaQuestionPage();
                        break;
                    case "accueil":
                        r = ctl.getContentSiteVitrineAccueilPage();
                        break;
                    case "cgu":
                        r = ctl.getContentSiteVitrineConditionGeneralesPage();
                        break;
                    case "consultation":
                        r = ctl.getContentSiteVitrineConsultationPage();
                        break;
                    case "declaration":
                        r = ctl.getContentSiteVitrineDeclarationAccessibilitePage();
                        break;
                    case "mentions":
                        r = ctl.getContentSiteVitrineMentionsLegalesPage();
                        break;
                    case "politique":
                        r = ctl.getContentSiteVitrinePolitiqueConfidentialitePage();
                        break;
                    case "qag":
                        r = ctl.getContentSiteVitrineQuestionAuGouvernementPage();
                        break;
                    default:
                        throw new IllegalArgumentException("unknown page");
                }
                return Functions.map("json", json(r.getBody()), "uri", uri(c), "cacheControl", r.getHeaders().getCacheControl());
            } catch (Throwable t) {
                return failure(t, c);
            }
        });

        // GetLastNewsUseCase with a fixed UTC clock
        F.put("s9News", a -> {
            CannedClient c = new CannedClient(Functions.s(a, "payload"));
            try {
                GetLastNewsUseCase uc = new GetLastNewsUseCase(new NewsRepository(new NewsStrapiRepository(strapi(c))), clock(a), new NewsJsonMapper());
                Object r = uc.execute();
                return Functions.map("json", r == null ? null : json(r), "uri", uri(c));
            } catch (Throwable t) {
                return failure(t, c);
            }
        });

        // ParticipationCharterController with a fixed UTC clock
        F.put("s9Charter", a -> {
            CannedClient c = new CannedClient(Functions.s(a, "payload"));
            try {
                ParticipationCharterController ctl = new ParticipationCharterController(new ParticipationCharterUseCase(
                        new ParticipationCharterRepositoryImpl(new ParticipationCharterStrapiRepository(strapi(c)), clock(a))));
                return Functions.map("json", json(ctl.getParticipationCharterText().getBody()), "uri", uri(c));
            } catch (Throwable t) {
                return failure(t, c);
            }
        });

        // AppFeedbackJsonMapper.toDomain + AppFeedbackMapper.toDto: the stored type, or null (HTTP 400)
        F.put("s9FeedbackType", a -> {
            AppFeedbackJson body = new AppFeedbackJson(Functions.s(a, "type"), "d", null);
            AppFeedbackInserting d = new AppFeedbackJsonMapper().toDomain(body, "00000000-0000-4000-9000-000000000001");
            if (d == null) {
                return null;
            }
            return new AppFeedbackMapper().toDto(d).getType();
        });
    }
}
