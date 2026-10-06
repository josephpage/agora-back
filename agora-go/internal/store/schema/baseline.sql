-- Baseline schema equivalent to what Hibernate ddl-auto=update created for the
-- Kotlin backend (dumped from the reference with pg_dump --schema-only).
-- Idempotent: a no-op on an existing production database.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE TABLE IF NOT EXISTS public.acme_account (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    account_url character varying(500),
    created_at timestamp(6) without time zone NOT NULL,
    key_pem text NOT NULL,
    server_url character varying(500) NOT NULL
);

CREATE TABLE IF NOT EXISTS public.acme_certificate (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    certificate text NOT NULL,
    created_at timestamp(6) without time zone NOT NULL,
    deployed_at timestamp(6) without time zone,
    domain character varying(255) NOT NULL,
    expires_at timestamp(6) without time zone NOT NULL,
    private_key text NOT NULL,
    status character varying(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS public.acme_challenge (
    token character varying(255) NOT NULL,
    created_at timestamp(6) without time zone NOT NULL,
    key_authorization text NOT NULL
);

CREATE TABLE IF NOT EXISTS public.acme_order (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp(6) without time zone NOT NULL,
    domain character varying(255) NOT NULL,
    domain_key_pem text NOT NULL,
    order_url character varying(500) NOT NULL,
    status character varying(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS public.agora_users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    authorization_level integer NOT NULL,
    created_date timestamp(6) without time zone,
    fcm_token text,
    last_connection_date timestamp(6) without time zone,
    password text,
    is_banned smallint
);

CREATE TABLE IF NOT EXISTS public.app_feedbacks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    app_version character varying(255),
    created_date timestamp(6) without time zone,
    description text,
    device_model character varying(255),
    os_version character varying(255),
    type character varying(255),
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.consultation_results (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    choice_id text,
    consultation_id text,
    question_id text,
    response_count integer NOT NULL
);

CREATE TABLE IF NOT EXISTS public.demographic_info_ask_date (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    ask_date timestamp(6) without time zone,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.feedbacks_consultation_update (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consultation_update_id text,
    created_date timestamp(6) without time zone,
    is_positive integer NOT NULL,
    updated_date timestamp(6) without time zone,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.feedbacks_qag (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_date timestamp(6) without time zone,
    is_helpful smallint,
    qag_id character varying(255),
    updated_date timestamp(6) without time zone,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.low_priority_qags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    qag_id uuid
);

CREATE TABLE IF NOT EXISTS public.moderatus_locked_qags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    lock_date timestamp(6) without time zone,
    qag_id uuid
);

CREATE TABLE IF NOT EXISTS public.notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    date timestamp(6) without time zone,
    description text,
    title text,
    type text,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.qag_delete_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    delete_date timestamp(6) without time zone,
    qag_id uuid,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.qag_updates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    moderated_date timestamp(6) without time zone,
    motif_id character varying(255),
    qag_id uuid,
    reason character varying(255),
    should_delete_flag integer NOT NULL,
    status integer NOT NULL,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.qags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    description text,
    motif_id character varying(255),
    post_date timestamp(6) without time zone,
    status integer NOT NULL,
    thematique_id character varying(255),
    title text,
    user_id uuid,
    username text
);

CREATE TABLE IF NOT EXISTS public.reponses_consultation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    choice_id text,
    consultation_id text,
    participation_date timestamp(6) without time zone,
    participation_id uuid,
    question_id text,
    response_text text,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.supports_qag (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    qag_id uuid,
    support_date timestamp(6) without time zone,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.user_answered_consultation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consultation_id text,
    participation_date timestamp(6) without time zone,
    user_id uuid
);

CREATE TABLE IF NOT EXISTS public.users_data (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_date timestamp(6) without time zone,
    event_type character varying(255),
    fcm_token character varying(255),
    ip_address_hash character varying(255),
    platform character varying(255),
    user_agent character varying(255),
    user_id character varying(255),
    version_code character varying(255),
    version_name character varying(255)
);

CREATE TABLE IF NOT EXISTS public.users_profile (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    city_type text,
    consultation_frequency text,
    department text,
    gender text,
    job_category text,
    primary_department text,
    public_meeting_frequency text,
    secondary_department text,
    user_id uuid,
    vote_frequency text,
    year_of_birth integer
);

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acme_account_pkey') THEN
    ALTER TABLE ONLY public.acme_account ADD CONSTRAINT acme_account_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acme_certificate_pkey') THEN
    ALTER TABLE ONLY public.acme_certificate ADD CONSTRAINT acme_certificate_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acme_challenge_pkey') THEN
    ALTER TABLE ONLY public.acme_challenge ADD CONSTRAINT acme_challenge_pkey PRIMARY KEY (token);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acme_order_pkey') THEN
    ALTER TABLE ONLY public.acme_order ADD CONSTRAINT acme_order_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'agora_users_pkey') THEN
    ALTER TABLE ONLY public.agora_users ADD CONSTRAINT agora_users_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'app_feedbacks_pkey') THEN
    ALTER TABLE ONLY public.app_feedbacks ADD CONSTRAINT app_feedbacks_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'consultation_results_pkey') THEN
    ALTER TABLE ONLY public.consultation_results ADD CONSTRAINT consultation_results_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'demographic_info_ask_date_pkey') THEN
    ALTER TABLE ONLY public.demographic_info_ask_date ADD CONSTRAINT demographic_info_ask_date_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'feedbacks_consultation_update_pkey') THEN
    ALTER TABLE ONLY public.feedbacks_consultation_update ADD CONSTRAINT feedbacks_consultation_update_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'feedbacks_qag_pkey') THEN
    ALTER TABLE ONLY public.feedbacks_qag ADD CONSTRAINT feedbacks_qag_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'low_priority_qags_pkey') THEN
    ALTER TABLE ONLY public.low_priority_qags ADD CONSTRAINT low_priority_qags_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'moderatus_locked_qags_pkey') THEN
    ALTER TABLE ONLY public.moderatus_locked_qags ADD CONSTRAINT moderatus_locked_qags_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'notifications_pkey') THEN
    ALTER TABLE ONLY public.notifications ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'qag_delete_log_pkey') THEN
    ALTER TABLE ONLY public.qag_delete_log ADD CONSTRAINT qag_delete_log_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'qag_updates_pkey') THEN
    ALTER TABLE ONLY public.qag_updates ADD CONSTRAINT qag_updates_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'qags_pkey') THEN
    ALTER TABLE ONLY public.qags ADD CONSTRAINT qags_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'reponses_consultation_pkey') THEN
    ALTER TABLE ONLY public.reponses_consultation ADD CONSTRAINT reponses_consultation_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'supports_qag_pkey') THEN
    ALTER TABLE ONLY public.supports_qag ADD CONSTRAINT supports_qag_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uk_feedbacks_consultation_update_consultationupdateid_userid') THEN
    ALTER TABLE ONLY public.feedbacks_consultation_update ADD CONSTRAINT uk_feedbacks_consultation_update_consultationupdateid_userid UNIQUE (consultation_update_id, user_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uk_feedbacks_qag_qagid_userid') THEN
    ALTER TABLE ONLY public.feedbacks_qag ADD CONSTRAINT uk_feedbacks_qag_qagid_userid UNIQUE (qag_id, user_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uks0j47wjkmy7sv7daaol839f0c') THEN
    ALTER TABLE ONLY public.users_profile ADD CONSTRAINT uks0j47wjkmy7sv7daaol839f0c UNIQUE (user_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'user_answered_consultation_pkey') THEN
    ALTER TABLE ONLY public.user_answered_consultation ADD CONSTRAINT user_answered_consultation_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'users_data_pkey') THEN
    ALTER TABLE ONLY public.users_data ADD CONSTRAINT users_data_pkey PRIMARY KEY (id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'users_profile_pkey') THEN
    ALTER TABLE ONLY public.users_profile ADD CONSTRAINT users_profile_pkey PRIMARY KEY (id);
  END IF;
END $$;
