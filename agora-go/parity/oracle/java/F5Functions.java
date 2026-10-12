import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.core.JsonToken;
import com.fasterxml.jackson.databind.JsonNode;
import org.springframework.http.MediaType;

import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Oracle functions of the request layer (F5): the JVM pieces that turn request bytes into Java
 * strings. Byte arrays travel as base64 ("b64").
 */
final class F5Functions {
    private F5Functions() {
    }

    // org.apache.catalina.connector.Request.unescape
    private static String tomcatUnescape(String s) {
        if (s == null || s.indexOf('\\') == -1) {
            return s;
        }
        StringBuilder buf = new StringBuilder();
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c != '\\') {
                buf.append(c);
            } else {
                if (++i >= s.length()) {
                    throw new IllegalArgumentException();
                }
                buf.append(s.charAt(i));
            }
        }
        return buf.toString();
    }

    private static byte[] bytes(JsonNode a) {
        return Base64.getDecoder().decode(Functions.s(a, "b64"));
    }

    private static Object defaultValue(Class<?> t) {
        if (t == boolean.class) {
            return false;
        }
        if (t == int.class) {
            return 0;
        }
        if (t == long.class) {
            return 0L;
        }
        return null;
    }

    private static int[] units(String s) {
        int[] u = new int[s.length()];
        for (int k = 0; k < u.length; k++) {
            u[k] = s.charAt(k);
        }
        return u;
    }

    static void register(Map<String, Oracle.Fn> F) {
        // Tomcat ByteChunk.toString / Charset.decode: UTF-8 with REPLACE (query parameters).
        F.put("javaUtf8Decode", a -> units(Charset.forName("UTF-8").decode(ByteBuffer.wrap(bytes(a))).toString()));

        // Spring's conversion of a @RequestParam / @PathVariable text to Integer or Boolean
        // (StringToNumberConverterFactory → NumberUtils.parseNumber, StringToBooleanConverter).
        F.put("springConvert", a -> {
            Class<?> type = "Boolean".equals(Functions.s(a, "type")) ? Boolean.class : Integer.class;
            try {
                Object v = org.springframework.core.convert.support.DefaultConversionService.getSharedInstance()
                        .convert(Functions.s(a, "s"), type);
                return Functions.map("value", v == null ? null : v.toString());
            } catch (RuntimeException e) {
                return Functions.map("error", e.getClass().getSimpleName());
            }
        });

        // LocalDateTime.atZone(zone).toInstant() (DateUtils.toDate): epoch milliseconds.
        F.put("javaAtZone", a -> java.time.LocalDateTime.parse(Functions.s(a, "wall"))
                .atZone(java.time.ZoneId.of(Functions.s(a, "zone"))).toInstant().toEpochMilli());

        // pgjdbc TimestampUtils.toTimestamp: the wall clock set on a lenient GregorianCalendar of
        // the JVM zone (timestamp without time zone columns): epoch milliseconds.
        F.put("javaCalendarTimestamp", a -> {
            java.time.LocalDateTime w = java.time.LocalDateTime.parse(Functions.s(a, "wall"));
            java.util.GregorianCalendar cal = new java.util.GregorianCalendar(java.util.TimeZone.getTimeZone(Functions.s(a, "zone")));
            cal.clear();
            cal.set(w.getYear(), w.getMonthValue() - 1, w.getDayOfMonth(), w.getHour(), w.getMinute(), w.getSecond());
            cal.set(java.util.Calendar.MILLISECOND, w.getNano() / 1_000_000);
            return cal.getTimeInMillis();
        });

        // ShallowEtagHeaderFilter → ServletWebRequest.checkNotModified(etag) for a GET whose
        // If-None-Match lines are "values" (servlet request/response stubbed with proxies).
        F.put("springCheckNotModified", a -> {
            List<String> vals = new ArrayList<>();
            for (JsonNode n : a.get("values")) {
                vals.add(n.asText());
            }
            String method = Functions.s(a, "method");
            jakarta.servlet.http.HttpServletRequest req = (jakarta.servlet.http.HttpServletRequest) java.lang.reflect.Proxy.newProxyInstance(
                    F5Functions.class.getClassLoader(), new Class<?>[]{jakarta.servlet.http.HttpServletRequest.class}, (p, m, args) -> {
                        switch (m.getName()) {
                            case "getHeaders":
                                return "If-None-Match".equalsIgnoreCase((String) args[0]) ? java.util.Collections.enumeration(vals) : java.util.Collections.emptyEnumeration();
                            case "getHeader":
                                return "If-None-Match".equalsIgnoreCase((String) args[0]) && !vals.isEmpty() ? vals.get(0) : null;
                            case "getMethod":
                                return method;
                            case "getDateHeader":
                                return -1L;
                            default:
                                return defaultValue(m.getReturnType());
                        }
                    });
            int[] status = {200};
            jakarta.servlet.http.HttpServletResponse resp = (jakarta.servlet.http.HttpServletResponse) java.lang.reflect.Proxy.newProxyInstance(
                    F5Functions.class.getClassLoader(), new Class<?>[]{jakarta.servlet.http.HttpServletResponse.class}, (p, m, args) -> {
                        switch (m.getName()) {
                            case "getStatus":
                                return status[0];
                            case "setStatus":
                                status[0] = (Integer) args[0];
                                return null;
                            default:
                                return defaultValue(m.getReturnType());
                        }
                    });
            return new org.springframework.web.context.request.ServletWebRequest(req, resp).checkNotModified(Functions.s(a, "etag"));
        });

        // DefaultCorsProcessor for a preflight: HttpHeaders.getAccessControlRequestHeaders of the
        // request lines (bytes read as ISO-8859-1 like Tomcat) then CorsConfiguration.checkHeaders
        // with the registry default "*": the Access-Control-Allow-Headers value (ISO-8859-1 bytes,
        // base64), "" when absent, or {"rejected": true}.
        F.put("springCorsAllowHeaders", a -> {
            org.springframework.http.HttpHeaders h = new org.springframework.http.HttpHeaders();
            for (JsonNode n : a.get("values")) {
                h.add("Access-Control-Request-Headers", new String(Base64.getDecoder().decode(n.asText()), java.nio.charset.StandardCharsets.ISO_8859_1));
            }
            org.springframework.web.cors.CorsConfiguration c = new org.springframework.web.cors.CorsConfiguration();
            c.addAllowedHeader("*");
            List<String> allowed = c.checkHeaders(h.getAccessControlRequestHeaders());
            if (allowed == null) {
                return Functions.map("rejected", true);
            }
            org.springframework.http.HttpHeaders out = new org.springframework.http.HttpHeaders();
            if (!allowed.isEmpty()) {
                out.setAccessControlAllowHeaders(allowed);
            }
            String v = out.getFirst("Access-Control-Allow-Headers");
            return Functions.map("value", v == null ? "" : Base64.getEncoder().encodeToString(v.getBytes(java.nio.charset.StandardCharsets.ISO_8859_1)));
        });

        // Tomcat 10.1 cookies: Rfc6265CookieProcessor.parseCookieHeader on the Cookie header lines
        // (raw bytes, base64) with the connector's limit of 200, then Request.convertCookies
        // (servlet Cookie name validation, values decoded with the processor's UTF-8, unescape).
        // {"tooMany": true} when ServerCookies.addCookie throws (CoyoteAdapter: 400).
        F.put("tomcatCookies", a -> {
            org.apache.tomcat.util.http.MimeHeaders mh = new org.apache.tomcat.util.http.MimeHeaders();
            for (JsonNode n : a.get("values")) {
                byte[] b = Base64.getDecoder().decode(n.asText());
                mh.addValue("Cookie").setBytes(b, 0, b.length);
            }
            org.apache.tomcat.util.http.ServerCookies sc = new org.apache.tomcat.util.http.ServerCookies(4);
            sc.setLimit(200);
            org.apache.tomcat.util.http.Rfc6265CookieProcessor p = new org.apache.tomcat.util.http.Rfc6265CookieProcessor();
            try {
                p.parseCookieHeader(mh, sc);
            } catch (IllegalArgumentException e) {
                return Functions.map("tooMany", true);
            }
            List<Object> out = new ArrayList<>();
            for (int i = 0; i < sc.getCookieCount(); i++) {
                org.apache.tomcat.util.http.ServerCookie s = sc.getCookie(i);
                try {
                    jakarta.servlet.http.Cookie c = new jakarta.servlet.http.Cookie(s.getName().toString(), null);
                    s.getValue().getByteChunk().setCharset(p.getCharset());
                    c.setValue(tomcatUnescape(s.getValue().toString()));
                    out.add(List.of(c.getName(), c.getValue()));
                } catch (IllegalArgumentException e) {
                    // Request.convertCookies: ignore bad cookie
                }
            }
            return Functions.map("cookies", out);
        });

        // Kotlin CharSequence.lines() and String.toDoubleOrNull() (kotlin-stdlib of the reference).
        F.put("kotlinLines", a -> kotlin.text.StringsKt.lines(Functions.s(a, "s")));
        // (kotlinToDoubleOrNull of Functions gives the bits as a JSON number; here as text, exact for any long)
        F.put("kotlinToDoubleOrNullBits", a -> {
            Double d = kotlin.text.StringsKt.toDoubleOrNull(Functions.s(a, "s"));
            return d == null ? Functions.map("null", true) : Functions.map("bits", Long.toString(Double.doubleToRawLongBits(d)));
        });

        // Tomcat request.getRemoteAddr(): InetAddress.getHostAddress() of the peer socket address
        // (raw 4 or 16 bytes, optional numeric IPv6 scope).
        F.put("javaInetHostAddress", a -> {
            byte[] b = bytes(a);
            int scope = a.has("scope") ? Functions.i(a, "scope") : -1;
            try {
                java.net.InetAddress ia = scope >= 0 && b.length == 16
                        ? java.net.Inet6Address.getByAddress(null, b, scope)
                        : java.net.InetAddress.getByAddress(b);
                return ia.getHostAddress();
            } catch (java.net.UnknownHostException e) {
                return Functions.map("error", e.getClass().getSimpleName());
            }
        });

        // Charset.forName(name).decode(bytes) (InputStreamReader semantics: REPLACE).
        F.put("javaCharsetDecode", a -> {
            Charset cs;
            try {
                cs = Charset.forName(Functions.s(a, "charset"));
            } catch (IllegalArgumentException e) {
                return Functions.map("error", e.getClass().getSimpleName());
            }
            return Functions.map("name", cs.name(), "utf16", units(cs.decode(ByteBuffer.wrap(bytes(a))).toString()));
        });

        // Every charset of this JVM with its aliases (to generate the Go table).
        F.put("javaCharsets", a -> {
            List<Object> out = new ArrayList<>();
            for (Map.Entry<String, Charset> e : Charset.availableCharsets().entrySet()) {
                Charset cs = e.getValue();
                out.add(Functions.map("name", cs.name(), "aliases", new ArrayList<>(cs.aliases()), "canDecode", true));
            }
            return out;
        });

        // The tokens Spring MVC's Jackson parser produces for the FIRST root value of a request body
        // (ObjectMapper.readValue(InputStream) with encoding auto-detection). Strings and names as
        // UTF-16 units, numbers as text; {"error":..} when the parser fails before the value ends.
        F.put("jacksonTokens", a -> {
            List<Object> toks = new ArrayList<>();
            try (JsonParser p = Functions.springJson().getFactory().createParser(bytes(a))) {
                int depth = 0;
                JsonToken t;
                while ((t = p.nextToken()) != null) {
                    switch (t) {
                        case START_OBJECT: toks.add("{"); depth++; break;
                        case END_OBJECT: toks.add("}"); depth--; break;
                        case START_ARRAY: toks.add("["); depth++; break;
                        case END_ARRAY: toks.add("]"); depth--; break;
                        case FIELD_NAME: toks.add(Functions.map("name", units(p.getCurrentName()))); break;
                        case VALUE_STRING: toks.add(Functions.map("string", units(p.getText()))); break;
                        case VALUE_NUMBER_INT:
                        case VALUE_NUMBER_FLOAT: toks.add(Functions.map("number", p.getText())); break;
                        case VALUE_TRUE: toks.add("true"); break;
                        case VALUE_FALSE: toks.add("false"); break;
                        case VALUE_NULL: toks.add("null"); break;
                        default: toks.add(t.toString());
                    }
                    if (depth == 0) {
                        break;
                    }
                }
                if (toks.isEmpty()) {
                    return Functions.map("error", "no content");
                }
            } catch (Exception e) {
                return Functions.map("error", e.getClass().getSimpleName(), "tokens", toks);
            }
            return Functions.map("tokens", toks);
        });

        // MediaType.parseMediaType + the facts the request layer needs.
        F.put("springMediaType", a -> {
            MediaType mt;
            try {
                mt = MediaType.parseMediaType(Functions.s(a, "s"));
            } catch (Exception e) {
                return Functions.map("error", e.getClass().getSimpleName());
            }
            Map<String, Object> params = new LinkedHashMap<>(mt.getParameters());
            Charset cs = mt.getCharset();
            return Functions.map(
                    "type", mt.getType(),
                    "subtype", mt.getSubtype(),
                    "params", params,
                    "charset", cs == null ? null : cs.name(),
                    "wildcardType", mt.isWildcardType(),
                    "wildcardSubtype", mt.isWildcardSubtype(),
                    "json", MediaType.APPLICATION_JSON.includes(mt) || new MediaType("application", "*+json").includes(mt),
                    "xml", MediaType.APPLICATION_XML.includes(mt) || MediaType.TEXT_XML.includes(mt)
                            || new MediaType("application", "*+xml").includes(mt));
        });
    }
}
