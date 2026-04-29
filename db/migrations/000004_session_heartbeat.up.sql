alter table sessions
  add column last_seen_at timestamptz not null default now();

create index sessions_user_last_seen_idx on sessions(user_id, last_seen_at desc);
