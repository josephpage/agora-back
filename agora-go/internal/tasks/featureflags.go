package tasks

import (
	"context"
	"fmt"
	"strings"

	"agora/internal/app"
	"agora/internal/modules/login"
)

// featureNames are the constants of domain/AgoraFeature in declaration order
// (AgoraFeature.values()), with their enum names as printed by the Kotlin logger.
var featureNames = []struct {
	name    string
	feature login.Feature
}{
	{"SignUp", login.FeatureSignUp},
	{"Login", login.FeatureLogin},
	{"QagSelect", login.FeatureQagSelect},
	{"FeedbackResponseQag", login.FeatureFeedbackResponseQag},
	{"FeedbackConsultationUpdate", login.FeatureFeedbackConsultationUpdate},
	{"SuspiciousUserDetection", login.FeatureSuspiciousUserDetection},
	{"DeleteBannedUsersSupports", login.FeatureDeleteBannedUsersSupports},
	{"QagAnonymization", login.FeatureQagAnonymization},
}

// LogFeatureFlags is FeatureFlagsLogger.afterPropertiesSet. The Kotlin bean runs
// at the start-up of EVERY process (the web server and each custom command), so
// cmd/agora calls it before dispatching. A flag that cannot be read (Redis) is a
// Kotlin start-up failure: the error is returned and the process exits.
// Reading a flag also initializes its shared Redis key from the environment, as
// the Kotlin repository does.
func LogFeatureFlags(ctx context.Context, a *app.App) error {
	a.Log.Info("Agora FeatureFlags:")
	lines := make([]string, 0, len(featureNames))
	for _, f := range featureNames {
		enabled, err := login.Get(a).IsFeatureEnabled(ctx, f.feature)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("[%s] %t", f.name, enabled))
	}
	a.Log.Info(strings.Join(lines, "\n"))
	return nil
}
