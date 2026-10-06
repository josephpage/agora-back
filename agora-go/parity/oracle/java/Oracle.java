import ch.qos.logback.classic.Level;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.classic.spi.IThrowableProxy;
import ch.qos.logback.core.AppenderBase;
import com.fasterxml.jackson.core.JsonGenerator;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.slf4j.ILoggerFactory;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedOutputStream;
import java.io.BufferedReader;
import java.io.FileDescriptor;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationTargetException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

/**
 * JVM oracle for the Kotlin -> Go parity effort.
 *
 * <p>Long-running process that answers function calls over stdin/stdout, one JSON object per line,
 * using the REAL Kotlin/JVM implementations of the reference jar (see {@link Functions}).
 *
 * <p>Request (one line):
 * <pre>{"id":N,"fn":"name","args":{...}}</pre>
 * optional {@code "env":{"VAR":"value","OTHER":null}} temporarily overrides (or, with null, unsets)
 * process environment variables for that call only (System.getenv() is patched via reflection).
 *
 * <p>Response (one line, always pure ASCII: every non-ASCII UTF-16 code unit is written as a
 * {@code \\uXXXX} escape so lone surrogates survive the transport):
 * <pre>{"id":N,"ok":true,"result":ANY}
 * {"id":N,"ok":false,"error":"SimpleClassName: message","exception":"fully.qualified.Class","originalMessage":"..."}</pre>
 *
 * <p>stdout carries nothing but protocol lines; System.out is redirected to stderr and logging is
 * muted. The process exits when stdin reaches EOF.
 */
@SuppressWarnings({"deprecation", "unchecked"})
public final class Oracle {

    @FunctionalInterface
    interface Fn {
        Object call(JsonNode args) throws Throwable;
    }

    /** Protocol mapper. Output is ASCII-only (ESCAPE_NON_ASCII), so lone surrogates are preserved. */
    static final ObjectMapper PROTO = new ObjectMapper();

    static {
        PROTO.getFactory().configure(JsonGenerator.Feature.ESCAPE_NON_ASCII, true);
    }

    static final Map<String, Fn> FNS = new TreeMap<>();

    public static void main(String[] argv) throws Exception {
        // Keep the real stdout for the protocol and make sure nothing else can reach it.
        OutputStream out = new BufferedOutputStream(new FileOutputStream(FileDescriptor.out), 1 << 16);
        System.setOut(System.err);

        quietLogging();
        Functions.register(FNS);
        FNS.put("ping", a -> "pong");
        FNS.put("functions", a -> new ArrayList<>(FNS.keySet()));

        BufferedReader in = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8), 1 << 16);
        System.err.println("oracle: ready (" + FNS.size() + " functions)");
        String line;
        while ((line = in.readLine()) != null) {
            if (line.isBlank()) {
                continue;
            }
            byte[] resp = handle(line);
            out.write(resp);
            out.write('\n');
            out.flush();
        }
    }

    // ------------------------------------------------------------------------------------------

    static byte[] handle(String line) {
        JsonNode id = null;
        try {
            JsonNode req = PROTO.readTree(line);
            if (req == null || !req.isObject()) {
                throw new IllegalArgumentException("request must be a JSON object");
            }
            id = req.get("id");
            JsonNode fnNode = req.get("fn");
            if (fnNode == null || !fnNode.isTextual()) {
                throw new IllegalArgumentException("missing 'fn'");
            }
            Fn fn = FNS.get(fnNode.textValue());
            if (fn == null) {
                throw new IllegalArgumentException("unknown function: " + fnNode.textValue());
            }
            JsonNode args = req.get("args");
            if (args == null || args.isNull()) {
                args = PROTO.createObjectNode();
            }
            if (!args.isObject()) {
                throw new IllegalArgumentException("'args' must be a JSON object");
            }
            final JsonNode fargs = args;
            LogCapture.reset();
            Object result = withEnv(req.get("env"), () -> fn.call(fargs));
            if (result instanceof String s && fargs.path("utf16").asBoolean(false)) {
                result = Functions.text16(s);
            }
            Map<String, Object> resp = new LinkedHashMap<>();
            resp.put("id", id);
            resp.put("ok", true);
            resp.put("result", result);
            return encode(resp);
        } catch (Throwable t) {
            return errorResponse(id, t);
        }
    }

    static byte[] errorResponse(JsonNode id, Throwable t) {
        try {
            Throwable root = unwrap(t);
            Map<String, Object> resp = new LinkedHashMap<>();
            resp.put("id", id);
            resp.put("ok", false);
            resp.put("error", describe(root));
            resp.put("exception", root.getClass().getName());
            if (root instanceof JsonProcessingException jpe) {
                resp.put("originalMessage", jpe.getOriginalMessage());
            }
            return encode(resp);
        } catch (Throwable t2) {
            // Last resort, hand-written so that it cannot fail.
            String msg = String.valueOf(t2.getClass().getSimpleName());
            return ("{\"id\":null,\"ok\":false,\"error\":\"" + msg + ": cannot encode response\"}")
                    .getBytes(StandardCharsets.US_ASCII);
        }
    }

    static byte[] encode(Object o) throws JsonProcessingException {
        // ESCAPE_NON_ASCII guarantees pure ASCII, so ISO_8859_1/US_ASCII are equivalent here.
        return PROTO.writeValueAsString(o).getBytes(StandardCharsets.ISO_8859_1);
    }

    static Throwable unwrap(Throwable t) {
        while (t instanceof InvocationTargetException && t.getCause() != null) {
            t = t.getCause();
        }
        return t;
    }

    /** "SimpleClassName: message" */
    static String describe(Throwable t) {
        String name = t.getClass().getSimpleName();
        if (name.isEmpty()) {
            name = t.getClass().getName();
        }
        String msg = t.getMessage();
        if (msg == null && t.getCause() != null && t.getCause() != t) {
            msg = describe(t.getCause());
        }
        return name + ": " + (msg == null ? "" : msg);
    }

    // ------------------------------------------------------------------------------------------
    // Environment overrides (System.getenv is read by the Kotlin code under test)

    @FunctionalInterface
    interface Body {
        Object call() throws Throwable;
    }

    @SuppressWarnings("unchecked")
    static Map<String, String> mutableEnv() throws Exception {
        Map<String, String> env = System.getenv();
        Field f = env.getClass().getDeclaredField("m"); // Collections$UnmodifiableMap.m
        f.setAccessible(true);
        return (Map<String, String>) f.get(env);
    }

    static Object withEnv(JsonNode overrides, Body body) throws Throwable {
        if (overrides == null || overrides.isNull() || overrides.isEmpty()) {
            return body.call();
        }
        if (!overrides.isObject()) {
            throw new IllegalArgumentException("'env' must be a JSON object");
        }
        Map<String, String> env;
        try {
            env = mutableEnv();
        } catch (Exception e) {
            throw new IllegalStateException("env overrides unavailable (need --add-opens java.base/java.util=ALL-UNNAMED): " + e);
        }
        Map<String, String> saved = new LinkedHashMap<>();
        Iterator<Map.Entry<String, JsonNode>> it = overrides.fields();
        while (it.hasNext()) {
            Map.Entry<String, JsonNode> e = it.next();
            saved.put(e.getKey(), env.get(e.getKey()));
        }
        try {
            it = overrides.fields();
            while (it.hasNext()) {
                Map.Entry<String, JsonNode> e = it.next();
                if (e.getValue().isNull()) {
                    env.remove(e.getKey());
                } else {
                    env.put(e.getKey(), e.getValue().asText());
                }
            }
            return body.call();
        } finally {
            for (Map.Entry<String, String> e : saved.entrySet()) {
                if (e.getValue() == null) {
                    env.remove(e.getKey());
                } else {
                    env.put(e.getKey(), e.getValue());
                }
            }
        }
    }

    // ------------------------------------------------------------------------------------------
    // Logging: drop everything, but remember ERROR messages of the current request.

    static void quietLogging() {
        try {
            ILoggerFactory f = LoggerFactory.getILoggerFactory();
            if (f instanceof LoggerContext ctx) {
                ctx.reset(); // discard whatever the classpath logback.xml configured (console, sentry)
                ch.qos.logback.classic.Logger root = ctx.getLogger(Logger.ROOT_LOGGER_NAME);
                root.setLevel(Level.ERROR);
                LogCapture cap = new LogCapture();
                cap.setContext(ctx);
                cap.start();
                root.addAppender(cap);
            }
        } catch (Throwable t) {
            System.err.println("oracle: could not configure logging: " + t);
        }
    }

    static final class LogCapture extends AppenderBase<ILoggingEvent> {
        private static final List<String> MESSAGES = new ArrayList<>();

        static synchronized void reset() {
            MESSAGES.clear();
        }

        /** ERROR messages logged since the last reset(), joined by " | ". */
        static synchronized String drain() {
            String s = String.join(" | ", MESSAGES);
            MESSAGES.clear();
            return s;
        }

        @Override
        protected void append(ILoggingEvent ev) {
            StringBuilder sb = new StringBuilder(ev.getFormattedMessage());
            IThrowableProxy tp = ev.getThrowableProxy();
            if (tp != null) {
                sb.append(" [").append(tp.getClassName()).append(": ").append(tp.getMessage()).append(']');
            }
            synchronized (LogCapture.class) {
                MESSAGES.add(sb.toString());
            }
        }
    }
}
