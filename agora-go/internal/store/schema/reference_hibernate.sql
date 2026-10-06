--
-- PostgreSQL database dump
--

\restrict Cf2Ac9bP3COTbepCfSysK2anbcusfEhMq9niPKIW0u5gV1H7IRNPtR5zQVDI4FA

-- Dumped from database version 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)
-- Dumped by pg_dump version 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: unaccent; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public;


--
-- Name: EXTENSION unaccent; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION unaccent IS 'text search dictionary that removes accents';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: acme_account; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acme_account (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    account_url character varying(500),
    created_at timestamp(6) without time zone NOT NULL,
    key_pem text NOT NULL,
    server_url character varying(500) NOT NULL
);


--
-- Name: acme_certificate; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acme_certificate (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    certificate text NOT NULL,
    created_at timestamp(6) without time zone NOT NULL,
    deployed_at timestamp(6) without time zone,
    domain character varying(255) NOT NULL,
    expires_at timestamp(6) without time zone NOT NULL,
    private_key text NOT NULL,
    status character varying(50) NOT NULL
);


--
-- Name: acme_challenge; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acme_challenge (
    token character varying(255) NOT NULL,
    created_at timestamp(6) without time zone NOT NULL,
    key_authorization text NOT NULL
);


--
-- Name: acme_order; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acme_order (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp(6) without time zone NOT NULL,
    domain character varying(255) NOT NULL,
    domain_key_pem text NOT NULL,
    order_url character varying(500) NOT NULL,
    status character varying(50) NOT NULL
);


--
-- Name: agora_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agora_users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    authorization_level integer NOT NULL,
    created_date timestamp(6) without time zone,
    fcm_token text,
    last_connection_date timestamp(6) without time zone,
    password text,
    is_banned smallint
);


--
-- Name: app_feedbacks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_feedbacks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    app_version character varying(255),
    created_date timestamp(6) without time zone,
    description text,
    device_model character varying(255),
    os_version character varying(255),
    type character varying(255),
    user_id uuid
);


--
-- Name: consultation_results; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.consultation_results (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    choice_id text,
    consultation_id text,
    question_id text,
    response_count integer NOT NULL
);


--
-- Name: demographic_info_ask_date; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.demographic_info_ask_date (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    ask_date timestamp(6) without time zone,
    user_id uuid
);


--
-- Name: feedbacks_consultation_update; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.feedbacks_consultation_update (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consultation_update_id text,
    created_date timestamp(6) without time zone,
    is_positive integer NOT NULL,
    updated_date timestamp(6) without time zone,
    user_id uuid
);


--
-- Name: feedbacks_qag; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.feedbacks_qag (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_date timestamp(6) without time zone,
    is_helpful smallint,
    qag_id character varying(255),
    updated_date timestamp(6) without time zone,
    user_id uuid
);


--
-- Name: low_priority_qags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.low_priority_qags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    qag_id uuid
);


--
-- Name: moderatus_locked_qags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.moderatus_locked_qags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    lock_date timestamp(6) without time zone,
    qag_id uuid
);


--
-- Name: notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    date timestamp(6) without time zone,
    description text,
    title text,
    type text,
    user_id uuid
);


--
-- Name: qag_delete_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.qag_delete_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    delete_date timestamp(6) without time zone,
    qag_id uuid,
    user_id uuid
);


--
-- Name: qag_updates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.qag_updates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    moderated_date timestamp(6) without time zone,
    motif_id character varying(255),
    qag_id uuid,
    reason character varying(255),
    should_delete_flag integer NOT NULL,
    status integer NOT NULL,
    user_id uuid
);


--
-- Name: qags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.qags (
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


--
-- Name: reponses_consultation; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reponses_consultation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    choice_id text,
    consultation_id text,
    participation_date timestamp(6) without time zone,
    participation_id uuid,
    question_id text,
    response_text text,
    user_id uuid
);


--
-- Name: supports_qag; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.supports_qag (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    qag_id uuid,
    support_date timestamp(6) without time zone,
    user_id uuid
);


--
-- Name: user_answered_consultation; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_answered_consultation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consultation_id text,
    participation_date timestamp(6) without time zone,
    user_id uuid
);


--
-- Name: users_data; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users_data (
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


--
-- Name: users_profile; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users_profile (
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


--
-- Name: acme_account acme_account_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acme_account
    ADD CONSTRAINT acme_account_pkey PRIMARY KEY (id);


--
-- Name: acme_certificate acme_certificate_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acme_certificate
    ADD CONSTRAINT acme_certificate_pkey PRIMARY KEY (id);


--
-- Name: acme_challenge acme_challenge_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acme_challenge
    ADD CONSTRAINT acme_challenge_pkey PRIMARY KEY (token);


--
-- Name: acme_order acme_order_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acme_order
    ADD CONSTRAINT acme_order_pkey PRIMARY KEY (id);


--
-- Name: agora_users agora_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agora_users
    ADD CONSTRAINT agora_users_pkey PRIMARY KEY (id);


--
-- Name: app_feedbacks app_feedbacks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_feedbacks
    ADD CONSTRAINT app_feedbacks_pkey PRIMARY KEY (id);


--
-- Name: consultation_results consultation_results_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.consultation_results
    ADD CONSTRAINT consultation_results_pkey PRIMARY KEY (id);


--
-- Name: demographic_info_ask_date demographic_info_ask_date_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.demographic_info_ask_date
    ADD CONSTRAINT demographic_info_ask_date_pkey PRIMARY KEY (id);


--
-- Name: feedbacks_consultation_update feedbacks_consultation_update_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feedbacks_consultation_update
    ADD CONSTRAINT feedbacks_consultation_update_pkey PRIMARY KEY (id);


--
-- Name: feedbacks_qag feedbacks_qag_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feedbacks_qag
    ADD CONSTRAINT feedbacks_qag_pkey PRIMARY KEY (id);


--
-- Name: low_priority_qags low_priority_qags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.low_priority_qags
    ADD CONSTRAINT low_priority_qags_pkey PRIMARY KEY (id);


--
-- Name: moderatus_locked_qags moderatus_locked_qags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.moderatus_locked_qags
    ADD CONSTRAINT moderatus_locked_qags_pkey PRIMARY KEY (id);


--
-- Name: notifications notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);


--
-- Name: qag_delete_log qag_delete_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.qag_delete_log
    ADD CONSTRAINT qag_delete_log_pkey PRIMARY KEY (id);


--
-- Name: qag_updates qag_updates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.qag_updates
    ADD CONSTRAINT qag_updates_pkey PRIMARY KEY (id);


--
-- Name: qags qags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.qags
    ADD CONSTRAINT qags_pkey PRIMARY KEY (id);


--
-- Name: reponses_consultation reponses_consultation_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reponses_consultation
    ADD CONSTRAINT reponses_consultation_pkey PRIMARY KEY (id);


--
-- Name: supports_qag supports_qag_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.supports_qag
    ADD CONSTRAINT supports_qag_pkey PRIMARY KEY (id);


--
-- Name: feedbacks_consultation_update uk_feedbacks_consultation_update_consultationupdateid_userid; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feedbacks_consultation_update
    ADD CONSTRAINT uk_feedbacks_consultation_update_consultationupdateid_userid UNIQUE (consultation_update_id, user_id);


--
-- Name: feedbacks_qag uk_feedbacks_qag_qagid_userid; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feedbacks_qag
    ADD CONSTRAINT uk_feedbacks_qag_qagid_userid UNIQUE (qag_id, user_id);


--
-- Name: users_profile uks0j47wjkmy7sv7daaol839f0c; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users_profile
    ADD CONSTRAINT uks0j47wjkmy7sv7daaol839f0c UNIQUE (user_id);


--
-- Name: user_answered_consultation user_answered_consultation_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_answered_consultation
    ADD CONSTRAINT user_answered_consultation_pkey PRIMARY KEY (id);


--
-- Name: users_data users_data_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users_data
    ADD CONSTRAINT users_data_pkey PRIMARY KEY (id);


--
-- Name: users_profile users_profile_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users_profile
    ADD CONSTRAINT users_profile_pkey PRIMARY KEY (id);


--
-- PostgreSQL database dump complete
--

\unrestrict Cf2Ac9bP3COTbepCfSysK2anbcusfEhMq9niPKIW0u5gV1H7IRNPtR5zQVDI4FA

