import com.fasterxml.jackson.databind.JsonNode;
import fr.gouv.agora.config.AcmeConfig;
import org.shredzone.acme4j.Login;
import org.shredzone.acme4j.Session;
import org.shredzone.acme4j.challenge.Http01Challenge;
import org.shredzone.acme4j.toolbox.AcmeUtils;
import org.shredzone.acme4j.toolbox.JSON;
import org.shredzone.acme4j.toolbox.JoseUtils;
import org.shredzone.acme4j.util.CSRBuilder;
import org.shredzone.acme4j.util.KeyPairUtils;

import javax.crypto.spec.SecretKeySpec;
import java.io.StringReader;
import java.io.StringWriter;
import java.lang.reflect.Field;
import java.net.URL;
import java.security.KeyFactory;
import java.security.KeyPair;
import java.security.PrivateKey;
import java.security.PublicKey;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.spec.PKCS8EncodedKeySpec;
import java.security.spec.RSAPublicKeySpec;
import java.security.spec.X509EncodedKeySpec;
import java.util.Base64;
import java.util.List;
import java.util.Map;

/**
 * Oracle functions of slice S11 (ACME): the acme4j key pair / PEM helpers, the CSR builder, the external
 * account binding, the HTTP-01 key authorization, AcmeUtils.toAce, java.util.Base64 and
 * AcmeConfig.allDomains. The AES-GCM helper (AcmeCryptoHelper) is covered by aesGcmEncrypt / aesGcmDecrypt.
 */
@SuppressWarnings({"unchecked"})
final class S11Functions {
    private S11Functions() {
    }

    private static String b64(byte[] b) {
        return Base64.getEncoder().encodeToString(b);
    }

    private static byte[] unb64(String s) {
        return Base64.getDecoder().decode(s);
    }

    /** Rebuilds a KeyPair from a PKCS#8 RSA private key (the public key comes from the CRT parameters). */
    static KeyPair pairFromPkcs8(String pkcs8B64) throws Exception {
        KeyFactory kf = KeyFactory.getInstance("RSA");
        PrivateKey priv = kf.generatePrivate(new PKCS8EncodedKeySpec(unb64(pkcs8B64)));
        RSAPrivateCrtKey crt = (RSAPrivateCrtKey) priv;
        PublicKey pub = kf.generatePublic(new RSAPublicKeySpec(crt.getModulus(), crt.getPublicExponent()));
        return new KeyPair(pub, priv);
    }

    static String writePem(KeyPair kp) throws Exception {
        StringWriter w = new StringWriter();
        KeyPairUtils.writeKeyPair(kp, w);
        return w.toString();
    }

    static Map<String, Object> describe(KeyPair kp) {
        return Functions.map(
                "algorithm", kp.getPrivate().getAlgorithm(),
                "pkcs8B64", b64(kp.getPrivate().getEncoded()),
                "x509B64", b64(kp.getPublic().getEncoded()));
    }

    static void register(Map<String, Oracle.Fn> F) {
        // KeyPairUtils.createKeyPair(bits) + writeKeyPair: the PEM acme4j stores in the database
        F.put("acmeKeyPairCreate", a -> {
            KeyPair kp = KeyPairUtils.createKeyPair(Functions.i(a, "bits"));
            Map<String, Object> m = describe(kp);
            m.put("pem", writePem(kp));
            return m;
        });
        // KeyPairUtils.readKeyPair(StringReader(pem))
        F.put("acmeKeyPairRead", a -> describe(KeyPairUtils.readKeyPair(new StringReader(Functions.s(a, "pem")))));
        // KeyPairUtils.writeKeyPair of the key pair rebuilt from a PKCS#8 private key
        F.put("acmeKeyPairWrite", a -> writePem(pairFromPkcs8(Functions.s(a, "pkcs8B64"))));

        // AcmeUtils.toAce
        F.put("acmeToAce", a -> AcmeUtils.toAce(Functions.s(a, "s")));
        // Base64.getDecoder() / getUrlDecoder() on a String, result as hex
        F.put("javaBase64Decode", a -> {
            Base64.Decoder d = a.path("url").asBoolean(false) ? Base64.getUrlDecoder() : Base64.getDecoder();
            byte[] out = d.decode(Functions.s(a, "s"));
            StringBuilder sb = new StringBuilder();
            for (byte x : out) {
                sb.append(String.format("%02x", x));
            }
            return sb.toString();
        });

        // CSRBuilder: addDomain for each name, sign(keyPair), getEncoded (DER, base64)
        F.put("acmeCsr", a -> {
            CSRBuilder csr = new CSRBuilder();
            for (JsonNode d : a.get("domains")) {
                csr.addDomain(d.textValue());
            }
            csr.sign(pairFromPkcs8(Functions.s(a, "pkcs8B64")));
            return b64(csr.getEncoded());
        });

        // AccountBuilder.withKeyIdentifier(kid, encodedMacKey) + JoseUtils.createExternalAccountBinding
        F.put("acmeEab", a -> {
            PublicKey pub = KeyFactory.getInstance("RSA").generatePublic(new X509EncodedKeySpec(unb64(Functions.s(a, "x509B64"))));
            SecretKeySpec mac = new SecretKeySpec(AcmeUtils.base64UrlDecode(Functions.s(a, "macKey")), "HMAC");
            return JoseUtils.createExternalAccountBinding(Functions.s(a, "kid"), pub, mac, new URL(Functions.s(a, "url")));
        });

        // Http01Challenge.getAuthorization() for a login on the given key pair
        F.put("acmeKeyAuthorization", a -> {
            KeyPair kp = pairFromPkcs8(Functions.s(a, "pkcs8B64"));
            Session session = new Session("acme://letsencrypt.org/staging");
            Login login = new Login(new URL("https://acme.example/acct/1"), kp, session);
            JSON json = JSON.parse("{\"type\":\"http-01\",\"url\":\"https://acme.example/chall/1\",\"status\":\"pending\",\"token\":"
                    + Oracle.PROTO.writeValueAsString(Functions.s(a, "token")) + "}");
            return new Http01Challenge(login, json).getAuthorization();
        });

        // downloadCertificate(): the PEM chain built by the use case (Base64 MIME encoder, 64 columns)
        F.put("acmeChainPem", a -> {
            StringBuilder sb = new StringBuilder();
            for (JsonNode d : a.get("der")) {
                sb.append("-----BEGIN CERTIFICATE-----\n");
                sb.append(Base64.getMimeEncoder(64, "\n".getBytes()).encodeToString(unb64(d.textValue())));
                sb.append("\n-----END CERTIFICATE-----\n");
            }
            return sb.toString();
        });

        // AcmeConfig.allDomains for the given ACME_DOMAIN / ACME_SANS
        F.put("acmeAllDomains", a -> {
            AcmeConfig cfg = new AcmeConfig();
            set(cfg, "domain", Functions.s(a, "domain"));
            set(cfg, "sans", Functions.s(a, "sans"));
            List<String> all = cfg.getAllDomains();
            return all;
        });
    }

    private static void set(AcmeConfig cfg, String field, String value) throws Exception {
        Field f = AcmeConfig.class.getDeclaredField(field);
        f.setAccessible(true);
        f.set(cfg, value);
    }
}
