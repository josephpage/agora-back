-- Reverts 0001_hot_path_indexes.up.sql (agora --migrate=down).
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_users_data_signup;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_uac_user_consultation;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_uac_consultation_user;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_supports_qag_user_qag;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_supports_qag_qag_user;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_reponses_consultation_cqc;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_qags_user_post_date;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_qags_status_thematique_post_date;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_qags_status_post_date;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_qag_updates_qag;
DROP INDEX CONCURRENTLY IF EXISTS agora_idx_notifications_user;
