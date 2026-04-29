drop index if exists sessions_user_last_seen_idx;

alter table sessions
  drop column if exists last_seen_at;
