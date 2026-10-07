import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.type.TypeFactory;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.fasterxml.jackson.module.kotlin.ExtensionsKt;
import fr.gouv.agora.config.AcmeConfig;
import fr.gouv.agora.domain.LoginTokenData;
import fr.gouv.agora.infrastructure.acme.repository.AcmeCryptoHelper;
import fr.gouv.agora.infrastructure.common.StrapiRichText;
import fr.gouv.agora.infrastructure.common.StrapiRichTextKt;
import fr.gouv.agora.infrastructure.login.BuildResult;
import fr.gouv.agora.infrastructure.login.DecodeResult;
import fr.gouv.agora.infrastructure.login.LoginTokenGenerator;
import fr.gouv.agora.infrastructure.utils.IpAddressUtils;
import fr.gouv.agora.security.jwt.JwtTokenUtils;
import fr.gouv.agora.usecase.qag.ContentSanitizer;
import jakarta.servlet.http.HttpServletRequest;
import kotlin.Pair;
import kotlin.text.Regex;
import kotlin.text.StringsKt;
import org.owasp.html.HtmlPolicyBuilder;
import org.springframework.http.converter.json.Jackson2ObjectMapperBuilder;
import org.springframework.web.util.HtmlUtils;

import java.io.ByteArrayInputStream;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.text.NumberFormat;
import java.time.LocalDateTime;
import java.time.format.DateTimeFormatter;
import java.util.ArrayList;
import java.util.Base64;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.UUID;

/**
 * The oracle functions. Every function calls the REAL Kotlin/JVM code of the reference jar.
 *
 * <p>Conventions:
 * <ul>
 *   <li>args are JSON objects; a missing required arg raises IllegalArgumentException;</li>
 *   <li>functions returning a Java String return it as a plain JSON string, unless the request
 *       carries {@code "utf16":true}, in which case the result is {@code {"text":..,"utf16":[..]}};</li>
 *   <li>functions whose output may legitimately contain lone surrogates (sanitize, sanitizeRaw,
 *       htmlUnescape, kotlinTake) always return {@code {"text":..,"utf16":[..]}}.</li>
 * </ul>
 */
@SuppressWarnings({"deprecation", "unchecked"})
final class Functions {
    private Functions() {
    }

    // ------------------------------------------------------------------------------------------
    // Argument helpers

    static String s(JsonNode a, String k) {
        JsonNode n = a.get(k);
        if (n == null) {
            throw new IllegalArgumentException("missing argument '" + k + "'");
        }
        if (n.isNull()) {
            return null;
        }
        if (!n.isTextual()) {
            throw new IllegalArgumentException("argument '" + k + "' must be a string");
        }
        return n.textValue();
    }

    static String sOpt(JsonNode a, String k) {
        JsonNode n = a.get(k);
        return n == null || n.isNull() ? null : s(a, k);
    }

    static int i(JsonNode a, String k) {
        JsonNode n = a.get(k);
        if (n == null) {
            throw new IllegalArgumentException("missing argument '" + k + "'");
        }
        if (!n.isIntegralNumber() || !n.canConvertToInt()) {
            throw new IllegalArgumentException("argument '" + k + "' must be a 32-bit integer");
        }
        return n.intValue();
    }

    /** A double from {"bits": <raw IEEE-754 bits as long/unsigned/hex string>} or {"d": number | "NaN" | "Infinity"...}. */
    static double dbl(JsonNode a) {
        JsonNode b = a.get("bits");
        if (b != null && !b.isNull()) {
            if (b.isIntegralNumber()) {
                return Double.longBitsToDouble(b.bigIntegerValue().longValue()); // low 64 bits (accepts unsigned)
            }
            if (b.isTextual()) {
                String t = b.textValue().trim();
                if (t.startsWith("0x") || t.startsWith("0X")) {
                    return Double.longBitsToDouble(Long.parseUnsignedLong(t.substring(2), 16));
                }
                return Double.longBitsToDouble(new java.math.BigInteger(t).longValue());
            }
            throw new IllegalArgumentException("argument 'bits' must be an integer");
        }
        JsonNode d = a.get("d");
        if (d == null) {
            throw new IllegalArgumentException("missing argument 'd' (or 'bits')");
        }
        if (d.isNumber()) {
            return d.doubleValue();
        }
        if (d.isTextual()) {
            switch (d.textValue()) {
                case "NaN":
                    return Double.NaN;
                case "Infinity":
                case "+Infinity":
                    return Double.POSITIVE_INFINITY;
                case "-Infinity":
                    return Double.NEGATIVE_INFINITY;
                default:
                    return Double.parseDouble(d.textValue());
            }
        }
        throw new IllegalArgumentException("argument 'd' must be a number");
    }

    static Map<String, Object> text16(String s) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("text", s);
        if (s == null) {
            m.put("utf16", null);
        } else {
            int[] units = new int[s.length()];
            for (int k = 0; k < units.length; k++) {
                units[k] = s.charAt(k);
            }
            m.put("utf16", units);
        }
        return m;
    }

    static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int k = 0; k < kv.length; k += 2) {
            m.put((String) kv[k], kv[k + 1]);
        }
        return m;
    }

    /** Raised when Kotlin code swallowed an exception and returned a Failure object. */
    static final class Failure extends RuntimeException {
        Failure(String message) {
            super(message);
        }
    }

    // ------------------------------------------------------------------------------------------
    // Jackson mappers

    private static ObjectMapper jsonMapper;
    private static ObjectMapper xmlMapper;
    private static ObjectMapper appMapper;

    /** The mapper Spring MVC builds under @EnableWebMvc for application/json. */
    static synchronized ObjectMapper springJson() {
        if (jsonMapper == null) {
            jsonMapper = Jackson2ObjectMapperBuilder.json().build();
        }
        return jsonMapper;
    }

    /** The mapper Spring MVC builds under @EnableWebMvc for application/xml (MappingJackson2XmlHttpMessageConverter). */
    static synchronized ObjectMapper springXml() {
        if (xmlMapper == null) {
            xmlMapper = Jackson2ObjectMapperBuilder.xml().build();
        }
        return xmlMapper;
    }

    /** config/ObjectMapperConfig.kt */
    static synchronized ObjectMapper appMapper() {
        if (appMapper == null) {
            ObjectMapper m = ExtensionsKt.registerKotlinModule(ExtensionsKt.jacksonObjectMapper());
            m.registerModule(new JavaTimeModule());
            m.configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);
            appMapper = m;
        }
        return appMapper;
    }

    private static final Map<String, String> ALIASES = new HashMap<>();

    static {
        ALIASES.put("String", "java.lang.String");
        ALIASES.put("Object", "java.lang.Object");
        ALIASES.put("Integer", "java.lang.Integer");
        ALIASES.put("Long", "java.lang.Long");
        ALIASES.put("Double", "java.lang.Double");
        ALIASES.put("Boolean", "java.lang.Boolean");
        ALIASES.put("List", "java.util.List");
        ALIASES.put("Set", "java.util.Set");
        ALIASES.put("Map", "java.util.Map");
    }

    static Class<?> loadClass(String name) throws ClassNotFoundException {
        name = name.trim();
        String alias = ALIASES.get(name);
        if (alias != null) {
            name = alias;
        }
        switch (name) {
            case "int":
                return int.class;
            case "long":
                return long.class;
            case "double":
                return double.class;
            case "boolean":
                return boolean.class;
            default:
                return Class.forName(name);
        }
    }

    /** Parses "pkg.Class", "pkg.Class[]", "java.util.List<pkg.Class>", "Map<String,pkg.Class>". */
    static JavaType parseType(TypeFactory tf, String spec) throws ClassNotFoundException {
        spec = spec.trim();
        if (spec.endsWith("[]")) {
            return tf.constructArrayType(parseType(tf, spec.substring(0, spec.length() - 2)));
        }
        int lt = spec.indexOf('<');
        if (lt < 0) {
            return tf.constructType(loadClass(spec));
        }
        if (!spec.endsWith(">")) {
            throw new IllegalArgumentException("bad type: " + spec);
        }
        Class<?> raw = loadClass(spec.substring(0, lt));
        String inner = spec.substring(lt + 1, spec.length() - 1);
        List<JavaType> params = new ArrayList<>();
        int depth = 0;
        int start = 0;
        for (int k = 0; k <= inner.length(); k++) {
            char c = k < inner.length() ? inner.charAt(k) : ',';
            if (c == '<') {
                depth++;
            } else if (c == '>') {
                depth--;
            } else if (c == ',' && depth == 0) {
                params.add(parseType(tf, inner.substring(start, k)));
                start = k + 1;
            }
        }
        return tf.constructParametricType(raw, params.toArray(new JavaType[0]));
    }

    static JavaType typeArg(ObjectMapper m, JsonNode a) throws ClassNotFoundException {
        String cn = a.hasNonNull("className") ? s(a, "className") : s(a, "type");
        return parseType(m.getTypeFactory(), cn);
    }

    /**
     * Decodes args.json the way Spring MVC does by default (ObjectMapper.readValue(InputStream, JavaType)
     * on the UTF-8 request body). Options: "jsonB64" = exact request bytes (base64);
     * "via":"string" = read from a Java String instead (keeps lone surrogates).
     */
    static Object decode(ObjectMapper m, JavaType t, JsonNode a) throws Exception {
        if (a.hasNonNull("jsonB64")) {
            byte[] bytes = Base64.getDecoder().decode(s(a, "jsonB64"));
            return m.readValue(new ByteArrayInputStream(bytes), t);
        }
        String json = s(a, "json");
        if ("string".equals(sOpt(a, "via"))) {
            return m.readValue(json, t);
        }
        return m.readValue(new ByteArrayInputStream(json.getBytes(StandardCharsets.UTF_8)), t);
    }

    /** Serializes through the byte/UTF-8 generator, like the Spring HTTP message converters do. */
    static String encode(ObjectMapper m, Object v) throws Exception {
        return new String(m.writeValueAsBytes(v), StandardCharsets.UTF_8);
    }

    // ------------------------------------------------------------------------------------------
    // IpAddressUtils reflection helpers

    static Object invokePrivate(Object target, Class<?> cls, String name, Class<?>[] types, Object... args) throws Throwable {
        Method m = cls.getDeclaredMethod(name, types);
        m.setAccessible(true);
        try {
            return m.invoke(target, args);
        } catch (java.lang.reflect.InvocationTargetException e) {
            throw e.getCause() != null ? e.getCause() : e;
        }
    }

    static HttpServletRequest fakeRequest(JsonNode a) {
        final String xff = sOpt(a, "xForwardedFor");
        final String xra = sOpt(a, "xRemoteAddress");
        final String remote = sOpt(a, "remoteAddr");
        return (HttpServletRequest) Proxy.newProxyInstance(
                Functions.class.getClassLoader(), new Class<?>[]{HttpServletRequest.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getHeader": {
                            String h = (String) args[0];
                            if ("X-Forwarded-For".equalsIgnoreCase(h)) {
                                return xff;
                            }
                            if ("X-Remote-Address".equalsIgnoreCase(h)) {
                                return xra;
                            }
                            return null;
                        }
                        case "getRemoteAddr":
                            return remote;
                        case "toString":
                            return "FakeHttpServletRequest";
                        case "hashCode":
                            return System.identityHashCode(proxy);
                        case "equals":
                            return proxy == args[0];
                        default:
                            throw new UnsupportedOperationException(method.getName());
                    }
                });
    }

    // ------------------------------------------------------------------------------------------
    // Registration

    static void register(Map<String, Oracle.Fn> F) {

        // ---- HTML sanitizing --------------------------------------------------------------
        // usecase/qag/ContentSanitizer.kt: HtmlUtils.htmlUnescape(policyFactory.sanitize(content)).take(maxLength)
        F.put("sanitize", a -> text16(new ContentSanitizer().sanitize(s(a, "content"), i(a, "maxLength"))));
        // OWASP policy output only (before the unescape)
        F.put("sanitizeRaw", a -> text16(new HtmlPolicyBuilder().toFactory().sanitize(s(a, "content"))));
        F.put("htmlUnescape", a -> text16(HtmlUtils.htmlUnescape(s(a, "s"))));

        // ---- Strapi rich text -----------------------------------------------------------------
        F.put("richTextToHtml", a -> {
            ObjectMapper m = appMapper();
            JavaType t = m.getTypeFactory().constructCollectionType(List.class, StrapiRichText.class);
            List<StrapiRichText> blocks = m.readValue(s(a, "json"), t);
            return StrapiRichTextKt.toHtml(blocks);
        });
        F.put("richTextToHtmlBody", a -> {
            ObjectMapper m = appMapper();
            JavaType t = m.getTypeFactory().constructCollectionType(List.class, StrapiRichText.class);
            List<StrapiRichText> blocks = m.readValue(s(a, "json"), t);
            return StrapiRichTextKt.toHtmlBody(blocks);
        });

        // ---- java / kotlin stdlib -------------------------------------------------------------
        F.put("uuidFromString", a -> UUID.fromString(s(a, "s")).toString());
        F.put("kotlinToIntOrNull", a -> StringsKt.toIntOrNull(s(a, "s")));
        F.put("kotlinToLongOrNull", a -> StringsKt.toLongOrNull(s(a, "s")));
        F.put("kotlinToDoubleOrNull", a -> {
            Double d = StringsKt.toDoubleOrNull(s(a, "s"));
            return d == null ? null : map("bits", Double.doubleToRawLongBits(d), "text", Double.toString(d));
        });
        F.put("kotlinTrim", a -> StringsKt.trim((CharSequence) s(a, "s")).toString());
        F.put("kotlinIsBlank", a -> StringsKt.isBlank(s(a, "s")));
        F.put("containsIgnoreCase", a -> StringsKt.contains(s(a, "s"), s(a, "other"), true));
        F.put("replaceDiacritics", a -> fr.gouv.agora.infrastructure.utils.StringUtils.INSTANCE.replaceDiacritics(s(a, "s")));
        F.put("urlEncode", a -> URLEncoder.encode(s(a, "s"), StandardCharsets.UTF_8));
        F.put("javaStringLength", a -> s(a, "s").length());
        F.put("kotlinTake", a -> text16(StringsKt.take(s(a, "s"), i(a, "n"))));
        F.put("regexReplace", a -> new Regex(s(a, "pattern")).replace(s(a, "input"), s(a, "replacement")));

        // ---- dates / numbers ----------------------------------------------------------------------
        F.put("isoDateTime", a -> LocalDateTime
                .of(i(a, "year"), i(a, "month"), i(a, "day"), i(a, "hour"), i(a, "minute"), i(a, "second"), i(a, "nano"))
                .format(DateTimeFormatter.ISO_DATE_TIME));
        F.put("doubleToString", a -> Double.toString(dbl(a)));
        F.put("mathRound", a -> Math.round(dbl(a)));
        F.put("numberFormat", a -> NumberFormat.getInstance(Locale.forLanguageTag(s(a, "locale"))).format(dbl(a)));

        // ---- Jackson / Spring MVC -----------------------------------------------------------
        F.put("jsonRoundTrip", Functions::jsonRoundTrip);
        F.put("jsonDecode", Functions::jsonRoundTrip);
        F.put("xmlSerialize", a -> {
            Object v = decode(springJson(), typeArg(springJson(), a), a);
            return encode(springXml(), v);
        });
        F.put("xmlSerializeValue", a -> {
            Object v = decode(springJson(), springJson().getTypeFactory().constructType(Object.class), a);
            return encode(springXml(), v);
        });

        // ---- login token (AES, env: LOGIN_TOKEN_*) ---------------------------------------------------
        F.put("loginTokenEncode", a -> {
            BuildResult r = new LoginTokenGenerator().buildLoginToken(new LoginTokenData(s(a, "userId")));
            if (r instanceof BuildResult.Success ok) {
                return map("token", ok.getLoginToken());
            }
            String logs = Oracle.LogCapture.drain();
            throw new Failure(logs.isEmpty() ? "BuildResult.Failure" : logs);
        });
        F.put("loginTokenDecode", a -> {
            DecodeResult r = new LoginTokenGenerator().decodeLoginToken(s(a, "token"));
            if (r instanceof DecodeResult.Success ok) {
                return map("userId", ok.getLoginTokenData().getUserId());
            }
            String logs = Oracle.LogCapture.drain();
            throw new Failure(logs.isEmpty() ? "DecodeResult.Failure" : logs);
        });

        // ---- remote address hash (env: REMOTE_ADDRESS_*) ----------------------------------------------
        // private IpAddressUtils.hash(String), called by reflection
        F.put("ipHash", a -> invokePrivate(IpAddressUtils.INSTANCE, IpAddressUtils.class, "hash",
                new Class<?>[]{String.class}, s(a, "ip")));
        // IpAddressUtils.retrieveIpAddress(request) (private) with a fake HttpServletRequest:
        // args xForwardedFor / xRemoteAddress / remoteAddr (each optional or null)
        F.put("ipRetrieve", a -> invokePrivate(IpAddressUtils.INSTANCE, IpAddressUtils.class, "retrieveIpAddress",
                new Class<?>[]{HttpServletRequest.class}, fakeRequest(a)));
        F.put("ipRetrieveHash", a -> IpAddressUtils.INSTANCE.retrieveIpAddressHash(fakeRequest(a)));

        // ---- JWT (env: JWT_SECRET) -------------------------------------------------------------------
        F.put("jwtGenerate", a -> {
            Map<String, Object> claims = new LinkedHashMap<>();
            if (a.hasNonNull("claims")) {
                claims = new com.fasterxml.jackson.databind.ObjectMapper().convertValue(a.get("claims"), LinkedHashMap.class);
            }
            Pair<String, Long> p = JwtTokenUtils.INSTANCE.generateToken(s(a, "userId"), claims.isEmpty() ? Collections.emptyMap() : claims);
            return map("token", p.getFirst(), "expirationEpochMilli", p.getSecond());
        });
        F.put("jwtParse", a -> {
            String token = s(a, "token");
            boolean valid = JwtTokenUtils.INSTANCE.isCorrectSignatureAndTokenNotExpired(token);
            String userId = JwtTokenUtils.INSTANCE.extractUserId(token);
            return map("valid", valid, "userId", userId);
        });

        // ---- ACME AES-GCM (AcmeCryptoHelper with AcmeConfig.encryptionKey = key) ---------------------
        F.put("aesGcmEncrypt", a -> acmeHelper(s(a, "key")).encrypt(s(a, "plaintext")));
        F.put("aesGcmDecrypt", a -> acmeHelper(s(a, "key")).decrypt(s(a, "ciphertext")));

        // ---- slice S0 (thematiques, theme hebdo, referentiels) ---------------------------------------
        S0Functions.register(F);
        S2Functions.register(F);

        // ---- request layer (F5): byte decoding, Jackson tokens, media types -------------------------
        F5Functions.register(F);
    }

    static Object jsonRoundTrip(JsonNode a) throws Exception {
        ObjectMapper m = springJson();
        Object v = decode(m, typeArg(m, a), a);
        return encode(m, v);
    }

    static AcmeCryptoHelper acmeHelper(String key) throws Exception {
        AcmeConfig cfg = new AcmeConfig();
        Field f = AcmeConfig.class.getDeclaredField("encryptionKey");
        f.setAccessible(true);
        f.set(cfg, key);
        return new AcmeCryptoHelper(cfg);
    }
}
