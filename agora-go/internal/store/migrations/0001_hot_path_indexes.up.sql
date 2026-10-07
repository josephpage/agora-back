-- Hot-path indexes (Go-only migration, compatible with the Kotlin backend).
-- Applied with `agora --migrate=up`: each statement runs alone (CONCURRENTLY:
-- no write lock), IF NOT EXISTS makes re-runs harmless. Measured with
-- parity/perf/bench_queries.sh on the scale-5000 seed (see PERF.md).
-- No UNIQUE constraint: anonymizeSupportsOnSelectedQags rewrites user_id to
-- the zero UUID (duplicates), and user_answered_consultation has duplicates.

-- supports_qag per user: supporting list/count (Q3, Q5), supported ids on every list page (Q6), support checks (Q14/Q15)
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_supports_qag_user_qag ON supports_qag (user_id, qag_id) INCLUDE (support_date);
-- supports_qag per QaG: count(DISTINCT user_id) of every list and of the details (Q1, Q2, Q3, Q9c, Q10, Q13)
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_supports_qag_qag_user ON supports_qag (qag_id, user_id);
-- user_answered_consultation per user: answered ids (/consultations), answered count, has answered
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_uac_user_consultation ON user_answered_consultation (user_id, consultation_id);
-- user_answered_consultation per consultation: participant count, has answered
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_uac_consultation_user ON user_answered_consultation (consultation_id, user_id);
-- notifications of a user (paginated list, deletion)
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_notifications_user ON notifications (user_id);
-- consultation results fallback (every results cache miss, i.e. after each submit)
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_reponses_consultation_cqc ON reponses_consultation (consultation_id, question_id, choice_id);
-- qags by status: lists, counts, responses, trending, moderation queue
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_qags_status_post_date ON qags (status, post_date DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_qags_status_thematique_post_date ON qags (status, thematique_id, post_date DESC);
-- qags of a user: ask status / insert (Q12), author branch of the supporting list
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_qags_user_post_date ON qags (user_id, post_date DESC);
-- qag_updates of a QaG (trending)
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_qag_updates_qag ON qag_updates (qag_id) INCLUDE (status, moderated_date);
-- signup history of an IP hash + user agent (suspicious user detection); partial: logins never pay for it
CREATE INDEX CONCURRENTLY IF NOT EXISTS agora_idx_users_data_signup ON users_data (ip_address_hash, user_agent) WHERE event_type = 'signup';
