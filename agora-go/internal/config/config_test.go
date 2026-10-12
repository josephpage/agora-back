package config

import "testing"

func lookupOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestPortLikeServerPortCustomizer(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{"ALLOWED_ORIGINS": "https://a", "JWT_SECRET": "x", "DATABASE_URL": "postgres://u:p@h:5432/d"}
	}
	for _, tc := range []struct {
		env  map[string]string
		port string
		ok   bool
	}{
		{map[string]string{}, "8080", true},
		{map[string]string{"PORT": "+8081"}, "8081", true},
		{map[string]string{"AGORA_PORT": "8082", "PORT": "9"}, "8082", true},
		{map[string]string{"AGORA_PORT": ""}, "", false},
		{map[string]string{"PORT": "80a"}, "", false},
	} {
		env := base()
		for k, v := range tc.env {
			env[k] = v
		}
		c, err := LoadFrom(lookupOf(env))
		if (err == nil) != tc.ok || (tc.ok && c.Port != tc.port) {
			t.Errorf("%v: %v %v", tc.env, c, err)
		}
	}
}

func TestSpringBooleanTrimsLikeJava(t *testing.T) {
	if v, err := springBoolean("\x1ftrue "); err != nil || !v {
		t.Errorf("chars <= U+0020 are trimmed: %v %v", v, err)
	}
	if _, err := springBoolean(" true"); err == nil {
		t.Error("U+00A0 is not trimmed by String.trim()")
	}
}

func TestNotificationFanoutPerMinute(t *testing.T) {
	for _, tc := range []struct {
		value string
		set   bool
		want  int
		ok    bool
	}{
		{"", false, DefaultNotificationFanoutPerMinute, true},
		{"", true, DefaultNotificationFanoutPerMinute, true},
		{"30000", true, 30000, true},
		{"0", true, 0, false},
		{"-5", true, 0, false},
		{"1e4", true, 0, false},
	} {
		env := map[string]string{"ALLOWED_ORIGINS": "https://a"}
		if tc.set {
			env["AGORA_NOTIFICATION_FANOUT_PER_MINUTE"] = tc.value
		}
		c, err := LoadFrom(lookupOf(env))
		if (err == nil) != tc.ok || (tc.ok && c.NotificationFanoutPerMinute != tc.want) {
			t.Errorf("%q (set=%v): %v %v", tc.value, tc.set, c, err)
		}
	}
}
