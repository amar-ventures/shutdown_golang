# Shutdown Daemon

This project is a Go-based daemon that interacts with a Supabase database to manage device statuses and handle shutdown requests. It uses Supabase's REST API for authentication and database operations.

## Features

- **Authentication**: Authenticates users using Supabase's email-password authentication.
- **Device Status Management**: Periodically updates the device's status (`on`, `off`, or `unknown`) in the `devices` table.
- **Shutdown Request Handling**: Listens for shutdown requests and executes a system shutdown if a valid request is received.
- **Row-Level Security (RLS)**: Adheres to Supabase's RLS policies for secure database access.



## Prerequisites

- Go 1.20 or later installed on your system.
- A Supabase project with the following table and policies:

### Supabase Table Schema

```sql
create table devices (
  id uuid default uuid_generate_v4() primary key,
  user_id uuid references auth.users not null,
  name text not null,
  status text check (status in ('on', 'off', 'unknown')) not null default 'unknown',
  last_seen timestamp with time zone,
  first_online_at timestamp with time zone,
  shutdown_requested jsonb,
  created_at timestamp with time zone default timezone('utc'::text, now()) not null
);

-- Enable RLS
alter table devices enable row level security;

-- Select policy
create policy "Users can view their own devices"
  on devices for select
  using (auth.uid() = user_id);

-- Update policy
create policy "Users can update their own devices"
  on devices for update
  using (auth.uid() = user_id);

-- Insert policy
create policy "Users can create their own devices"
  on devices for insert
  with check (auth.uid() = user_id);