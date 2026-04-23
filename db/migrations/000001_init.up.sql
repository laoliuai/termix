create extension if not exists "pgcrypto";

create table users (
  id uuid primary key default gen_random_uuid(),
  email text not null unique,
  display_name text not null,
  password_hash text not null,
  role text not null check (role in ('admin', 'user')),
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  last_login_at timestamptz null
);

create table devices (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id),
  device_type text not null check (device_type in ('host', 'android')),
  platform text not null check (platform in ('macos', 'ubuntu', 'android')),
  label text not null,
  hostname text null,
  machine_fingerprint text null,
  app_version text null,
  last_seen_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  disabled_at timestamptz null
);

create index devices_user_type_idx on devices(user_id, device_type);

create table refresh_tokens (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id),
  device_id uuid not null references devices(id),
  token_hash text not null,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  revoked_at timestamptz null
);

create index refresh_tokens_user_device_idx on refresh_tokens(user_id, device_id);

create table sessions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id),
  host_device_id uuid not null references devices(id),
  name text null,
  tool text not null check (tool in ('claude', 'codex', 'opencode')),
  launch_command text not null,
  cwd text not null,
  cwd_label text not null,
  tmux_session_name text not null unique,
  status text not null check (status in ('starting', 'running', 'idle', 'disconnected', 'exited', 'failed')),
  preview_text text null,
  last_error text null,
  last_exit_code integer null,
  started_at timestamptz not null default now(),
  last_activity_at timestamptz not null default now(),
  ended_at timestamptz null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index sessions_user_status_activity_idx on sessions(user_id, status, last_activity_at desc);
