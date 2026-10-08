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

    private static byte[] bytes(JsonNode a) {
        return Base64.getDecoder().decode(Functions.s(a, "b64"));
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
